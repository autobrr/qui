# Remote Filesystem Backend Design (SSH/SFTP-native)

Status: draft for maintainer review. Supersedes the remote-helper design
(PR #1913, closed but kept as reference — it documents the deployable-agent
tier and its NDJSON wire protocol, which remain the fallback if this design
hits a performance wall).

## Context and Decision

qui services perform filesystem work through `fsops.Backend` (#1914–#1916).
Instances running on a different host than qui need a remote implementation.

The prior design deployed a `qui-helper` binary to the remote host and spoke
NDJSON to it over SSH. **Decision: no deployed agent.** The remote backend
uses native SSH primitives on a single connection:

- the SFTP subsystem (one channel) for everything the protocol can express,
- exec sessions (additional channels on the same connection) for what it
  cannot, when the key permits exec,
- a capability probe at connect time that decides which tier the instance
  gets. Nothing is persisted; capabilities are re-probed on every connect.

Rationale: no binaries to build per arch or keep in lockstep with qui;
shared seedboxes essentially always offer SFTP but do not always allow exec;
the key's own restrictions — not qui configuration — pick the trade-off.

## Capability Tiers

**SFTP-only** (key restricted to `internal-sftp`): stat, lstat, readdir,
walks, mkdir, remove, free space (`statvfs@openssh.com`), and hardlink-tree
creation (`hardlink@openssh.com`) all work. SFTP v3 attrs carry no inode or
nlink, so file identity is unavailable: `Lstat`/`WalkDir` set `FileIDErr`
when identity is requested. Consumers already degrade on zero FileID —
orphan-scan alias dedup switches off, hardlinked-copy detection reports
no-evidence, dirscan's FileID index skips the files. Reflinks are
unsupported (no SFTP equivalent of FICLONE; `copy-data` is a byte copy, not
CoW). The degraded mode must be surfaced in the UI, not silent.

**SFTP+exec**: identity arrives via `find`/`stat` sweeps, reflink support is
probed and used, `SameFilesystem` is exact. Full functionality.

## Operation Mapping

| fsops.Backend | SFTP-only | with exec |
|---|---|---|
| `Stat` / `Lstat` | `SSH_FXP_STAT` / `LSTAT` (identity → `FileIDErr`) | + `stat` for identity |
| `ReadDir` | `SSH_FXP_READDIR` (attrs included, no per-entry stat) | same |
| `WalkDir` | recursive readdir | `find -printf` streams a tree's paths+identity in one round trip |
| `Statfs` | `statvfs@openssh.com` | same (fallback `df -P`) |
| `SameFilesystem` | fsid compare from statvfs, if the server reports real fsids (probe; some return zero) | `stat -c %d` compare |
| `MkdirAll` | `SSH_FXP_MKDIR` walk-up | same |
| `Remove` | `REMOVE`/`RMDIR`; recursive = readdir-driven bottom-up | recursive = `rm -rf --` |
| `HardlinkTree` | `hardlink@openssh.com` per file | same |
| `ReflinkTree` | unsupported | `cp --reflink=always` |
| `RemoveTree` | `REMOVE` over the recorded handle | same |
| `SupportsReflink` | false | probed once per fs root |

`StatBatch`/`LstatBatch` were dropped from `fsops.Backend` pending a
consumer; this backend is that consumer. pkg/sftp pipelines concurrent
requests over the one session, and the exec path fills a batch with a single
`xargs -0 stat` round trip — the batch seam is what makes remote hardlink
indexing (hundreds of thousands of lstats) survivable. Re-add them in the
PR that implements this backend.

## File Identity Over the Wire — OPEN QUESTION

Remote identity is (device, inode) parsed from GNU `find`/`stat` output. But
`hardlink.FileID` is platform-compiled: unix builds carry `Dev`/`Ino`,
Windows builds carry a volume serial plus a 16-byte identifier. A qui host
on Windows cannot represent a Linux seedbox's identity in today's struct.

Options: (a) an opaque byte/string identity form in fsops (raised by
com6056 on #1914); (b) pack remote dev/ino into the Windows struct's
identifier bytes. To be settled in review of this doc. Note that a Windows
*remote* reports a 16-byte NTFS file ID, which fits neither unix dev/ino
nor option (b)'s packing — one more argument for the opaque form.

Related guard regardless of representation: FileID comparisons are only
valid within one backend/host — dev+ino pairs collide across machines, so
cross-instance comparisons must check same-backend first.

## Exec Conventions

- `LC_ALL=C` and NUL separators everywhere; torrent filenames can contain
  nearly anything except NUL and `/`.
- Probe GNU vs BSD userland at connect (`find -printf` and `stat -c` are
  GNU-only); degrade the affected ops per-tool on BSD remotes.
- Every exec carries a timeout and honors ctx cancellation; output is
  size-capped.

## Security

- Dedicated SSH key per instance; never the user's personal key.
- Private key stored AES-GCM encrypted with AAD binding (instance id +
  field), same `sessionSecret` pattern as existing credential encryption.
- Host key verification is TOFU: captured on first connect, enforced after.
  `InsecureIgnoreHostKey` is forbidden.
- Recommended `authorized_keys` template stays the tight one:
  `command="internal-sftp",restrict ...` — that key yields the SFTP-only
  tier. Granting exec is the user's explicit choice via a less-restricted
  key.
- Optional middle tier: a ~20-line POSIX forced-command allowlist script
  (technically a deployed artifact, but human-auditable) restores exec's
  benefits without an unrestricted key. Not required for any tier to work.
- Never log credentials or key material.

## Connection Pool

One pool keyed by instance: lazy dial, reconnect backoff 5s→60s with ±20%
jitter, every operation ctx-cancellable. The sftp client and exec sessions
share the one `x/crypto/ssh` connection. Concurrency comes from sftp
request pipelining plus bounded parallel exec sessions — no helper-process
lifecycle to manage.

## Schema

Half of the old design's schema survives: SSH columns on `instances`
(host, port, user, encrypted private key, host-key fingerprint). No
helper-deploy columns, no persisted capabilities. `HasFilesystemAccess`
resolves to local | remote | none. This is the slimmed scope for #1917.

## API

- `POST /instances/{id}/ssh-test` — dial with provided credentials, return
  host-key fingerprint for TOFU confirmation plus the capability report.
- `DELETE /instances/{id}/ssh-credentials`.
- No deploy/redeploy/helper endpoints.

## Frontend

SSH configuration on the instance form; the test flow confirms the host-key
fingerprint and shows the probed tier. Instances on the SFTP-only tier show
a degraded-mode indicator naming what's off (hardlink dedup, reflinks).

## Windows Remotes

Not a v1 target, but not special-cased either — the probe handles them for
free. Win32-OpenSSH's sftp-server covers the core ops (stat, readdir,
walks, mkdir, remove), so if the basic-op probe passes, the instance gets
the SFTP-only tier with whatever extensions the server actually advertises
(`statvfs@openssh.com` and `hardlink@openssh.com` support is
version-dependent in Win32-OpenSSH — trust the probe, not assumptions; a
server without the hardlink extension means link-tree cross-seeding is off
for that instance, and degraded mode says so). The exec tier never lights:
exec lands in cmd/PowerShell and the GNU-userland probe fails. A PowerShell
exec dialect (`fsutil file queryfileid`, `fsutil hardlink list`,
`Get-ChildItem` sweeps) is possible later if demand appears; the opaque
FileID form keeps that door open. Remote Windows paths surface in
SFTP's `/C:/...` form and stay slash-delimited at the fsops boundary like
every other remote path.

## Rollout

1. Foundation (open): #1914 backend interface, #1915 callsite migration,
   #1916 missing-files.
2. #1917 reshaped to the schema above.
3. Remote backend: pool + SFTP implementation + capability probe (re-adds
   batch methods), API endpoints, OpenAPI.
4. Frontend.
5. Feature rollout per service, degraded-mode UX.

Helper/agent tier: explicitly deferred. If SFTP+exec hits a real
performance wall, #1913 has the protocol design ready.

## Open Questions

1. File identity wire form — option (a) or (b) above.
2. `SameFilesystem` on SFTP-only when the server zeroes fsids: conservative
   `false`, or refuse ops that require the answer?
3. BSD/macOS remotes: which exec probes degrade, and is SFTP-only the
   supported floor there?
