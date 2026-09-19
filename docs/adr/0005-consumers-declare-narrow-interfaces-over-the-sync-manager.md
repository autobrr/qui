---
status: accepted
date: 2026-09-17
---

# Consumers declare narrow interfaces over the sync manager

The qBittorrent sync manager stays one concrete type. A package that needs to fake it in a test declares its own unexported interface, with at most five methods, over the methods one flow calls. The interface exists only when it has two adapters at merge time: the sync manager and a test fake. Production fills the field from the concrete sync manager in the constructor. No method branches on a nil interface field to pick between the two. Consumers with no test fake hold the concrete type. Issue #2745.

## Considered options

- **One interface over the whole sync manager.** Rejected: the torrents handler calls 55 distinct methods and the automations service 22, so the interface would mirror the type and nothing could fake it.
- **Nil-checked function fields with forwarders**, as the orphan scan service had. Rejected: every new faked method costs a field, a forwarder, and a nil check, and production and tests take different branches.
- **A test-only constructor that fills the interfaces**, as the torrents handler had. Rejected: same dual path, and the production constructor left the fields nil.
- **A route-group split of the torrents handler.** Rejected: a structural change to make an interface fit, not a fix for how the handler is tested.
- **A read interface for the automations service's own reads.** Rejected: no test fakes them, so the seam would be hypothetical.

## Consequences

- Prior art that already follows this: the SSE stream manager's `syncProvider` (4 methods) and the backup service's `backupReader` (5). The cross-seed `qbittorrentSync` (17) is over the cap and is a separate decision.
- The orphan scan service reads through `syncReader`, `clientReadiness`, `instanceLister`, and `lastRunReader`. The automations hardlink, missing-files, and skipped-files checks read through `filesReader`. The torrents handler adds, downloads, and resolves content through `torrentAdder`, `torrentDownloader`, and `torrentContentResolver`.
- A change that adds a sixth method to one of these, adds an interface without a test fake, or adds a nil-check forwarder reverses this decision and needs a new ADR.
