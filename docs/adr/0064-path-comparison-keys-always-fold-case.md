---
status: accepted
date: 2026-11-20
---

# Path comparison keys always fold case

Orphan scan, folder cleanup, automations, and cross-seed compare paths from qBittorrent with paths from disk. `fsops.NormalizePath` builds the key for each comparison, and it always folds case.

The operating system of the qui host does not say whether the scanned filesystem ignores case. macOS APFS and Windows volumes ignore case by default, and Docker mounts them into a Linux container. There, qBittorrent can report two save paths that differ only in case for one folder. A key that keeps case then calls an owned file an orphan, and orphan scan deletes it.

The fold keeps no state. Orphan scan fills its file map before the walk and runs one goroutine for each instance, so a flag for each scan, or one global flag, that differs between the write and the read of the map turns every file into an orphan. The fold also covers names with invalid UTF-8, because `strings.ToLower` and `encoding/json` replace a bad byte the same way. That is how a name from disk matches the same name after a round trip through the qBittorrent API. A UTF-8 validity guard in front of the fold would report such an owned file as an orphan.

## Consequences

- Two names that differ only in case, or only in their invalid bytes, share one key. Orphan scan can then miss an orphan. It never deletes an owned file for this reason.
- If a user reports a missed orphan, keep a folded second index and confirm each hit with `os.SameFile`.
- A key is for comparison only. It never reaches an `os` call or the database, because on a case-sensitive filesystem it can name another file.
- `strings.ToLower` can turn an NFC string into a string that is not NFC, so the key is cleaned again after the fold.
- Keys keep the separators of their path dialect. A slash key would merge a UNC path with a rooted path.

## Considered options

- **Fold only on a case-insensitive host.** Rejected: `runtime.GOOS` describes the host of qui, not the volume behind a mount.
- **Detect case sensitivity for each scan root.** Rejected: the answer would be state that the write and the read of the file map must share, and a mismatch deletes owned files.
