---
status: accepted
date: 2026-10-23
---

# Cross-seed adds give facts and the Adder decides

Every cross-seed add goes through the `Adder`: the regular add, the hardlink and reflink add, the season-pack add, and the Dir Scan add. The caller gives facts about the torrent. The `Adder` makes the Add verdict, adds the torrent, resumes it or hands it to Settle, and runs the External Program. The caller owns the placement and what it materializes. Issue #3409.

## Considered options

- **Each add makes its own decisions, the status quo.** Rejected: fixes landed in one add and missed the others. Examples are the download path (#2432), the blocklist (#2833), and the link-fallback rule (#3029).
- **A pure verdict function, with the add sequence left in each caller.** Rejected: the callers would keep the add, the resume, the Settle call, and the External Program call. That sequence drifted too. Only two adds ran the External Program, and only one retried a failed add.
- **One module that also builds the placement.** Rejected: each source has its own category, tag, and save path settings. The state options drifted. The placement did not.

## Consequences

- A new add rule is one change in the `Adder` and one row in its table test.
- A new cross-seed source calls the `Adder` with its facts.
- The `forbidigo` rule with the message `use crossseed.Adder` blocks a direct `AddTorrent` call in the cross-seed and Dir Scan packages.
- A manual assembly from several episodes skips the blocklist, as the user docs say.
- A change that lets a caller pass a resume rule or a state option reverses this decision and needs a new ADR.
