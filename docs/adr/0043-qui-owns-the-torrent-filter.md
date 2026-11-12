---
status: accepted
date: 2026-11-08
---

# qui owns the torrent filter

qui decides the rows and the sidebar counts of the torrent list with one rule in the torrent list query module (`internal/qbittorrent/torrent_list_query.go`). It does not use the filter of the go-qbittorrent library for the torrent list. go-qbittorrent gives a read that takes the rule of qui and copies only the matching torrents. Issue #3835.

Before this decision, a single simple filter used the library filter, and each other filter and each count used rules in qui. Running on qBittorrent 4.x, Completed, `seeding`, unknown status names, one parent category, and No tracker gave a count that differed from the rows, or rows that changed with a second filter.

## Considered options

- **Remove the library filter and filter a full copy of the library.** Rejected: a filtered request costs about 4 times the time and 15 times the memory.
- **Let the library narrow the list first, and let qui decide each row.** Rejected: this keeps the second table in the request path.
- **Generate the rule from recorded qBittorrent results.** Rejected: qui differs from qBittorrent on purpose, so a generator needs hand-written overrides, and a state that a probe cannot reach drops out of the table.

## Deliberate differences from qBittorrent

Recorded results from real releases prove each one, unless the text says otherwise. `TestStatusRuleMatchesQBittorrent` and `TestCategoryAndTagRulesMatchQBittorrent` list each one by name.

- An unknown status gives no rows, so a typo in a bulk action touches nothing. qBittorrent returns every torrent.
- Each filter name works on each version. qBittorrent returns every torrent for a name that the version does not have: the wrong pair of `running`, `resumed`, `stopped`, and `paused`, and `checking` and `moving` on 4.3.9.
- `uploading` means `seeding`. qBittorrent treats it as unknown.
- State names, `stalled_seeding`, and the Tracker health statuses narrow the list. qBittorrent treats them as unknown.
- A stopped torrent in `error`, `missingFiles`, or `checkingResumeData` counts as running. qBittorrent reads a stopped flag that its API does not return, so the recordings give `yes` or `mixed` in Stopped, and `no` or `mixed` in Running. A stopped torrent that rechecks on 4.3.9 also counts as running, for the same reason.
- The recordings can give a `mixed` verdict in Running for `pausedUP` and `stoppedUP`, because one capture run can differ from the next. qui keeps these states out of Running.
- Stopped torrents stay out of Downloading.
- Active follows one state list on each version. Below 5.2, qBittorrent also counts `metaDL`, `forcedMetaDL`, `moving`, and a `stalledDL` torrent that uploads. From 5.2, it uses the transfer rate.
- qBittorrent 4.3.9 has no `tag` parameter on `torrents/info`, so it returns every torrent for `tag=x` and for `tag=`. qui narrows the list on each version.
- Below WebAPI 2.9.0 (qBittorrent 4.3.x to 4.5.x), the API cannot report the subcategories setting, so qui uses an exact category match. qBittorrent includes subcategories if the user turned the setting on.
- Through the proxy, `filter=unregistered` and `filter=tracker_down` remove those torrents from the list, because a test locks this in. This is not a recorded result.
- A status name matches in any letter case. qBittorrent treats `SEEDING` as unknown. This is not a recorded result.
- The proxy splits a category name at a comma. qBittorrent reads the whole value as one name. This is not a recorded result.
- A merged tracker shows the largest count of its domains, but its rows are the union of its domains. This is not a recorded result.
- In a Unified view with one instance that has subcategories on and one that has them off, the second instance can return more rows for a parent category than its count. This is not a recorded result.

## Consequences

- A `forbidigo` rule reports the library filter options in the production code of `internal/qbittorrent`.
- Reannounce keeps the library filter on purpose: it asks qBittorrent for the stalled torrents among its candidates. Cross-seed and the other reads of reannounce read the Sync snapshot since #4047 and #4050 (ADR 0051), where qui applies its own rule. That rule also matches a category exactly, so their results did not change.
- A new qBittorrent release needs one capture run. A changed result fails the test, and a person then decides if qui follows.

## Benchmark results

`BenchmarkFilteredList` sends one filtered page of 300 rows through `GetTorrentsWithFilters`, with cached counts and the cached read, at 50,000 torrents. The table gives the median of 6 runs on one machine, before and after this change.

| Filter | Time before | Time after | Memory before | Memory after |
|---|---|---|---|---|
| `completed` | 26.5 ms | 21.1 ms | 73.1 MB | 26.2 MB |
| `running` | 42.7 ms | 28.6 ms | 126.2 MB | 35.6 MB |
| `stopped` | 6.1 ms | 4.2 ms | 6.0 MB | 5.0 MB |
| `stalled` | 9.7 ms | 7.9 ms | 15.3 MB | 10.0 MB |
| `stalled_uploading` | 6.0 ms | 4.1 ms | 6.0 MB | 5.0 MB |
| `stalled_downloading` | 6.1 ms | 4.0 ms | 6.2 MB | 5.0 MB |
| `error` | 6.1 ms | 3.8 ms | 6.0 MB | 5.0 MB |
| one category | 6.5 ms | 5.1 ms | 9.7 MB | 6.8 MB |
| one tag | 37.8 ms | 28.1 ms | 97.9 MB | 32.9 MB |
| `completed` and one category | 5.3 ms | 4.9 ms | 6.1 MB | 5.0 MB |
| `completed` and one tag | 22.1 ms | 19.0 ms | 49.7 MB | 20.5 MB |

The counts pass with a tracker mapping (`BenchmarkHotCountsWithTrackers`, 10,000 torrents) went from 3.35 ms to 3.67 ms per pass, inside the run-to-run noise of 11%.
