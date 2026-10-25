---
status: accepted
date: 2026-10-25
---

# Every search for one torrent asks one Search scope

The completion search, the Library Scan, and the search dialog get the Torznab indexers and the RED/OPS sites for one torrent from one module. All three follow the rules of the Library Scan.

A failed music check starts the RED/OPS cooldown, because the files of a torrent do not change. A missing API key starts no cooldown, so a key that the user adds later works at the next run.
