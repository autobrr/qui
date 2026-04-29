# qui Remote Helper — Design Document

> This is one of two parallel design proposals for enabling qui's filesystem-dependent features against remote qBittorrent instances. The companion document `remote-agent.md` describes a daemon model that runs as a long-lived service on the seedbox and dials qui over HTTPS. **This document** describes a helper-binary model that qui invokes over SSH, with one persistent helper process per instance multiplexed over an SSH stdio channel. Both designs sit behind the same `internal/fsops` `Backend` interface (§8) and share the same `pkg/fsexec` security-critical layer (§6) — service code in qui is invariant across the two. The implementation choice affects deployment, auth, transport, and runtime supervision; it does not affect what features work or how dirscan/orphanscan/automations are written.

## 1. Context & Motivation

qui has a cluster of features that work only when the qui process and qBittorrent share a host: cross-seed hardlink/reflink injection, dirscan, orphanscan, three automation rule conditions (missing-files, path-based free-space, hardlink-scope), and the post-delete managed-cleanup pass. All of these reach into the local filesystem via `os.*`/`filepath.*`/`unix.*`. A non-trivial fraction of users run qBittorrent on remote seedboxes/VPS while qui lives on a home server or in a container. For them, every feature listed above is dead — and the network-mount workarounds (SSHFS, rclone, NFS) either misreport inode/nlink (breaking cross-seed) or are operationally painful. The complete inventory and user-visible mapping live in §2 and the bullets below.

**Proposal.** Ship a small static Go binary, `qui-helper`, that qui pushes to the remote host via SCP and then drives via SSH. qui maintains one persistent SSH connection per instance and runs `qui-helper serve --stdio` as a long-lived session over that connection. Commands flow as NDJSON on the helper's stdin; results flow back as NDJSON on stdout; structured logs flow on stderr. The helper executes filesystem ops via the same `pkg/fsexec` primitives qui uses locally, so path-safety policy is identical. There is no listening port on the seedbox, no pairing string, no bearer-token dance — SSH credentials are the only secret, encrypted at rest in qui exactly the way qBittorrent passwords already are.

**Features the agent unlocks.** Identical to the daemon design. With a deployed helper, every filesystem-dependent feature qui ships today works against a remote qBittorrent instance with the same UX users get on a co-located install:

- **Cross-seed inject — hardlink mode.** When cross-seed identifies a torrent that matches data already in the user's library, qui materializes a hardlink tree on the seedbox so qBittorrent can immediately seed the matched torrent without re-downloading any bytes. The helper's `pkg/fsexec` primitives execute the link tree atomically and roll back cleanly on partial failure. Includes the same-filesystem precondition check (the link-tree base directory and the source data must share a device) so qui never attempts a hardlink that would cross filesystems.
- **Cross-seed inject — reflink mode.** Same shape as hardlink mode but using copy-on-write reflinks (XFS, Btrfs, ZFS). Eliminates even the inode pressure of hardlinks. The helper reports per-root reflink support on first connect (and refreshes on reconnect); qui only dispatches `tree.reflink` jobs for roots whose underlying filesystem supports it, and falls back to hardlinks where it doesn't.
- **Dirscan.** Scheduled or webhook-triggered directory walks build a FileID index of files on the seedbox, used by cross-seed to identify match candidates without round-tripping qBittorrent's API for every file. NDJSON streaming over the stdio channel keeps memory bounded on both sides for trees with 10k+ files.
- **Orphanscan.** Periodic walks of save directories surface files no torrent claims, with the safety layer described in §6: an ignore-list of well-known sensitive paths (`.ssh`, `.config`, `.gnupg`, etc.), and a per-root acknowledgement requirement when the configured root is broader than qBittorrent's known save paths. The helper's audit log records every destructive op; qui's dispatcher log correlates one-to-one by `requestID`.
- **Missing-files automation condition.** Automation rules can trigger on actual filesystem state — *"pause torrents whose files have gone missing on disk"* — rather than relying on qBittorrent's reported state, which can be stale after manual file moves or external cleanup. Batch-friendly: one `fs.stat` command covers an entire torrent's file list.
- **Free-space automation condition — path-based.** Automation rules can read disk space at a specific path on the seedbox via `fs.statfs`, not just qBittorrent's reported value. Useful when the qBittorrent save path differs from the partition the user actually wants to monitor (e.g. ZFS dataset quotas, separate cache vs. archive volumes).
- **Hardlink-scope automation condition.** Rules can branch on whether a torrent's data is hardlinked to files outside qBittorrent's managed directory — *"only delete this torrent if its files are unique"* / *"only act on torrents whose data is shared with my media library"*. The helper builds a per-instance hardlink index (`fs.lstat` + `fs.fileid`) across all torrents on each automation cycle, cached for 2 min. High-volume but cached; the cache invalidates on torrent set change.
- **Managed delete cleanup.** When an automation runs a `deleteWithFiles` action, qBittorrent removes the files but leaves empty parent directories behind. qui's post-action cleanup walks up the parent chain (`fs.stat` + `fs.remove`) pruning empty dirs until it hits a configured "managed delete base dir". The helper surface is small (a handful of ops per delete) but destructive — the audit log captures every directory removal. Lives in `internal/qbittorrent/delete_cleanup.go`, triggered by automation delete actions when `managed_delete_enabled` and base dirs are configured.

These eight features are everything the helper enables. The complete operation-level inventory lives in §2; this section is the user-visible mapping of "what becomes possible once you deploy the helper."

**Success criteria.** A user adds SSH credentials to an instance in qui's UI, clicks "Deploy helper", and sees every feature listed above work over the wire with the same UX they have today on a co-located host. No inbound port required on the seedbox. No bearer token to copy across machines. Existing local-filesystem deployments see zero behavior change. Re-deploying after an upgrade, rotating credentials, or revoking access is straightforward.

**Non-goals.**
- Not a general-purpose RPC. Helper commands are scoped exactly to qui's filesystem features.
- The helper does not execute arbitrary user scripts or proxy qBittorrent's WebUI. (qui-driven external programs remain on qui's host.)
- v1 does not support one helper serving multiple qui installs. SSH credentials and helper deployment are per-instance.
- No agent-listens mode (no inbound port on the seedbox). Communication is SSH-only.
- No NAT traversal needed: SSH is universal on every seedbox.
- No helper-side scheduling. The helper executes commands qui dispatches; qui owns scheduling.
- The helper does not manage qBittorrent itself. It is purely a filesystem broker.
- Not premium-gated.

## 2. Filesystem Feature Inventory

| Feature | Op kind | Volume | Latency | Code locations |
|---|---|---|---|---|
| Cross-seed inject — hardlink tree | `MkdirAll`, `Lstat`, `Link`, `Remove`, `SameFilesystem` | 1–~100 ops/match | User-facing, sub-second per call | `pkg/hardlinktree/create.go`, `internal/services/dirscan/inject.go` (`createLinkTree`) |
| Cross-seed inject — reflink tree | `MkdirAll`, `Lstat`, platform CoW clone, `Remove` | 1–~100 ops/match | User-facing, sub-second per call | `pkg/reflinktree/reflink.go`, `pkg/reflinktree/reflink_{linux,darwin,windows}.go` |
| Cross-seed link-tree teardown | `Remove` (per file), empty-dir cleanup | tens to ~100 | Best-effort cleanup | `pkg/hardlinktree/create.go` (`Rollback`), `pkg/reflinktree/reflink.go` (`Rollback`), `internal/services/dirscan/inject.go` (`rollbackLinkTree`) |
| Dirscan — directory walk with FileID | `WalkDir`, `Lstat`, FileID extraction | 10k+ files per scan tree | Background scheduler (manual/webhook trigger also) | `internal/services/dirscan/scanner.go`, `pkg/hardlink/{fileid,linkcount,linkinfo}_{unix,windows}.go` |
| Dirscan — FileID index build | `WalkDir`, FileID extraction, hardlink count | very high (one full sweep per save path per indexer cycle) | Background | `internal/services/dirscan/fileid_index.go` |
| Orphanscan — walk + classification | `WalkDir`, `Lstat`, FileID for dedup | very high | Hourly background | `internal/services/orphanscan/walker.go` |
| Orphanscan — destructive cleanup | `Lstat`, `Remove`, `RemoveAll`, ignore-list & TFM re-check | tens per run typical | Background, destructive | `internal/services/orphanscan/delete.go` |
| Automations — missing-files condition | `Stat` per torrent file | hundreds per cycle (~5s cycles) | Latency-sensitive | `internal/services/automations/missing_files.go` |
| Automations — free-space condition | `Statfs` (Unix) / `GetDiskFreeSpaceEx` (Windows) | 1 per evaluation per source | Latency-sensitive | `internal/services/automations/free_space.go`, `free_space_windows.go` |
| Automations — hardlink-scope condition | `Lstat` + FileID extraction across all torrents | 10k+ files per build, cached 2 min | Background per evaluation cycle | `internal/services/automations/hardlink_index.go` (`buildHardlinkIndex`) |
| Automations — managed delete cleanup | `Stat` + `Remove` on parent dirs (empty-only) | tens per delete action | Best-effort; destructive | `internal/qbittorrent/delete_cleanup.go` (`cleanupManagedDeleteTargets`, `pruneEmptyManagedDeleteDir`) |
| Same-filesystem precondition | `Stat` ×2 + dev-id compare | 1 per inject | User-facing | `pkg/fsutil/samefs.go`, `samefs_{unix,windows}.go` |

This is the complete RPC surface. Anything outside this table stays local to the qui host (trackericons cache, backups DataDir, externalprograms exec, license files, settings).

## 3. Architecture Overview

```
+--------------------------------+                +---------------------------------+
|   qui host                     |                |   seedbox / VPS                 |
|                                |                |                                 |
|  +--------------------------+  |                |  +---------------------------+  |
|  |  services/dirscan        |  |                |  |  qBittorrent              |  |
|  |  services/orphanscan     |  |                |  +-----------+---------------+  |
|  |  services/automations    |  |                |              | local FS         |
|  |  services/crossseed      |  |                |              v                  |
|  +-----------+--------------+  |                |  +---------------------------+  |
|              |                 |                |  |  ~/data, ~/seed, ...      |  |
|              v                 |                |  +-----------+---------------+  |
|  +--------------------------+  |                |              ^                  |
|  |  internal/fsops          |  |                |              | os.* via         |
|  |  ┌──────────┐┌─────────┐ |  |                |              | openat2(BENEATH) |
|  |  │ Local    ││ Remote  │ |  |                |              |                  |
|  |  └──────────┘└────┬────┘ |  |                |  +-----------+---------------+  |
|  +-------------------+------+  |                |  |  qui-helper serve --stdio |  |
|              |                 |                |  |  (one persistent process) |  |
|              v                 |                |  |  pkg/fsexec primitives    |  |
|  +--------------------------+  |                |  |  audit log on disk        |  |
|  |  internal/sshpool        |  |                |  +-----------+---------------+  |
|  |  *ssh.Client per inst    |  |                |              ^                  |
|  |  one persistent Session  |  |                |              | NDJSON           |
|  |  per instance            +-----SSH stdio-----+              | stdin/stdout     |
|  |  cmds in / results out   |  |                |                                 |
|  +--------------------------+  |                |              |                  |
+--------------------------------+                +---------------------------------+
                                                       (helper binary; deployed via scp)
```

**Lifecycle.**
- qui boot: load instances. For each instance with SSH credentials configured and a deployed helper, the in-memory `sshpool` lazily establishes the SSH connection on first use. No upfront dial.
- First use of any FS op for an instance: `sshpool.GetClient(instanceID)` → opens `*ssh.Client` (TCP+TLS+SSH handshake) → opens one `ssh.Session` → starts `qui-helper serve --stdio --root <root1> --root <root2>` over that session. The helper writes a startup banner with version + capabilities + reflink-supported roots to stdout. qui caches this on the instance record.
- Steady-state op dispatch: qui writes a Command envelope (NDJSON) to the session's stdin; the helper executes; the helper writes a Result envelope (NDJSON) to stdout; qui's reader correlates by `requestID` and unblocks the waiting `fsops.Remote` caller.
- Liveness: the SSH connection itself is the heartbeat. If the connection drops, qui's reader gets EOF/error; in-flight ops are failed with `connection_lost`; qui reconnects on the next call (lazy reconnection).
- TCP keepalive on the SSH connection (`ClientConfig.ServerAliveInterval` equivalent) prevents idle drops by stateful firewalls.
- Shutdown (qui): close the helper's stdin → helper exits cooperatively → SSH session ends. No remote state to clean up.
- Shutdown (helper): SIGTERM (e.g. seedbox reboot) → helper finishes its current op or rolls back atomically, exits → qui's session.Wait() returns; in-flight ops fail with `connection_lost`.

The persistent-helper choice is what makes scale tractable. A short-lived `qui-helper` per call would fork+exec on every op — at high-frequency automation cycles (missing-files every 5s with hundreds of stats) this dominates wall-clock. With one helper per instance, fork cost is paid once at first connect; subsequent ops are dispatcher-cheap.

## 4. Transport, Framing & Streaming

**Decision: SSH (golang.org/x/crypto/ssh) as the transport. NDJSON as the framing on both stdin and stdout. One persistent SSH connection per instance, one persistent helper session within it.**

