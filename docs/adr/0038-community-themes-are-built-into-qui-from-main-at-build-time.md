---
status: accepted
date: 2026-11-03
---

# Community themes are built into qui from main at build time

The qui build fetches every theme in the `themes` folder of `autobrr/qui-community-themes` from `main` and embeds it in the binary. qui does not download the themes at runtime, unlike the peer client logos in ADR 0037. The logos stay out of the binary because a GPL-3.0 logo file would make each release GPL-3.0. The community themes are MIT, and MIT allows the embed with its notice. A runtime download would only move the question of the Apache-2.0 color values to the community repo, and those values are already in every release since 2025-08. Built-in themes keep their variations, the login page, and the return after a license renewal, and custom themes lose all three. So the gallery in #3768 was dropped. The cost is that a new theme reaches users in the next qui release, not when its pull request merges. A reversal means building that gallery.
