---
status: accepted
date: 2026-11-27
---

# Torrent search excludes with the `-word` syntax of the qBittorrent WebUI

A word in a Torrent search that starts with `-` or `!` is an Excluded term, and it hides each torrent whose name, category, or tag it matches. The qBittorrent WebUI filter box uses `-word`, so users already know it, and `!` is the common NOT prefix. As in the WebUI, a lone `-` or `!` is ignored and `+word` searches like `word`. Only the start of a word counts, so `this-title-group.mkv` and `Artist - Album` search as before. A query with no prefixed word searches exactly as typed, spaces included, so it finds the same rows as before. Once a word has a prefix, the other words search joined with single spaces.

An Excluded term keeps the wildcard rules of the search, and `[` also makes it a wildcard pattern. Without a wildcard it matches as text or as punctuation-blind words, in one piece: `-web.dl` hides `WEB-DL`, `WEB.DL`, and `WEB DL`. It never uses fuzzy rank, never matches its words apart, and never checks the hash, because each of these would hide torrents that the user did not name. A hash check would make `-ac3` hide a random 1 to 3% of torrents.

The search splits the query on whitespace with a handwritten parser on the standard library. There are no quoted phrases: a phrase goes through punctuation, as in `-web.dl`. The torrent search module in `internal/qbittorrent/torrent_search.go` owns the split, every kind of match, and the fuzzy library, and a forbidigo rule keeps other code away from them, in the shape of ADR 0029. The search still runs after the torrent list rule of ADR 0043.

## Considered options

- **bleve or go-lucene.** Rejected: both bring their own matching and read `[ ( { : ! -` as syntax, so a release name would parse as a query.
- **mattn/go-searchquery.** Rejected: it has no negation.
- **A shell splitter (shlex, go-shellwords, go-shellquote).** Rejected: each fails on `Don't` and reads `#` as a comment.

## Consequences

- `-x265` used to find x265 torrents, and now hides them.
- A pasted full name with a word that starts with `-` hides that torrent. A replay on 7,127 torrents of a maintainer library found 1 such name.
