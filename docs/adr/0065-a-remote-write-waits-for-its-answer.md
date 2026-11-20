---
status: accepted
date: 2026-11-20
---

# A remote write waits for its answer

The remote backend sends each file operation to the SFTP server and waits for the answer. When the caller cancels, a read stops waiting, because a read changes nothing.

A write does not stop waiting. A write that the caller gives up halfway leaves the server in a state that the caller cannot know: the file or the link may or may not exist. So each mutation runs to its answer (`awaitMutation` in `internal/fsops/remote`), the caller records what the call made, and only then honours the cancel. This keeps the record of a link tree exact, which ADR 0060 depends on.

## Consequences

- A cancel does not shorten a write. `pkg/sftp` gives a request no timeout, so the wait ends with an answer or with a closed connection: after about 45 seconds when the keepalives fail, or when the pool closes a connection whose calls got no answer for an hour.
- A server that answers the other calls on the connection but never this one holds the write with no limit.

## Considered options

- **Give up a write at cancel, like a read.** Rejected: the rollback would then remove too little or too much.
