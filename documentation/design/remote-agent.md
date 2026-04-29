# qui Remote Agent — Design Document

## 1. Context & Motivation

qui has a cluster of features that work only when the qui process and qBittorrent share a host: cross-seed hardlink/reflink injection, dirscan, orphanscan, three automation rule conditions (missing-files, path-based free-space, hardlink-scope), and the post-delete managed-cleanup pass. All of these reach into the local filesystem via `os.*`/`filepath.*`/`unix.*`. A non-trivial fraction of users run qBittorrent on remote seedboxes/VPS while qui lives on a home server or in a container. For them, every feature listed above is dead — and the network-mount workarounds (SSHFS, rclone, NFS) either misreport inode/nlink (breaking cross-seed) or are operationally painful. The complete inventory and user-visible mapping live in §2 and the bullets below.

**Proposal.** Ship a separate single-binary daemon, `qui-agent`, that runs on the same host as the remote qBittorrent. The agent **dials qui** over outbound HTTPS, registers, and long-polls for filesystem jobs. qui dispatches FS ops to the agent through that connection instead of calling `os.*` locally. The agent is opt-in per-instance and additive: it lands behind a new interface so the existing local-filesystem code path is untouched until the user opts in.

**Features the agent unlocks.** With a paired agent, every filesystem-dependent feature qui ships today works against a remote qBittorrent instance with the same UX users get on a co-located install:

- **Cross-seed inject — hardlink mode.** When cross-seed identifies a torrent that matches data already in the user's library, qui materializes a hardlink tree on the seedbox so qBittorrent can immediately seed the matched torrent without re-downloading any bytes. The agent's `pkg/fsexec` primitives execute the link tree atomically and roll back cleanly on partial failure. Includes the same-filesystem precondition check (the link-tree base directory and the source data must share a device) so qui never attempts a hardlink that would cross filesystems.
- **Cross-seed inject — reflink mode.** Same shape as hardlink mode but using copy-on-write reflinks (XFS, Btrfs, ZFS). Eliminates even the inode pressure of hardlinks. The agent reports per-root reflink support at registration and on every heartbeat (`reflinkRoots`); qui only dispatches `tree.reflink` jobs for roots whose underlying filesystem supports it, and falls back to hardlinks where it doesn't.
- **Dirscan.** Scheduled or webhook-triggered directory walks build a FileID index of files on the seedbox, used by cross-seed to identify match candidates without round-tripping qBittorrent's API for every file. NDJSON streaming over the agent connection keeps memory bounded on both sides for trees with 10k+ files.
- **Orphanscan.** Periodic walks of save directories surface files no torrent claims, with the safety layer described in §6: an ignore-list of well-known sensitive paths (`.ssh`, `.config`, `.gnupg`, etc.), and a per-root acknowledgement requirement when the configured root is broader than qBittorrent's known save paths. The agent's audit log records every destructive op; qui's dispatcher log correlates one-to-one by `requestID`.
- **Missing-files automation condition.** Automation rules can trigger on actual filesystem state — *"pause torrents whose files have gone missing on disk"* — rather than relying on qBittorrent's reported state, which can be stale after manual file moves or external cleanup. Batch-friendly: one `fs.stat` job covers an entire torrent's file list.
- **Free-space automation condition — path-based.** Automation rules can read disk space at a specific path on the seedbox via `fs.statfs`, not just qBittorrent's reported value. Useful when the qBittorrent save path differs from the partition the user actually wants to monitor (e.g. ZFS dataset quotas, separate cache vs. archive volumes).
- **Hardlink-scope automation condition.** Rules can branch on whether a torrent's data is hardlinked to files outside qBittorrent's managed directory — *"only delete this torrent if its files are unique"* / *"only act on torrents whose data is shared with my media library"*. The agent builds a per-instance hardlink index (`fs.lstat` + `fs.fileid`) across all torrents on each automation cycle, cached for 2 min. High-volume but cached; the cache invalidates on torrent set change.
- **Managed delete cleanup.** When an automation runs a `deleteWithFiles` action, qBittorrent removes the files but leaves empty parent directories behind. qui's post-action cleanup walks up the parent chain (`fs.stat` + `fs.remove`) pruning empty dirs until it hits a configured "managed delete base dir". The agent surface is small (a handful of ops per delete) but destructive — the audit log captures every directory removal. Lives in `internal/qbittorrent/delete_cleanup.go`, triggered by automation delete actions when `managed_delete_enabled` and base dirs are configured.

These eight features are everything the agent enables. The complete operation-level inventory lives in §2; this section is the user-visible mapping of "what becomes possible once you pair an agent."

**Success criteria.** A user installs `qui-agent` on a seedbox, runs `qui-agent pair <one-time-string>` with a string copied out of qui's UI, and sees every feature listed above work over the wire with the same UX they have today on a co-located host. No inbound port required on the seedbox. Existing local-filesystem deployments see zero behavior change. Re-pairing, rotating credentials, or revoking access is straightforward.

**Non-goals.**
- Not a general-purpose RPC. Job ops are scoped exactly to qui's filesystem features.
- The agent does not execute user scripts, exec arbitrary programs, or proxy qBittorrent's WebUI.
- v1 does not support one agent serving multiple qui installs (each agent is paired to exactly one qui via its bearer; one-to-one).
- No agent-listens mode in v1 (no inbound port on the seedbox). The agent always dials qui. This deliberately removes the "do I have a forwarded port?" question from the install flow.
- No NAT traversal needed: outbound HTTPS is universal on every seedbox (qBittorrent already needs it for trackers, RSS, etc.).
- No agent-side scheduling. The agent executes jobs qui dispatches; qui owns scheduling.
- The agent does not manage qBittorrent itself. It is purely a filesystem broker.
- v1 is not premium-gated.

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
|   (publicly reachable URL)     |                |                                 |
|                                |                |  +---------------------------+  |
|  +--------------------------+  |                |  |  qBittorrent              |  |
|  |  services/dirscan        |  |                |  +-----------+---------------+  |
|  |  services/orphanscan     |  |                |              | local FS         |
|  |  services/automations    |  |                |              v                  |
|  |  services/crossseed      |  |                |  +---------------------------+  |
|  +-----------+--------------+  |                |  |  ~/data, ~/seed, ...      |  |
|              |                 |                |  +-----------+---------------+  |
|              v                 |                |              ^                  |
|  +--------------------------+  |                |              | os.* via         |
|  |  internal/fsops          |  |                |              | openat2(BENEATH) |
|  |  ┌──────────┐┌─────────┐ |  |                |              |                  |
|  |  │ Local    ││ Remote  │ |  |                |  +-----------+---------------+  |
|  |  └──────────┘└────┬────┘ |  |                |  |  qui-agent (executor)     |  |
|  +-------------------+------+  |                |  |  long-polls qui for jobs  |  |
|              |                 |                |  |  bearer auth + path safe  |  |
|              v                 |                |  |  audit log                |  |
|  +--------------------------+  |                |  +-----------+---------------+  |
|  |  internal/agent          |  |                |              |                  |
|  |  Dispatcher (job queue)  |  |                |              |                  |
|  |  per registered agent    |  |                |              |                  |
|  +--------------------------+  |                |              |                  |
|              ^                 |                |              |                  |
|              |  /api/agent/v1/{poll,result,heartbeat} (HTTPS) |                  |
|              +---------------- agent dials qui ---------------+                  |
+--------------------------------+                +---------------------------------+
                                                       (single binary; tarball install)
```

**Lifecycle.**
- qui boot: load registered agents from DB. The in-memory `agent.Dispatcher` exists as a singleton; per-agent state (inbox, pending-results) is allocated lazily on the agent's first poll, not at boot. No outbound dial — qui is the server.
- Agent boot: `qui-agent serve` reads its config (qui URL, bearer, optional cert pin, allowed roots). Opens N parallel long-poll connections to `POST /api/agent/v1/poll`. Each connection blocks until qui dispatches a job or the per-poll timeout (default 30s) elapses, at which point qui returns 204 and the agent reconnects.
- Heartbeat: agent posts `POST /api/agent/v1/heartbeat` every 30s with current version, capabilities, allowed roots, reflink roots, and inflight-ops count. qui records `last_seen_at` and refreshes its informational copy of the roots/capabilities. Three missed heartbeats → "disconnected" state in the instance card (reuses `InstanceErrorStore`).
- Job execution: agent receives a `Job{requestID, op, args, deadline}`, executes it under path-safety guards, posts the result to `POST /api/agent/v1/result/{requestID}` (JSON for one-shots, NDJSON streamed for walks). qui's dispatcher correlates by `requestID` and unblocks the waiting `fsops.Remote` caller.
- Shutdown (agent): drain in-flight ops up to a 5s deadline, close polls. qui marks the agent stale on next heartbeat miss.
- Shutdown (qui): close pending result channels with a clean error; agent's in-flight POST returns; agent reconnects on the next start.

## 4. Transport, Framing & Streaming

**Decision: HTTPS + JSON for one-shot calls. NDJSON-over-HTTPS for streamed walks. No gRPC. No WebSocket.**

Justification:
- **JSON over HTTPS** is the same pattern qui already speaks to qBittorrent (`internal/qbittorrent/client.go` uses stdlib `net/http`). No new wire format, no new client toolchain, no new TLS code path. Easy for users to debug with `curl`.
- **gRPC** buys efficient streaming and codegen but adds protoc, a runtime, generated stubs, and a meaningfully bigger binary. The endpoint surface is small (~12) and stable. The complexity isn't paid back here.
- **WebSocket** handles long-lived bidirectional but qui is purely the requester.
- **NDJSON** (one JSON object per line, `\n`-framed, `application/x-ndjson`) is the right answer for `fs.walk` (the only streamed op in v1; FileID indexing is `fs.walk` with `WantFileID:true`) because:
  - Realistic dirscan trees can hit 10k+ files. Buffering a single response means tens of MB resident on agent and qui at once. Streaming chunks bound memory.
  - The qui-side caller already wants to consume entries lazily (the existing `filepath.WalkDir` callback pattern is per-entry).
  - Backpressure is free (HTTP/1.1 stream + TCP).
  - Implementation is `json.Encoder` per line on the agent and `bufio.Scanner` + `json.Unmarshal` on qui. No new dependency.

10k-file walk back-of-the-envelope: ~250 bytes/line × 10k = 2.5 MB streamed; ~20 ms at gigabit. The bottleneck is `WalkDir` itself (filesystem-bound), not framing.

**Compression.** Enable gzip via `Accept-Encoding` — walks and FileID dumps compress 3–5×. Stdlib handles it transparently when the server sets `Content-Encoding: gzip`.

**Request size bounds.** Max request body 16 MiB. Max response body unbounded for streamed endpoints; capped at 64 MiB for non-streamed responses.

## 5. Auth & Pairing

**Decision: pairing-token bootstraps an agent-generated bearer. The agent generates the bearer locally at pair time using `crypto/rand` (32 bytes, base64url-encoded), submits it to qui over the TLS-protected `/register` POST body. qui hashes on receipt and stores only the hash; the plaintext exists only in the request handler's stack frame on qui's side, and persistently only on the seedbox. Optional qui-cert SHA-256 pin in the pairing string for self-signed qui deployments. No mTLS in v1.**

Why this shape rather than the RFC's three-field paste:
- The agent dials qui, so qui is the TLS server — there's nothing for qui to pin against the agent. The asymmetry that "qui validates the agent" disappears; instead it's "qui authenticates the agent's bearer". One credential, one direction.
- Pairing-by-string collapses the install UX to one paste. No URL/token/fingerprint copy-fest.
- One-time pairing tokens are a well-trodden device-onboarding pattern (think `tailscale up --auth-key`, GitHub Actions runner registration). The threat model is well understood.
- The bearer itself only ever needs to be hashed on qui's side (we verify on incoming polls, never re-present). HMAC-SHA256 with `sessionSecret` is enough; AES-GCM-encrypted-at-rest is unnecessary because the bearer is never decrypted, only compared.

**Why agent-generated rather than qui-generated.** A naive design pre-issues the bearer qui-side and hands it to the agent during `/register`. That requires qui to hold the bearer plaintext in its DB until the agent picks it up — and qui's `internal/backups/` service captures the SQLite file on its schedule. A backup taken during the ~10-minute pairing window would contain a live bearer. Inverting who generates the bearer eliminates this exposure entirely: the bearer plaintext exists only on the seedbox where the agent runs, never on qui's disk.

**Pairing string format.**

```
qui-pair_v1_<base64url(payload)>
```

Where `payload` is a packed JSON:
```json
{
  "instanceID": 7,
  "pairingToken": "<32-byte random>",
  "quiURL": "https://qui.example.com",
  "quiCertPin": "sha256:2F:B8:...:9C",   // optional, only if qui is on a self-signed cert
  "expiresAt": "2026-04-27T18:00:00Z"
}
```

The `pairingToken` is a single-use credential that maps to a queued `agent_pairings` row in qui (TTL ~10 min). The agent exchanges it for a long-lived bearer on `POST /api/agent/v1/register`.

**Why base64url(JSON) and not a more compact format.** The encoded string runs ~250 chars in practice. JSON costs us 30–40% over a custom binary or CBOR layout, but buys two things worth keeping in v1:
- **Debuggability.** `echo "<encoded>" | base64 -d` produces human-readable fields. Critical for support and CI failure debugging.
- **Forward-compat.** New fields land cleanly with old agents (unknown keys ignored), no version bump needed.

250 chars sits in the same league as SSH public keys and JWT tokens that users paste routinely. If post-launch verbosity becomes a real complaint, switching the encoded payload to CBOR is a wire-format change behind a version bump (`qui-pair_v2_...`), no design rework.

**Pairing UX.**

In qui:
- User opens an instance → "Filesystem access" → "Remote agent" → "Generate pairing string".
- qui creates an `agent_pairings` row (instanceID, fresh pairingToken, expiresAt = now+10min). **No bearer is generated yet — the agent generates it.**
- Modal shows the pairing string with a copy button and a "waiting for agent" status.

On the seedbox:
```
$ qui-agent pair qui-pair_v1_eyJpbnN0YW5jZUlEIjo3LC4uLn0= --root ~/data --root ~/seed
qui-agent v0.1.0
Decoded qui URL:  https://qui.example.com
Verifying TLS:    matched pinned cert (sha256:2F:B8:...:9C)
Registering...    OK (assigned agent UUID 3c4f8b9a-2e7d-4a5f-9c1e-8b6d2f4a1c3e)
Allowed roots:
  /home/seedbox-user/data
  /home/seedbox-user/seed
