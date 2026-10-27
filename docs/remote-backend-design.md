# Remote Filesystem Backend Design (SSH/SFTP-native)

Status: largely built; kept as the design reference. Supersedes the remote-helper design
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
  gets. Only the tier is stored, in `instances.ssh_tier`, and every connect
  probes the host again and writes it.

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

**SFTP+exec** (tier `exec`): the key runs a shell with GNU `find` and
`stat`. Identity comes from `find`/`stat`, and `SameFilesystem` is
exact. Only the tier is stored, not each probe flag, because no other flag
changes a capability yet. Reflink is not probed, and reflinks are off on
every remote tier. A host without the GNU tools gets tier `sftp`, and so
does a login shell that does not quote like POSIX: every command is
single-quoted for that shell, and fish reads `\'` inside single quotes as
an escape, so the probe runs `printf '%s' 'a\'` and expects `a\` before it
asks for the tools.

`SameFilesystem` with unusable fsids (servers that report zeroes, no exec
fallback): returns an explicit error, never a guess in either direction.
Both migrated callers (season-pack base-dir selection, dirscan link-tree
placement) already treat the error as "don't hardlink" and fall back to
non-link strategies, which is the safe degradation.

## Operation Mapping

| fsops.Backend | SFTP-only | with exec |
|---|---|---|
| `Stat` / `Lstat` | `SSH_FXP_STAT` / `LSTAT` (identity → `FileIDErr`) | + `stat -L` / `stat` for identity, never following a link for `Lstat` |
| `ReadDir` | `SSH_FXP_READDIR` (attrs included, no per-entry stat) | same |
| `WalkDir` | recursive readdir | `find -printf` streams a tree's paths+identity in one round trip; qui reads all of it and sorts it in Go |
| `Statfs` | `statvfs@openssh.com` | same |
| `SameFilesystem` | fsid compare from statvfs, if the server reports real fsids (probe; some return zero) | `stat -c %d` compare |
| `MkdirAll` | `SSH_FXP_MKDIR` walk-up | same |
| `Remove` | `REMOVE`/`RMDIR`; recursive = readdir-driven bottom-up | same |
| `HardlinkTree` | `hardlink@openssh.com` per file | same |
| `ReflinkTree` | unsupported | unsupported |
| `RemoveTree` | `REMOVE` over the recorded handle | same |
| `SupportsReflink` | false | false |

Batch stat is the optional `fsops.BatchStater`, with `StatBatch` and
`LstatBatch`. It is not on `fsops.Backend`. A caller that knows all its paths
calls `fsops.StatAll` or `fsops.LstatAll`. Each helper uses the batch when the
backend has it, and one `Stat` or `Lstat` per path when it does not. The local
backend has no batch. `StatAll` follows symlinks and `LstatAll` does not.

On the exec tier, one `find -maxdepth 0 -printf` reads up to 1,000 paths with
their identity in two round trips. `StatBatch` adds `-L`. A single `Stat` or
`Lstat` answers each path that `find` did not print, such as a missing path,
so the errors are the same as for one path. `find -L` prints a broken link as
the link itself, so `StatBatch` sends that path to `Stat` too. On the sftp
tier, a single call answers each path.

The single calls of a batch run 32 at a time on the one sftp client, so they
share round trips. In the tests, 64 paths at 25 ms of delay take about 50 ms,
not 1.6 s. The same pipeline answers the files of a completed torrent after the user moved
them away. On the exec tier, that torrent costs one `find`, plus about one
round trip for each 32 missing files. After a lost connection, the batches
stop, and at most one single call dials the host again. If the host is back,
the single calls answer. If not, each path that is left gets its own
`fsops.ErrConnectionLost` at once, because the pool remembers the failed dial.

The exec tier uses `find` and not `xargs -0 stat`, because `find` prints the
same record as the walk, so one parser reads both. Cross-seed hardlink
matching uses `LstatBatch` since #2726. A 20-file pack takes about 200 ms at
100 ms of delay, not about 6 s. The missing-files condition (#2933) will be the
first caller of `StatAll`. The hardlink index keeps one `Lstat` per file. It
needs trusted identity, which only a local instance has, and a local backend
has no batch.

## File Identity Over the Wire — DECIDED

Remote identity is (device, inode) parsed from GNU `find`/`stat` output. Before
#2970, `hardlink.FileID` was platform-compiled: unix builds carried `Dev`/`Ino`,
Windows builds a volume serial plus a 16-byte identifier, so a qui host on
Windows could not represent a Linux seedbox's identity.

**Decision: `hardlink.FileID` becomes an opaque, tagged, comparable
fixed-size form, landed in #2970 before the remote backend uses it**
(raised by com6056 on #1914). Opaque is the only viable shape: unix identity is 16 bytes,
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
persists it as `dir_scan_files.file_id` under a partial index on
`(directory_id, file_id)` matched by rename detection, so the encoding
change ships with a migration that sets `dir_scan_files.file_id` to
NULL; there is no legacy read path, the next scan fills the column
again. `IsZero()` survives unchanged; `==` and bare map keys give way
to the helper below, and the rest of the churn is per-platform
constructors and test literals.

Guard regardless of representation: FileID comparisons are only valid
within one backend/host, and `==` still compiles cross-host, so
comparisons go through `fsops.SameFile`, which takes the two instances
the IDs came from, and indexes key on `fsops.FileKey`, the ID bound to
its instance. IDs of different kinds never match; two kind 3 IDs match
only when both instances have the same SSH host and port, and a kind 3 ID
with no instance or no SSH host matches nothing. The bytes a
unix ID stores are still the device and inode, under the tag.
Remote-sourced identity is always kind 3, even
when the remote is unix and the bytes are a `dev`/`ino`: it is parsed
from command output on a host qui does not control, and the tag keeps
peer-asserted identity type-distinct from kernel-attested identity. Peer
identity is advisory. It may suppress work (dedup accounting,
already-seeding skips) but never expands a destructive action. The guard
is the capability `TrustedIdentity`, which only a local instance has.
The include-hardlinks delete option, the `HARDLINK_SCOPE` and
`HARDLINK_SCOPE_CROSS` conditions and hardlink grouping in automations gate
on it, not on Identity. A remote host that reports `nlink=1` for a file
linked into a library would otherwise make that torrent read as scope
`none`, and a "Remove Upgraded Torrents" rule would delete it. A hostile remote can never reach a
worse outcome than the SFTP-only tier already produces with no identity
at all.

## Exec Conventions

- `LC_ALL=C` and NUL separators everywhere; torrent filenames can contain
  nearly anything except NUL and `/`.
- Probe GNU vs BSD userland at connect (`find -printf` and `stat -c` are
  GNU-only). A host without GNU `find` and `stat` gets tier `sftp`.
- Every exec honors ctx cancellation. One command has a 30 s timeout and
  keeps at most 1 MiB of output. `find` has no total output cap: it has a
  cap for each record, a cap of 500,000 entries, and ctx.
- One connection runs at most four exec sessions at a time.

## Path Domains

Every path handed to a Backend belongs to that backend's filesystem, in
that filesystem's native form — the interface itself is path-domain
agnostic. The local backend speaks host paths, so today's callsites using
host `filepath` are correct by construction. The remote backend speaks
slash-delimited POSIX paths regardless of the qui host's OS, which means
host `filepath` must never touch a remote path: on a Windows host,
`filepath.IsAbs("/data")` is false and `Join` inserts backslashes
(raised by Audionut on #1914). Slice 3c (#2724) gives every backend a
path dialect, `Backend.Paths() fsops.PathDialect` (`Join`, `Dir`, `Base`,
`Clean`, `IsAbs`, `Rel`, `FromSlash`, `ToSlash`, `Separator`): the local
and noop backends answer `fsops.HostPaths`, which is the host `filepath`,
so local callsites keep their exact behavior; the remote backend answers
`fsops.SlashPaths`, which is `path` with `FromSlash`/`ToSlash` as the
identity and a slash-only `Rel` (`path` has none, and `filepath.Rel`
answers with backslashes on a Windows host), so a Windows-hosted qui
operating a unix remote is correct by construction rather than by luck.
Like `Backend`, `PathDialect` exceeds the five-method guideline on
purpose: it mirrors `filepath`'s grammar, and a smaller split would leave
callers reaching for the host package again. A path that goes to or comes from a
backend is manipulated with that backend's dialect; host-only paths (the
data dir, backups) keep `filepath`. Paths from qBittorrent's API arrive
slash-delimited and stay inside their instance's backend domain end to
end. 3c moved the free-space path source, missing-files, the hardlink
index, the dirscan scanner and the fileid index onto the dialect. Orphan
scan moved in #2930: its comparison keys keep the dialect's own separators
(a canonical slash key would merge a Windows UNC path with a rooted one),
peers' roots and file maps are built in the scanning backend's dialect, and
ignore paths are validated at save time in the dialect of the instance's
backend, which is the one the scan reads them in. Cross-seed moved in #2943:
link mode, season packs, the Manual match preview and local-match evidence
take the dialect from the backend each function holds, and a season pack
plans and creates its tree on one backend read. `hardlinktree`'s plan
builders take the dialect as a leading parameter, through a four-method
`hardlinktree.PathDialect` that `fsops.PathDialect` satisfies (`fsops`
imports `hardlinktree`, so the dependency cannot point the other way), and
they check torrent paths in slash grammar on every host. When the
destination dialect is Windows they also reject any component Windows
cannot store as a file name: a colon, which NTFS reads as an alternate data
stream of another file (found natively by Audionut on #3140), the other
reserved characters, control characters, a trailing dot or space, and the
reserved device names. The partial pool,
`hardlinktree.Create`, reflink creation and the shared-allocation check are
host code and run only for local instances. Managed-delete cleanup (#2947),
the sync manager and dirscan's `buildLinkDestDir` (#2919) still use host
`filepath`.

## Path and Command Safety

- Torrent- and API-derived paths are validated before any SFTP or exec use,
  under the same boundary rules the local backends enforce: slash-delimited,
  no leading separator, no `..` component, no drive-letter or UNC form where
  a relative path is required. Absolute remote paths are permitted only as
  instance-level roots (e.g. save paths reported by qBittorrent); torrent
  file names always join beneath one.
- Exec command lines never interpolate raw paths into a shell string:
  arguments are strictly quoted, and options are terminated with `--`
  before any path for `find` and `stat`.
- Symlink policy for destructive ops: recursion never follows a link, on
  any tier. A symlinked directory is never descended; the link itself is
  removed, never the target's contents. The local backend's
  `os.RemoveAll` traverses `openat`-style, so it refuses links and is
  immune to a mid-walk swap. SFTP v3 gets only the first half, and an
  exec-tier remote removes over SFTP too: no `openat`, no `O_NOFOLLOW`,
  every path re-resolved server-side,
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
- Key derivation: `GetEncryptionKey` now derives the key from the whole
  `sessionSecret` with HKDF-SHA256 rather than truncating it (#2521). New
  writes carry a `qui2:` prefix, so the stored format is decidable.
  Rows written under the truncated key stay readable through
  `GetLegacyEncryptionKey`, and each store rewrites its own on first
  start. A row that does not decrypt is left alone and warns on every
  start until the credential is entered again.
- Host key verification is TOFU with explicit confirmation: the first-seen
  key is held ephemeral and surfaced as a fingerprint via the ssh-test
  flow; it is persisted and enforced only after the user confirms it (or
  it matches a preconfigured fingerprint). No connection is trusted for
  real operations before that. `InsecureIgnoreHostKey` is forbidden.
  First contact offers only `ssh.SupportedAlgorithms().HostKeys`,
  ed25519 first. A host that signs only with SHA-1 `ssh-rsa` or
  `ssh-dss` cannot be pinned, and `ssh-test` reports the failed
  negotiation.
- What gets pinned is the marshaled public key and its algorithm, not a
  display string; later connects put the pinned key's algorithms first in
  `HostKeyAlgorithms` (a multi-key host offers the key the client prefers,
  so a still-valid pin is never mismatched by accident) and leave the rest
  allowed, so a host that changed key type reports as a mismatch the user
  can act on rather than a failed negotiation. Verification is always the
  byte comparison against the pin. Fingerprints render as `SHA256:` for
  humans only.
- A host-key change after pinning fails closed: no automatic re-pin. A
  pin that fails to decrypt is never "unpinned": `ssh-test` reports it as
  `pin_unreadable` with the presented key (when the host answers) and no
  probe, nothing runs over
  that connection, and the way out is the replace route with its heavier
  confirmation, the same door a mismatch uses (an endpoint change drops the
  pin as it always does, and takes first contact). A probe the connection
  does not survive, whether the request was cancelled or the deadline
  fired, is an error, never a partial capability report. An empty
  pin column is unpinned and takes the first-contact flow: there is no
  separate "was pinned" state, so a database writer who clears the column
  is not detected. What that buys them is a first-contact confirmation the
  user sees in place of the mismatch flow, not a silent re-pin; the AAD
  binding above still refuses the transplant and redirect edits. Writing
  the pin is one-shot:
  `SetHostKeyPin` refuses an instance that is already pinned, so
  replacing a live pin is a separate, named operation on the
  confirmed-mismatch path. The mismatch surfaces both fingerprints and
  both key types behind a
  confirmation deliberately heavier than first contact, one that names
  interception as a possible cause and points at out-of-band
  verification. A legitimate re-key and an interception look identical to
  qui, so the user makes that call, never the code. On a background
  reconnect nobody is there to prompt, so a mismatch fails every fsop on
  the instance. The refusal lives only in memory (see Connection Pool):
  confirming or replacing the pin clears it, and so does a restart, after
  which the next dial is refused again while the key is still wrong.
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

One pool keyed by instance. Each instance gets one `x/crypto/ssh`
connection with one sftp client on it, dialed lazily on the first
operation. A keepalive goes out every 30s and
the host has 15s to answer it; a silent host is closed, and the next
caller redials. A dead sftp channel on a live transport is treated the
same way as a dropped connection: the entry is cleared and the next caller
redials. Opening the sftp subsystem is bounded by the dial timeout, so a
host that accepts the handshake and then stalls the subsystem request
fails the call instead of wedging the instance. A failed dial is memoised so a job touching hundreds of
paths pays for one attempt: the retry delay starts at 5s, doubles to
60s, and carries ±20% jitter so instances that went down together do not
come back in lockstep.

A host-key mismatch and an unreadable pin are not retried at all, because
waiting does not make a wrong key right. That refusal lives only in
memory, and the pool never infers a change from a caller's snapshot of
the instance: the code that changes what a connection depends on tells it.
Saving or clearing SSH credentials and confirming or replacing the pin
invalidate the instance's entry, and deleting the instance removes it;
both end the session and clear any memo. Every dial reads the current
row, never a caller's snapshot, so the next caller redials with the values
just written, and a host whose key is still wrong is refused again on that
dial. Two callers holding different snapshots of one instance therefore
share one connection. A failed read of the row is local and is not
memoised. A refusal caused by a pin that would not decrypt outlives an
out-of-band fix of the encryption key, because that fix writes no row.
Saving the SSH credentials or the pin again clears it, and so does a
restart.
Saving the instance invalidates its entry too when the save changes the
filesystem mode, which the local access flag decides. Any other edit
leaves the session alone. A dial for a row that is not in remote mode,
including a row with credentials and no confirmed pin, is refused before
it connects and is not memoised.
A connection nobody has used for ten minutes is closed.

Exec sessions share the same connection, at most four at a time.
Concurrency comes from sftp request pipelining plus these exec sessions,
with no helper process to manage. A running exec counts as use, so the idle
close does not cut a long `find`. Every connect runs the GNU probe and
writes the tier to the row when it changed, so an existing instance moves to
`exec` on its first connect, and a key that lost its shell moves back to
`sftp`. A probe that gets no answer writes nothing, and that connection
runs no exec. An exec that a caller started on the stored `exec` tier, on a
connection whose probe found `sftp`, fails with `fsops.ErrUnsupported`.

## Schema

Half of the old design's schema survives: SSH columns on `instances` —
host, port, user, the AEAD-encrypted private key (AAD: instance id +
field), and the pinned host key stored as the marshaled public key plus
its algorithm under the same AEAD (AAD: instance id + field + host +
port). Not a fingerprint column: the `HostKeyAlgorithms` preference and
the mismatch flow both need the full key, and fingerprints are
display-only (see Security). No helper-deploy columns. Of the probe
results, only the tier is stored (`ssh_tier`, `sftp` or `exec`): the SSH
test route, the pin routes and every pool connect write it again. `FilesystemAccessMode` resolves to local | remote | none.
This is the slimmed scope for #1917, which also carries the credential
store that owns these columns: the AEAD write and read path, and a pin
the store sets once, drops when the host or port changes, and keeps when
the credentials are cleared. Replacing a live pin lands with the mismatch
flow in a later PR. Columns without the code that owns them cannot be
tested end to end, and the AAD binding is only real once something
applies it. Note that the AAD carries the instance id, so credentials can
only be encrypted after the row exists — the endpoints below all operate
on an existing instance, and a future create-with-credentials call would
need insert-then-update in one transaction.

## API

- `PUT /instances/{id}/ssh-credentials` — store host, port, username and
  private key; a host or port change drops the pin.
- `DELETE /instances/{id}/ssh-credentials` — clear the credentials, keep
  the pin.
- `POST /instances/{id}/ssh-test` — dial with the stored credentials and
  report the presented host key, its relation to the pin (`unpinned`,
  `pinned`, `mismatch`, `pin_unreadable`, `error`) and, when the key is
  trusted or unpinned, the capability report.
- `POST /instances/{id}/ssh-host-key` — re-dial, require the host to
  present exactly the echoed key, pin it; 409 when already pinned.
- `POST /instances/{id}/ssh-host-key/replace` — the same over an existing
  pin, reached only from the mismatch or unreadable-pin screen; 409 when
  unpinned.
- No deploy/redeploy/helper endpoints.

## Frontend

SSH configuration on the instance form; the test flow confirms the host-key
fingerprint and shows the probed tier. The UI draws the capabilities of an
instance from the stored tier, so it needs no connection to do so.
Reflinks are off on every remote tier. Instances on the SFTP-only tier show
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
every other remote path. Because such a remote uses `SlashPaths`, the Windows
name check above does not apply, so a POSIX name containing `:` is still
accepted and would land as an NTFS alternate data stream, a known gap.

## Field Validation

The tier model above was checked against a commercial shared seedbox
(Debian 11, OpenSSH 8.4p1) on 2026-08-23, before the remote backend
implementation existed. The probe was read-only apart from two self-cleaned
scratch directories and a temporarily added, uniquely tagged
`authorized_keys` line.

- `statvfs@openssh.com` and `hardlink@openssh.com` both work over the sftp
  subsystem. `df` returned filesystem numbers and `ln` created a real
  hardlink. The sftp attribute set has no link-count field, and the only
  place the server exposes one is the free-form `ls -l` longname, which
  the protocol tells clients not to parse and `pkg/sftp` discards, so link
  identity only ever arrives through the exec tier (the `find -printf`
  sweep below).
  `limits@openssh.com` is absent (it arrived in OpenSSH 8.6), so the
  backend must tolerate that extension missing.
- A `command="internal-sftp",restrict` key behaves exactly as the Security
  section specifies: sftp works, exec is refused with "This service allows
  sftp connections only." The recommended template holds as written.
- The exec tier finds full GNU userland (findutils 4.8, coreutils 8.32), so
  `find -printf` identity sweeps work without the BSD degradation path.
- Hardlinks across directories keep consistent inode, device and nlink, and
  `find -printf '%D %i %n'` reports them, so the exec-tier identity design
  holds on the real filesystem.
- Reflink is unsupported (XFS without reflink). Providers mostly do not
  enable it on shared hosts, and where it exists it is a dedicated-host
  option on request. qui does not probe it: reflinks are off on every
  remote tier, and hardlink mode is the real path there.
- Latency is the load-bearing number: about 740 ms per sftp readdir round
  trip from a home connection, and 2.1 to 3.5 s per cold connect, auth and
  exec.
  A 500-directory walk is roughly six minutes serial over sftp against one
  `find` exec round trip, which is why the batch methods, the exec tier and
  the pooled persistent connection are all necessary rather than
  optimizations.

## Rollout

1. Foundation (merged): #1914 backend interface, #1915 callsite migration.
   #1916 (missing-files) was closed as superseded — #1915 carries that
   migration along with every other callsite.
2. #1917: the schema above plus its credential store.
3. Remote backend, in slices:
   - 3a (#2722): one-shot dialing, host-key pinning, the capability probe
     and the credential endpoints.
   - 3b (#2723): the persistent connection pool and the SFTP backend's
     read methods. Writes refuse with `fsops.ErrUnsupported`.
   - 3c (#2724): path dialect on the backend and the callsite sweep.
   - 3d (#2725): SFTP write operations.
   - 3e (#2726): the exec tier and remote file identity. The pool hands out
     the ssh client for bounded exec sessions, and batch lstat for
     cross-seed matching. The rest of batch stat is #2971.
4. Frontend. Lands last. It is the only UI path that sets up SSH access,
   so it gates the rollout for users. Until #2917 ships, no UI text names
   SSH. `web/scripts/no-ssh-in-locales.test.mjs` enforces this for the
   locales. Text that only a remote instance renders says "remote
   instance" and stays. Text a local user also sees keeps its develop
   wording, and the slice adds it to the reverse list in #2917's body.
   (#2916 Decisions)
5. Feature rollout per service, degraded-mode UX. Consumers admit an
   instance on a filesystem capability (`Pool.Require`,
   `models.FilesystemCapabilitiesOf`): automations
   (missing-files condition; the hardlink index, which gates on Identity
   and which 3e opens to exec-tier remotes, while the automations fields
   that read it gate on `TrustedIdentity`; rule save and dry-run
   validation), dirscan, cross-seed (link mode, manual assemble,
   mediainfo, season pack, partial pool, local-match detection), the sync
   manager's hardlink base dir, the disc-scan route (which requires
   `CapabilityContent`), and two routes that read file content, which
   no `Backend` method covers yet: the torrents handler's local-access
   routes and the proxy mediainfo route. The free-space
   path source: its preview and scheduled-run paths call `Statfs` only
   after `Pool.Require` grants the Read capability, and rule save
   validation also checks Read. A remote-mode instance has Read, so it
   already reports remote free space, and that is the intended figure. A gate
   reads the instance before the work starts, so the reads after it
   resolve backend and mode from one later read and refuse every mode but
   local. Dirscan does that through
   `Pool.LocalBackend`, and missing files through
   `Pool.Resolve`. An instance whose local access was turned off after the
   gate is then refused rather than read over SSH at a local path. Orphan
   scan admits an instance on Read, walks the backend `Pool.Require`
   returns for Read, and deletes from a run scanned over SSH only on the exec
   tier, when the walk read every file's identity and the SSH endpoint has not
   changed since the scan (#3236).
   Each remaining gate lifts in its own slice, with the degraded-mode
   handling that service needs, and the API-driven checks
   (missing files, orphan scan) become the field test of that slice. Every
   remote read that loses its connection, or that the pool will not dial,
   fails with `fsops.ErrConnectionLost`. When the pool refused on purpose
   (a host key mismatch, an unusable pin, a closed pool, an instance no
   longer in remote mode), its sentinel stays in the chain, and the rest
   of the cause is text only. A walk ends with one `Err` entry carrying
   it, so a consumer that skips per-directory errors still learns the tree
   was cut short.

Helper/agent tier: explicitly deferred. If SFTP+exec hits a real
performance wall, #1913 has the protocol design ready.

## Open Questions

1. BSD/macOS remotes get tier `sftp`, like every non-GNU host. Is a BSD
   exec dialect worth adding?
