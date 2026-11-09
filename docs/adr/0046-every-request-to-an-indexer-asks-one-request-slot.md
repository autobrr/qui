---
status: accepted
date: 2026-11-09
---

# Every request to an indexer asks one Request slot, and only searches and Dir Scan downloads wait for the Request gap

The search queue, the capabilities sync, and each try of a torrent download ask one module if the request can go to the indexer now. The module owns the test of the Rate-limit cooldown, the wait for the Request gap, the reservation, the record of the answer, and the rate-limit error. Before this change, each caller combined six methods of the rate limiter. A Dir Scan download went out while a search to the same Native indexer was open, and its retries skipped the gap (#2735).

On a Native indexer, a search and a Dir Scan download wait for the Request slot, hold it while the request is open, and start the Request gap at the answer. Every other torrent download goes at once and does not delay a search. This covers the Add click, the cross-seed dialog, RSS, and Library Scan. qui already keeps one Rate-limit cooldown for searches and one for downloads, so it treats the two as separate budgets at the tracker. A wait on every download makes an Add click hang for up to 60 seconds and makes an RSS run with 8 candidates take 8 minutes. Dir Scan waits because it downloads a torrent file for each candidate, in bulk and with no person waiting.

A capabilities request never waits and does not start the Request gap. It is rare, and a person waits for it in a dialog. It is refused during the search cooldown, and a 429 on it starts that cooldown.

A hung search frees the slot at its timeout. The other choices were a failed Dir Scan download or a scan that hangs until a restart. The search queue keeps its own rule that fails queued searches behind a hung search.

When the Request gap ends, the first caller to ask gets the slot. A fairness rule between searches and downloads is a separate request (#2733).

No lint rule guards the module. All sends to an indexer are in one large service file, so a rule on the file path catches almost no wrong call.