Configuration written to ~/.config/qui-agent/config.toml.
Bearer written to    ~/.config/qui-agent/bearer (mode 0600).

Run `qui-agent serve` (or enable the systemd unit) to start polling.
```

`pair` does:
1. Decodes the pairing string. Validates `expiresAt`.
2. **Generates a 32-byte random bearer** with `crypto/rand`, encodes as `qui-agent_v1_<base64url>`. The plaintext lives in the agent's process memory and (after step 5) on disk; never anywhere else.
3. Builds an `*http.Client` with TLS verification: standard CA validation if `quiCertPin == ""`, or `tls.Config.VerifyPeerCertificate` pinned to the SHA-256 if set. Hostname verification skipped only in pinning mode.
4. POSTs to `${quiURL}/api/agent/v1/register` with the full `RegisterRequest` (§7.4): `{pairingToken, bearer, agentUUID, agentVersion, protoVersion, capabilities, allowedRoots, reflinkRoots, platform, hostname}`. The body travels over TLS only; qui never logs or persists the plaintext.
5. qui validates the pairingToken, marks it consumed, hashes the received bearer with `HMAC-SHA256(sessionSecret, "agent-bearer:" + bearer)`, persists the agent registration (writes the `agents` row with `bearer_hash`, `agent_uuid`, all advertised fields, and the `registered_from_addr` qui observed on the request). Returns `{instanceID}` only — never re-presents the bearer.
6. Agent writes config + bearer to disk, exits.

**qui side details.**
- qui never stores the bearer plaintext at any point. The `/register` handler hashes the incoming bearer in-memory, writes the hash to `agents.bearer_hash`, and the request body / handler stack frame is GC'd. A snapshot of qui's DB taken at any moment contains only hashes.
- HMAC keying with `sessionSecret` means an attacker who steals just `agents.bearer_hash` (without `sessionSecret`) can't verify a candidate bearer offline — they need both. With 32 bytes of entropy in the bearer plus the keyed hash, brute-forcing is infeasible.
- AAD-bound encryption is unneeded; we don't store reversible ciphertexts of the bearer at all.
- The instance card flips to "Remote agent: connected (vX.Y.Z, 2 roots)" as soon as the first heartbeat arrives.

**Bearer rotation.** "Rotate agent bearer" button on the instance card invalidates the current bearer hash and generates a new pairing string. The old bearer starts returning 401 on the next poll; user re-runs `qui-agent pair <new-string>` on the seedbox. No service-side state to clean up.

**Bearer revocation.** "Unlink agent" on the instance card deletes the registration. All polls return 401 immediately. Equivalent to "stop and reconfigure".

**Allowed roots.** Set on the seedbox at `pair` time via `--root` flags (or by editing `config.toml` later). Hard-bound on the agent: the agent rejects any job whose target paths aren't under one of the allowed roots. Even if qui is fully compromised, blast radius is "the directories the seedbox operator allowed". qui does not get to widen this list at runtime.

## 6. Path Safety

The path-safety model is the security backbone. Bearer tokens get stolen; allowed-roots policy must hold even when authentication fails.

**Agent-side enforcement.**
- All incoming paths are required to be absolute, `filepath.Clean`-ed, and rooted under one of the allowed roots.
- Linux: every open uses `os.Root` (Go 1.24+) wrapping the allowed-roots directory, which uses `openat2(RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS)` under the hood. Mature in Go 1.26.
- macOS / Windows: `os.Root` works with a userspace-resolution fallback. We additionally `lstat` and reject any symlink encountered during traversal of a request path. README notes Linux as the recommended OS for full kernel-enforced isolation.
- Destructive ops (`fs.remove`, `fs.removeall`, `tree.remove`) refuse to operate on the allowed-root itself; the resolved path must be a strict descendant.
- `O_NOFOLLOW` on every direct open. Symlinks within the allowed root are not traversed.
- `..` rejected at request validation time, before resolution.
- Device-ID guard: on startup the agent records `dev` of each allowed root and rejects any op whose resolved path is no longer on that device. Protects against an allowed root being unmounted while the agent runs (deletes would otherwise land on the bare mountpoint, which is on a different filesystem). Cheap to implement, real value; included in v1.

**Audit log.** A separate `audit.log` (configurable path, in-process size-based rotation) records every `fs.remove`, `fs.removeall`, `tree.remove`. JSON lines: `{ts, op, path, request_id, qui_url, outcome, error}` (`error` populated only on `outcome: "failure"`). Survives bearer revocation; the operator's accountability trail. Note: `request_id` is generated qui-side and propagated through the job payload, so qui's logs and the agent's audit log correlate one-to-one.

**TOCTOU contract.** `pkg/fsexec.ResolveSafe(rootName, requestPath)` returns an `*os.Root` handle to the root plus a relative resolved path. Every subsequent op against that resolved path uses the **same handle** via `*at` syscalls (or `os.Root` methods that wrap them on Go 1.24+). Concretely:
- The walker calls `safeRoot.Lstat(rel)` and `safeRoot.OpenInRoot(rel)` rather than re-resolving from absolute paths.
- `pkg/fsexec.RemoveAt(safeRoot, rel)` is the only way the agent removes anything; it never shells out to `os.Remove` with an absolute path.
- A single op holds one handle for the duration of its execution. The kernel guarantees the resolved file referred to by the handle won't be substituted by an attacker mid-op (no symlink swap, no parent-dir rename escape).
- For batched ops (`StatBatch`, `LstatBatch`), the same root handle is reused across all paths in the batch. One resolve, many syscalls.

This is the single most important contract in `pkg/fsexec` and is the property the path-safety property tests verify (synthetic mid-op symlink/rename injection must not change the file the op affects).

**Rate limiting.** Now lives on **qui's side** (since qui is the server). Failed bearer attempts on `/api/agent/v1/poll`, `/api/agent/v1/heartbeat`, `/api/agent/v1/result/*`, and `/api/agent/v1/register` are tracked per remote IP. After 5 failures in 60s, the IP gets a 60s cooldown (429 with `Retry-After`). Successful auth resets. The agent has nothing to rate-limit (it never accepts inbound connections).

**Destructive-op scope safety (qui-side, layered on top of allowed-roots).**

The allowed-roots list defines *reachability* — what paths the agent can touch. It does not define *destruction policy* — what qui's services should ask the agent to delete. These are separate layers. A user reasonably wants to point the agent at `$HOME` (the seedbox-typical layout) and still expect orphanscan not to chew through their dotfiles.

This safety lives in qui's services, not in `pkg/fsexec`, because the policy is service-specific:

- **Orphanscan** is the only destructive service today, and it is the obvious risk. Walked at `/home/alice`, it would treat every non-torrent file as a deletion candidate — including `~/.ssh/authorized_keys`, which on a seedbox would lock the user out of their own box. Two guards:
  1. **Default ignore-list of well-known sensitive paths**, hardcoded in qui's orphanscan service: dotfiles at the root level (`.ssh`, `.gnupg`, `.config`, `.local`, `.cache`, `.bashrc`, `.profile`, etc.), plus anything matching a configurable glob list. Orphanscan never proposes these for deletion regardless of allowed-roots.
  2. **Broad-root acknowledgement.** When orphanscan's configured target root contains `qBittorrent`'s save paths AND non-save-path content (heuristic: the root or its immediate parent is *not* referenced as a `save_path` in qBit's API for that instance), qui's orphanscan UI requires per-root opt-in: a checkbox on the orphanscan settings page reading "I understand orphanscan will treat any non-torrent file under `/home/alice` as a deletion candidate. Confirm." The agent's allowed-roots policy doesn't change; only orphanscan's willingness to dispatch `fs.removeall` against that scope.
- **Cross-seed inject** is destructive only in the sense that it creates trees and rolls back on failure — it never deletes user content.
- **No other service** dispatches destructive ops in v1.

This is qui-side logic and applies equally to the Local backend. Same policy on a co-located install — just protects against orphanscan-on-`$HOME` via the same heuristics.

## 7. Protocol Spec

All endpoints live on qui (the server). The agent is the client. Versioning at the URL prefix: `/api/agent/v1/...`. Major bumps move to `/v2/`. Capability strings advertised at registration and refreshed on each heartbeat allow additive features without a major bump.

### 7.1 Endpoints (agent → qui)

| Method | Path | Streaming | Auth | Purpose |
|---|---|---|---|---|
| POST | `/api/agent/v1/register` | no | pairing-token | Exchange pairing token for bearer; submit version + capabilities + allowed roots |
| POST | `/api/agent/v1/poll` | no (long-poll) | bearer | Block up to `pollTimeout` waiting for the next job; returns one Job or 204 |
| POST | `/api/agent/v1/result/{requestID}` | NDJSON or JSON | bearer | Post execution result for a previously dispatched job |
| POST | `/api/agent/v1/heartbeat` | no | bearer | Liveness ping; refresh capabilities + version |
| POST | `/api/agent/v1/unregister` | no | bearer | Voluntary deregistration on agent shutdown (best-effort) |

**Note.** The job-payload shapes (`Stat`, `Lstat`, `Walk`, `Statfs`, `SameFS`, `Mkdir`, `Remove`, `RemoveAll`, `TreeCreate`, `TreeRemove`) keep their existing types from §7.2 below — but they no longer correspond to URL paths. They're embedded inside the `Job.Args` field and dispatched through the single `/poll` channel.

### 7.2 Job and Result envelope

```go
package proto

type Job struct {
    RequestID string          `json:"requestID"`           // UUID, qui-generated
    Op        string          `json:"op"`                  // "fs.stat", "tree.hardlink", ...
    Args      json.RawMessage `json:"args"`                // op-specific payload (StatRequest, TreeCreateRequest, ...)
    Deadline  string          `json:"deadline"`            // RFC3339; agent aborts if exceeded
}
// Whether an op streams is determined by Op alone via a static map shared between qui and agent.
// Streaming ops in v1: fs.walk. Both sides know that fs.walk implies NDJSON framing on /result/{id}.

type Result struct {
    RequestID string          `json:"requestID"`
    OK        bool            `json:"ok"`
    Code      string          `json:"code,omitempty"`     // "path_not_allowed", etc.
    Error     string          `json:"error,omitempty"`
    Payload   json.RawMessage `json:"payload,omitempty"`  // op-specific response (StatResponse, TreeCreateResponse, ...)
}
```

For **non-streaming** ops, the agent posts a single `Result` to `/api/agent/v1/result/{requestID}` and qui forwards `Result.Payload` to the waiting `fsops.Remote` caller.

For **streaming** ops (`fs.walk` is the only one in v1), the agent posts NDJSON to `/api/agent/v1/result/{requestID}` with `Content-Type: application/x-ndjson`. Each line is one `WalkEntry`. The final line carries `Done: true` (or an error frame). qui's result handler reads line-by-line and pushes onto the channel returned by `backend.WalkDir(...)`. The HTTP request stays open for the duration of the walk; the agent flushes every N entries (default 256) for backpressure.

### 7.3 Why the job-queue model fits

A naïve approach would have qui POST each FS op directly to the agent. We can't do that in this direction (the agent isn't listening). The dispatcher pattern works because:
- It maps cleanly onto Go's blocking-call interface: `fsops.Remote.Stat(ctx, path)` enqueues a Job, registers a result channel, blocks until the channel fires (or `ctx.Done`).
- A single agent maintains a small pool of long-poll connections (default 4), giving us 4-way parallelism for free without any custom multiplexing.
- Streaming results on `/result/{requestID}` use the same NDJSON framing the original design used; only the *direction* of the streaming HTTP body inverts (agent now POSTs the stream rather than serving it on GET).
- `requestID` provides idempotency tokens for free: if a `/result/{id}` POST fails mid-stream and the agent retries, qui rejects the second attempt with 409 (already received).

**TreePlan atomicity is preserved.** The `TreeCreate` op carries the entire `TreePlan` as `Job.Args`; the agent executes `hardlinktree.Create(plan)` (or `reflinktree.Create(plan)`) in one go and rolls back on partial failure. Network blips during the agent's POST of the result don't corrupt on-disk state — the plan executes fully or rolls back fully before any HTTP traffic leaves the agent. Same atomicity guarantees as today's local code.

### 7.4 Op-specific payload types

The op payloads are unchanged from the prior design — the operations themselves haven't moved, only the dispatch shape. Shared package `pkg/agent/proto`:

```go
package proto

// All paths are absolute. The agent rejects any path not under an allowed root.

// Registration & lifecycle messages (not embedded in Job.Args; they're top-level bodies on the
// /api/agent/v1/{register,heartbeat,unregister} endpoints).

type RegisterRequest struct {
    PairingToken  string   `json:"pairingToken"`
    Bearer        string   `json:"bearer"`        // agent-generated, 32 bytes crypto/rand, base64url
    AgentUUID     string   `json:"agentUUID"`
    AgentVersion  string   `json:"agentVersion"`
    ProtoVersion  string   `json:"protoVersion"`  // "1"
    Capabilities  []string `json:"capabilities"`
    AllowedRoots  []string `json:"allowedRoots"`
    ReflinkRoots  []string `json:"reflinkRoots"`  // subset of AllowedRoots whose filesystem supports CoW reflinks
    Platform      string   `json:"platform"`      // "linux", "darwin", "windows"
    Hostname      string   `json:"hostname"`
}
type RegisterResponse struct {
    InstanceID int `json:"instanceID"`
    // No bearer field: the agent already has the bearer it generated. qui never re-presents it.
}

type HeartbeatRequest struct {
    AgentVersion  string   `json:"agentVersion"`
    Capabilities  []string `json:"capabilities"`
    AllowedRoots  []string `json:"allowedRoots"`  // refreshed every heartbeat; qui's stored copy is informational
    ReflinkRoots  []string `json:"reflinkRoots"`
    InflightOps   int      `json:"inflightOps"`
    MaxInflightOps int     `json:"maxInflightOps"`
    UptimeSec     int64    `json:"uptimeSec"`
}

// Poll envelopes. Every /api/agent/v1/poll POST carries InflightOps/MaxInflightOps so
// qui knows whether it can dispatch a new job. The response carries up to one Job and
// any pending Cancellations for this agent.

type PollRequest struct {
    InflightOps    int `json:"inflightOps"`
    MaxInflightOps int `json:"maxInflightOps"`
}

type PollResponse struct {
    Job           *Job     `json:"job,omitempty"`
    Cancellations []string `json:"cancellations,omitempty"` // requestIDs the agent should abort
}

// Op payloads embedded in Job.Args / Result.Payload below.

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
// Streamed: one WalkEntry per NDJSON line; final line has Done:true
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
    Done      bool   `json:"done,omitempty"`
    Truncated bool   `json:"truncated,omitempty"` // last line if MaxEntries hit
}

type StatfsRequest  struct{ Path string `json:"path"` }
type StatfsResponse struct {
    BytesAvailable int64  `json:"bytesAvailable"`
    BytesTotal     int64  `json:"bytesTotal"`
    Filesystem     string `json:"filesystem,omitempty"` // best-effort
}

// ReadDir is a one-level non-streaming read. Used by callsites that need to inspect
// immediate children without paying the cost of a streamed walk (e.g. disc-layout
// marker detection, parent-dir validation, root accessibility checks).
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
    Truncated bool       `json:"truncated,omitempty"` // true if MaxEntries hit
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

// Note: Job.RequestID is the canonical idempotency token for every op.
// It is not duplicated inside individual op-payload types.

// FileID indexing is performed via fs.walk with WantFileID:true. No separate proto type needed.
```

**Error model.** Two layers, since errors can originate at the HTTP layer or inside an op:
- **Transport errors** (auth, rate limit, malformed body, agent disconnected): qui returns HTTP `400/401/403/409/429/503` to the agent's POST. The agent surfaces these in its log and backs off.
- **Op errors**: the agent always posts a `Result` with `OK: false`, `Code: "..."`, `Error: "..."` to `/api/agent/v1/result/{requestID}`. qui's dispatcher turns the result into a typed Go error for the waiting `fsops.Remote` caller. Stable codes: `path_not_allowed`, `path_not_found`, `permission_denied`, `cross_device`, `tree_partial_rollback`, `version_skew`, `request_too_large`, `internal`, `agent_offline`, `deadline_exceeded`.

**Idempotency.** Where natural, ops are idempotent (`MkdirAll` is, `Stat` is, hardlink-create skips identical existing targets per `pkg/hardlinktree/create.go`). The `Job.RequestID` is the idempotency token end-to-end: the agent caches the last 1024 request IDs (and outcomes) for 5 minutes so duplicate dispatches don't double-act, and qui rejects a second `/result/{id}` POST with 409 once the first has been consumed.

**Per-op timeouts.** `Job.Deadline` is set qui-side per op (default 30s for one-shots, 30 min for trees/walks). The agent aborts and returns `Code: "deadline_exceeded"` if the deadline lapses mid-execution. qui's `fsops.Remote` callers also pass `context.Context` with their own deadlines; if the caller cancels first, qui delivers a cancel through the cancellation protocol (§7.5).

### 7.5 Cancellation, Crash Recovery & Connection Lifecycle

Cancellation is what makes long-running ops (walks, large tree creates) cooperative with qui's request lifecycle. The protocol is built on three invariants.

**Invariant 1 — `poll_concurrency` open polls at all times.**

The agent maintains exactly `poll_concurrency` (default 4) open `/poll` connections. When a poll's response arrives (a job, a cancel set, or 204-idle), the agent **immediately opens a replacement poll** before dispatching the body to its executor. Even when the agent is at `MaxInflightOps` and refusing new jobs, the polls stay open so cancels have a delivery channel. There is no separate "control channel".

**Invariant 2 — Cancels ride on the next poll response, not on any other endpoint.**

When a qui-side caller cancels its `context.Context`:
1. The dispatcher marks the requestID as cancel-pending. The result channel is *not* closed yet — we wait for the agent to confirm.
2. On the next poll from the same agent (which is at most ~1 RTT away under steady state, or up to one in-flight op's completion under saturation), qui returns `PollResponse{Cancellations: [requestID, ...]}`.
3. The agent's executor calls the matching `cancelFunc` for each cancelled requestID. The op's `context.Context` fires.
4. The op exits cooperatively (see Invariant 3) and posts a result via `/result/{requestID}` with `OK: false, Code: "cancelled"` (or, for streamed ops, a final NDJSON frame `{"done":true,"err":"cancelled"}`).
5. qui's dispatcher receives the result, unblocks the original caller with `context.Canceled`, removes the requestID from the pending map.

**Cancels are idempotent.** A cancel for an unknown or already-completed requestID is a no-op. A duplicate cancel logs once and is dropped. There's no separate ack channel — the agent signals cancellation completion by posting the result, same code path as any other op. This collapses the state machine and makes the failure cases trivial: any way the agent can finish an op (success / error / cancel / deadline) flows through `/result/{id}`.

**Latency bound.**
- Under Invariant 1, the agent always has open polls — the replacement poll opens *before* the executor dispatches the body of a consumed poll. There is no "all polls consumed" worst case; the count of open polls is `poll_concurrency` minus the sub-millisecond window between response-arrival and replacement-open.
- End-to-end cancel latency: cancel ride-along on the next poll response (~1 RTT, typically < 50 ms over the public internet) → agent's `cancelFunc` fires → op exits at the next `pkg/fsexec` ctx-check → result POSTed. Total ~100 ms in normal conditions.
- The remaining tail comes from `pkg/fsexec`'s ctx-check granularity: the walker checks ctx between entries, so a cancel mid-walk lands within the time it takes to process one directory entry (microseconds to ms). For atomic ops like `tree.hardlink` mid-creation, the check is between each link; for a 1000-file plan, cancel falls into the rollback path within ~1ms.
- For long-running uncancellable ops (none exist in v1; all `pkg/fsexec` primitives are ctx-aware), the deadline (`Job.Deadline`, default 30s/30min) is the upper bound. No op can run past that even without an explicit cancel.

**Invariant 3 — `pkg/fsexec` primitives are ctx-aware.**

Every `pkg/fsexec` primitive accepts a `context.Context` and checks it at well-defined yield points:
- **Walker:** between every directory entry and on every callback return. A cancel mid-walk stops emitting after the current entry; the executor flushes the final NDJSON frame `{"done":true,"err":"cancelled"}` and closes the body.
- **Batched ops** (`StatBatch`, `LstatBatch`): between each path's syscall.
- **`HardlinkTree` / `ReflinkTree`:** between each `Link` / clone call; on cancel, falls into the existing `Rollback` path automatically. Atomicity preserved.
- **`Remove` / `RemoveAll`:** `RemoveAll` checks ctx between top-level entries; single-file `Remove` is uncancellable (the syscall completes in microseconds).

The contract is documented in `pkg/fsexec` package docs and tested with synthetic mid-op cancels in the property-test suite.

**Crash recovery.**

| Failure | Detection | Recovery |
|---|---|---|
| Agent crashes between dispatch and result | qui's pending-results TTL fires after 5 min | Channel closed with `agent_offline`; caller unblocks with that error |
| Agent crashes mid-NDJSON stream | qui's reader gets EOF/RST on the open `/result/{id}` connection | Stream channel closed with `agent_offline`; caller unblocks; agent's heartbeat staleness fires within 90s and the instance card flips to "stale" |
| qui crashes during agent's `/result/{id}` POST | Agent's POST returns a connection error | Agent's executor logs and gives up — the job is qui's to re-dispatch when qui restarts; agent state is fully reactive, holds nothing across qui crashes |
| qui restarts | All in-memory pending-results lost | Agent's next poll comes back with fresh state; qui has no record of in-flight ops, doesn't need any |
| Agent restarts | Bearer file persists; new poll loop starts | qui sees a heartbeat resume, instance card flips back to "connected"; in-flight pending on qui's side eventually times out and reports `agent_offline` to those callers |
| Network partition | Both sides see connection errors on their open polls / posts | Agent backs off and reconnects (exponential, 5s → 60s); qui surfaces "stale" status; abandoned pending-results sweep on the 5-min TTL |

**Connection lifecycle.**

The agent's poll loop is the single source of truth for "is this agent online":
- On startup: open `poll_concurrency` polls. Each post carries `PollRequest{InflightOps, MaxInflightOps}`.
- On each response: process cancellations first (they don't count against MaxInflightOps), then start the executor on the Job (if any), then open a replacement poll. The replacement happens before the executor blocks the goroutine, so the open-poll count is preserved.
- On 401: log, sleep `60s` (rate-limit sympathy), then retry. After 3 sequential 401s, surface a `re-pair required` log line and back off to once-per-minute polls.
- On 5xx / network error: exponential backoff (5s → 60s, jitter ±20%), reconnect.
- On graceful shutdown (SIGTERM): cancel all running jobs, wait up to 5s for in-flight `/result/{id}` posts, then exit. qui sees the agent go stale on the next heartbeat-window check.

### 7.6 Op input bounds

Per-op caps are enforced server-side (qui rejects with 400 `request_too_large` before enqueuing) and mirrored by qui's `fsops.Remote` callers (refuse to submit beyond cap). Outer envelope is the 16 MiB body cap from §4; per-op inner caps are tighter:

| Op / field | Cap | Rationale |
|---|---|---|
| `StatRequest.Paths` | 1024 | Missing-files runs per-torrent; torrents have hundreds of files, not tens of thousands |
| `LstatRequest.Paths` | 1024 | Same |
| `WalkRequest.IgnorePaths` | 1024 | Orphanscan ignore list is small in practice |
| `WalkRequest.IgnoreDirNames` | 256 | Pattern names, not paths |
| `ReadDirRequest.MaxEntries` | 8192 (default cap) | One-level directory reads are bounded by FS limits in practice; cap protects against pathological cases (`/tmp` with 100k entries) |
| `RemoveRequest.IgnorePaths` | 1024 | Same as walk |
| `TreeCreateRequest.Plan.Files` | 10 000 | A single cross-seed match never exceeds this; if it does, split the plan |
| `WalkRequest.Root` length | 4 096 bytes | Linux PATH_MAX |
| Any single path in any field | 4 096 bytes | Linux PATH_MAX |

If a qui-side caller's batch exceeds a cap, the `fsops.Remote` adapter chunks the call automatically (e.g. `StatBatch(2048)` becomes two sequential `fs.stat` jobs internally; results are merged before returning to the caller). Stage C's per-op tests include a chunking case for `Stat` and `Lstat`.

### 7.7 Content encoding

`Content-Encoding: gzip` is supported in **both directions**:

- **qui → agent** on `PollResponse`: tiny payloads (a Job, maybe cancellations); gzip is allowed but not required.
- **agent → qui** on `/api/agent/v1/result/{requestID}` POST body: NDJSON-streamed walks compress 3–5×; the agent should set `Content-Encoding: gzip` for streamed results when the body would exceed 64 KiB. qui's handler wraps the request body with `gzip.NewReader` when the header is set.

Stdlib transparent encoding is used end-to-end; no third-party dependency.

## 8. fsops Abstraction in qui

This is where the design has the most leverage in the qui codebase. Today, services call `os.*`/`filepath.*`/`unix.*` directly across ~20 files. Stage B introduces the interface and refactors callsites. After Stage B, swapping in the Remote impl is a one-line change in the resolver.

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
    Name      string  // basename only; caller joins with the request path if needed
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
    Err     error // walk errors, including filepath.SkipDir if the impl wants
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
    ReadDir(ctx context.Context, path string, maxEntries int) ([]DirEntry, bool, error) // bool = truncated
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
    Kind         string   // "local" | "remote"
    AgentVersion string   // remote only
    AllowedRoots []string // remote only
    ReflinkRoots []string // subset of AllowedRoots whose underlying FS supports reflinks
    Capabilities []string
}
```

**Two implementations.**

`internal/fsops/local`:
- A thin adapter (~150 lines of typed delegation) over `pkg/fsexec`. The adapter knows about qui-side ergonomics — `<-chan WalkEntry`, `RemoveOptions`, `BackendInfo`, the capability list — and translates them into the simpler callback-shaped primitives in `pkg/fsexec`.
- `pkg/fsexec` is the security-critical layer that holds path safety in *exactly one place*: `ResolveSafe(root, path)`, the allowed-roots policy, `os.Root` wrapping, `openat2(RESOLVE_BENEATH)`, walker with a callback API, and primitives for stat/mkdir/remove/statfs/samefs/fileid. Both the qui-side `Local` adapter and the agent's job executor delegate to it. Two implementations of "is this path allowed" is how subtle CVEs get born; we don't do that.
- `HardlinkTree`/`ReflinkTree`/`RemoveTree` delegate to the existing `pkg/hardlinktree` and `pkg/reflinktree` packages directly (those already have the right shape; no wrapping needed).
- Behavior-preserving. Once Stage B lands, `go test ./... -race -count=3` must pass with no functional change.

`internal/fsops/remote`:
- Talks to the in-process **agent dispatcher** (a sibling subsystem at `internal/agent/dispatcher.go`), *not* directly to the agent. There is no outbound HTTP from `fsops.Remote`; the agent is on the other side of the long-poll, not at a URL `fsops.Remote` can dial.
- Each method does: (1) serialize args to the matching `pkg/agent/proto` request type; (2) call `dispatcher.Submit(ctx, instanceID, op, args)` which generates a `requestID`, drops the `Job` into the per-agent inbox, and registers a result channel (whether the response streams is determined by the op name, looked up in the static op-registry); (3) block on the channel (or `ctx.Done`); (4) deserialize the response payload into the matching response type and return.
- `WalkDir` returns the result channel directly (typed `<-chan WalkEntry`); the dispatcher streams NDJSON entries from the agent's `/result/{id}` POST body straight onto it.
- `SupportsReflink(path)` and `Info(ctx)` are **cache reads** — they consult `agents.reflink_roots` and `agents.capabilities` (refreshed on every heartbeat) rather than dispatching an RPC. The agent already advertised the answer at register time and keeps it current via heartbeats; `fsops.Remote` just looks it up in the locally-cached `Agent` record.
- All other methods cache nothing; every call dispatches a fresh job through the dispatcher.

**Dispatcher (`internal/agent/dispatcher.go`).** New subsystem owned by qui:
```go
type Job struct {
    RequestID string
    Op        string
    Args      json.RawMessage
    Deadline  time.Time
}
type pendingResult struct {
    oneShot chan proto.Result      // for non-streaming ops
    stream  chan<- json.RawMessage // for streamed ops; closed on Done or err
    cancel  context.CancelFunc
}
type Dispatcher struct {
    mu       sync.RWMutex
    inboxes  map[int]chan Job        // keyed by instance ID
    pending  map[string]*pendingResult // keyed by requestID
    // ... metrics, semaphores, etc.
}
// IsStreamingOp(op string) bool is the single source of truth for whether an op streams,
// shared between qui's dispatcher and the agent's executor.
func (d *Dispatcher) Register(instanceID int, queueDepth int) (chan Job, func())
func (d *Dispatcher) Submit(ctx context.Context, instanceID int, op string, args json.RawMessage) (chan proto.Result, <-chan json.RawMessage, error)
func (d *Dispatcher) DeliverOneShot(requestID string, r proto.Result) error
func (d *Dispatcher) DeliverStreamFrame(requestID string, line json.RawMessage) error
func (d *Dispatcher) DeliverStreamDone(requestID string, err error) error
```

The HTTP handlers (`internal/api/handlers/agent.go`) are thin wrappers over the dispatcher: `/poll` reads from the inbox, `/result/{id}` calls `DeliverOneShot` or feeds the stream-frame methods line-by-line.

**Resolver.** New `internal/fsops/pool.go`:
- `type Pool struct { ... }` keyed by `instanceID`.
- `func (p *Pool) GetBackend(ctx, instanceID) (Backend, error)`.
- For instances with no agent registered, returns the singleton `LocalBackend` if `has_local_filesystem_access=true`, else a no-op backend that errors with `"filesystem access disabled for this instance"`.
- For instances with a registered agent, returns a `RemoteBackend` bound to the dispatcher. Cheap; no per-instance state to cache here (the dispatcher holds it).
- Health: derived from `Agent.last_seen_at` rather than an active probe. If `now - last_seen_at > 90s` (3 missed heartbeats), the resolver returns the `RemoteBackend` but it surfaces `agent_offline` errors immediately on Submit. UI shows the staleness via `InstanceErrorStore` exactly like a qBit failure.

**Refactor surface (Stage B).** Callsites that change from `os.Foo` / `filepath.WalkDir` to `backend.Foo`:
- `internal/services/dirscan/scanner.go` (entire walker — biggest refactor; `filepath.WalkDir` callback becomes channel-based `backend.WalkDir`).
- `internal/services/dirscan/fileid_index.go` (uses `WalkDir` with `WantFileID:true`; no dedicated method needed).
- `internal/services/dirscan/inject.go` `createLinkTree` (largest semantic change — calls `backend.HardlinkTree` / `backend.ReflinkTree`; same-FS check becomes `backend.SameFilesystem`).
- `internal/services/dirscan/inject.go` `rollbackLinkTree` → `backend.RemoveTree`.
- `internal/services/orphanscan/walker.go` (entire walker).
- `internal/services/orphanscan/delete.go` (`os.Remove`, `os.RemoveAll`, `os.Lstat` → `backend.Remove` with `RemoveOptions`).
- `internal/services/automations/missing_files.go` (`os.Stat` → `backend.Stat` or `StatBatch`).
- `internal/services/automations/free_space.go` (`unix.Statfs` → `backend.Statfs`). The `FreeSpaceSourceType` enum gains a third value `agentPath`.
- `internal/services/automations/hardlink_index.go` `buildHardlinkIndex` (`os.Lstat` + `pkg/hardlink.GetFileID` per torrent file → `backend.LstatBatch` with `WantFileID:true`). High-volume; the 2-minute cache layer in this file is preserved unchanged — it sits above the Backend boundary.
- `internal/qbittorrent/delete_cleanup.go` `cleanupManagedDeleteTargets` and `pruneEmptyManagedDeleteDir` (`os.Stat` + `os.Remove` on parent dirs → `backend.Stat` + `backend.Remove` with `Recursive:false`). Destructive, so flows through the audit log on the agent side.
- `internal/services/crossseed/service.go` `FindMatchingBaseDir` (`os.MkdirAll` per candidate base dir + `fsutil.SameFilesystem(sourcePath, baseDir)` → `backend.MkdirAll` + `backend.SameFilesystem`). Composite operation — qui-side orchestration over existing primitives; no new op needed.
- `pkg/fsutil/samefs.go` callsites (e.g. `internal/services/dirscan/inject.go`) → `backend.SameFilesystem`.

`pkg/hardlinktree`, `pkg/reflinktree`, `pkg/hardlink`, and `pkg/fsutil` stay where they are. They define the canonical `TreePlan` and the local execution semantics. The `Local` backend depends on them. The `Remote` backend serializes `TreePlan` over the wire, and the agent re-uses the same packages.

## 9. Schema & Instance Model Changes

**Recommendation: keep `has_local_filesystem_access` on `instances` untouched, and put all agent state in two new tables: `agents` (one row per registered agent, 1:1 with instance) and `agent_pairings` (transient one-time pairing tokens).**

Rationale:
- Per-instance agent state has more fields than the prior design (capabilities, allowed roots, last-seen-at, version, platform, hostname). Adding them as columns to `instances` would be column-bloat.
- The 1:1 relationship is enforced by `agents.instance_id` being the primary key.
- `agent_pairings` is naturally separate: it has a TTL and is consumed on first use.
- A future migration could flatten `instances.has_local_filesystem_access` and the existence of an `agents` row into a single `filesystem_access` enum on `instances`. Not a v1 problem.

**New tables.**

```sql
-- Migration 070_add_remote_agent.sql

-- Persistent agent registration. One row per instance with a paired agent.
CREATE TABLE agents (
    instance_id           INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    agent_uuid            TEXT NOT NULL UNIQUE,
    bearer_hash           TEXT NOT NULL,        -- HMAC-SHA256(sessionSecret, "agent-bearer:" + plaintext)
    proto_version         TEXT NOT NULL,        -- "1"
    agent_version         TEXT NOT NULL,
    capabilities          TEXT NOT NULL,        -- JSON array
    allowed_roots         TEXT NOT NULL,        -- JSON array
    reflink_roots         TEXT NOT NULL DEFAULT '[]',  -- JSON array; subset of allowed_roots whose FS supports CoW reflinks
    platform              TEXT NOT NULL,
    hostname              TEXT NOT NULL,        -- self-reported by agent
    registered_from_addr  TEXT NOT NULL,        -- remote_addr qui observed on /register; surfaced in UI
    registered_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at          DATETIME,             -- updated on each heartbeat
    last_seen_from_addr   TEXT,                 -- remote_addr of the most recent heartbeat (catches roaming)
    last_op_at            DATETIME              -- updated on each result delivery
);
CREATE INDEX idx_agents_last_seen ON agents(last_seen_at);

-- Transient pairings. Holds only the pairing token; bearer is agent-generated and never lands here.
CREATE TABLE agent_pairings (
    pairing_token  TEXT PRIMARY KEY,            -- 32 random bytes, base64url; the user-visible secret
    instance_id    INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at     DATETIME NOT NULL,
    consumed_at    DATETIME
);
CREATE INDEX idx_agent_pairings_expires ON agent_pairings(expires_at);
```

A periodic sweep deletes expired/consumed `agent_pairings` rows (re-using qui's existing background-task cadence).

**No bearer plaintext on qui at any point.** The agent generates the bearer at pair time using `crypto/rand` and sends it once over the TLS-protected `/register` POST body. qui hashes on receipt (`HMAC-SHA256(sessionSecret, "agent-bearer:" + bearer)`) and writes the hash to `agents.bearer_hash`. The plaintext exists only in the request handler's stack frame on qui's side, then in agent process memory and on the seedbox's `~/.config/qui-agent/bearer`. A snapshot of qui's SQLite file at any moment contains only hashes.

**No changes to `instances`.** The existing `has_local_filesystem_access` bool stays. The new `agents` row determines "remote" mode for an instance.

**Model files.**
- `internal/models/agent.go` (new): `Agent` struct, `AgentStore` (Create/Get/Update/Delete/UpdateLastSeen).
- `internal/models/agent_pairing.go` (new): `AgentPairing` struct, `AgentPairingStore` (Create/Consume/DeleteExpired).
- `internal/models/instance.go`: unchanged (no new fields).

**Helper.** `internal/models/instances.HasFilesystemAccess(ctx, instance) (bool, FilesystemMode)` returns true if `has_local_filesystem_access || agentExistsForInstance(instance.id)`, plus the mode (`"local" | "agent" | "none"`). All existing `HasLocalFilesystemAccess` callsites get migrated to this helper.

**Handler DTOs.**
- `POST /api/instances/{id}/agent/pairing` — generates a pairing token, returns the pairing string + `expiresAt`. (Replaces the prior "Test connection" endpoint.)
- `DELETE /api/instances/{id}/agent/pairing` — voids any open pairing token for this instance (used by the modal Cancel button).
- `DELETE /api/instances/{id}/agent` — unlinks (deletes the `agents` row + any open pairing).
- `POST /api/instances/{id}/agent/rotate-bearer` — re-pair semantics: deletes the existing `agents` row in the same transaction that creates a new `agent_pairings` row. The old bearer's hash is gone immediately; old polls return 401 on next attempt. The new pairing string is returned for the user to run on the seedbox. PK constraint on `agents.instance_id` is satisfied because the row is removed before the new one is inserted by `/register`.
- `GET /api/instances/{id}/agent` — returns `{registeredAt, registeredFromAddr, lastSeenAt, lastSeenFromAddr, lastOpAt, agentVersion, capabilities, allowedRoots, reflinkRoots, platform, hostname, inflightOps}` for the instance card.

The agent-side endpoints (`/api/agent/v1/...`) live in a new `internal/api/handlers/agent.go` and don't go through the per-instance routing.

**Tests.** Three-way table test for filesystem mode (none / local / agent). Pairing-flow test (generate pairing → register → bearer arrives at agent). Bearer-rotation test (old bearer 401s after rotate).

## 10. Agent Binary

**Location.** `cmd/qui-agent/main.go`, sibling to `cmd/qui/`.

**Shared types.** `pkg/agent/proto` (not `internal/`) because the agent binary itself is "external tooling" that imports it. Keeps the door open for downstream tools (debug CLI, smoke tests) to import the same wire types.

**Build target.** Separate make target `make agent`, separate Go build (`go build -o qui-agent ./cmd/qui-agent`). The agent binary's import graph is exactly:
- `cmd/qui-agent/...` (and `cmd/qui-agent/internal/...` subpackages — Go's `internal/` rules permit this and keep agent code unimportable from elsewhere)
- `pkg/agent/proto`
- `pkg/fsexec` (path safety + FS primitives, shared with qui's `Local` backend)
- `pkg/hardlinktree`, `pkg/reflinktree`, `pkg/hardlink`, `pkg/fsutil`

No SQLite, no embedded frontend, **no top-level `internal/` dependencies**. The CI import-graph guard (Stage A) enforces this. Single-digit-MB binary; qui's main binary unchanged in size for users who don't run the agent.

**Unprivileged by design.** The agent runs as a regular user — no root, no system-wide install, no Docker required. Default paths follow the XDG Base Directory spec; the only state the agent needs lives under the user's `$HOME`. This is the canonical seedbox install model; system-wide installs are an option, not the default.

**Config (`$XDG_CONFIG_HOME/qui-agent/config.toml`, default `~/.config/qui-agent/config.toml`).**

```toml
[qui]
url              = "https://qui.example.com"   # set by `qui-agent pair`
cert_pin         = ""                           # optional sha256 of qui's leaf cert (self-signed qui only)
poll_concurrency = 4                            # parallel long-poll connections
poll_timeout     = "30s"                        # qui returns 204 if idle this long; agent reconnects

[auth]
# Plaintext bearer (mode 0600). Written by `qui-agent pair`. Required to dial qui.
bearer_file = "${XDG_CONFIG_HOME}/qui-agent/bearer"

[fs]
allowed_roots = ["/home/seedbox-user/data", "/home/seedbox-user/seed"]

[limits]
walker_concurrency = 4
max_inflight_ops   = 32           # cap on concurrent jobs the agent will accept from qui
op_timeout         = "30s"
tree_op_timeout    = "30m"
walk_op_timeout    = "30m"

[logging]
level         = "info"
log_file      = "${XDG_STATE_HOME}/qui-agent/agent.log"   # default ~/.local/state/qui-agent/agent.log
audit_file    = "${XDG_STATE_HOME}/qui-agent/audit.log"
rotate_max_mb = 50
rotate_keep   = 5
```

Resolution order: CLI flag > env (`QUI_AGENT_QUI_URL` etc.) > TOML > XDG default. `qui-agent serve --qui-url https://... --bearer-file /tmp/x` runs ad-hoc for local testing.

**No TLS server config.** The agent does not listen for incoming connections. There is no cert/key to manage. TLS verification on the *outbound* dial uses the system trust store unless `cert_pin` is set, in which case standard verification is replaced with a leaf-cert SHA-256 check (hostname verification skipped — pinning the leaf is strictly stronger). qui's typical deployment is behind Caddy/Traefik/Nginx with a CA-signed cert, in which case `cert_pin` stays empty.

**Bearer at rest is plaintext mode 0600.** No encryption-at-rest. This is a deliberate v1 decision, not a placeholder. The threat model:
- Anyone who can read `~/.config/qui-agent/bearer` (mode 0600) has either shell as the user or root on the seedbox. Both already imply full control of the qBittorrent payload, which is the data the agent would be brokering anyway. The bearer is the least of the user's worries at that point.
- Adding encryption with a key the agent can read automatically (e.g. derived from machine ID, a sibling file, etc.) is security theater — the attacker reads both files.
- Adding encryption with a key the user must enter (passphrase, hardware token) breaks systemd / `@reboot` cron / unattended restart. The agent stops being a daemon.
- Matches every CI runner (GitHub Actions, GitLab Runners, Drone, Jenkins agents) and every seedbox tool today.

**Future opt-in:** if there's demand for kernel-keyring storage on Linux, `[auth] keyring = true` is a Stage D extension. The bearer would live in the user's session keyring; the daemon would `request_key` at startup. Linux-only, so it stays opt-in. Out of scope for v1.

**Log rotation is in-process** (size-based, defaults above). Seedbox users typically cannot configure system-wide `logrotate`; the agent never depends on it.

**Subcommands.**
- `qui-agent pair <pairing-string> [--root /path ...]`: decode the string, generate a 32-byte bearer locally, dial qui's `/api/agent/v1/register`, persist config + bearer + allowed roots, exit. **Error paths:**
  - Pairing string expired → `pairing-token expired; generate a new one in qui` (exit 1).
  - TLS verification fails and `quiCertPin` is empty → `qui's TLS certificate isn't trusted by this system. Either install qui's cert as a trusted CA, or generate a new pairing string with cert pinning enabled (qui's instance settings show this option when qui is on a self-signed cert)` (exit 1).
  - TLS verification fails and `quiCertPin` is set but doesn't match → `qui's leaf certificate fingerprint doesn't match the pinned value. Cert may have been rotated; generate a new pairing string` (exit 1).
  - qui returns 4xx → print qui's error body verbatim, exit 1.
- `qui-agent serve`: open `poll_concurrency` long-poll connections to qui, send heartbeats every 30s, execute jobs as they arrive. **Error paths:**
  - No bearer file at startup → `no bearer found at ~/.config/qui-agent/bearer. Run 'qui-agent pair <pairing-string>' first; get the pairing string from your qui instance: Instance → Edit → Filesystem access → Remote agent → Generate pairing string` (exit 1).
  - Persistent 401 (3 sequential auth failures) → `bearer rejected by qui. Re-pair required: run 'qui-agent pair <new-string>' with a fresh pairing string from qui's instance settings` (continues to back off polls but logs prominently).
- `qui-agent unpair`: post `/api/agent/v1/unregister` (best-effort), wipe `bearer` file and `config.toml` regardless of POST outcome (network errors and qui-down don't block local cleanup), exit 0. Local cleanup is the primary purpose; the unregister POST is courtesy.
- `qui-agent status`: print last-poll/last-result timestamps, current in-flight job count, qui URL, agent UUID. Useful for support diagnostics.
- `qui-agent version`.

**Bearer rotation is qui-driven**, not agent-driven: the user clicks "Rotate bearer" on the instance card in qui, qui issues a new pairing string, and the user re-runs `qui-agent pair <new-string>`. There is no `rotate-token` or `rotate-cert` subcommand on the agent side anymore — the agent doesn't have any independent secret to rotate.

**Ship artifacts.** Single static Go binary. No external dependencies, no privileged install paths required. Same release pipeline as qui:
- **Tarball** (`qui-agent_${VERSION}_${OS}_${ARCH}.tar.gz`) — extract, drop in `~/bin/`, run. This is the canonical install path for shared seedboxes.
- *Optional* user-systemd unit at `~/.config/systemd/user/qui-agent.service`. Bring up with `loginctl enable-linger $USER && systemctl --user enable --now qui-agent`. Works on seedboxes that allow per-user systemd (Whatbox and others).
- *Optional* `@reboot` crontab snippet for boxes without user-systemd: `@reboot ~/bin/qui-agent serve >> ~/.local/state/qui-agent/agent.log 2>&1`. Ubiquitous fallback.
- *Optional* system-wide systemd unit (`User=qui-agent`, `ProtectSystem=strict`, `ReadWritePaths=` allowed roots, `NoNewPrivileges=yes`, `PrivateTmp=yes`) for self-hosted machines where the operator has root.
- *Optional* Dockerfile + `linuxserver`-style image with `PUID/PGID` for users who want it.
- *Optional* `com.autobrr.qui-agent.plist` for launchd (macOS).

None of these are mandatory. The binary plus any way to keep it running suffices; tmux/screen sessions also work for ad-hoc deployments. The "supervisor menu" is exposed in the install docs in priority order: user-systemd → `@reboot` cron → manual.

**Why no Windows service in v1.** Ship the binary, but don't bother with an MSI. Windows seedboxes are uncommon enough that documenting `nssm` or Task Scheduler is fine.

## 11. Pairing / Onboarding Flow

End-to-end:

1. **In qui UI** (any browser): Instance → Edit → "Filesystem access" → "Remote agent" → "Generate pairing string". qui creates an `agent_pairings` row (10-min TTL) and shows a modal with:
   - The pairing string (`qui-pair_v1_…`) with a copy button.
   - The exact command to run on the seedbox: `qui-agent pair <string> --root ~/data --root ~/seed`.
   - A live status: "Waiting for agent to register… (9:42 remaining)".
2. **On the seedbox** (no root required):
   - User SSHs in.
   - Drops the `qui-agent` binary into a personal directory (e.g. `curl -L … | tar xz -C ~/bin`).
   - Runs `~/bin/qui-agent pair <pairing-string> --root ~/data --root ~/seed`.
3. The `pair` subcommand:
   - Decodes the pairing string. Validates `expiresAt`.
   - **Generates a 32-byte random bearer locally** with `crypto/rand`.
   - Dials qui's `/api/agent/v1/register` over HTTPS (using `quiCertPin` if present, else system trust).
   - Submits the full `RegisterRequest` (§5, §7.4): `{pairingToken, bearer, agentUUID, agentVersion, protoVersion, capabilities, allowedRoots, reflinkRoots, platform, hostname}`.
   - qui validates the token, persists the `agents` row (capturing `registered_from_addr` from the request), returns `{instanceID}`. Bearer plaintext never lands on qui's disk; the agent already has it.
   - Agent writes `~/.config/qui-agent/{config.toml, bearer}` (mode 0600), prints a summary, exits.
4. **Back in qui UI**: the modal flips to "✓ Paired with agent on `seedbox.example` (vX.Y.Z, 4 capabilities, 2 roots)" automatically (qui polls the pairings row).
5. **On the seedbox**: user starts `qui-agent serve` under their preferred supervisor — any option from §10 (user-systemd if available, `@reboot` cron otherwise, system-wide systemd or Docker if root is present, tmux/screen for ad-hoc).
6. The agent opens `poll_concurrency` long-poll connections to qui. The instance card flips to "Remote agent: connected (last seen 2s ago)".

Total user actions: one paste, one shell command. No URLs, fingerprints, or tokens to copy across machines.

**Re-pairing.** If the user reinstalls the agent or migrates seedboxes, they click "Re-pair" on the instance card. qui invalidates the existing bearer hash, generates a new pairing string, and the same flow runs. Old bearer 401s on next poll.

## 12. Versioning & Capability Negotiation

**Two layers.**

**Proto version** lives in the URL prefix (`/api/agent/v1`). Bumping to `/v2` is a hard break: qui won't accept a v2 agent's register call until qui itself ships v2 server code. v1 is forever.

**Capabilities** are additive. The agent advertises its capability list in the `RegisterRequest` and refreshes it on every `Heartbeat`:
```
["fs.stat", "fs.lstat", "fs.readdir", "fs.walk", "fs.fileid", "fs.statfs", "fs.samefs",
 "fs.mkdir", "fs.remove", "fs.removeall",
 "tree.hardlink", "tree.reflink", "tree.remove"]
```

`fs.fileid` is a flag, not an op — it means the agent can populate `WalkEntry.FileID` (and `LstatEntry.FileID`) when the request asks for it. Required by dirscan and orphanscan even though they don't dispatch a `fs.fileid` op directly.

qui persists the latest `capabilities`, `allowed_roots`, and `reflink_roots` on the `agents` row and consults them before dispatching a job. Capabilities answer "can the agent perform this op at all?"; the root lists answer "is the specific path eligible for this op?". A static map of `feature → required_capabilities` lives in qui code:
- Cross-seed inject hardlink → `tree.hardlink`, `fs.samefs` (pre-flight check picks the link-tree base dir)
- Cross-seed inject reflink → `tree.reflink`, `fs.samefs`, **plus** the chosen base dir must be under one of `agents.reflink_roots` (CoW support is per-filesystem, not per-agent)
- Dirscan → `fs.walk`, `fs.fileid`, `fs.readdir` (`fs.readdir` for disc-layout marker detection and root accessibility checks)
- Orphanscan → `fs.walk`, `fs.fileid`, `fs.readdir`, `fs.remove`, `fs.removeall` (`fs.readdir` for disc-parent validation; `fs.remove` for single orphan files; `fs.removeall` for orphan directories)
- Missing-files automation condition → `fs.stat`
- Free-space automation condition (path source) → `fs.statfs`
- Hardlink-scope automation condition → `fs.lstat`, `fs.fileid`
- Managed delete cleanup → `fs.stat`, `fs.remove`

If a required capability is missing when the user enables a feature: hard block in the UI with a specific message ("Your remote agent (v0.0.5) doesn't support `tree.reflink` (required for reflink cross-seed mode). Upgrade the agent and retry."). If the chosen base dir is not under `reflink_roots`, the UI surfaces the same hard-block style message ("This filesystem doesn't support reflinks; pick a different base directory or fall back to hardlink mode"). If the agent dispatches a job whose op isn't in the capability list, qui rejects internally with `version_skew` rather than enqueueing.

**Register-time gating.** qui rejects `register` from an agent whose `protoVersion` doesn't match a supported value. This catches major-version skew before any work is dispatched.

**Semver.** `MAJOR` bumps the proto version (`/v1` → `/v2`). `MINOR` adds capabilities (additive). `PATCH` is fixes that don't change the wire. Capabilities never disappear within a major version.

## 13. Concurrency, Backpressure & Rate Limits

**qui side.**
- Per-agent inbox is a buffered channel (default capacity 32). When full, `dispatcher.Submit` blocks (or fails fast on `ctx.Done`). High-volume callers (dirscan, orphanscan) mostly run one streaming op at a time, so the cap is a safety valve, not a bottleneck.
- The pending-results map enforces a 5-min TTL on dispatched-but-undelivered ops. An agent that disappears mid-job has its in-flight results closed with `agent_offline` after the heartbeat-staleness threshold.
- Per-instance: dispatcher tracks `inflight_count`; when the agent's reported `max_inflight_ops` cap (from heartbeat) is hit, qui pauses dequeue until a result lands. This avoids overwhelming agents on slow disks.
- Per-call timeouts via `context.Context` from the caller; `Job.Deadline` reflects the soonest deadline.
- Bearer-failure backoff: an agent posting a bad bearer to `/poll` gets a per-IP 60s cooldown after 5 failures in 60s (§6).

**Agent side.**
- Walker concurrency cap (default 4 concurrent walks).
- `poll_concurrency` (default 4) parallel long-poll connections, kept open at all times so cancels always have a delivery channel (§7.5 Invariant 1).
- `max_inflight_ops` (default 32) is the agent's local safety valve. The agent surfaces it on every `PollRequest`; qui consults it before dispatching a new job. Cancels are not bounded by this cap.
- Streamed result POSTs flush every N entries (default 256) and on any walker callback that takes > 50 ms.

**Cancellation.** Detailed in §7.5. Summary: cancels ride on the next poll response, propagate through `pkg/fsexec` via `context.Context`, and complete through the same `/result/{id}` path as any other op. Idempotent on duplicate cancels; no separate ack channel.

**Request ID propagation.** Every `Job.RequestID` is a UUID generated on qui side. The agent's audit log records it; qui's logs record it. End-to-end correlation in support cases is free.

**Heartbeat rhythm.** 30s interval. After 3 sequential heartbeat failures (network errors or non-2xx responses), the agent treats qui as likely down and backs off polls to once-per-minute until qui responds. qui treats `last_seen_at < now - 90s` (3 missed heartbeat windows) as "agent disconnected" and surfaces it on the instance card.

## 14. Observability

**Agent.**
- Structured zerolog (matches qui's choice) JSON output to `agent.log`.
- Separate `audit.log` containing destructive ops only. JSON lines: `{ts, op, path, request_id, qui_url, outcome, error}` (`error` populated only on `outcome: "failure"`). Format mirrors §6's audit-log spec exactly.
- Optional Prometheus metrics via a local-only `--metrics-addr 127.0.0.1:9090` (loopback-only by default; the agent does not expose anything publicly). Counters: `qui_agent_jobs_total{op,outcome}`, `qui_agent_walk_entries_total`, `qui_agent_destructive_ops_total{op}`, histograms `qui_agent_job_duration_seconds_bucket{op,le}`, `qui_agent_poll_wait_seconds_bucket`.
- `qui-agent status` subcommand prints the same data on demand (no listening socket needed for diagnostics).

**qui.**
- Agent connection status is **derived from `agents.last_seen_at`**. Background sweeper (`internal/agent/sweeper.go`) runs three independent tickers:
  - **Staleness** (every 30s, threshold 90s): flags agents with `last_seen_at < now - 90s` as stale, writes an `InstanceErrorStore` entry. Cleared on the next heartbeat. UI banner reuses qui's existing failure-surface plumbing.
  - **Pairings cleanup** (every 5min): deletes `agent_pairings` rows where `expires_at < now` or `consumed_at IS NOT NULL AND consumed_at < now - 5min` (give the consume + register path a small grace window).
  - **Pending-results TTL** (every 30s, threshold 5min): closes any in-memory `pendingResult` whose dispatch was more than 5 minutes ago. The corresponding `fsops.Remote` caller's channel is closed with `agent_offline`.
  - All three live in one file with shared structured-log fields so operators see a coherent "sweeper activity" stream.
- New "Filesystem backend" status row on each instance card:
  - `Local` (using qui-host filesystem)
  - `Remote agent: connected (vX.Y.Z, last seen 2s ago, 4/32 inflight)`
  - `Remote agent: stale (last seen 4 min ago)`
  - `Remote agent: not paired`
- Dispatcher emits structured logs per job: `instanceID`, `agentUUID`, `op`, `requestID`, `durationMS`, `outcome`. New Prometheus counters on qui's existing `/metrics` endpoint: `qui_agent_dispatched_total{op,outcome}`, `qui_agent_inflight_jobs{instanceID}`, `qui_agent_queue_depth{instanceID}`, `qui_agent_heartbeat_age_seconds{instanceID}`.
- The automations activity ledger already exists; agent jobs flow through it transparently because activity logging is at the service layer, above the backend interface.

## 15. Frontend Changes

**`web/src/components/instances/InstanceForm.tsx` (currently has a single `hasLocalFilesystemAccess` toggle near line 200).**

Replace the toggle with a `RadioGroup`:

```
Filesystem access
( ) None — qui will not touch the filesystem for this instance
( ) Local — qui runs on the same host as qBittorrent
( ) Remote agent — qui dispatches FS ops to a qui-agent on the qBittorrent host
```

When "Remote agent" is selected, the form shows different states depending on whether an agent is already paired:

**No agent paired yet:**
- Single button: **"Generate pairing string"**. On click: posts to `POST /api/instances/{id}/agent/pairing`, opens a modal.

**Pairing modal:**
- The pairing string in a monospace block with a copy button.
- The exact shell command to run on the seedbox (with the user's chosen roots interpolated, defaulting to `~/data` and `~/seed`).
- A "Required capabilities" preview: lists which agent capabilities the instance will need based on its current cross-seed/dirscan/orphanscan/automations settings. Hints if the user has enabled reflink mode or hardlink mode.
- **Scope warning** (only shown when the chosen roots include `$HOME`, `/home/$USER`, or any 2-component path that names a user home): "Root `/home/alice` covers your full home directory. Orphanscan will require explicit per-root acknowledgement before it operates here (see §6). For tighter scope, list specific torrent data subdirectories instead — e.g. `~/data`, `~/seed`."
- A live "Waiting for agent to register…" spinner with countdown (10:00 → 0:00). Polls `GET /api/instances/{id}/agent` every 2s; when the agent registers, the modal flips to a green confirmation view that prominently displays the **hostname and registering IP**, e.g.:
  > ✓ Paired with `seedbox.example` at `198.51.100.42` (v0.1.0, 4 capabilities, 2 roots).
  > Re-pair if this isn't your seedbox.
  The "Re-pair" link is surfaced in the same modal so a user who notices a mismatch can rotate the bearer in one click. The modal does not auto-close; the user dismisses it explicitly. This gives the user a beat to verify the IP / hostname match what they expect.
- Cancel button voids the pairing token.

**Agent paired:**
- Read-only summary card: agent UUID (truncated), version, platform, hostname, allowed roots, reflink-supported roots, capabilities, "last seen Xs ago" (from `lastSeenAt`), "last op Xm ago" (from `lastOpAt`), in-flight ops count.
- **Connection-source row** (key for spotting a roaming or replaced agent): "Registered from `198.51.100.42` on Apr 27" (from `registeredFromAddr`/`registeredAt`) and, if different, "Last heartbeat from `198.51.100.99`" (from `lastSeenFromAddr`). A mismatch between registered and last-seen address surfaces as a yellow info banner — could be benign (DHCP rotation, IPv6 vs IPv4 fall-through) or a sign that a different agent has taken over the bearer. The user clicks through to a "Re-pair if this isn't expected" action.
- Three buttons: **"Test pairing"** (dispatches a `diag.echo` job; UI shows "Test successful (round-trip 42ms)" or the error), **"Re-pair"** (calls `POST .../rotate-bearer`, which deletes the existing `agents` row and issues a fresh pairing string), **"Unlink"** (deletes the registration), and a tertiary link "View activity" (filtered dispatcher logs — nice-to-have).
- **Capability-downgrade banner.** When `agents.capabilities` (refreshed by each heartbeat) drops a capability the instance currently relies on, surface a red banner: "Reflink mode is enabled but your agent (v0.0.5) no longer reports `tree.reflink` support. Cross-seed inject is blocked until you upgrade the agent or disable reflink mode." Pre-flight checks in qui's services use the same capability list — they refuse to dispatch the job and return `version_skew` to the caller, which surfaces in the cross-seed UI as a clear error rather than a silent failure.

**TypeScript types (`web/src/types/index.ts`).** New `Agent` type — must match `GET /api/instances/{id}/agent` response from §9:
```ts
type Agent = {
  agentUuid: string
  agentVersion: string
  protoVersion: string
  capabilities: string[]
  allowedRoots: string[]
  reflinkRoots: string[]
  platform: 'linux' | 'darwin' | 'windows'
  hostname: string
  registeredAt: string
  registeredFromAddr: string
  lastSeenAt: string | null
  lastSeenFromAddr: string | null
  lastOpAt: string | null
  inflightOps: number
}
```
The `Instance` type stays unchanged on its existing fields; the agent record is fetched via a separate `useAgent(instanceId)` hook.

**Capability hints in the form.** Same as before — when the agent's reported capabilities don't cover something the user has enabled (`tree.reflink` while `useReflinks: true`), surface an inline warning: "Reflink mode is enabled but your agent doesn't support `tree.reflink`. Upgrade the agent or disable reflink mode."

## 16. Security Model & Threat Model

**Trust boundary.** qui authenticates the agent's bearer on every request. The agent verifies qui's TLS cert (CA-backed by default; pinned via `cert_pin` in self-signed deployments). Both sides treat the bearer as a long-lived shared secret, generated by qui at pairing time.

**Adversaries considered.**

*Stolen bearer (off the seedbox).*
- The bearer is on the seedbox at `~/.config/qui-agent/bearer` (mode 0600). An attacker who can read it has already gained shell on the seedbox under the agent's UID — at that point qBittorrent's data is already exposed regardless of the agent.
- An attacker on a *different* host with the bearer can dial qui's `/api/agent/v1/poll` and impersonate the agent. They can't *cause* destructive ops (qui only dispatches what qui's services request), but they can:
  - Drain the job inbox: receive jobs and return false results, breaking the user's workflows.
  - Lie about FS state (e.g. claim files are missing when they aren't), confusing missing-files automation.
- Mitigations: bearer rotation by qui (one click → re-pair), per-IP auth-failure rate limit, audit-log on the agent (a real seedbox) shows the actual on-disk ops the impersonator caused vs. didn't.
- **The attacker cannot cause file deletion that qui didn't already request.** Destructive ops originate qui-side from user-driven flows (orphanscan, cross-seed inject). A bearer alone doesn't let the attacker make qui dispatch new destructive jobs.

*MITM on the wire.*
- Standard TLS verification (CA-signed qui) prevents this everywhere CAs work.
- `cert_pin` covers self-signed qui deployments; pinning the leaf is strictly stronger than CA validation since it ignores trust anchors.
- The agent does not fall back to plain HTTP. Ever.

*Compromised seedbox.*
- An attacker who roots the seedbox owns the qBittorrent payload. The agent doesn't expand the data blast radius — qui dispatches commands to a host the user already trusts with their data.
- qui's exposure: a malicious agent can return false results, refuse jobs, or replay bearer-authorized polls until rotated. They cannot pivot inbound to qui's other endpoints (the bearer is scoped to `/api/agent/v1/*` by handler-level check; the same bearer doesn't authenticate any UI/API endpoint).
- The audit log on the agent is on the seedbox itself, so a rooted seedbox can rewrite it. Cross-checking is operator's job (compare agent audit log against qui's dispatcher log if you suspect compromise).

*Compromised qui host.*
- Already game-over for qui's local secrets and qBit tokens. An attacker who reads `sessionSecret` can verify any bearer hash but cannot recover bearer plaintexts (HMAC is one-way; the agent-generated bearer plaintext never resides on qui's disk at any point — see §5 and §9).
- The attacker can issue new pairing strings and pair their own malicious agent against the user's instances. They already have the qui DB at this point, so the marginal harm is the seedbox-side filesystem operations against allowed roots — bounded by the operator's allowed-roots policy on the agent.
- The blast radius on the seedbox side is bounded by allowed roots: a fully compromised qui can dispatch deletion jobs only against the directories the seedbox operator allowed. `O_NOFOLLOW` + `RESOLVE_BENEATH` ensure qui can't talk the agent into touching anything outside.

*Replay / denial of service.*
- `requestID` dedup (5-min cache, 1024 most recent) prevents accidental double-fire from agent retries on `/result/{id}`.
- Per-IP auth-failure rate limit on qui prevents bearer brute force.
- A malicious agent can DoS qui's dispatcher by holding open many polls without ever returning. Mitigations: per-agent connection cap (`poll_concurrency` agreed at register time, default 4), idle-timeout on each poll (default 30s).

*Allowed-roots misconfiguration.*
- The allowed-roots list defines the agent's *reachability scope*, not authorization for destructive ops. The agent runs as the user and already has filesystem access to everything under their UID; allowed-roots narrows what qui can drive *via this agent*. See §6's "Destructive-op scope safety" paragraph for the layer that protects against well-known-but-broad scopes.
- Refuse single-component paths (`/`, `/data`, `/mnt`) and well-known sweeping parents (`/home`, `/Users`, `/var`, `/etc`, `/usr`). These expose far more than the user owns — they are accidents waiting to happen. Roots must have ≥ 2 components and must not name a system parent.
- `$HOME` (e.g. `/home/alice`) is allowed. The agent's UID already has full access to it; refusing it would force the user into a paperwork dance for no security gain. Destructive-op safety on `$HOME`-as-root is the orphanscan layer's job (see §6).
- No `~`, no relative paths. Roots must be absolute and `Cleaned`.

*Pairing-token leak.*
- A leaked pairing string (e.g. screenshared, pasted into chat by mistake, browser extension/malware on the qui machine) lets anyone who can reach qui claim the bearer first and become the registered agent. The malicious agent doesn't have access to the user's actual seedbox files — it's a separate machine — but it can drain dispatched jobs, return false results, and confuse qui's services until the user re-pairs.
- **Mitigations layered to maximize the chance the user catches it:**
  1. **10-min TTL + single-use** on the pairing token. The window is bounded.
  2. **Modal displays the registering IP and hostname prominently** (not a small chip) the moment the agent registers. A user who knows their seedbox's IP catches a mismatch at a glance.
  3. **Modal does not auto-dismiss.** The user explicitly closes it after verifying. Auto-close was a footgun on mobile / hurried users.
  4. **One-click "Re-pair" available in the same modal.** If the displayed values don't match, the user invalidates the malicious bearer immediately without hunting for the option in instance settings.
- **Known accepted risk:** the malicious agent's bearer is *active* between registration and re-pair (typically seconds to minutes). qui's services may dispatch a small number of jobs during this window; the malicious agent returns false results. Damage is bounded by qui's own service logic — the agent can't *cause* destructive ops it wasn't asked to do, only return wrong answers about file states. A future iteration could add a confirm-before-finalize gate (modal click required to activate the bearer); the design space is documented but not in v1 scope.

## 17. Compatibility & Migration

- Existing instances continue to use `Local` backend if they had `has_local_filesystem_access=true`, no-op backend otherwise. Zero behavior change.
- Migration 070 only adds two new tables. No changes to `instances` and no data migration.
- Frontend: the radio group's default selection mirrors the current bool: existing instances with `hasLocalFilesystemAccess=true` show as "Local", others show as "None".
- Existing `HasLocalFilesystemAccess` checks (e.g. `internal/proxy/handler.go`, `internal/qbittorrent/sync_manager.go`, `internal/api/handlers/dirscan.go`, `internal/api/handlers/automations.go`, `internal/services/dirscan/inject.go`, `internal/services/automations/hardlink_index.go`, `internal/qbittorrent/delete_cleanup.go`) get a small refactor: instead of `if !instance.HasLocalFilesystemAccess { reject }`, they call a helper `instances.HasFilesystemAccess(ctx, instance) (bool, FilesystemMode)` that returns true if the bool is set OR an `agents` row exists for the instance, plus the mode (`"local" | "agent" | "none"`). Gating language unchanged.

## 18. Phased Implementation Plan

The phasing is organized around **internal-correctness milestones**, not user-shipping milestones. Nothing is exposed to users until the entire system is provably correct end-to-end. Hardening, observability, and the integration-test harness are intrinsic to Stage A — they're how we know the platform works — not bolted on as a final phase. Stages A and B can run in parallel once `pkg/agent/proto` and `pkg/fsexec` are stable; Stage C joins them; Stage D is release engineering.

### Stage A — Platform foundations + CI/test infrastructure

Build everything that doesn't change as filesystem features come and go. The deliverable is a paired agent that can dispatch a single synthetic `diag.echo` job round-trip — no real FS ops yet. The platform is what subsequent stages consume.

**CI / lint / test foundations.** Built first, in Stage A, so every later commit benefits.
- New CI workflow `agent-ci.yml`:
  - **Cross-compile matrix** for `cmd/qui-agent`: linux/{amd64,arm64}, darwin/{amd64,arm64}, windows/amd64. Build artifacts uploaded for smoke testing.
  - **`make test-agent`**: unit tests for `pkg/fsexec`, `pkg/agent/proto`, `internal/agent`, `cmd/qui-agent/...` under `-race -count=3` (per CLAUDE.md).
  - **`make test-integration-agent`**: dual-backend harness runs against the synthetic `diag.echo` op (Stage C extends this for every real op).
  - **`make lint-agent`**: golangci-lint v2 on the new packages, applying the existing project profile (dupl, gocognit, funlen, errcheck, gocritic, etc.).
  - **Import-graph guard**: a small Go check in CI that runs `go list -deps ./cmd/qui-agent/...` and fails the build if any imported package matches `^github.com/autobrr/qui/internal/(?!agent/...)`. Catches accidental boundary crossings at PR time.
  - **Path-safety property tests** (`pkg/fsexec` with random `..`/symlink/devicemount injection): runs on every PR. Non-trivial runtime; gets its own job.
  - **Dispatcher race / fan-out stress test**: long-running variant exercising large concurrent in-flight job counts and forced agent-disconnect mid-stream. Runs post-merge on `main`, not per-PR.
- New `Makefile` targets: `make agent`, `make test-agent`, `make test-integration-agent`, `make lint-agent`. All chained from `make test` and `make lint` so the existing developer workflow remains a single command.
- A reusable test fixture, `testutil/agentfixture`, that boots an in-process qui dispatcher, spawns `cmd/qui-agent serve` as a subprocess, programmatically pairs them, and returns a `Backend` that drives the agent. Stage C reuses this fixture verbatim for every op test — no copy-paste integration scaffolding.

**Platform code.**
- `pkg/fsexec/`: `ResolveSafe(root, path)`, allowed-roots policy, `os.Root` wrapping, `openat2(RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS)`, `O_NOFOLLOW`, `..` rejection, device-ID guard, walker with a callback API, primitives for stat/lstat/mkdir/remove/statfs/samefs/fileid. Path safety lives here, exactly once.
- `pkg/agent/proto/`: `Job`, `Result`, `RegisterRequest/Response`, `HeartbeatRequest`, op-payload type sketches.
- `internal/agent/dispatcher.go`: per-instance inbox, pending-results map, cancellation propagation, streaming-frame demux, `requestID` dedup, idle-timeout sweeper.
- Schema migration 070 (`agents` + `agent_pairings`) + `internal/models/agent.go` + `internal/models/agent_pairing.go` + stores + bearer-hashing helper.
- `internal/api/handlers/agent.go`: `/api/agent/v1/{register,poll,result,heartbeat,unregister}` with bearer auth, per-IP rate limit, structured logs.
- `cmd/qui-agent/`: `main.go` + subcommands (`pair`, `serve`, `unpair`, `status`, `version`); subpackages live under `cmd/qui-agent/internal/{agentclient,executor}` so the agent binary stays standalone.
- Frontend: pairing modal (`AgentPairingModal.tsx`), status card (`AgentStatusCard.tsx`), `useAgent` hook, RadioGroup change in `InstanceForm.tsx`. Pairing UX is part of the platform — without it the platform has no front door.
- Observability primitives: structured zerolog throughout, audit log with in-process size-based rotation, Prometheus metrics on both sides, instance-card status surfacing via existing `InstanceErrorStore`.
- Synthetic `diag.echo` capability: the only op the agent advertises in Stage A. Used for the round-trip integration tests and the user-facing "Test pairing" button. Disappears as a checkbox in Stage C when real ops exist (the capability stays for diagnostics).

**Acceptance for Stage A** (measurable gates; Stage A is "done" only when every gate is green).

*Coverage gates.* Per-package line coverage from `go test -coverprofile`:
- `pkg/fsexec` ≥ **90%** line, ≥ **85%** branch (security-critical; nearly every line is a path-safety check or syscall wrapper).
- `pkg/fsexec/safety.go` ≥ **95%** line — the path-resolution and allowed-roots logic specifically.
- `pkg/agent/proto` ≥ **80%** line (mostly serialization; round-trip tests cover most of it).
- `internal/agent/dispatcher.go` ≥ **85%** line.
- `internal/api/handlers/agent.go` ≥ **80%** line.
- `cmd/qui-agent/internal/{agentclient,executor}` ≥ **80%** line each.
- Frontend pairing components: snapshot tests + at least one rendering test per state (waiting / paired / re-pair / unlinked).

*Race / concurrency gates.* All run under `-race`:
- `make test-agent` per-PR: `-count=3` baseline.
- Post-merge `dispatcher race / fan-out stress`: `-count=10`, 256 concurrent in-flight `diag.echo` jobs, 16 simulated agent disconnect-during-job injections, completes in < 5 min wall clock with **zero** race-detector hits and **zero** leaked goroutines (`goleak` assertion at test exit).
- Integration test runs **100 disconnect/reconnect cycles** within a single test invocation: kill `qui-agent` subprocess, wait, restart it, verify polls resume, verify no leaked `pendingResult` entries in the dispatcher.
- Integration test runs **64 parallel poll connections** (overriding the default `poll_concurrency=4`) and dispatches 4096 sequential `diag.echo` jobs: all 4096 results returned exactly once (no drops, no double-deliveries, no out-of-order results within a single sequential dispatch chain), wall-clock < 60s in CI.

*Functional integration tests.* Each runs end-to-end against `cmd/qui-agent serve` as a subprocess:
- **Pairing flow:** 50 pair-then-unpair cycles. Each verifies: pairing string ≤ 300 chars, modal flips to confirmed within 5s of agent register, `agents` row created, unpair removes the row, `agent_pairings` sweeper cleans the consumed row within its TTL.
- **Pairing-token attacks:** expired token → 410 Gone with `pairing_expired`. Duplicate use → 409 Conflict. Malformed string → 400.
- **Bearer rotation:** pair, rotate-bearer endpoint hit, old bearer's next poll returns 401 with `pairing_invalidated`, re-pair with new string succeeds, dispatched ops resume against new agent within 30s.
- **Cancel propagation:** 1000 cancel cycles. Steady-state cancel latency < **200ms p95**, < 500ms p99 (dispatch a long-running `diag.echo` with sleep, cancel after 50ms, measure caller-unblock latency). Saturated-state cancel (all polls consumed) bounded by longest in-flight op completion.
- **Heartbeat staleness:** kill agent process; assert qui's instance card flips to "stale" within **90–100s** (90s threshold + 10s tolerance). On agent restart, card flips back to "connected" within **30s** of next heartbeat.
- **Streaming framing:** `diag.echo` accepts a `stream: true` mode that emits N NDJSON entries; verify line-perfect arrival order in qui's reader for N ∈ {1, 10, 1000, 10000} and bytes-flushed correlate with N at the expected boundaries (every 256 lines per §13).
- **Crash recovery:** kill qui mid-stream → agent's POST returns connection error within 5s → walker aborts, no orphaned goroutines on the agent. Kill agent mid-stream → qui's reader sees EOF, channel closes with `agent_offline`, caller unblocks with that error.
- **Allowed-roots enforcement:** dispatch a `diag.echo` job with a path payload that escapes the allowed roots (`..`, symlink-out, mount-bind escape per §19's path-safety enumeration); agent rejects with `path_not_allowed`, audit log records the attempt.
- **Multiple sweepers:** verify staleness sweeper, pairings sweeper, pending-results sweeper all run on their cadences; assert sweeper actions in structured logs.

*Build / hygiene gates.*
- `make agent` cross-compiles cleanly for **linux/{amd64,arm64}, darwin/{amd64,arm64}, windows/amd64**. Each binary's `--version` invocation succeeds in CI.
- `make lint-agent` (golangci-lint v2) reports **zero findings**.
- Import-graph guard: `go list -deps ./cmd/qui-agent/...` contains **zero** packages matching `^github.com/autobrr/qui/internal/(?!agent/...)`. CI also runs a negative test (deliberate violation must fail).
- All Go tests pass `-race -count=3` per CLAUDE.md.
- `make test-openapi` passes if the agent endpoints touched the OpenAPI spec.

*Performance baselines (recorded, not gated; tracked across releases).*
- Pair-to-first-poll latency p95.
- Heartbeat round-trip p95.
- Cancel latency p95 (steady-state).
- Idle poll body size.
- Memory / FD usage of `qui-agent serve` after 24h soak with synthetic load.

These are recorded in CI artifacts so regressions are visible across PRs even if they don't fail the build.

**Non-gates.** Stage A explicitly does *not* exercise real FS operations on either Backend — those land in Stages B and C. The platform's correctness is proven on a synthetic op (`diag.echo`); each real op gets its own dual-backend integration test in Stage C.

### Stage B — `Backend` interface + Local impl + callsite refactor

Land the polymorphism in qui's existing services. Behavior-preserving; no agent involvement. Can run in parallel with Stage A once `pkg/fsexec` is stable.

**Scope.**
- `internal/fsops/{backend.go,types.go}`: the `Backend` interface (channels, `RemoveOptions`, `BackendInfo`).
- `internal/fsops/local/local.go`: thin adapter (~150 lines of typed delegation) over `pkg/fsexec`.
- `internal/fsops/pool.go`: resolver. For Stage B, always returns `Local` or no-op; Stage C extends.
- Callsite refactor: every `os.*` / `filepath.WalkDir` / `unix.Statfs` / `pkg/fsutil.SameFilesystem` site listed in §8 migrates to `Backend.*`. ~10 service files.
- `instances.HasFilesystemAccess` helper rolls out, replacing direct `HasLocalFilesystemAccess` reads.

**Acceptance for Stage B.**
- Existing test suite passes `-race -count=3` with zero behavioral diff vs. `develop`.
- No regressions in dirscan, orphanscan, automations, cross-seed.
- Existing instances default to "Local" or "None" in the new RadioGroup; behavior unchanged.

### Stage C — Wire Remote ops, one at a time

The dual-backend integration matrix from Stage A grows: each FS op gets a `Job.Op` + agent-executor switch case + `Backend` method on `Remote`, plus matching integration tests that run **twice** (`Local` and `Remote`) and require equivalent observable outcomes. Same tests, two backends — that's the parity guarantee.

**Order** (pure ops first, destructive ops later, streaming op in its natural complexity progression):
1. `fs.stat`
2. `fs.statfs`
3. `fs.samefs`
4. `fs.lstat`
5. `fs.readdir`
6. `fs.mkdir`
7. `fs.walk` (streaming — exercises NDJSON framing under real load; FileID-indexing is a flag on the same op)
8. `fs.remove`
9. `fs.removeall`
10. `tree.hardlink` (atomic; tests rollback with synthetic mid-tree failures)
11. `tree.reflink` (capability-gated)
12. `tree.remove`

**Each op is one PR.** Per-PR scope: proto-args type, agent-executor switch case, `Backend` method on `Remote` (the `Local` method already exists from Stage B), capability advertisement, dual-backend integration test, audit-log assertion if destructive, capability-missing UI test if applicable. The CI matrix from Stage A grows by one test row per PR; nothing else changes structurally.

**Acceptance per PR.**
- Both backends produce equivalent observable outcomes for that op.
- For destructive ops: agent audit-log entries and qui dispatcher-log entries correlate one-to-one by `requestID`.
- Capability hint surfaces correctly in the frontend if the user has enabled a feature requiring this op but the agent doesn't advertise it.

**Stage C exit criterion.** Every feature in §2 works end-to-end on a real seedbox, with the dual-backend matrix proving parity vs. the local code path. At this point the system is internally complete.

### Stage D — Release engineering

Distribution and documentation. Hardening, observability, and the integration-test harness are not in this stage — they were intrinsic to Stage A and exercised throughout B and C. Stage D is what users actually receive.

**Scope.**
- Release artifacts produced by Stage A's cross-compile matrix: `qui-agent` tarballs, Docker image (`linuxserver`-style with `PUID/PGID`), sample systemd units (user + system), launchd plist, sample `@reboot` cron snippet.
- User-facing docs: `documentation/docs/remote-agent.md` (install, pair, supervisor menu, troubleshooting), README updates, install one-pagers.
- Multi-day soak test against real seedboxes if available; security review pass on path-safety, allowed-roots policy defaults, and audit-log retention.

**Acceptance for Stage D.**
- Public release. Users can install the agent, pair it, and use every feature in §2.

## 19. Testing & Verification

**Unit.**
- `internal/fsops/local/*_test.go`: full interface coverage on `t.TempDir()`. Includes streaming walker bounded-memory test (10k synthesized files), hardlink tree create + rollback equality with current `pkg/hardlinktree` tests, statfs sanity (≥ 0).
- `internal/agent/dispatcher_test.go`: enqueue + dequeue + result correlation; cancel propagation; pending-results TTL eviction; stream-frame ordering; double-deliver rejection (409).
- `internal/fsops/remote/*_test.go`: against an in-process dispatcher + a fake agent goroutine that drains the inbox and posts canned results. Tests: agent_offline error when no agent registered, ctx cancellation unblocks the caller, NDJSON streaming pushes onto the channel in order, version_skew error when capability missing.
- `cmd/qui-agent/*_test.go`: pair flow against a fake qui (httptest server); poll-loop reconnect on 204; bearer-401 → backoff; allowed-roots rejection.
- `pkg/fsexec/*_test.go` **path-safety property tests** (the security-critical layer; tested exhaustively):
  - `..` traversal (path containing `..` segments at any depth)
  - Symlink chains within the allowed root (must not be followed)
  - Symlink targets pointing outside the allowed root (must reject with `path_not_allowed`)
  - Symlink-swap TOCTOU (resolve completes, then attacker swaps the symlink before the syscall lands; must use the resolved handle and not be affected)
  - Mount-bind escape (a directory in the allowed root is bind-mounted from outside; the device-ID guard catches it)
  - NUL byte injection in the path (must reject)
  - Very long paths (PATH_MAX boundary at 4096; rejected cleanly)
  - Relative paths (no leading `/`; must reject)
  - Empty path (must reject)
  - Path equal to the allowed root itself (allowed for read ops; refused for `fs.remove`, `fs.removeall`, `tree.remove` — only strict descendants are destructible)
  - Redundant slashes (`//`, trailing `/`; must `Clean` before resolve)
  - Control characters (`\r`, `\n`, `\t` in path; must reject for audit-log integrity)
  - Unicode normalization on macOS (NFC vs NFD must not be a bypass — paths normalized before comparison)
  - Parent-directory rename mid-walk (TOCTOU; resolved handle protects against)
  - Special device targets (`/dev/...`, `/proc/...`); refused regardless of allowed-roots policy
  - FIFO / socket / block-device targets (statable; not destructible)

**Integration.**
- `internal/fsops/integration_test.go` boots an in-process qui dispatcher and runs `cmd/qui-agent serve` as a subprocess. Pairs them automatically via a programmatically generated pairing string, then runs the full feature suite end-to-end against both `Local` (using a tempdir as "fake host") and `Remote` (against the agent subprocess). Same tests, two backends; results must match.
- Cross-seed inject scenario: build a synthetic searchee, build a `TreePlan`, hardlink-tree via agent, verify on-disk via `os.Stat`, rollback, verify removed.
- Orphanscan scenario: seed a tempdir with files, build a fake `TorrentFileMap` covering some, walk, delete the rest, verify in-use files survive.
- Missing-files: stat a known path that exists, then a removed one, expect correct missing flags.
- Statfs: against a tempdir, verify `BytesAvailable > 0`.
- Samefs: same root, different roots on different mounts (Linux only — macOS mount tricks are a hassle).
- **Pairing/lifecycle:** generate pairing string, run `pair` subprocess, observe registration; rotate bearer, observe old bearer 401s; unlink, observe agent's `serve` exits gracefully.

**Regression / soak.**
- The CI matrix extends to include the remote-agent integration job.
- All Go tests under `-race -count=3` per `CLAUDE.md`.

**Manual verification checklist (release blocker).**
- Pair against a real seedbox (no overlay nets — direct outbound HTTPS only).
- Run a 50k-file dirscan, observe streaming, no OOM on either side.
- Run an orphanscan with destructive deletion against a sacrificial directory; confirm agent audit log entries match qui's dispatcher log one-for-one (correlate by `requestID`).
- Inject a cross-seed match; verify hardlink tree on disk, then rollback via the cross-seed UI; verify cleanup.
- Rotate the bearer; verify qui surfaces the disconnect on the next missed heartbeat and the user can re-pair without restarting qui.

## 20. Open Questions

These are the items that genuinely need information we don't have yet. Decisions and policies that came up during design have been moved into the relevant sections.

1. **Capability `fs.fileid` on Windows.** Windows `FileID` is a `(VolumeSerial, FileIndex)` tuple. Need a careful look at `pkg/hardlink/fileid_windows.go` before shipping to confirm cross-volume `FileID` comparisons aren't accidentally meaningful (e.g. two distinct files on different volumes that happen to share an index value).

2. **Reflinks in Docker on common seedbox topologies.** `pkg/reflinktree` handles the platform-specific CoW syscalls correctly today, but reflink behavior inside a Docker container on a host bind-mount (the common seedbox topology) needs end-to-end verification. Particularly on ZFS-backed hosts (Hetzner, some Whatbox plans) and Btrfs (less common). Stage D acceptance includes a known-good Dockerfile that doesn't break CoW; if any platform combination forces a fall-through to regular hardlink/copy, document the matrix.

3. **`poll_concurrency` default.** 4 is a guess. Lower means more queueing on qui's side under burst (orphanscan + dirscan kick off together); higher means more idle TCP sockets and more dispatcher state. Worth tuning during the Stage D soak test against real seedbox load. Until then, 4 is the placeholder; expose as a config knob from day one so it's adjustable without a release.

## 21. Critical Files for Implementation

**Shared (Stage A) — `pkg/` so it's importable by the agent without crossing `internal/`.**
- `pkg/fsexec/safety.go` (new — `ResolveSafe`, allowed-roots policy, `os.Root` wrapping, device-ID guard)
- `pkg/fsexec/walker.go`, `stat.go`, `mkdir.go`, `remove.go`, `statfs.go`, `samefs.go`, `fileid.go` (new — primitives, callback API)
- `pkg/agent/proto/proto.go` (new — `Job`, `Result`, `RegisterRequest/Response`, `HeartbeatRequest`, op payloads)

**qui-side platform (Stage A).**
- `internal/agent/dispatcher.go` (new — per-instance inbox + pending-results + cancellation + streaming demux)
- `internal/agent/sweeper.go` (new — pending-pairings + stale-results garbage collection)
- `internal/api/handlers/agent.go` (new — `/api/agent/v1/{register,poll,result,heartbeat,unregister}`)
- `internal/api/handlers/instances.go` (extend with `POST /api/instances/{id}/agent/pairing`, `DELETE /api/instances/{id}/agent`, `POST .../rotate-bearer`, `GET .../agent`)
- `internal/database/migrations/070_add_remote_agent.sql` (new — `agents` + `agent_pairings` tables)
- `internal/models/agent.go` (new — `Agent` model + store)
- `internal/models/agent_pairing.go` (new — pairing model + store)

**qui-side fsops abstraction (Stage B).**
- `internal/fsops/backend.go` (new — `Backend` interface)
- `internal/fsops/local/local.go` (new — thin adapter over `pkg/fsexec`)
- `internal/fsops/remote/remote.go` (new — `Backend` impl backed by the dispatcher; populated through Stage C op-by-op)
- `internal/fsops/pool.go` (new — resolver)

**Agent binary (Stage A).**
- `cmd/qui-agent/main.go` (new — entrypoint; subcommands `pair`/`serve`/`unpair`/`status`/`version`)
- `cmd/qui-agent/internal/agentclient/client.go` (new — outbound TLS, pairing, polling, heartbeat, result POST)
- `cmd/qui-agent/internal/executor/executor.go` (new — `Job.Op` switch; calls `pkg/fsexec` primitives)

**Frontend (Stage A).**
- `web/src/components/instances/InstanceForm.tsx` (replace toggle with radio + "Generate pairing string" entry point)
- `web/src/components/instances/AgentPairingModal.tsx` (new — pairing UX with copy button + live status)
- `web/src/components/instances/AgentStatusCard.tsx` (new — paired-agent summary, re-pair / unlink actions)
- `web/src/hooks/useAgent.ts` (new — `GET /api/instances/{id}/agent`)
- `web/src/types/index.ts` (add `Agent` type)

**CI / build (Stage A).**
- `.github/workflows/agent-ci.yml` (new — cross-compile matrix, `make test-agent`, `make test-integration-agent`, `make lint-agent`, import-graph guard, path-safety property tests, dispatcher race/fan-out stress)
- `Makefile` (extend with `make agent`, `make test-agent`, `make test-integration-agent`, `make lint-agent`)
- `testutil/agentfixture/fixture.go` (new — boots in-process dispatcher + spawns `cmd/qui-agent serve` subprocess + auto-pairs; reused for every op test in Stage C)
- `scripts/check-agent-imports.go` (new — `go list -deps` walker that fails on `internal/` imports outside `internal/agent/...`)

## 22. Verification

End-to-end verification once Stage C lands (every op wired to the agent):

1. `make backend && make agent` builds both binaries cleanly.
2. Start qui locally on `https://localhost:7476` (with a self-signed cert for the demo).
3. In the qui UI, edit an instance, choose "Remote agent" → "Generate pairing string". Copy the string.
4. On a second machine (or in another tempdir, simulating the seedbox):
   ```
   mkdir -p /tmp/qui-agent-root
   ./qui-agent pair "qui-pair_v1_..." --root /tmp/qui-agent-root
   ./qui-agent serve
   ```
   Expect the qui UI's pairing modal to flip to "✓ Paired" within a couple of seconds, and the instance card to show "Remote agent: connected".
5. Trigger a dirscan against `/tmp/qui-agent-root`. Observe job dispatch in qui's structured logs (`op=fs.walk requestID=…`) and matching agent log lines. NDJSON streaming arrives line-by-line on qui's side.
6. Inject a synthetic cross-seed match with a small `TreePlan`. Verify hardlinks on disk under `/tmp/qui-agent-root/...`. Trigger rollback via the cross-seed UI and verify cleanup.
7. Stop the agent (`Ctrl-C`). Verify qui surfaces "Remote agent: stale (last seen 90s ago)" in the instance card after three missed heartbeats. Ongoing scans return `agent_offline` rather than hanging.
8. Restart the agent. Verify it reconnects and the instance card flips back to "connected" within one heartbeat.
9. Click "Rotate bearer" in qui. Verify the agent's next poll returns 401, agent logs the auth failure, qui shows "Re-pair required". Run `qui-agent pair <new-string>` and verify recovery.
10. Run `make test` (`-race -count=3`) — every test passes including the new fsops interface conformance, dispatcher unit tests, and the dual-backend integration tests.
11. Run `make lint` — passes.