Justification:
- **SSH** is universally available on every seedbox, already the channel users use to administer their boxes, and supports everything we need: byte-level stdio streaming, multiplexing within a single TCP connection (via `ssh.Session`), keepalives, signal forwarding for cancellation. No new public ports, no firewall rules.
- **Pure-Go `golang.org/x/crypto/ssh`** rather than shelling out to the system `ssh` binary. Everything is in-process, qui can manage the connection lifecycle directly, no environment-variable-passing-through-shell escaping, no ControlMaster socket management on disk. Same dep qui already has (it pulls in `golang.org/x/crypto`).
- **NDJSON over stdio** is the same framing the daemon design uses over HTTPS. Each line on stdin is one Command envelope; each line on stdout is one Result frame (or a streamed-result frame for walks). Implementation is `json.Encoder` per line on each side and `bufio.Scanner` + `json.Unmarshal` on the reader. No new dependency.
- **Persistent helper** rather than fork-per-call. Eliminates ~5–15 ms of Go runtime startup + exec overhead per op. At the workloads in §13, this matters.

10k-file walk back-of-the-envelope: ~250 bytes/line × 10k = 2.5 MB streamed; ~20 ms at gigabit. The bottleneck is `WalkDir` itself (filesystem-bound), not the SSH stream.

**Compression.** SSH does not transparently compress payloads in `golang.org/x/crypto/ssh`. We don't need it: NDJSON over a typical seedbox link compresses 3–5× but our workloads are FS-bound, not network-bound. If a future workload becomes network-bound we can wrap the stdio stream in `compress/flate` on both sides, but defer.

**Concurrency on a single connection.** A single `*ssh.Client` supports many `ssh.Session`s concurrently. We use exactly one session for the long-running helper. Concurrent op dispatch is multiplexed at the **application layer** via NDJSON request IDs (qui writes commands to stdin in any order; the helper handles them with a goroutine pool and writes results to stdout tagged with their requestID). Same pattern as the daemon's dispatcher; just hosted within the helper process.

**Request size bounds.** Stdin and stdout don't have a single "request body" — they are continuous streams. Per-command line-length cap: 16 MiB. Streamed result frames (walk entries) are individually bounded; the stream as a whole is not.

## 5. Auth & Deployment

**Decision: SSH credentials per instance (key or password). Stored encrypted at rest in qui exactly the way qBittorrent passwords already are. Helper binary deployed via SCP from qui's UI with one click. No separate pairing flow, no bearer token, no TLS pinning.**

Why this shape rather than a daemon's bearer-and-pairing-string dance:
- The user already understands SSH. They likely already have an SSH key authorized on the seedbox; if not, they know how to add one.
- SSH credentials are something qui can use to deploy the helper itself (no manual scp step). One UI click replaces the daemon design's "paste this string on the seedbox" step.
- TLS pinning, AES-GCM bearer encryption, pairing tokens — all unnecessary. SSH host key verification + standard SSH auth (key or password) is the trust model.
- The helper binary is the only credential-bearing artifact. Its presence on the seedbox proves qui deployed it.

