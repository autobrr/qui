# Remote Filesystem Backend Design (SSH/SFTP-native)

Status: draft for maintainer review. Supersedes the remote-helper design
(PR #1913, closed but kept as reference — it documents the deployable-agent
tier and its NDJSON wire protocol, which remain the fallback if this design
hits a performance wall).

## Context and Decision

PR #1914 adds `fsops.Backend`; service callsites migrate from direct
`os.*` calls to it in #1915. Instances running on a different host than qui
need a remote implementation.

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
walks, mkdir, and remove always work; free space and hardlink-tree
creation depend on the `statvfs@openssh.com` and `hardlink@openssh.com`
extensions, which the connect probe tracks independently — an operation
whose extension the server does not advertise returns an explicit
unsupported error and appears in the capability report, rather than the
tier being assumed to include it. SFTP v3 attrs carry no inode or
nlink, so file identity is unavailable: `Lstat`/`WalkDir` set `FileIDErr`
when identity is requested. Consumers already degrade on zero FileID —
orphan-scan alias dedup switches off, hardlinked-copy detection reports
no-evidence, dirscan's FileID index skips the files. Reflinks are
unsupported (no SFTP equivalent of FICLONE; `copy-data` is a byte copy, not
CoW). Missing identity also changes hardlink-tree conflict handling: the
local backend proves an existing target is the same link (`os.SameFile`)
and skips it idempotently, but SFTP-only cannot verify that, so an
existing target is always a conflict and fails the create — never a
silent skip (raised by Audionut on #1914). The degraded mode must be
surfaced in the UI, not silent.

**SFTP+exec**: identity arrives via `find`/`stat` sweeps, reflink support is
probed and used, `SameFilesystem` is exact. "Full functionality" means
every operation has either a supported SFTP extension or an exec
fallback — the probe verifies that per operation, it is not implied by
the tier label.

`SameFilesystem` with unusable fsids (servers that report zeroes, no exec
fallback): returns an explicit error, never a guess in either direction.
Both migrated callers (season-pack base-dir selection, dirscan link-tree
placement) already treat the error as "don't hardlink" and fall back to
non-link strategies, which is the safe degradation.

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

## File Identity Over the Wire — DECIDED

Remote identity is (device, inode) parsed from GNU `find`/`stat` output. But
`hardlink.FileID` is platform-compiled: unix builds carry `Dev`/`Ino`,
Windows builds carry a volume serial plus a 16-byte identifier. A qui host
on Windows cannot represent a Linux seedbox's identity in today's struct.

**Decision: `hardlink.FileID` becomes an opaque, tagged, comparable
fixed-size form, implemented in the remote-backend PR** (raised by com6056
on #1914). Opaque is the only viable shape: unix identity is 16 bytes,
Windows identity is up to 24, so no packing into the other platform's
struct is lossless in either direction, and the type is already shared
across develop's consumers, so the ripple is the same size now or later.
Tagged, because untagged bytes let a unix `dev=1,ino=2` collide with a
Windows identity in the same space: `struct { kind uint8; raw [24]byte }`,
kind 0 for no identity, 1 for unix `dev`+`ino` big endian, 2 for a
Windows volume serial plus the 16-byte file id, 3 for a remote-provided
blob. Constructors zero the unused tail of `raw` so `==` stays
well-defined, and `IsZero()` then means "no identity" rather than "a
backend wrote zeros" (load-bearing: the hardlink index trusts any FileID
returned without error, so zeroes-as-identity would collapse torrents
into one delete-safe group). `Bytes()` returns the tagged form: dirscan
keys its FileID index on `string(FileID.Bytes())` and persists it as
`dir_scan_files.file_id` under a partial index on
`(directory_id, file_id)` matched by rename detection, so the encoding
change ships with a migration (or a read path accepting the legacy
widths) whenever it lands. Consumer churn is otherwise unchanged: `==`,
map keys, and `IsZero()` all survive; the churn is per-platform
constructors and test literals.

Guard regardless of representation: FileID comparisons are only valid
within one backend/host, and `==` still compiles cross-host, so
comparisons go through a helper that takes the backend scope (or the
scope rides in the value). Remote-sourced identity is always kind 3, even
when the remote is unix and the bytes are a `dev`/`ino`: it is parsed
from command output on a host qui does not control, and the tag keeps
peer-asserted identity type-distinct from kernel-attested identity. Peer
identity is advisory. It may suppress work (dedup accounting,
already-seeding skips) but never expands a destructive action:
automations' delete-expansion groups torrents by a FileID signature and
deletes the group with files, so on a remote backend that expansion
degrades to the no-identity behavior. A hostile remote can never reach a
worse outcome than the SFTP-only tier already produces with no identity
at all.

## Exec Conventions

- `LC_ALL=C` and NUL separators everywhere; torrent filenames can contain
  nearly anything except NUL and `/`.
- Probe GNU vs BSD userland at connect (`find -printf` and `stat -c` are
  GNU-only); degrade the affected ops per-tool on BSD remotes.
- Every exec carries a timeout and honors ctx cancellation; output is
  size-capped.

## Path Domains

Every path handed to a Backend belongs to that backend's filesystem, in
that filesystem's native form — the interface itself is path-domain
agnostic. The local backend speaks host paths, so today's callsites using
host `filepath` are correct by construction. The remote backend speaks
slash-delimited POSIX paths regardless of the qui host's OS, which means
host `filepath` must never touch a remote path: on a Windows host,
`filepath.IsAbs("/data")` is false and `Join` inserts backslashes
(raised by Audionut on #1914). The remote-backend PR introduces a path
dialect for backend-owned path manipulation (Join/Dir/Base/IsAbs/Rel);
the local backend's dialect is the host `filepath`, so existing callsites
keep their exact behavior, and a Windows-hosted qui operating a unix
remote becomes correct by construction rather than by luck. Paths from
qBittorrent's API arrive slash-delimited and stay inside their instance's
backend domain end to end.

## Path and Command Safety

- Torrent- and API-derived paths are validated before any SFTP or exec use,
  under the same boundary rules the local backends enforce: slash-delimited,
  no leading separator, no `..` component, no drive-letter or UNC form where
  a relative path is required. Absolute remote paths are permitted only as
  instance-level roots (e.g. save paths reported by qBittorrent); torrent
  file names always join beneath one.
- Exec command lines never interpolate raw paths into a shell string:
  arguments are strictly quoted, and options are terminated with `--`
  before any path for `find`, `stat`, `xargs`, `rm`, and `cp`.
- Symlink policy for destructive ops: recursion never follows a link, on
  any tier. A symlinked directory is never descended; the link itself is
  removed, never the target's contents. `rm -rf --` and the local
  backend's `os.RemoveAll` both traverse `openat`-style, so they refuse
  links and are immune to a mid-walk swap. SFTP v3 gets only the first
  half: no `openat`, no `O_NOFOLLOW`, every path re-resolved server-side,
  so a swapped ancestor directory can redirect an unlink between the walk
  and the remove. That race is unfixable over v3; bound it by
  re-`lstat`ing immediately before each unlink. `RemoveTree` is a path
  list, not a walk, and takes the same shape per entry: unlink only,
  directories only when empty.
- The never-follow rule covers identity as well as traversal: identity
  feeding a destructive decision comes from `lstat`, so a planted symlink
  cannot attribute another file's identity to a path inside the tree.
- The remote-backend PR carries tests for traversal payloads, option-like
  filenames (`-rf`), shell metacharacters in paths, and symlinked
  directories inside a tree marked for recursive removal.

## Security

- Dedicated SSH key per instance; never the user's personal key.
- Private key stored AES-GCM encrypted, keyed from `sessionSecret` like
  existing credential encryption; the AAD binding (instance id + field)
  is new here, today's credential stores pass no additional data. The
  pinned host key is stored under the same AEAD with host and port in its
  AAD as well: the pin fixes where the key gets used, and encrypting the
  key alone says nothing about that. A plaintext-column edit redirecting
  the instance then fails as a decryption error, which is unambiguous
  tampering, rather than as a host-key mismatch, which is not. The scope
  is deliberate: this defeats a DB-write attacker and cross-instance
  transplant, not a stolen data directory (`sessionSecret` sits beside
  `qui.db` by default) and not rollback to an older row set for the same
  instance, which needs state outside the DB. Tests cover
  tampered-ciphertext failing closed rather than falling back to TOFU.
  The motivating deployment is Postgres, where the database can live on a
  different host from the app and `sessionSecret`: there, a DB-write
  attacker without app-host access is a realistic position, and the AAD
  binding is what stops credential transplant and instance redirection.
  On single-host SQLite the binding is cheap belt-and-suspenders.
  Mechanically this stays the product's one crypto pattern — the same
  AES-GCM/`sessionSecret` helpers the existing credential stores use,
  with an AAD argument those stores simply haven't passed before.
- Host key verification is TOFU with explicit confirmation: the first-seen
  key is held ephemeral and surfaced as a fingerprint via the ssh-test
  flow; it is persisted and enforced only after the user confirms it (or
  it matches a preconfigured fingerprint). No connection is trusted for
  real operations before that. `InsecureIgnoreHostKey` is forbidden.
- What gets pinned is the marshaled public key and its algorithm, not a
  display string; later connects constrain `HostKeyAlgorithms` to the
  pinned type, so a key-type change is a mismatch, never a negotiation
  accident. Fingerprints render as `SHA256:` for humans only.
- A host-key change after pinning fails closed: no automatic re-pin, and
  no fallback to TOFU if the stored pin is missing or unreadable. The
  mismatch surfaces both fingerprints and both key types behind a
  confirmation deliberately heavier than first contact, one that names
  interception as a possible cause and points at out-of-band
  verification. A legitimate re-key and an interception look identical to
  qui, so the user makes that call, never the code. On a background
  reconnect nobody is there to prompt, so a mismatch parks the instance
  in a needs-reconfirmation state and fails every fsop until a human
  clears it.
- The pin belongs to the host, not the credential: deleting SSH
  credentials keeps the pin, `ssh-test` against a pinned instance routes
  into the mismatch flow rather than first contact, and editing host or
  port drops the pin deliberately and requires fresh confirmation. Tests
  cover mismatch-fails-closed, no re-pin via `ssh-test`, and key-type
  change.
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

Half of the old design's schema survives: SSH columns on `instances` —
host, port, user, the AEAD-encrypted private key (AAD: instance id +
field), and the pinned host key stored as the marshaled public key plus
its algorithm under the same AEAD (AAD: instance id + field + host +
port). Not a fingerprint column: the `HostKeyAlgorithms` constraint and
the mismatch flow both need the full key, and fingerprints are
display-only (see Security). No helper-deploy columns, no persisted
capabilities. `HasFilesystemAccess` resolves to local | remote | none.
This is the slimmed scope for #1917, which also carries the credential
store that owns these columns: the AEAD write and read path, and setting
or clearing the pin. Columns without the code that owns them cannot be
tested end to end, and the AAD binding is only real once something
applies it. Note that the AAD carries the instance id, so credentials can
only be encrypted after the row exists — the endpoints below all operate
on an existing instance, and a future create-with-credentials call would
need insert-then-update in one transaction.

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

1. Foundation (open): #1914 backend interface, #1915 callsite migration.
   #1916 (missing-files) was closed as superseded — #1915 carries that
   migration along with every other callsite.
2. #1917: the schema above plus its credential store.
3. Remote backend: pool + SFTP implementation + capability probe (re-adds
   batch methods), API endpoints, OpenAPI.
4. Frontend.
5. Feature rollout per service, degraded-mode UX.

Helper/agent tier: explicitly deferred. If SFTP+exec hits a real
performance wall, #1913 has the protocol design ready.

## Open Questions

1. BSD/macOS remotes: which exec probes degrade, and is SFTP-only the
   supported floor there?
