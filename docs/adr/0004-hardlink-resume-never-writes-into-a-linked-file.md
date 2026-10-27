---
status: accepted
date: 2026-09-13
---

# A hardlink resume never writes into a linked file

A hardlink shares its bytes with the source torrent's file. When a linked file fails its recheck and the torrent resumes, qBittorrent downloads the failed pieces through the link and changes the file the source torrent seeds. Before every automatic resume of a hardlink add, qui reads the file list and the piece states, and refuses the resume when a linked file is below 100% and one of its failed pieces lies outside every pending file's piece range. The threshold rule, the byte budget, and the partial pool cannot override this. PR #2688, discussion #2686.

## Considered options

- **Trust the threshold and the budget alone**, as before. Rejected: both count bytes, neither asks where the bytes sit, so a name-and-size match with different content passes inside the slack and the source gets rewritten.
- **Block every linked file below 100%.** Rejected: a piece that spans a linked file and a pending file fails every recheck because the pending part is absent, so no partial pack would ever resume. Such a piece is undecidable after the fact; the Piece boundary safety check setting decides before the add whether it is accepted at all. The check excuses only those pieces.
- **Apply the check to reflink adds.** Rejected: a reflink clone is copy-on-write, so a download into the clone never reaches the source.
- **Block when the piece states cannot be read.** Rejected: an empty or failed read would make every boundary pack look mismatched. The entry retries on the next poll and the absolute timeout drops it paused, which still protects the source.
- **Key the linked set by file index.** Rejected: qBittorrent drops pad files from its file list and renumbers, so metainfo indexes drift on hybrid and v2 torrents. The set is keyed by torrent path.
- **Unlink the mismatched file and download it fresh.** Out of scope, #2687. The check leaves the torrent paused and names the file.

## Consequences

- The check runs in the settle module (`crossseed.Settler`) for every path that adds a torrent: cross-seed reuse and link modes, season pack, and Dir Scan. It runs only for hardlink adds with bytes left, after the gate of the add already said resume. It adds one file list fetch and one piece state fetch per evaluation, the same shape as the existing forgiveness pass. Bounded concurrency on the settle worker is a separate change if the queue ever needs it.
- A change that widens the straddle excuse, adds a toggle to skip the check, or resumes on a failed evidence read reverses this decision. It needs a new ADR and has to delete a linked-file row of `TestSettle`.
- The verdict reaches the user through the log and a `resume` row in the season pack history. Cross-seed adds have no persisted per-torrent result after the recheck, so those rely on the log.
- A Dir Scan partial hardlink add gets the same check through the settle module. Dir Scan has no threshold or budget rule: its gate resumes after the recheck. A failed read retries on the next poll. A blocked resume leaves the torrent paused, and only the log reports it.
