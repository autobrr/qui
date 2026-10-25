---
status: accepted
date: 2026-10-25
---

# Every search for one torrent asks one Search scope

The completion search, the Library Scan, and the search dialog get the Torznab indexers and the RED/OPS sites for one torrent from one module. All three follow the rules of the Library Scan. Before this change, the completion search skipped Torznab for RED/OPS sources and asked RED/OPS only for RED/OPS sources (#1536). These exceptions are gone. The cost is more RED/OPS requests from completion searches, because each music completion now asks RED/OPS.

RED/OPS and Torznab run at the same time, and RED/OPS gets 45 s in every search. Completion searches run one at a time for each instance. Without the limit, a slow RED/OPS queue holds back every completion on the instance.

A failed music check starts the RED/OPS cooldown, because the files of a torrent do not change. A missing API key starts no cooldown, so a key that the user adds later works at the next run.

qui removes the Torznab indexer of each RED/OPS site that has an API key. With one key, the old rule asked that site two times.

A forbidigo rule keeps other code away from the scope helpers. One table test covers the rules.