**SSH credential model.** Per instance, the user provides:
- **Host** (`seedbox.example.com:22` — port optional, defaults 22).
- **Username** on the seedbox.
- **Authentication** (one of):
  - **Private key** (paste OpenSSH-format key; passphrase optional but recommended).
  - **Password** (for boxes that don't support keys; not recommended but supported).
- **Known-hosts entry** captured on first connect: qui fetches the host's public key via SSH banner, displays it to the user with fingerprint, asks them to confirm. Stored on the instance record and used for verification on every subsequent connect (same TOFU model SSH uses by default).

All credential material is encrypted at rest using `sessionSecret` (HMAC-SHA256-keyed AES-GCM, same pattern as `internal/models/instance.go`'s `encrypt`/`decrypt` helpers).

**Deploy helper flow.** With SSH credentials saved, the user clicks **"Deploy helper"**:
1. qui opens an SSH connection.
2. qui detects the remote architecture: `ssh seedbox uname -m && uname -s` → maps to one of the cross-compiled helper binaries embedded in qui (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64).
3. qui SCPs the matching binary to `~/.config/qui-helper/qui-helper` (mode 0700). XDG path; no root needed.
4. qui executes `~/.config/qui-helper/qui-helper version --json` over the SSH connection to confirm the deployment.
5. qui parses the version output (semver + capability list + reflink-supported roots) and stores it on the instance record.
6. UI flips to "Deployed (vX.Y.Z, 13 capabilities, 2 reflink-capable roots)".

If the user later upgrades qui and the bundled helper version is newer than what's on the seedbox, qui auto-redeploys on the next op. The user never sees a "your helper is outdated" prompt unless the auto-redeploy fails (e.g. SSH credentials no longer work).

**Allowed roots.** Set on the instance form alongside SSH credentials. Hard-bound on the helper: the helper rejects any command whose target paths aren't under one of the allowed roots. Even if qui is fully compromised, blast radius is "the directories the seedbox operator allowed". qui passes allowed roots as `--root` flags when spawning `qui-helper serve --stdio --root /home/user/data --root /home/user/seed`.

**Credential rotation.** User updates the SSH key/password in the instance form → next connect uses the new credentials. No helper-side rotation needed (the helper doesn't have its own secret; its authority is entirely "qui can SSH in").

**Credential revocation.** User clears the SSH credentials in the form → all FS ops for that instance fail with `no_credentials`. Optional cleanup: a "Remove helper" button that SSHes in (if creds still work) and deletes `~/.config/qui-helper/`. If credentials are gone, the helper binary stays orphaned on the seedbox until the user removes it manually — harmless without an SSH connection in qui to drive it.

## 6. Path Safety

The path-safety model is the security backbone. SSH credentials get stolen; allowed-roots policy must hold even when authentication fails.

**Helper-side enforcement.** Identical to the daemon design; same `pkg/fsexec` primitives:
- All incoming paths are required to be absolute, `filepath.Clean`-ed, and rooted under one of the allowed roots passed at startup.
- Linux: every open uses `os.Root` (Go 1.24+) wrapping the allowed-roots directory, which uses `openat2(RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS)` under the hood. Mature in Go 1.26.
- macOS / Windows: `os.Root` works with a userspace-resolution fallback. We additionally `lstat` and reject any symlink encountered during traversal of a request path. README notes Linux as the recommended OS for full kernel-enforced isolation.
- Destructive ops (`fs.remove`, `fs.removeall`, `tree.remove`) refuse to operate on the allowed-root itself; the resolved path must be a strict descendant.
- `O_NOFOLLOW` on every direct open. Symlinks within the allowed root are not traversed.
- `..` rejected at command validation time, before resolution.
- Device-ID guard: on startup the helper records `dev` of each allowed root and rejects any op whose resolved path is no longer on that device. Protects against an allowed root being unmounted while the helper runs (deletes would otherwise land on the bare mountpoint, which is on a different filesystem). Cheap to implement, real value; included.

**Audit log.** A separate `audit.log` (configurable path, in-process size-based rotation) records every `fs.remove`, `fs.removeall`, `tree.remove`. JSON lines: `{ts, op, path, request_id, qui_session_id, outcome, error}` (`error` populated only on `outcome: "failure"`). Lives at `~/.local/state/qui-helper/audit.log` on the seedbox by default. Survives credential revocation; the operator's accountability trail. Note: `request_id` is generated qui-side and propagated through the command envelope, so qui's logs and the helper's audit log correlate one-to-one.

**TOCTOU contract.** `pkg/fsexec.ResolveSafe(rootName, requestPath)` returns an `*os.Root` handle to the root plus a relative resolved path. Every subsequent op against that resolved path uses the **same handle** via `*at` syscalls (or `os.Root` methods that wrap them on Go 1.24+). Concretely:
- The walker calls `safeRoot.Lstat(rel)` and `safeRoot.OpenInRoot(rel)` rather than re-resolving from absolute paths.
- `pkg/fsexec.RemoveAt(safeRoot, rel)` is the only way the helper removes anything; it never shells out to `os.Remove` with an absolute path.
- A single op holds one handle for the duration of its execution. The kernel guarantees the resolved file referred to by the handle won't be substituted by an attacker mid-op (no symlink swap, no parent-dir rename escape).
- For batched ops (`StatBatch`, `LstatBatch`), the same root handle is reused across all paths in the batch.

This is the single most important contract in `pkg/fsexec` and is the property the path-safety property tests verify.

**Rate limiting.** Lives on **qui's side**; qui throttles the rate at which it dispatches commands to the helper. The helper has no inbound listener, so there is no "auth failure rate limit" — a stranger can't reach the helper without already having SSH credentials, at which point they have shell on the seedbox and the helper is moot.

**Destructive-op scope safety.** Same orphanscan-side guards as the daemon design (§6): default ignore-list of well-known sensitive paths (`.ssh`, `.gnupg`, `.config`, `.local`, `.cache`, dotfiles), broad-root acknowledgement when orphanscan's target root contains qBittorrent's save paths AND non-save-path content. This logic lives qui-side, not in `pkg/fsexec` — same as the daemon design — because the policy is service-specific.

## 7. Protocol Spec

The protocol is symmetric NDJSON over the helper's stdin and stdout. There is no HTTP. Versioning is in the helper binary's reported version + capability list (§12). All payload types are shared between qui and the helper through `pkg/agent/proto`.

### 7.1 Channels

Three streams, all on the same `ssh.Session`:

| Stream | Direction | Format | Purpose |
|---|---|---|---|
| stdin | qui → helper | NDJSON Commands | Op dispatches, cancellations |
| stdout | helper → qui | NDJSON Results | Op results, walk-stream frames, info banners |
| stderr | helper → qui | structured zerolog JSON | Helper-side logs, errors, audit annotations |

The stderr stream is consumed by qui's logger and surfaced into qui's structured-log pipeline so support can correlate qui-side requestIDs with helper-side log entries.

### 7.2 Command and Result envelope

```go
package proto

type Command struct {
    RequestID string          `json:"requestID"`           // UUID, qui-generated
    Op        string          `json:"op"`                  // "fs.stat", "tree.hardlink", "control.cancel", ...
    Args      json.RawMessage `json:"args"`                // op-specific payload (StatRequest, TreeCreateRequest, ...)
    Deadline  string          `json:"deadline,omitempty"`  // RFC3339; helper aborts if exceeded
}
// Whether an op streams is determined by Op alone via a static map shared between qui and helper.
// Streaming ops in v1: fs.walk. Both sides know that fs.walk yields multiple Result frames before Done.

type Result struct {
    RequestID string          `json:"requestID"`
    OK        bool            `json:"ok"`
    Code      string          `json:"code,omitempty"`     // "path_not_allowed", "cancelled", ...
    Error     string          `json:"error,omitempty"`
    Payload   json.RawMessage `json:"payload,omitempty"`  // op-specific response (StatResponse, TreeCreateResponse, ...)
    Done      bool            `json:"done,omitempty"`     // last frame for streamed ops
    Frame     int             `json:"frame,omitempty"`    // monotonic per-requestID frame counter (streamed ops only)
}
```

For **non-streaming** ops, the helper writes a single `Result` line to stdout with `Done: true`. qui's reader correlates by `requestID` and forwards `Result.Payload` to the waiting `fsops.Remote` caller.

For **streaming** ops (`fs.walk`), the helper writes many `Result` lines tagged with the same `requestID`, each containing one `WalkEntry` in `Result.Payload`. The final line carries `Done: true` (and optionally `Code: "cancelled"` or an error). qui's reader pushes each frame onto the channel returned by `backend.WalkDir(...)`, closing it on the Done frame. The session stays open across the stream; no separate stream channel.

### 7.3 Why the stdio multiplex model fits

A naïve approach would have qui spawn one `ssh.Session` per op (or one `qui-helper` process per op). Both impose ~5–15 ms of startup overhead per op which dominates at the high-frequency workloads in §13. The persistent multiplex approach:

- Maps cleanly onto Go's blocking-call interface: `fsops.Remote.Stat(ctx, path)` writes a Command envelope with a fresh requestID, registers a result channel keyed by that requestID, blocks until the channel fires (or `ctx.Done`).
- Concurrent ops on the same instance multiplex over the single stdio channel. A 4-way concurrent dispatch is no more expensive than a serial one; the helper handles them with goroutines internally.
- `requestID` provides idempotency tokens for free: if qui retries (e.g. after reconnect), the helper's last-N-requestIDs cache rejects duplicates.
- Stream framing is the same NDJSON pattern as the daemon's `/result/{id}` POST body — only the *direction* of stream production differs (helper writes lines on stdout vs. POSTing them).

**TreePlan atomicity is preserved.** The `tree.hardlink` op carries the entire `TreePlan` as `Command.Args`; the helper executes `hardlinktree.Create(plan)` (or `reflinktree.Create(plan)`) in one go and rolls back on partial failure. SSH disconnects during the op kill the helper process before the Result is written; qui's reader sees EOF, marks the requestID as `connection_lost`. The on-disk state is whatever `hardlinktree.Create`'s atomic-with-rollback semantics produced — same atomicity guarantees as today's local code.

### 7.4 Op-specific payload types

The op payloads are unchanged from the daemon design — the operations themselves haven't moved, only the dispatch shape. Shared package `pkg/agent/proto`:

```go
package proto

// All paths are absolute. The helper rejects any path not under an allowed root.

// Lifecycle messages (sent on stdout as info banners, not embedded in Command/Result).

type HelloBanner struct {
    HelperVersion string   `json:"helperVersion"`
    ProtoVersion  string   `json:"protoVersion"` // "1"
    Capabilities  []string `json:"capabilities"`
    AllowedRoots  []string `json:"allowedRoots"`
    ReflinkRoots  []string `json:"reflinkRoots"`  // subset of AllowedRoots whose FS supports CoW reflinks
    Platform      string   `json:"platform"`     // "linux", "darwin", "windows"
    Hostname      string   `json:"hostname"`
    PID           int      `json:"pid"`
    StartedAt     string   `json:"startedAt"`    // RFC3339
}
// Emitted as the first stdout line on session startup. qui parses it before sending any commands.

// Op payloads embedded in Command.Args / Result.Payload below.

type StatRequest  struct { Paths []string `json:"paths"` }
type StatEntry struct {
    Path    string `json:"path"`
    Exists  bool   `json:"exists"`
    Size    int64  `json:"size,omitempty"`
    ModTime string `json:"modTime,omitempty"` // RFC3339Nano
    IsDir   bool   `json:"isDir,omitempty"`
    Mode    uint32 `json:"mode,omitempty"`
    Err     string `json:"err,omitempty"` // "not_found", "permission", etc.
}
type StatResponse struct { Entries []StatEntry `json:"entries"` }

type LstatRequest struct {
    Paths      []string `json:"paths"`
    WantFileID bool     `json:"wantFileID,omitempty"`
    WantNlinks bool     `json:"wantNlinks,omitempty"`
}
type LstatEntry struct {
    StatEntry
    IsSymlink bool   `json:"isSymlink,omitempty"`
    FileID    []byte `json:"fileID,omitempty"` // hardlink.FileID.Bytes()
    Nlinks    uint64 `json:"nlinks,omitempty"`
}

type WalkRequest struct {
    Root           string   `json:"root"`
    SkipHidden     bool     `json:"skipHidden,omitempty"`
    IgnoreDirNames []string `json:"ignoreDirNames,omitempty"`
    IgnorePaths    []string `json:"ignorePaths,omitempty"`
    WantFileID     bool     `json:"wantFileID,omitempty"`
    WantNlinks     bool     `json:"wantNlinks,omitempty"`
    MaxEntries     int      `json:"maxEntries,omitempty"` // 0 = unlimited
}
// Streamed: multiple Result frames carrying WalkEntry payloads; final frame has Done:true.
type WalkEntry struct {
    Path      string `json:"path,omitempty"`
    RelPath   string `json:"relPath,omitempty"`
    IsDir     bool   `json:"isDir,omitempty"`
    IsSymlink bool   `json:"isSymlink,omitempty"`
    Size      int64  `json:"size,omitempty"`
    ModTime   string `json:"modTime,omitempty"`
    Mode      uint32 `json:"mode,omitempty"`
    FileID    []byte `json:"fileID,omitempty"`
    Nlinks    uint64 `json:"nlinks,omitempty"`
    Err       string `json:"err,omitempty"`
    Truncated bool   `json:"truncated,omitempty"` // last frame if MaxEntries hit
}

type StatfsRequest  struct{ Path string `json:"path"` }
type StatfsResponse struct {
    BytesAvailable int64  `json:"bytesAvailable"`
    BytesTotal     int64  `json:"bytesTotal"`
    Filesystem     string `json:"filesystem,omitempty"` // best-effort
}

type ReadDirRequest struct {
    Path       string `json:"path"`
    MaxEntries int    `json:"maxEntries,omitempty"` // 0 = no cap (subject to per-op cap)
}
type DirEntry struct {
    Name      string `json:"name"`
    IsDir     bool   `json:"isDir,omitempty"`
    IsSymlink bool   `json:"isSymlink,omitempty"`
    Size      int64  `json:"size,omitempty"`
    ModTime   string `json:"modTime,omitempty"`
    Mode      uint32 `json:"mode,omitempty"`
}
type ReadDirResponse struct {
    Entries   []DirEntry `json:"entries"`
    Truncated bool       `json:"truncated,omitempty"`
}

type SameFSRequest  struct { Path1, Path2 string }
type SameFSResponse struct { Same bool `json:"same"` }

type MkdirRequest struct {
    Path string `json:"path"`
    Perm uint32 `json:"perm"`
}

type RemoveRequest struct {
    Path        string   `json:"path"`
    Recursive   bool     `json:"recursive,omitempty"`
    IgnorePaths []string `json:"ignorePaths,omitempty"` // server-side ignore list (orphanscan)
}
type RemoveResponse struct {
    Removed      bool   `json:"removed"`
    Disposition  string `json:"disposition,omitempty"` // "deleted" | "skipped_missing" | "skipped_ignored"
    RemovedBytes int64  `json:"removedBytes,omitempty"`
}

// TreePlan IS the existing pkg/hardlinktree.TreePlan.
type TreeCreateRequest struct {
    Plan     hardlinktree.TreePlan `json:"plan"`
    Mode     string                `json:"mode"`               // "hardlink" or "reflink"
    SourceFS string                `json:"sourceFS,omitempty"`
}
type TreeCreateResponse struct {
    Created       int      `json:"created"`
    SkippedExists int      `json:"skippedExists"`
    RolledBack    bool     `json:"rolledBack"`
    Err           string   `json:"err,omitempty"`
    DiagFiles     []string `json:"diagFiles,omitempty"` // truncated debug, opt-in
}

type TreeRemoveRequest struct {
    Plan hardlinktree.TreePlan `json:"plan"`
}

// Control ops on the same stdin channel (Op = "control.cancel", "control.shutdown")

type CancelRequest struct {
    RequestIDs []string `json:"requestIDs"` // ops to abort
}

// Note: Command.RequestID is the canonical idempotency token for every op.
// It is not duplicated inside individual op-payload types.

// FileID indexing is performed via fs.walk with WantFileID:true. No separate proto type needed.
```

**Error model.** Two layers:
- **Stream errors** (helper crashed, SSH disconnected, malformed NDJSON): qui's reader marks all in-flight requestIDs as `connection_lost` and closes their result channels. Triggers reconnection on the next call.
- **Op errors**: the helper writes a `Result` with `OK: false, Code: "...", Error: "..."`. qui's dispatcher turns the result into a typed Go error for the waiting `fsops.Remote` caller. Stable codes: `path_not_allowed`, `path_not_found`, `permission_denied`, `cross_device`, `tree_partial_rollback`, `version_skew`, `request_too_large`, `internal`, `connection_lost`, `cancelled`, `deadline_exceeded`.

**Idempotency.** Where natural, ops are idempotent (`MkdirAll` is, `Stat` is, hardlink-create skips identical existing targets per `pkg/hardlinktree/create.go`). The `Command.RequestID` is the idempotency token end-to-end: the helper caches the last 1024 request IDs (and outcomes) for 5 minutes so duplicate dispatches don't double-act. After a reconnect, qui re-dispatches with **new** requestIDs (the previous ones are dead with the previous session); idempotency at the helper protects against reissues only within a single session.

**Per-op timeouts.** `Command.Deadline` is set qui-side per op (default 30s for one-shots, 30 min for trees/walks). The helper aborts and writes `Code: "deadline_exceeded"` if the deadline lapses mid-execution. qui's `fsops.Remote` callers also pass `context.Context` with their own deadlines; if the caller cancels first, qui sends a `control.cancel` Command on stdin (see §7.5).

### 7.5 Cancellation, Crash Recovery & Connection Lifecycle

Cancellation is what makes long-running ops cooperative with qui's request lifecycle. The protocol is built on three invariants.

**Invariant 1 — stdin is always writeable.**

There is exactly one persistent SSH session per instance. As long as it's alive, qui can write a `control.cancel` Command at any time. There is no "request channel exhausted" worst case — stdin is a stream, not a pool. Cancels arrive at the helper in O(network latency) — typically ~1 RTT, ~50ms.

**Invariant 2 — Cancels are normal commands.**

When a qui-side caller cancels its `context.Context`:
1. qui's dispatcher writes a `Command{Op: "control.cancel", Args: {RequestIDs: [requestID, ...]}}` to stdin.
2. The helper's command reader picks it up, looks up the matching `cancelFunc` for each requestID, and invokes it. The op's `context.Context` fires.
3. The op exits cooperatively (see Invariant 3) and writes a Result with `OK: false, Code: "cancelled"` (or for streamed ops, a final frame with `Done: true, Code: "cancelled"`).
4. qui's reader receives the result, unblocks the original caller with `context.Canceled`, removes the requestID from the pending map.

**Cancels are idempotent.** A cancel for an unknown or already-completed requestID is a no-op. Duplicate cancels are dropped silently. The helper signals cancel completion by writing the result frame, same code path as any other op completion.

**Invariant 3 — `pkg/fsexec` primitives are ctx-aware.**

Identical to the daemon design. Every `pkg/fsexec` primitive accepts a `context.Context` and checks it at well-defined yield points (between walker entries, between batch syscalls, between link/clone calls in tree ops, between top-level entries in `RemoveAll`). On cancel, the op exits cooperatively and the executor writes the appropriate Result frame.

**Latency bound.**
- Steady state (idle helper): cancel arrives in ~1 RTT (~50 ms), op aborts within a `pkg/fsexec` ctx-check, result emitted promptly. End-to-end: ~100 ms.
- The remaining tail comes from `pkg/fsexec`'s ctx-check granularity: the walker checks ctx between entries, so a cancel mid-walk lands within the time it takes to process one directory entry. For atomic ops like `tree.hardlink` mid-creation, the check is between each link; for a 1000-file plan, cancel falls into the rollback path within ~1ms.
- For long-running uncancellable ops (none exist; all `pkg/fsexec` primitives are ctx-aware), the deadline (`Command.Deadline`, default 30s/30min) is the upper bound.

**Crash recovery.**

| Failure | Detection | Recovery |
|---|---|---|
| Helper crashes (panic, OOM, SIGTERM from seedbox reboot) | qui's stdout reader gets EOF | All in-flight pending results closed with `connection_lost`; SSH session reaped; `sshpool` marks the connection invalid; next call re-establishes |
| SSH connection dropped (network blip, firewall idle-timeout, server restart) | qui's reader gets read error or write fails | Same as helper crash — connection invalidated, lazy reconnect on next call |
| qui crashes during an op | Helper's stdin reader gets EOF on next blocking read | Helper cancels all in-flight op contexts (triggering `pkg/fsexec` cooperative exits / rollbacks), waits up to **30s grace period** for ops to drain, then exits cleanly. Tree ops mid-creation roll back atomically within this window. On qui restart, a fresh `*ssh.Client` + `ssh.Session` is established. |
| qui restarts | All in-memory pending-results lost | Next call to `fsops.Remote` re-establishes from scratch. The helper had no qui-derived state — clean. |
| Helper restarts (reboot, manual kill) | qui's reader gets EOF | Same as helper crash. qui transparently reconnects on next call. |
| TCP keepalive failure | `*ssh.Client` reports error within `ServerAliveInterval × CountMax` | Connection invalidated; reconnect on next call. |

**Stdin-EOF shutdown sequence.** When the helper's stdin reader detects EOF (qui crashed, quit, or closed the session):
1. Helper cancels the root context shared by all in-flight ops. Each op's `pkg/fsexec` primitive sees `ctx.Done()` at its next yield point and exits cooperatively. Tree ops enter their rollback path.
2. Helper waits up to **30 seconds** for all in-flight ops to drain. This accommodates the worst case: a large tree rollback (removing partially-created hardlinks) needs filesystem time.
3. After all ops drain or the 30s grace period expires, the helper flushes the audit log and exits 0.
4. If an op has not exited after 30s, the helper logs it as `"forced_exit"` in the audit log and exits 1. This should not happen in practice — `pkg/fsexec` primitives check ctx between each syscall — but the hard bound prevents a hung op from keeping the helper alive indefinitely.

In every failure mode, qui's `fsops.Remote` callers receive `context.DeadlineExceeded` (if their context had a deadline) or `connection_lost` (if not). Service code handles those exactly the way it handles any FS error today.

**Connection lifecycle.**

The `*ssh.Client` per instance is the single source of truth for "is this instance reachable":
- **On first FS op for an instance:** open `*ssh.Client` (TCP + SSH handshake, ~50–200 ms), open `ssh.Session`, start `qui-helper serve --stdio --root … --root …`. Helper writes a `HelloBanner` to stdout on startup; qui parses it and caches version/capabilities/reflink-roots on the instance record.
- **Steady-state:** stdin/stdout multiplex commands and results. `ServerAliveInterval=15s` keepalive on the SSH connection prevents idle drops by stateful firewalls.
- **On read error / EOF / write error:** mark the `*ssh.Client` as dead, close all pending result channels with `connection_lost`. The next FS op for this instance re-establishes from scratch.
- **On graceful shutdown (qui SIGTERM):** close the helper's stdin → helper sees EOF → finishes current op cooperatively → exits → SSH session ends naturally.
- **On bad SSH credentials:** initial connect returns auth-error; qui surfaces "credentials invalid" on the instance card and stops trying until the user updates them.

**Reconnect backoff.** If reconnection fails (network down, SSH server unreachable), qui backs off exponentially (5s → 60s, jitter ±20%). UI shows "instance unreachable" until reconnect succeeds. Once reconnected, queued ops resume normally.

**Network instability impact.** A single SSH connection per instance means a network blip fails ALL in-flight ops for that instance simultaneously. With `ServerAliveInterval=15s` and `CountMax=3`, detection takes up to ~45s. During that window, missing-files checks, automations, and dirscan are silently blocked. Combined with reconnect backoff, worst-case total unavailability per blip is ~60s. For home-server-to-seedbox links, users should expect intermittent feature unavailability during network instability. This is inherent to the single-connection model and acceptable for the self-hosted use case.

### 7.6 Op input bounds

Per-op caps are enforced helper-side (helper rejects with `request_too_large` before processing) and mirrored by qui's `fsops.Remote` callers (refuse to submit beyond cap). Outer envelope is the 16 MiB per-line cap from §4; per-op inner caps are tighter:

| Op / field | Cap | Rationale |
|---|---|---|
| `StatRequest.Paths` | 1024 | Missing-files runs per-torrent; torrents have hundreds of files, not tens of thousands |
| `LstatRequest.Paths` | 1024 | Same |
| `WalkRequest.IgnorePaths` | 1024 | Orphanscan ignore list is small in practice |
| `WalkRequest.IgnoreDirNames` | 256 | Pattern names, not paths |
| `ReadDirRequest.MaxEntries` | 8192 | One-level directory reads are bounded by FS limits in practice |
| `RemoveRequest.IgnorePaths` | 1024 | Same as walk |
| `TreeCreateRequest.Plan.Files` | 10 000 | A single cross-seed match never exceeds this; if it does, split the plan |
| `WalkRequest.Root` length | 4 096 bytes | Linux PATH_MAX |
| Any single path in any field | 4 096 bytes | Linux PATH_MAX |

If a qui-side caller's batch exceeds a cap, the `fsops.Remote` adapter chunks the call automatically (e.g. `StatBatch(2048)` becomes two sequential `fs.stat` commands; results are merged before returning to the caller). Stage C's per-op tests include a chunking case for `Stat` and `Lstat`.

### 7.7 Content encoding

The SSH transport does not transparently compress payloads in `golang.org/x/crypto/ssh`. NDJSON is uncompressed on the wire by default.

In practice this is fine: our workloads are FS-bound, not network-bound. A 2.5 MB walk-stream uncompressed transmits in ~20 ms at gigabit. If a future workload demands compression we can wrap each direction in `compress/flate.Writer`/`flate.Reader` on top of stdio — but defer until measured.

## 8. fsops Abstraction in qui

This is where the design has the most leverage in the qui codebase, and it is **identical** to the daemon design. Today, services call `os.*`/`filepath.*`/`unix.*` directly across ~20 files. Stage B introduces the interface and refactors callsites. After Stage B, swapping in the Remote impl (whether helper-over-SSH or daemon-over-HTTPS) is a one-line change in the resolver.

**Package.** `internal/fsops`.

**Core interface.** Designed as a near-drop-in for the patterns already in use, not a generic VFS. We expose exactly the operations the inventory in §2 needs. `WalkDir` is exposed as a streaming channel rather than a callback because callbacks don't survive an RPC hop cleanly.

```go
package fsops

import (
    "context"
    "io/fs"
    "time"

    "github.com/autobrr/qui/pkg/hardlink"
    "github.com/autobrr/qui/pkg/hardlinktree"
)

type FileInfo struct {
    Path      string
    Size      int64
    ModTime   time.Time
    IsDir     bool
    IsSymlink bool
    Mode      fs.FileMode
}

type DirEntry struct {
    Name      string
    IsDir     bool
    IsSymlink bool
    Mode      fs.FileMode
}

type LstatInfo struct {
    FileInfo
    FileID hardlink.FileID
    Nlinks uint64
}

type WalkEntry struct {
    LstatInfo
    RelPath string
    Err     error
}

type WalkOptions struct {
    SkipHidden     bool
    IgnoreDirNames []string
    IgnorePaths    []string
    WantFileID     bool
    WantNlinks     bool
    MaxEntries     int
}

type StatfsResult struct {
    BytesAvailable int64
    BytesTotal     int64
}

type RemoveOptions struct {
    Recursive   bool
    IgnorePaths []string
    RequestID   string
}

type TreeCreateResult struct {
    Created       int
    SkippedExists int
    RolledBack    bool
}

type Backend interface {
    // Read
    Stat(ctx context.Context, path string) (*FileInfo, error)
    StatBatch(ctx context.Context, paths []string) ([]*FileInfo, []error, error)
    Lstat(ctx context.Context, path string) (*LstatInfo, error)
    LstatBatch(ctx context.Context, paths []string) ([]*LstatInfo, []error, error)
    ReadDir(ctx context.Context, path string, maxEntries int) ([]DirEntry, bool, error)
    WalkDir(ctx context.Context, root string, opts WalkOptions) (<-chan WalkEntry, error)
    Statfs(ctx context.Context, path string) (*StatfsResult, error)
    SameFilesystem(ctx context.Context, p1, p2 string) (bool, error)
    FileID(ctx context.Context, path string) (hardlink.FileID, uint64, error)

    // Write (mutating)
    MkdirAll(ctx context.Context, path string, perm fs.FileMode) error
    Remove(ctx context.Context, path string, opts RemoveOptions) error

    // High-level (atomic, server-orchestrated)
    HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)
    ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)
    RemoveTree(ctx context.Context, plan *hardlinktree.TreePlan) error

    // Capabilities
    SupportsReflink(ctx context.Context, path string) (bool, string, error)

    // Diagnostic
    Info(ctx context.Context) (*BackendInfo, error)
    HealthCheck(ctx context.Context) error
}

type BackendInfo struct {
    Kind         string
    HelperVersion string
    AllowedRoots []string
    ReflinkRoots []string
    Capabilities []string
}
```

**Two implementations.**

`internal/fsops/local`:
- A thin adapter (~150 lines of typed delegation) over `pkg/fsexec`. Identical to the daemon design.

`internal/fsops/remote`:
- Talks to the in-process **SSH pool** (`internal/sshpool/pool.go`), *not* directly to the helper. There is no outbound HTTP from `fsops.Remote`; the helper is on the other side of an `ssh.Session`'s stdio.
- Each method does: (1) serialize args to the matching `pkg/agent/proto` request type; (2) call `sshpool.Submit(ctx, instanceID, op, args)` which generates a `requestID`, writes a Command line to the helper's stdin, and registers a result channel; (3) block on the channel (or `ctx.Done`); (4) deserialize the response payload into the matching response type and return.
- `WalkDir` returns the result channel directly (typed `<-chan WalkEntry`); the pool reads stream frames from stdout straight onto it.
- `SupportsReflink(path)` and `Info(ctx)` are **cache reads** — they consult the `instance.helper_capabilities` and `instance.helper_reflink_roots` columns refreshed from the `HelloBanner` on each connect. The helper already advertised the answer at session startup; `fsops.Remote` just looks it up.

**SSH pool (`internal/sshpool/pool.go`).** New subsystem owned by qui:
```go
type Command struct {
    RequestID string
    Op        string
    Args      json.RawMessage
    Deadline  time.Time
}
type pendingResult struct {
    oneShot chan proto.Result
    stream  chan<- json.RawMessage
    cancel  context.CancelFunc
}
type instanceClient struct {
    sshClient *ssh.Client
    session   *ssh.Session
    stdin     io.WriteCloser
    stdout    io.Reader
    stderr    io.Reader
    pending   map[string]*pendingResult
    banner    proto.HelloBanner
    mu        sync.Mutex
}
type Pool struct {
    mu        sync.RWMutex
    clients   map[int]*instanceClient // keyed by instance ID
}
// IsStreamingOp(op string) bool is the single source of truth for whether an op streams.
func (p *Pool) GetClient(ctx context.Context, instanceID int) (*instanceClient, error)
func (p *Pool) Submit(ctx context.Context, instanceID int, op string, args json.RawMessage) (chan proto.Result, <-chan json.RawMessage, error)
func (p *Pool) Cancel(ctx context.Context, instanceID int, requestIDs []string) error
func (p *Pool) Disconnect(instanceID int) error
```

The `instanceClient` runs a goroutine per session that reads stdout line by line, parses each Result, and routes to the matching `pendingResult`. A separate goroutine reads stderr and forwards structured logs to qui's logger.

**Resolver.** New `internal/fsops/pool.go`:
- `type Pool struct { ... }` keyed by `instanceID`.
- `func (p *Pool) GetBackend(ctx, instanceID) (Backend, error)`.
- For instances with no SSH credentials configured, returns the singleton `LocalBackend` if `has_local_filesystem_access=true`, else a no-op backend that errors with `"filesystem access disabled for this instance"`.
- For instances with SSH credentials configured AND a deployed helper, returns a `RemoteBackend` bound to the SSH pool. Cheap; no per-instance state to cache here (the pool holds it).
- Health: derived from the `instanceClient`'s connection state and `last_activity_at` (updated on every successful op). If the connection is dead, the resolver returns the `RemoteBackend` but it surfaces `connection_lost` errors immediately on Submit until reconnect succeeds.

**Refactor surface (Stage B).** Identical to the daemon design — Stage B is invariant across implementation choice. Callsites that change from `os.Foo` / `filepath.WalkDir` to `backend.Foo`:
- `internal/services/dirscan/scanner.go` (entire walker — biggest refactor; `filepath.WalkDir` callback becomes channel-based `backend.WalkDir`).
- `internal/services/dirscan/fileid_index.go` (uses `WalkDir` with `WantFileID:true`).
- `internal/services/dirscan/inject.go` `createLinkTree` (calls `backend.HardlinkTree` / `backend.ReflinkTree`; same-FS check becomes `backend.SameFilesystem`).
- `internal/services/dirscan/inject.go` `rollbackLinkTree` → `backend.RemoveTree`.
- `internal/services/orphanscan/walker.go` (entire walker).
- `internal/services/orphanscan/delete.go` (`os.Remove`, `os.RemoveAll`, `os.Lstat` → `backend.Remove` with `RemoveOptions`).
- `internal/services/automations/missing_files.go` (`os.Stat` → `backend.Stat` or `StatBatch`).
- `internal/services/automations/free_space.go` (`unix.Statfs` → `backend.Statfs`). The `FreeSpaceSourceType` enum gains a third value `agentPath`.
- `internal/services/automations/hardlink_index.go` `buildHardlinkIndex` (`os.Lstat` + `pkg/hardlink.GetFileID` per torrent file → `backend.LstatBatch` with `WantFileID:true`).
- `internal/qbittorrent/delete_cleanup.go` `cleanupManagedDeleteTargets` and `pruneEmptyManagedDeleteDir` (`os.Stat` + `os.Remove` on parent dirs → `backend.Stat` + `backend.Remove`).
- `internal/services/crossseed/service.go` `FindMatchingBaseDir` (`os.MkdirAll` per candidate base dir + `fsutil.SameFilesystem` → `backend.MkdirAll` + `backend.SameFilesystem`).
- `pkg/fsutil/samefs.go` callsites → `backend.SameFilesystem`.

`pkg/hardlinktree`, `pkg/reflinktree`, `pkg/hardlink`, and `pkg/fsutil` stay where they are. They define the canonical `TreePlan` and the local execution semantics. The `Local` backend depends on them. The `Remote` backend (helper-side) re-uses the same packages.

## 9. Schema & Instance Model Changes

**Recommendation: keep `has_local_filesystem_access` on `instances` untouched, and add SSH credentials + helper metadata as columns on the existing `instances` table.** No new tables required — the helper has no persistent identity beyond "qui can SSH into this host and find a binary at this path."

Compared to the daemon design, this is dramatically simpler: no `agents` table, no `agent_pairings` table, no bearer hashes. SSH credentials are encrypted with the same `sessionSecret` qui already uses for qBittorrent passwords.

**New columns on `instances`.**

```sql
-- Migration 074_add_remote_helper.sql (sqlite), 075_add_remote_helper.sql (postgres)
ALTER TABLE instances ADD COLUMN ssh_host                  TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_port                  INTEGER NOT NULL DEFAULT 22;
ALTER TABLE instances ADD COLUMN ssh_username              TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_auth_type             TEXT NOT NULL DEFAULT '' CHECK (ssh_auth_type IN ('', 'key', 'password'));
ALTER TABLE instances ADD COLUMN ssh_key_encrypted         TEXT NOT NULL DEFAULT '';   -- AES-GCM ciphertext, OpenSSH-format private key
ALTER TABLE instances ADD COLUMN ssh_key_passphrase_encrypted TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_password_encrypted    TEXT NOT NULL DEFAULT '';   -- AES-GCM ciphertext (alternative to key)
ALTER TABLE instances ADD COLUMN ssh_host_key              TEXT NOT NULL DEFAULT '';   -- captured on first connect; verified on every reconnect
ALTER TABLE instances ADD COLUMN helper_path               TEXT NOT NULL DEFAULT '~/.config/qui-helper/qui-helper';
ALTER TABLE instances ADD COLUMN helper_version            TEXT NOT NULL DEFAULT '';   -- last-observed semver from HelloBanner
ALTER TABLE instances ADD COLUMN helper_capabilities       TEXT NOT NULL DEFAULT '[]'; -- JSON array
ALTER TABLE instances ADD COLUMN helper_allowed_roots      TEXT NOT NULL DEFAULT '[]'; -- JSON array
ALTER TABLE instances ADD COLUMN helper_reflink_roots      TEXT NOT NULL DEFAULT '[]'; -- JSON array
ALTER TABLE instances ADD COLUMN helper_platform           TEXT NOT NULL DEFAULT '';   -- "linux", "darwin", "windows"
ALTER TABLE instances ADD COLUMN helper_deployed_at        DATETIME;
ALTER TABLE instances ADD COLUMN helper_last_activity_at   DATETIME;                   -- updated on every successful op
```

**No new tables.** The helper has no concept of "registration" — its presence on the seedbox is its identity. qui re-discovers it on each connect via the `HelloBanner` and refreshes the cached metadata.

**SSH key storage.** Private keys are stored encrypted with `sessionSecret` (AES-GCM, same pattern as `internal/models/instance.go`'s existing helpers). The passphrase, if provided, is stored separately and combined at connect-time. We do NOT store the decrypted key on disk; it lives in qui's process memory only after connection establishment.

**SSH host key (TOFU).** On first connect, qui captures the seedbox's SSH host public key (the `ssh.PublicKey` returned by the dial). qui shows the user the key fingerprint and asks them to confirm it before saving. On every subsequent connect, qui verifies the presented host key matches the saved one — if it doesn't, qui refuses to connect and surfaces "host key changed" in the UI (same model as `~/.ssh/known_hosts`).

**Model files.**
- `internal/models/instance.go`: extend `Instance` struct with the new SSH/helper fields; extend `InstanceStore.Create`/`Update` to accept SSH params.
- `internal/models/ssh_credentials.go` (new): `SSHCredentials` value type, helper for encrypt/decrypt of key + password material.
- No new store types — everything fits on the existing `Instance`.

**Helper.** `internal/models/instances.HasFilesystemAccess(ctx, instance) (bool, FilesystemMode)` returns true if `has_local_filesystem_access || (ssh_host != '' AND helper_deployed_at IS NOT NULL)`, plus the mode (`"local" | "helper" | "none"`). All existing `HasLocalFilesystemAccess` callsites get migrated to this helper.

**Handler DTOs.**
- `POST /api/instances/{id}/ssh-test` — accepts SSH credentials; qui attempts to dial, returns the host key fingerprint and a success/error status. The user confirms the fingerprint before save.
- `POST /api/instances/{id}/helper/deploy` — assumes valid SSH creds saved; pushes the helper binary, runs `version --json`, returns the parsed banner. Updates the `helper_*` fields on the instance.
- `POST /api/instances/{id}/helper/redeploy` — re-pushes the binary (e.g. after a qui upgrade includes a newer helper).
- `DELETE /api/instances/{id}/helper` — disconnects + (best-effort) removes the binary from the seedbox + clears `helper_*` fields. SSH credentials remain.
- `DELETE /api/instances/{id}/ssh-credentials` — clears all SSH credential fields. Helper deployment is retained on the seedbox until manually removed; qui can no longer drive it.
- `GET /api/instances/{id}/helper` — returns `{deployedAt, lastActivityAt, helperVersion, capabilities, allowedRoots, reflinkRoots, platform}` for the instance card.

**Tests.** Three-way table test for filesystem mode (none / local / helper). SSH-credential test (paste valid creds → connect succeeds, host key captured). Deploy test (push binary → `HelloBanner` parsed → fields populated). Reconnect-after-host-key-change test (rejected with clear error).

## 10. Helper Binary

**Location.** `cmd/qui-helper/main.go`, sibling to `cmd/qui/`.

**Shared types.** `pkg/agent/proto` (the same package the daemon design used) — the proto types are shared regardless of transport. Both designs can co-exist in the codebase if needed; only one ships.

**Build target.** Separate make target `make helper`, separate Go build (`go build -o qui-helper ./cmd/qui-helper`). The helper binary's import graph is exactly:
- `cmd/qui-helper/...` (and `cmd/qui-helper/internal/...` subpackages)
- `pkg/agent/proto`
- `pkg/fsexec` (path safety + FS primitives, shared with qui's `Local` backend)
- `pkg/hardlinktree`, `pkg/reflinktree`, `pkg/hardlink`, `pkg/fsutil`

No SQLite, no embedded frontend, **no top-level `internal/` dependencies**. The CI import-graph guard (Stage A) enforces this. Single-digit-MB binary; qui's main binary unchanged in size for users who don't run the helper.

**Cross-compile matrix and distribution.** Helpers are cross-compiled for:
- linux/amd64
- linux/arm64
- darwin/amd64
- darwin/arm64

Windows/amd64 is a best-effort Stage D addition (see §18).

qui's release CI publishes each binary as a **GitHub release asset** under the qui repository, named with a stable pattern: `qui-helper_v{version}_{os}_{arch}{ext}` (e.g. `qui-helper_v1.5.0_linux_amd64`). qui's main binary does **not** embed the helper binaries — that would add ~30 MB of mostly-unused weight to every qui release and force users to upgrade qui to upgrade the helper.

What qui *does* embed is a small constants file (~300 bytes total per release) containing the SHA256 of each cross-compiled helper artifact. **Deployment is qui-driven**: qui downloads the correct helper binary from GitHub releases to its own host, verifies the SHA256 against the embedded constant, then SCPs the verified binary to the seedbox over the existing SSH connection. The seedbox never needs outbound HTTPS to GitHub — only qui does, and it already has internet access.

The deploy flow:
1. qui probes the remote architecture via the SSH session: `uname -m && uname -s` → maps to one of the cross-compiled helper binaries.
2. qui downloads the matching binary from `https://github.com/autobrr/qui/releases/download/v{version}/qui-helper_v{version}_{os}_{arch}` to a temp file on qui's host.
3. qui verifies the downloaded binary's SHA256 against the embedded constant. Rejects on mismatch.
4. qui SCPs the verified binary to `~/.config/qui-helper/qui-helper.new` on the seedbox (mode 0700).
5. qui runs `mv ~/.config/qui-helper/qui-helper.new ~/.config/qui-helper/qui-helper` atomically over SSH.
6. qui runs `~/.config/qui-helper/qui-helper version --json` to confirm the deployment.

Failure modes are clean and surfaced verbatim in the UI:
- **SHA256 mismatch** (corrupted download or asset tampering): "Downloaded binary failed checksum verification. Try redeploying."
- **GitHub release missing the asset** (build failure or asset deletion): "qui-helper v{version} for {arch} isn't published. File an issue."
- **Transient network error** (GitHub temporarily unreachable from qui's host): retried once with backoff inside the deploy handler; if it still fails, surface the underlying error.
- **SCP failure** (seedbox disk full, permission denied): surfaced verbatim.

**Updates** follow the same flow. When qui upgrades to a newer version, the next deploy or auto-redeploy downloads the matching helper asset from the new GitHub release, verifies it, and SCPs it to the seedbox. The user doesn't see anything beyond a status notification ("Helper auto-upgraded to v1.6.0").

**Subcommands.**
- `qui-helper serve --stdio --root /path1 --root /path2 [--audit-log /path/to/audit.log]`: the long-running mode. Reads NDJSON commands from stdin, executes via `pkg/fsexec` primitives, writes results to stdout. Logs structured JSON to stderr. Writes the destructive-op audit log to the configured path (default `~/.local/state/qui-helper/audit.log`).
- `qui-helper version --json`: prints a `HelloBanner` JSON to stdout and exits 0. Used by qui to probe an installed helper without starting a session.
- `qui-helper version`: human-readable version output. For users who SSH in to debug.

No `pair`, `unpair`, `serve` (without `--stdio`), or `status` subcommands — those concepts don't exist in this design.

**Allowed roots.** Set on the qui side (per instance) and passed as `--root` flags to `qui-helper serve --stdio`. The helper rejects any command whose target paths aren't under one of the allowed roots; same hard-bound enforcement as the daemon design. Adding/removing a root requires an instance-config change in qui + the next reconnect (qui re-spawns the helper with new flags).

**Config file.** None. The helper takes everything via flags and environment. Stateless. Tearing the helper down is `rm -rf ~/.config/qui-helper/` (binary) and `rm -rf ~/.local/state/qui-helper/` (audit log).

**Logging.**
- **Operational logs** (info/warn/error): structured zerolog JSON to stderr. qui's stderr reader forwards them into qui's logging pipeline so support diagnoses cross-instance issues from one log stream.
- **Audit log:** separate file at `~/.local/state/qui-helper/audit.log`. JSON lines per destructive op, in-process size-based rotation (default 50 MB, keep 5 archives). qui can fetch the file via SSH (`cat audit.log`) when support needs it.

**Why no Docker / systemd / launchd.** The helper has no supervisor needs. It runs as long as qui's SSH session is open and exits when qui closes stdin (or when SSH disconnects). There is no "should I start at boot" question — qui starts the helper on first FS op after qui boots. No PID file, no port, no socket.

**Why no Windows special-casing.** Windows/amd64 is a Stage D best-effort addition to the cross-compile matrix. SSH support on Windows requires the user to have an SSH server running (OpenSSH for Windows, Cygwin's sshd, etc.) — uncommon for Windows seedboxes. Supported if present, but not integration-tested.

## 11. Deployment / Onboarding Flow

End-to-end:

1. **In qui UI** (any browser): Instance → Edit → "Filesystem access" → "Remote helper" → form fields appear:
   - **SSH host** (e.g. `seedbox.example.com`)
   - **SSH port** (default 22)
   - **SSH username**
   - **Authentication**: select "Private key" (recommended) or "Password".
     - Private key: paste OpenSSH-format key (also a passphrase field if the key is passphrase-protected).
     - Password: text input (with hide/show toggle).
   - **Allowed roots** (one or more textboxes; defaults to `~/data` and `~/seed`).
2. User clicks **"Test connection"**. qui dials the SSH endpoint:
   - On success: shows the host key fingerprint with "First time connecting? Confirm this matches what you expect." and a Confirm button.
   - On failure: shows the SSH error verbatim ("auth failed", "host unreachable", etc.).
3. User confirms the host key. qui saves the SSH credentials + host key on the instance row (encrypted at rest).
4. Form shows a **"Deploy helper"** button.
5. User clicks Deploy:
   - qui dials the SSH connection (now with the saved host key as a verifier).
   - qui probes architecture via SSH: `uname -m && uname -s`.
   - qui downloads the matching helper binary from GitHub releases to a temp file on qui's host.
   - qui verifies the downloaded binary's SHA256 against the constant embedded in qui for the running version.
   - qui SCPs the verified binary to `~/.config/qui-helper/qui-helper` on the seedbox (mode 0700, atomic via temp + rename).
   - qui runs `~/.config/qui-helper/qui-helper version --json` over SSH and parses the `HelloBanner`.
   - qui updates `instances.helper_version`, `helper_capabilities`, `helper_reflink_roots`, etc.
   - UI flips to "Deployed (vX.Y.Z, 13 capabilities, 2 reflink-capable roots, 2 allowed roots)".
6. After successful deploy, the UI shows an **`authorized_keys` hardening snippet** with a copy button:
   ```
   command="/home/user/.config/qui-helper/qui-helper serve --stdio --root /home/user/data --root /home/user/seed",no-port-forwarding,no-X11-forwarding,no-agent-forwarding ssh-ed25519 AAAA...
   ```
   Accompanied by: *"Optional but recommended: add this to `~/.ssh/authorized_keys` on the seedbox to restrict this SSH key to helper-only access. This prevents the key from being used for general shell access if it is ever compromised."*
7. Done. The instance card now shows "Remote helper: deployed". The first time any qui service performs an FS op for this instance, the SSH connection is established lazily and the long-running helper is started.

Total user actions: fill form, confirm host key, click Deploy. No paste of pairing strings, no terminal session on the seedbox.

**Re-deploying.** If qui upgrades and the bundled helper version is newer, the next FS op for the instance triggers an auto-redeploy (qui sees version mismatch in the `HelloBanner`, SCPs the new binary, restarts the session). The user is notified in the instance card with a brief "Helper auto-upgraded to vX.Y.Z" notification.

**Removing the helper.** "Remove helper" button on the instance card → SSHes in (if creds still work), deletes `~/.config/qui-helper/`, clears the `helper_*` fields. SSH credentials remain saved unless the user also clicks "Remove SSH credentials" separately.

## 12. Versioning & Capability Negotiation

**Two layers.**

**Proto version** lives in the `HelloBanner.ProtoVersion` field. Bumping to "2" is a hard break: qui won't talk to a v2 helper unless qui itself ships v2 client code. v1 is forever.

**Capabilities** are additive. The helper advertises its capability list in the `HelloBanner` on session startup:
```
["fs.stat", "fs.lstat", "fs.readdir", "fs.walk", "fs.fileid", "fs.statfs", "fs.samefs",
 "fs.mkdir", "fs.remove", "fs.removeall",
 "tree.hardlink", "tree.reflink", "tree.remove",
 "control.cancel"]
```

`fs.fileid` is a flag, not an op — it means the helper can populate `WalkEntry.FileID` (and `LstatEntry.FileID`) when the request asks for it. Required by dirscan and orphanscan even though they don't dispatch a `fs.fileid` op directly.

`control.cancel` is a special meta-op (§7.5) and every v1 helper supports it.

qui persists the latest `helper_capabilities`, `helper_allowed_roots`, and `helper_reflink_roots` on the `instances` row and consults them before dispatching a job. Capabilities answer "can the helper perform this op at all?"; the root lists answer "is the specific path eligible for this op?". A static map of `feature → required_capabilities` lives in qui code:
- Cross-seed inject hardlink → `tree.hardlink`, `fs.samefs` (pre-flight check picks the link-tree base dir)
- Cross-seed inject reflink → `tree.reflink`, `fs.samefs`, **plus** the chosen base dir must be under one of `instance.helper_reflink_roots`
- Dirscan → `fs.walk`, `fs.fileid`, `fs.readdir`
- Orphanscan → `fs.walk`, `fs.fileid`, `fs.readdir`, `fs.remove`, `fs.removeall`
- Missing-files automation condition → `fs.stat`
- Free-space automation condition (path source) → `fs.statfs`
- Hardlink-scope automation condition → `fs.lstat`, `fs.fileid`
- Managed delete cleanup → `fs.stat`, `fs.remove`

If a required capability is missing when the user enables a feature: hard block in the UI with a specific message ("Your remote helper (v0.0.5) doesn't support `tree.reflink` (required for reflink cross-seed mode). Redeploy the helper to upgrade."). If the chosen base dir is not under `helper_reflink_roots`, the UI surfaces a different message ("This filesystem doesn't support reflinks; pick a different base directory or fall back to hardlink mode").

**Auto-upgrade.** Because qui ships embedded helpers, qui knows what version it bundles. On every connect, qui compares the deployed `helper_version` against the bundled version; if the bundled is newer, qui re-deploys before opening the long-running session. Users get auto-upgrades with no manual step (assuming SSH credentials still work).

**Connect-time gating.** qui rejects a `HelloBanner` with `protoVersion` not matching a supported value. This catches major-version skew before any work is dispatched.

**Semver.** `MAJOR` bumps the proto version (`v1` → `v2`). `MINOR` adds capabilities (additive). `PATCH` is fixes that don't change the wire. Capabilities never disappear within a major version.

## 13. Concurrency, Backpressure & Rate Limits

**qui side (authoritative for dispatch rate).**
- Per-instance command queue: a buffered channel that the SSH writer goroutine drains line-by-line. Default capacity 32. When full, `sshpool.Submit` blocks (or fails fast on `ctx.Done`).
- The pending-results map enforces a 5-min TTL on dispatched-but-undelivered ops. A connection drop closes all pending results immediately with `connection_lost`.
- Per-instance: pool tracks `inflight_count`. **qui is the authoritative throttle** — `max_inflight_per_instance = 32` prevents qui from overwhelming the seedbox FS. The helper has a secondary safety valve (see below), but qui's limit is what governs steady-state dispatch.
- Per-call timeouts via `context.Context` from the caller; `Command.Deadline` reflects the soonest deadline.
- Reconnect backoff after connection loss: exponential 5s → 60s with ±20% jitter.
- SSH-auth-failure backoff: 60s pause after each auth error; 3 sequential failures put the instance into "credentials invalid" state until the user updates them.

**Helper side.**
- One persistent process; concurrent commands handled via goroutines.
- Walker concurrency cap (default 4 concurrent walks).
- `max_inflight_ops` (default 32) is the helper's local safety valve. This is a defensive backstop, not the primary throttle — qui's dispatch limit should prevent this from firing in practice. If it does fire, the helper rejects with `request_too_large` on subsequent commands until the queue drains.
- Streamed result frames flush every N entries (default 256) and on any walker callback that takes > 50 ms.

**Cancellation.** Detailed in §7.5. Summary: cancels are normal commands on stdin (`Op: control.cancel`), propagate through `pkg/fsexec` via `context.Context`, and complete through the same Result-frame path as any other op. Idempotent on duplicate cancels.

**Request ID propagation.** Every `Command.RequestID` is a UUID generated on qui side. The helper's audit log records it; qui's logs record it. End-to-end correlation in support cases is free.

**Liveness.** No application-level heartbeat — the SSH connection itself is the heartbeat. TCP keepalive (`ServerAliveInterval=15s`, `ServerAliveCountMax=3`) ensures qui detects dead connections within ~45s.

## 14. Observability

**Helper.**
- Structured zerolog JSON output to **stderr**. Every helper log line is consumed by qui's stderr reader and forwarded into qui's main log pipeline. Operators see helper logs and qui logs in one place.
- Separate `audit.log` on the seedbox containing destructive ops only. JSON lines: `{ts, op, path, request_id, qui_session_id, outcome, error}` (`error` populated only on `outcome: "failure"`). Format mirrors §6's audit-log spec exactly.
- `qui-helper version --json` returns the same data on demand for support diagnostics.
- No Prometheus endpoint on the helper — qui-side metrics (see below) cover operational visibility. Adding a loopback metrics port that nobody scrapes is unnecessary complexity.

**qui.**
- Helper connection status is **derived from `instance.helper_last_activity_at`** plus the in-memory connection state. UI shows:
  - `Local` (using qui-host filesystem)
  - `Remote helper: connected (vX.Y.Z, last activity 2s ago, 4/32 inflight)`
  - `Remote helper: connecting…`
  - `Remote helper: connection lost (reconnecting in 30s)`
  - `Remote helper: SSH credentials invalid`
  - `Remote helper: not deployed`
- Background sweeper (`internal/sshpool/sweeper.go`) runs three independent tickers:
  - **Connection health** (every 30s): pings each active connection; on failure, marks dead and triggers reconnect.
  - **Pending-results TTL** (every 30s, threshold 5min): closes any in-memory `pendingResult` whose dispatch was more than 5 minutes ago. The corresponding `fsops.Remote` caller's channel is closed with `connection_lost`.
  - **Reconnect backoff scheduler** (every 5s): processes queued reconnect attempts.
  - All three live in one file with shared structured-log fields.
- `sshpool` emits structured logs per command: `instanceID`, `op`, `requestID`, `durationMS`, `outcome`. Prometheus counters on qui's existing `/metrics` endpoint: `qui_helper_dispatched_total{op,outcome}`, `qui_helper_inflight_jobs{instanceID}`, `qui_helper_queue_depth{instanceID}`, `qui_helper_connection_state{instanceID,state}`.
- The automations activity ledger already exists; helper jobs flow through it transparently because activity logging is at the service layer, above the backend interface.

## 15. Frontend Changes

**`web/src/components/instances/InstanceForm.tsx` (currently has a single `hasLocalFilesystemAccess` toggle near line 200).**

Replace the toggle with a `RadioGroup`:

```
Filesystem access
( ) None — qui will not touch the filesystem for this instance
( ) Local — qui runs on the same host as qBittorrent
( ) Remote helper — qui drives a qui-helper binary over SSH on the qBittorrent host
```

When "Remote helper" is selected, the form reveals SSH credential fields:

**SSH credentials section:**
- `SSH host` (text, required)
- `SSH port` (number, default 22)
- `SSH username` (text, required)
- `Authentication`: dropdown with options "Private key" (default) / "Password"
  - Private key: textarea for OpenSSH-format key + optional passphrase field
  - Password: password input
- `Allowed roots` (one or more rows; defaults `~/data`, `~/seed`); the user can add/remove rows.
- **"Test connection"** button. On click: posts to `POST /api/instances/{id}/ssh-test` with the entered credentials.
  - Success: shows the host key fingerprint with a Confirm button. User confirms before save.
  - Failure: shows the SSH error.

**Helper status (after SSH credentials are saved):**
- If helper is not deployed: **"Deploy helper"** button. On click: posts to `POST /api/instances/{id}/helper/deploy`. Shows progress ("Detecting architecture… Downloading binary… Verifying checksum… Pushing to seedbox… Probing version…"). Result: "Deployed (vX.Y.Z, 13 capabilities, 2 reflink-capable roots)" + an `authorized_keys` hardening snippet with copy button (see §11 step 6).
- If helper is deployed: read-only summary card with `helperVersion`, `platform`, `hostname`, `allowedRoots`, `reflinkRoots`, `capabilities`, `helperDeployedAt`, `helperLastActivityAt`. Plus three buttons:
  - **"Test connection"** — opens a short SSH session, runs a `diag.echo` op, reports round-trip time.
  - **"Redeploy"** — re-pushes the binary (for upgrade or manual re-install).
  - **"Remove helper"** — disconnects + cleans up the binary on the seedbox.
- **Capability-downgrade banner.** When `helper_capabilities` (refreshed on each connect) drops a capability the instance currently relies on, surface a red banner: "Reflink mode is enabled but your helper (v0.0.5) no longer reports `tree.reflink` support. Cross-seed inject is blocked until you redeploy the helper or disable reflink mode." Pre-flight checks in qui's services use the same capability list — they refuse to dispatch the job and return `version_skew` to the caller.

**TypeScript types (`web/src/types/index.ts`).** Extend `Instance` with SSH and helper fields:
```ts
type Instance = {
  // ... existing ...
  sshHost?: string
  sshPort?: number
  sshUsername?: string
  sshAuthType?: 'key' | 'password' | ''
  sshHostKey?: string
  helperPath?: string
  helperVersion?: string
  helperCapabilities?: string[]
  helperAllowedRoots?: string[]
  helperReflinkRoots?: string[]
  helperPlatform?: 'linux' | 'darwin' | 'windows'
  helperDeployedAt?: string
  helperLastActivityAt?: string
}
```
The SSH key, key passphrase, and password are write-only (never round-trip back from the server, same pattern as the qBit password).

**Capability hints in the form.** Same as before — when the helper's reported capabilities don't cover something the user has enabled (`tree.reflink` while `useReflinks: true`), surface an inline warning: "Reflink mode is enabled but your helper doesn't support `tree.reflink`. Redeploy the helper or disable reflink mode."

## 16. Security Model & Threat Model

**Trust boundary.** qui has full SSH access to the seedbox via the credentials the user configured. The helper has no independent authentication — its authority derives from being the binary qui chose to launch over its SSH session. The audit log on the seedbox records what the helper actually did; qui's logs record what qui asked for; the two should always match.

**Adversaries considered.**

*Stolen SSH credentials (off the seedbox).*
- The SSH credentials are on qui's host, encrypted at rest with `sessionSecret`. An attacker who can read qui's database AND `sessionSecret` can decrypt them.
- With the credentials, the attacker has **full shell access to the seedbox under the configured user** — same authority qui has. This is strictly more powerful than the daemon design's bearer token, which was scoped to the agent's RPCs.
- **The helper's allowed-roots policy does NOT protect against this case.** If the attacker has SSH credentials, they can simply not use the helper and instead `rm -rf ~/data` directly via shell.
- Mitigations:
  - Use a **dedicated low-privilege user** on the seedbox for qui's SSH access. Most users won't, but document the option.
  - Use **SSH key restrictions** (e.g. `command="/path/to/qui-helper serve --stdio"` in `authorized_keys`) to lock the SSH session to running only the helper. This is a real hardening option — document it as recommended.
  - Use an **SSH key with a passphrase**. The passphrase is stored encrypted alongside the key; an attacker would need both.
  - Standard practice: rotate keys periodically, use ssh-agent forwarding, etc.

*MITM on the wire.*
- Standard SSH host-key verification prevents this. qui captures the host key on first connect and verifies it on every subsequent connect (TOFU model, same as `~/.ssh/known_hosts`).
- If the host key changes (e.g. attacker MITM, or legitimately the seedbox SSH host key was rotated), qui refuses to connect and surfaces "host key changed" to the user. The user has to manually clear the saved host key to accept the new one.

*Compromised seedbox.*
- An attacker who roots the seedbox owns the qBittorrent payload. The helper doesn't expand the data blast radius — qui dispatches commands to a host the user already trusts with their data.
- qui's exposure: a malicious helper can return false results, refuse jobs, or write fake audit-log entries. They cannot pivot inbound to qui's other endpoints — qui doesn't accept any inbound from the seedbox; the SSH connection is qui-initiated.
- The audit log on the helper is on the seedbox itself, so a rooted seedbox can rewrite it. Cross-checking is the operator's job (compare helper audit log against qui's dispatcher log if you suspect compromise).

*Compromised qui host.*
- Already game-over for qui's local secrets and qBit tokens. An attacker who reads `sessionSecret` can decrypt the SSH credentials and gain full shell on the seedbox. Same blast radius as the daemon design's "compromised qui issues new pairing strings", except more direct.
- The seedbox-side `pkg/fsexec` allowed-roots policy does NOT protect against this scenario for the reasons above (the attacker can bypass the helper). The only meaningful mitigation is SSH key restrictions (`command="..."` in `authorized_keys`) which lock the SSH session to executing the helper specifically. With that hardening, a compromised qui can dispatch deletion jobs only against the directories the seedbox operator allowed (the helper still enforces `pkg/fsexec`).

*Replay / denial of service.*
- `requestID` dedup (5-min cache, 1024 most recent) prevents accidental double-fire from qui retries.
- An attacker with SSH credentials can trivially DoS the seedbox; this is not a mitigation we provide.

*Allowed-roots misconfiguration.*
- The allowed-roots list defines the helper's *reachability scope*, not authorization for destructive ops. Same model as the daemon design.
- Refuse single-component paths (`/`, `/data`, `/mnt`) and well-known sweeping parents (`/home`, `/Users`, `/var`, `/etc`, `/usr`).
- `$HOME` (e.g. `/home/alice`) is allowed. The helper's UID already has full access to it; refusing it would force the user into a paperwork dance for no security gain. Destructive-op safety on `$HOME`-as-root is the orphanscan layer's job (see §6).
- No `~`, no relative paths. Roots must be absolute and `Cleaned`.

**The honest trade-off vs the daemon design:** SSH credentials are more powerful than a scoped bearer token. The daemon design's bearer is scoped to FS-RPC ops only; SSH is full shell. We accept this because:
1. The user already understands SSH and most already have it configured. The install UX is dramatically simpler.
2. The credential is the only secret. No bearer rotation, no AAD-bound encryption ceremony, no pairing tokens.
3. Restricted-shell hardening (`authorized_keys` `command=...`) is available for users who want to constrain the credential to helper-only access.
4. For self-hosted single-user deployments — the realistic user case — full-shell SSH access is the same authority the user has when they SSH in to administer their seedbox manually. We're not introducing new authority; we're using the authority they already have.

## 17. Compatibility & Migration

- Existing instances continue to use `Local` backend if they had `has_local_filesystem_access=true`, no-op backend otherwise. Zero behavior change.
- Migration 072/073 (sqlite/postgres) adds columns to the existing `instances` table with safe defaults. No new tables.
- Frontend: the radio group's default selection mirrors the current bool: existing instances with `hasLocalFilesystemAccess=true` show as "Local", others show as "None".
- Existing `HasLocalFilesystemAccess` checks (e.g. `internal/proxy/handler.go`, `internal/qbittorrent/sync_manager.go`, `internal/api/handlers/dirscan.go`, `internal/api/handlers/automations.go`, `internal/services/dirscan/inject.go`, `internal/services/automations/hardlink_index.go`, `internal/qbittorrent/delete_cleanup.go`) get a small refactor: instead of `if !instance.HasLocalFilesystemAccess { reject }`, they call a helper `instances.HasFilesystemAccess(ctx, instance) (bool, FilesystemMode)` that returns true if the bool is set OR SSH credentials + helper deployment are configured, plus the mode (`"local" | "helper" | "none"`). Gating language unchanged.

## 18. Phased Implementation Plan

The phasing is organized around **internal-correctness milestones**, not user-shipping milestones. Nothing is exposed to users until the entire system is provably correct end-to-end. Hardening, observability, and the integration-test harness are intrinsic to Stage A — they're how we know the platform works — not bolted on as a final phase. Stages A and B can run in parallel once `pkg/fsexec` is stable; Stage C joins them; Stage D is release engineering.

### Stage A — Platform foundations + CI/test infrastructure

Build everything that doesn't change as filesystem features come and go. The deliverable is a deployed helper that can dispatch a single synthetic `diag.echo` command round-trip — no real FS ops yet. The platform is what subsequent stages consume.

**CI / lint / test foundations.** Built first, in Stage A, so every later commit benefits.
- New CI workflow `helper-ci.yml`:
  - **Cross-compile matrix** for `cmd/qui-helper`: linux/{amd64,arm64}, darwin/{amd64,arm64}. CI publishes each binary as a GitHub release asset on every qui release; CI also computes SHA256 hashes for each and updates the embedded `helperChecksums` constants in qui's main binary. CI verifies that the published assets match the embedded checksums. (Windows/amd64 is deferred to Stage D as best-effort.)
  - **`make test-helper`**: unit tests for `pkg/fsexec`, `pkg/agent/proto`, `internal/sshpool`, `cmd/qui-helper/...` under `-race -count=3` (per CLAUDE.md).
  - **`make test-integration-helper`**: dual-backend harness runs against the synthetic `diag.echo` op (Stage C extends this for every real op).
  - **`make lint-helper`**: golangci-lint v2 on the new packages, applying the existing project profile.
  - **Import-graph guard**: a small Go check in CI that runs `go list -deps ./cmd/qui-helper/...` and fails the build if any imported package matches `^github.com/autobrr/qui/internal/(?!sshpool/...|.../proto)`. Catches accidental boundary crossings at PR time.
  - **Path-safety property tests** (`pkg/fsexec` with random `..`/symlink/devicemount injection): runs on every PR. Non-trivial runtime; gets its own job.
  - **SSH-pool reconnect / fan-out stress test**: long-running variant exercising large concurrent in-flight command counts and forced SSH-disconnect mid-stream. Runs post-merge on `main`, not per-PR.
- New `Makefile` targets: `make helper`, `make test-helper`, `make test-integration-helper`, `make lint-helper`.
- A reusable test fixture, `testutil/helperfixture`, that boots an in-process SSH server (`golang.org/x/crypto/ssh`'s server side), spawns `cmd/qui-helper serve --stdio` over a session, and returns a `Backend` driven through the pool. Stage C reuses this fixture verbatim for every op test.

**Platform code.**
- `pkg/fsexec/`: identical to the daemon design. `ResolveSafe`, allowed-roots policy, `os.Root` wrapping, `openat2(RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS)`, `O_NOFOLLOW`, `..` rejection, device-ID guard, walker with a callback API, primitives for stat/lstat/readdir/mkdir/remove/statfs/samefs/fileid. Path safety lives here, exactly once.
- `pkg/agent/proto/`: `Command`, `Result`, `HelloBanner`, op-payload type sketches.
- `internal/sshpool/pool.go`: per-instance `*ssh.Client` + persistent `ssh.Session`, command writer, result reader, pending-results map, cancellation propagation, streaming-frame demux, `requestID` dedup, idle-timeout sweeper.
- `internal/sshpool/transport.go`: SSH dial logic, host-key verification (TOFU), credential decryption, reconnect backoff.
- Schema migration 074/075 (extend `instances` with SSH/helper columns) + extend `internal/models/instance.go` + extend `InstanceStore` CRUD with SSH + helper params.
- `internal/api/handlers/instances.go`: extend with `POST /api/instances/{id}/ssh-test`, `POST .../helper/deploy`, `POST .../helper/redeploy`, `DELETE .../helper`, `DELETE .../ssh-credentials`, `GET .../helper`.
- `cmd/qui-helper/`: `main.go` + subcommands (`serve --stdio`, `version`, `version --json`); subpackages live under `cmd/qui-helper/internal/{server,executor}` so the helper binary stays standalone.
- Frontend: SSH credential form (`InstanceForm.tsx` extension), `HelperStatusCard.tsx` (new), `useHelper` hook, RadioGroup change. Deploy UX is part of the platform — without it the platform has no front door.
- Observability primitives: structured zerolog throughout, audit log with in-process size-based rotation, instance-card status surfacing via existing `InstanceErrorStore`. Prometheus metrics on qui's side only (helper has no metrics endpoint).
- Synthetic `diag.echo` capability: the only op the helper advertises in Stage A. Used for the round-trip integration tests and the user-facing "Test connection" button. Stays for diagnostics in later stages.

**Acceptance for Stage A** (measurable gates; Stage A is "done" only when every gate is green).

*Coverage gates.*
- `pkg/fsexec` ≥ **90%** line, ≥ **85%** branch.
- `pkg/fsexec/safety.go` ≥ **95%** line.
- `pkg/agent/proto` ≥ **80%** line.
- `internal/sshpool` ≥ **85%** line.
- `internal/api/handlers/instances` (new endpoints) ≥ **80%** line.
- `cmd/qui-helper/internal/{server,executor}` ≥ **80%** line each.
- Frontend pairing components: snapshot tests + at least one rendering test per state.

*Race / concurrency gates.*
- `make test-helper` per-PR: `-count=3` baseline.
- Post-merge `sshpool race / fan-out stress`: `-count=10`, 256 concurrent in-flight `diag.echo` commands, 16 simulated SSH-disconnect injections, completes in < 5 min wall clock with **zero** race-detector hits and **zero** leaked goroutines.
- Integration test runs **100 disconnect/reconnect cycles** within a single test invocation: kill the helper subprocess, wait, observe automatic reconnect on next call.
- Integration test runs **64 concurrent `diag.echo` commands** through a single SSH session and verifies all return exactly once with correct requestID correlation.

*Functional integration tests.*
- **Deploy flow:** 50 deploy-then-remove cycles. Each verifies: SSH dial succeeds, host key captured, binary SCPed, `version --json` parsed, instance record updated, remove cleans up.
- **Auth attacks:** wrong password → clean error; wrong key → clean error; wrong host key (rotation simulated) → "host key changed" error.
- **Capability check:** dispatch a `diag.echo` job; verify cross-correlation with helper-side audit log entries by `requestID`.
- **Cancel propagation:** 1000 cancel cycles. Steady-state cancel latency < **200ms p95**.
- **Crash recovery:** kill qui mid-stream → helper exits cleanly. Kill helper mid-stream → qui's reader sees EOF, channel closes with `connection_lost`. SSH disconnect → all pending fail with `connection_lost`, automatic reconnect.
- **Allowed-roots enforcement:** dispatch a `diag.echo` with a path payload that escapes the allowed roots; helper rejects with `path_not_allowed`, audit log records the attempt.
- **Sweeper invariants:** verify connection-health, pending-results-TTL, and reconnect-backoff sweepers all run on their cadences.

*Build / hygiene gates.*
- `make helper` cross-compiles cleanly for **linux/{amd64,arm64}, darwin/{amd64,arm64}**. Each binary's `version --json` invocation succeeds in CI.
- All cross-compiled helpers published as GitHub release assets; the embedded `helperChecksums` map covers all supported `(os, arch)` combinations.
- `make lint-helper` (golangci-lint v2) reports **zero findings**.
- Import-graph guard: `go list -deps ./cmd/qui-helper/...` contains **zero** packages matching `^github.com/autobrr/qui/internal/(?!agent/proto/...)`. CI runs a negative test (deliberate violation must fail).
- All Go tests pass `-race -count=3` per CLAUDE.md.

*Performance baselines (recorded, not gated; tracked across releases).*
- Connect-to-first-result latency p95.
- Op round-trip latency p95 (steady-state, single instance).
- Cancel latency p95.
- Memory / FD usage of `qui-helper serve --stdio` after 24h soak with synthetic load.

**Non-gates.** Stage A explicitly does *not* exercise real FS operations on either Backend — those land in Stages B and C. The platform's correctness is proven on a synthetic op (`diag.echo`); each real op gets its own dual-backend integration test in Stage C.

### Stage B — `Backend` interface + Local impl + callsite refactor

**Identical to the daemon design's Stage B.** Land the polymorphism in qui's existing services. Behavior-preserving; no helper involvement. Can run in parallel with Stage A once `pkg/fsexec` is stable.

**Scope.**
- `internal/fsops/{backend.go,types.go}`: the `Backend` interface (channels, `RemoveOptions`, `BackendInfo`).
- `internal/fsops/local/local.go`: thin adapter (~150 lines of typed delegation) over `pkg/fsexec`.
- `internal/fsops/pool.go`: resolver. For Stage B, always returns `Local` or no-op; Stage C extends.
- Callsite refactor: every site listed in §8 migrates to `Backend.*`.
- `instances.HasFilesystemAccess` helper rolls out, replacing direct `HasLocalFilesystemAccess` reads.

**Acceptance for Stage B.**
- Existing test suite passes `-race -count=3` with zero behavioral diff vs. `develop`.
- No regressions in dirscan, orphanscan, automations, cross-seed.
- Existing instances default to "Local" or "None" in the new RadioGroup; behavior unchanged.

### Stage C — Wire Remote ops, one at a time

The dual-backend integration matrix from Stage A grows: each FS op gets a `Command.Op` + helper-executor switch case + `Backend` method on `Remote`, plus matching integration tests that run **twice** (`Local` and `Remote`) and require equivalent observable outcomes.

**Order** (pure ops first, destructive ops later, streaming op in its natural complexity progression):
1. `fs.stat`
2. `fs.statfs`
3. `fs.samefs`
4. `fs.lstat`
5. `fs.readdir`
6. `fs.mkdir`
7. `fs.walk` (streaming)
8. `fs.remove`
9. `fs.removeall`
10. `tree.hardlink`
11. `tree.reflink` (capability-gated)
12. `tree.remove`

**Each op is one PR.** Per-PR scope: proto-args type, helper-executor switch case, `Backend` method on `Remote`, capability advertisement, dual-backend integration test, audit-log assertion if destructive, capability-missing UI test if applicable.

**Acceptance per PR.**
- Both backends produce equivalent observable outcomes for that op.
- For destructive ops: helper audit-log entries and qui pool-log entries correlate one-to-one by `requestID`.
- Capability hint surfaces correctly in the frontend.

**Stage C exit criterion.** Every feature in §2 works end-to-end on a real seedbox, with the dual-backend matrix proving parity vs. the local code path. At this point the system is internally complete.

### Stage D — Release engineering

Distribution and documentation. Hardening, observability, and the integration-test harness are not in this stage — they were intrinsic to Stage A and exercised throughout B and C.

**Scope.**
- Helper binaries are published as GitHub release assets alongside qui's release; the release pipeline builds both.
- **Windows/amd64 best-effort**: add windows/amd64 to the cross-compile matrix. No dedicated integration testing — SSH on Windows is uncommon for seedboxes. Ship it if it compiles and `version --json` works; document it as community-supported.
- User-facing docs: `documentation/docs/remote-helper.md` (install, deploy, troubleshooting, restricted-shell hardening), README updates, install one-pagers.
- Multi-day soak test against real seedboxes if available; security review pass on path-safety, allowed-roots policy defaults, and SSH credential storage.

**Acceptance for Stage D.**
- Public release. Users can configure SSH credentials, deploy the helper with one click, and use every feature in §2.

## 19. Testing & Verification

**Unit.**
- `internal/fsops/local/*_test.go`: full interface coverage on `t.TempDir()`. Includes streaming walker bounded-memory test (10k synthesized files), hardlink tree create + rollback equality with current `pkg/hardlinktree` tests, statfs sanity (≥ 0).
- `internal/sshpool/*_test.go`: command/result correlation, cancel propagation, pending-results TTL eviction, stream-frame ordering, double-deliver rejection, reconnect-on-disconnect, host-key TOFU verification.
- `internal/fsops/remote/*_test.go`: against an in-process `sshpool` connected to a fake helper (goroutine that reads NDJSON commands from a pipe and writes canned results). Tests: connection_lost when no helper running, ctx cancellation unblocks the caller, NDJSON streaming pushes onto the channel in order, version_skew error when capability missing.
- `cmd/qui-helper/*_test.go`: serve flow against piped stdio (httptest analog using `io.Pipe`), command parsing, allowed-roots rejection.
- `pkg/fsexec/*_test.go` **path-safety property tests** (the security-critical layer; tested exhaustively): same enumeration as the daemon design — `..` traversal, symlink chains, symlink target escapes, TOCTOU (symlink swap, parent rename), mount-bind escape, NUL injection, PATH_MAX boundary, relative paths, empty path, root-itself-as-target, redundant slashes, control characters, Unicode normalization on macOS, special device targets, FIFO/socket/block-device targets.

**Integration.**
- `internal/fsops/integration_test.go` boots an in-process SSH server (`crypto/ssh`'s server side) and runs `cmd/qui-helper serve --stdio` over a session. Pairs them automatically (no auth needed for the test server — keys generated in-process), then runs the full feature suite end-to-end against both `Local` (using a tempdir as "fake host") and `Remote` (against the helper subprocess). Same tests, two backends; results must match.
- Cross-seed inject scenario: build a synthetic searchee, build a `TreePlan`, hardlink-tree via helper, verify on-disk via `os.Stat`, rollback, verify removed.
- Orphanscan scenario: seed a tempdir with files, build a fake `TorrentFileMap` covering some, walk, delete the rest, verify in-use files survive.
- Missing-files: stat a known path that exists, then a removed one, expect correct missing flags.
- Statfs: against a tempdir, verify `BytesAvailable > 0`.
- Samefs: same root, different roots on different mounts (Linux only — macOS mount tricks are a hassle).
- **Deploy/lifecycle:** generate SSH credentials, deploy, observe `HelloBanner`; redeploy with newer version, observe upgrade; remove, observe cleanup.

**Regression / soak.**
- The CI matrix extends to include the remote-helper integration job.
- All Go tests under `-race -count=3` per `CLAUDE.md`.

**Manual verification checklist (release blocker).**
- Deploy against a real seedbox (no overlay nets — direct outbound SSH only).
- Run a 50k-file dirscan, observe streaming, no OOM on either side.
- Run an orphanscan with destructive deletion against a sacrificial directory; confirm helper audit log entries match qui's pool log one-for-one (correlate by `requestID`).
- Inject a cross-seed match; verify hardlink tree on disk, then rollback via the cross-seed UI; verify cleanup.
- Trigger a connection drop (e.g. `iptables` rule on the seedbox); verify qui surfaces "connection lost" within ~45s; remove the rule; verify automatic reconnect.
- Rotate SSH credentials via the form; verify the new credentials work; verify the old credentials are gone.

## 20. Open Questions

These are the items that genuinely need information we don't have yet.

1. **Restricted-shell hardening adoption.** The `command="qui-helper serve --stdio …"` pattern in `authorized_keys` is the right way to scope SSH access to helper-only. The deploy UI now shows the snippet with a copy button after successful deployment (§11 step 6). Open question: should we also generate a dedicated SSH key pair from qui's UI so the user can paste just the public key + command restriction into `authorized_keys`, instead of managing their own key? This would further simplify the hardening flow.

2. **Capability `fs.fileid` on Windows.** Windows `FileID` is a `(VolumeSerial, FileIndex)` tuple. Need a careful look at `pkg/hardlink/fileid_windows.go` before shipping to confirm cross-volume `FileID` comparisons aren't accidentally meaningful.

3. **Reflinks in Docker on common seedbox topologies.** `pkg/reflinktree` handles the platform-specific CoW syscalls correctly today, but reflink behavior inside a Docker container on a host bind-mount needs end-to-end verification. Particularly on ZFS-backed hosts and Btrfs.

4. **GitHub release-pipeline coupling.** qui downloads helper binaries from GitHub release assets at deploy time, verifies SHA256 against embedded constants, and SCPs them to the seedbox. Deploy success couples to (a) the release pipeline correctly publishing the asset for the qui version the user is running, and (b) GitHub's release-asset CDN being reachable from qui's host (not the seedbox). (b) is fine in practice — qui's host has internet access. (a) means a botched release with a missing asset breaks deploy until the next release ships. Worth a Stage D readiness check: CI gates that block a qui release tag if any helper asset is missing or fails checksum verification.

5. **SSH library quirks.** `golang.org/x/crypto/ssh` works but has rough edges (key parsing for some legacy formats, TOFU support requires manual implementation). Worth a Stage A spike to confirm the rough edges are tolerable before committing.

6. **Multi-architecture detection on the seedbox.** Some seedboxes have unusual `uname -m` output (e.g. ARM variants). The detection logic needs to handle the common cases gracefully and fall back cleanly.

7. **Host-key churn on cloud seedboxes.** Some hosting providers regenerate SSH host keys on instance restart. qui's TOFU model would refuse to connect after such a restart. Should we offer a "trust on next reconnect" affordance? Probably not — that defeats the security model. Document the limitation.

## 21. Critical Files for Implementation

**Shared (Stage A) — `pkg/` so it's importable by the helper without crossing `internal/`.**
- `pkg/fsexec/safety.go` (new — `ResolveSafe`, allowed-roots policy, `os.Root` wrapping, device-ID guard)
- `pkg/fsexec/walker.go`, `stat.go`, `mkdir.go`, `remove.go`, `statfs.go`, `samefs.go`, `fileid.go`, `readdir.go` (new — primitives, callback API)
- `pkg/agent/proto/proto.go` (new — `Command`, `Result`, `HelloBanner`, op payloads)

**qui-side platform (Stage A).**
- `internal/sshpool/pool.go` (new — per-instance `*ssh.Client` + persistent `ssh.Session` + pending-results + cancellation + streaming demux)
- `internal/sshpool/transport.go` (new — SSH dial, host-key TOFU, reconnect backoff)
- `internal/sshpool/sweeper.go` (new — connection-health + pending-results TTL + reconnect scheduler)
- `internal/sshpool/deploy.go` (new — arch detection, GitHub-release asset download to qui host, SHA256 verification, SCP to seedbox, atomic install)
- `internal/sshpool/helper_checksums.go` (new — generated by release CI; embeds SHA256 hashes for each supported `(os, arch)` of `qui-helper_v{thisQuiVersion}_*` so deploy can verify downloads without trusting the network)
- `internal/api/handlers/instances.go` (extend with SSH-test, helper-deploy, helper-redeploy, helper-remove, helper-status endpoints)
- `internal/database/migrations/074_add_remote_helper.sql` (new — SSH + helper columns on `instances`)
- `internal/models/instance.go` (extend with SSH + helper fields)
- `internal/models/ssh_credentials.go` (new — SSH credential value type, encrypt/decrypt helpers)

**qui-side fsops abstraction (Stage B).**
- `internal/fsops/backend.go` (new — `Backend` interface)
- `internal/fsops/local/local.go` (new — thin adapter over `pkg/fsexec`)
- `internal/fsops/remote/remote.go` (new — `Backend` impl backed by the SSH pool; populated through Stage C op-by-op)
- `internal/fsops/pool.go` (new — resolver)

**Helper binary (Stage A).**
- `cmd/qui-helper/main.go` (new — entrypoint; subcommands `serve --stdio`, `version`)
- `cmd/qui-helper/internal/server/server.go` (new — stdio loop, command parser, dispatcher to executor)
- `cmd/qui-helper/internal/executor/executor.go` (new — `Op` switch; calls `pkg/fsexec` primitives)

**Frontend (Stage A).**
- `web/src/components/instances/InstanceForm.tsx` (replace toggle with radio + SSH credential fields)
- `web/src/components/instances/HelperDeployModal.tsx` (new — host key confirmation + deploy progress)
- `web/src/components/instances/HelperStatusCard.tsx` (new — paired-helper summary, redeploy / remove actions)
- `web/src/hooks/useHelper.ts` (new — `GET /api/instances/{id}/helper`)
- `web/src/types/index.ts` (extend `Instance` type with SSH + helper fields)

**CI / build (Stage A).**
- `.github/workflows/helper-ci.yml` (new — cross-compile matrix, `make test-helper`, `make test-integration-helper`, `make lint-helper`, import-graph guard, path-safety property tests, sshpool race/fan-out stress)
- `Makefile` (extend with `make helper`, `make test-helper`, `make test-integration-helper`, `make lint-helper`)
- `testutil/helperfixture/fixture.go` (new — boots in-process SSH server + spawns `cmd/qui-helper serve --stdio` subprocess + auto-connects; reused for every op test in Stage C)
- `scripts/check-helper-imports.go` (new — `go list -deps` walker that fails on `internal/` imports outside `internal/sshpool/...` and `pkg/agent/proto`)

## 22. Verification

End-to-end verification once Stage C lands (every op wired to the helper):

1. `make backend && make helper` builds both cleanly.
2. Start qui locally on `https://localhost:7476`.
3. In the qui UI, edit an instance, choose "Remote helper", enter SSH credentials for a real seedbox (or a local test VM). Click "Test connection" — confirm host key fingerprint.
4. Click "Deploy helper". Watch the progress: arch detected, binary pushed, version probed. UI flips to "Deployed (vX.Y.Z, 14 capabilities, 2 reflink-capable roots)".
5. Trigger a dirscan against a configured allowed root. Observe command dispatch in qui's structured logs (`op=fs.walk requestID=…`) and matching helper log lines on stderr (forwarded into qui's logs). NDJSON streaming arrives line-by-line on qui's side.
6. Inject a synthetic cross-seed match with a small `TreePlan`. Verify hardlinks on disk. Trigger rollback via the cross-seed UI and verify cleanup.
7. Drop the SSH connection (e.g. `iptables -A INPUT -p tcp --dport 22 -j DROP` on the seedbox). Verify qui surfaces "connection lost" within ~45s. Remove the rule. Verify automatic reconnect on the next op.
8. Click "Redeploy" (or upgrade qui to a version with a newer embedded helper). Verify the new binary is pushed and the version updates in the status card.
9. Click "Remove helper". Verify the binary and audit log are cleaned up on the seedbox.
10. Run `make test` (`-race -count=3`) — every test passes including the new fsops interface conformance, sshpool unit tests, and the dual-backend integration tests.
11. Run `make lint` — passes.

## 23. Implementation Status

> Added 2026-04-29 after completing Stages A+B. This section tracks what has been built, what deviates from the design above, and what remains for Stage C.

### Completed (Stages A+B)

**Detailed build plan with per-phase notes:** `documentation/design/ssh-helper-plan.md`

| Area | Status | Notes |
|------|--------|-------|
| `pkg/agent/proto` | Done | 27 types (3 envelopes + 20 op payloads + 2 diag + `LstatResponse`). Op/error code constants added. |
| `internal/fsops` | Done | `Backend` interface (17 methods), `Pool` resolver, `NoopBackend`, sentinel errors. |
| `internal/fsops/local` | Done | All 17 methods, platform-specific Statfs, 26 tests. |
| Callsite refactors | Done | All 104 `os.*`/`unix.*` callsites across 10 files migrated to Backend. Zero behavioral change. |
| Schema | Done | Migration 072/073 — 16 SSH/helper columns on `instances`. |
| `HasFilesystemAccess` | Done | Returns `(FilesystemMode, bool)` — note: signature differs from §17's `(bool, FilesystemMode)`. Service-layer guards migrated; handler-layer guards deferred to Stage C. |
| `pkg/fsexec` | Done | `SafeRoot` + `Roots` + `ResolveSafe` with `os.Root` (Go 1.24+). 16 property tests. Individual primitive wrapper files omitted — `os.Root` provides all methods directly. |
| `internal/sshpool` | Scaffold | `Pool` with `Submit`/`Cancel` stubs (return "not implemented"). `tofuHostKeyCallback`, `buildSSHConfig` implemented and tested. `DetectArch`, `DeployHelper` implemented. Sweeper goroutines are stubs. |
| `cmd/qui-helper` | Scaffold | `serve --stdio` with `diag.echo` only. `version --json` outputs `HelloBanner`. 30s graceful shutdown on stdin EOF. Zero `internal/` imports. Cross-compiles for 4 platforms via `make helper`. |
| API endpoints | Scaffold | 6 endpoints registered on `/{instanceID}/`. All return "not implemented" responses. OpenAPI spec deferred. |

### Intentional Deviations from Design

1. **`HasFilesystemAccess` returns `(FilesystemMode, bool)` not `(bool, FilesystemMode)`** — Go convention.
2. **`pkg/fsexec` omits primitive wrapper files** (`stat.go`, `walker.go`, etc.) — Go 1.26's `os.Root` has all needed methods; thin wrappers add no value. Helper executor calls `sr.Root().Lstat(rel)` directly.
3. **`noopBackend` is unexported** — only accessible through the Pool.
4. **`local.Backend` not `local.LocalBackend`** — avoids stutter.
5. **OpenAPI spec not updated** — endpoints return scaffold responses; spec updates when responses are final.

### Deferred to Stage C

These items were removed from the scaffold to avoid shipping dead code. They must be restored when `Pool.Submit` wires real SSH dispatch:

- **`dialSSH(host, port, config)`** — SSH dial function. Uses `net.JoinHostPort` + `ssh.Dial` with `dialTimeout`.
- **`serverAliveInterval`** (15s), **`reconnectBaseDelay`** (5s), **`reconnectMaxDelay`** (60s) — keepalive and reconnect backoff constants.
- **`pendingResultsTTL`** (5min) — for pending result eviction in the sweeper.
- **`instanceClient` fields** (`instanceID`, `banner`, `pending`, `mu`) — per-connection state for SSH session management.
- **Real sweeper implementations** — health check (ping SSH), pending TTL (close stale channels), reconnect (exponential backoff with jitter).
- **`fsops/remote`** — Remote backend translating `Backend.*` calls into SSH pool `Submit` commands.
- **Each FS op in helper executor** — 12 ops, one PR each: `fs.stat` → `fs.statfs` → `fs.samefs` → `fs.lstat` → `fs.readdir` → `fs.mkdir` → `fs.walk` → `fs.remove` → `fs.removeall` → `tree.hardlink` → `tree.reflink` → `tree.remove`.
- **Handler-layer `HasLocalFilesystemAccess` migration** — ~15 references in API handlers to migrate to `HasFilesystemAccess`.
- **Frontend** — instance form radio group (None / Local / Remote helper), SSH credential fields, deploy modal, helper status card.
