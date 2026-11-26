---
status: accepted
date: 2026-11-20
---

# The client proxy refuses a delete that it cannot read

The client proxy forwards requests from other apps to qBittorrent. Before it forwards `torrents/delete`, it reads the hashes and the `deleteFiles` value, because a delete with files must not reach a helper torrent (`SyncManager.RefuseHelperFiles`; ADR 0056 describes the helper torrent of a Transfer).

qBittorrent accepts more forms of the request body than qui parses. A body that qui cannot read could still be a valid delete for qBittorrent, and qui could not check it. The proxy answers such a request with status 400 and does not forward it.

## Consequences

- An app that sends a delete in a form that qui does not parse gets an error where a direct call to qBittorrent succeeds. The fix is a new form in the parser of the proxy, not a pass-through.

## Considered options

- **Forward what qui cannot read.** Rejected: one unusual body would be a way around the helper torrent check.
