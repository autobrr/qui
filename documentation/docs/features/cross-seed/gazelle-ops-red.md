---
sidebar_position: 25
title: OPS/RED (Gazelle)
description: Cross-seed between Orpheus and Redacted via the Gazelle JSON APIs (no Torznab).
---

# OPS/RED (Gazelle)

qui can cross-seed between Orpheus (OPS) and Redacted (RED) using the trackers' Gazelle JSON APIs.

This path is **Gazelle-only**:

- No Torznab searches for OPS/RED torrents
- Searches only the opposite site (RED -> OPS, OPS -> RED)

## When It Applies

Only when the **source torrent** is OPS or RED, detected from the announce/tracker URL:

- RED announce host: `flacsfor.me`
- OPS announce host: `home.opsfet.ch`

If the torrent isn't sourced from OPS/RED, qui uses Torznab like normal.

## How It Matches

In order:

1. Infohash match using Gazelle-style `info["source"]` swap logic (gzlx-compatible)
2. Filename search + exact total size
3. Filename search + filelist verification (size multiset)

If the target tracker is down or errors, the torrent is treated as **no match** and the run continues (best-effort).

## Configuration

UI: **Cross-Seed -> Rules -> Gazelle (OPS/RED)**

- Enable Gazelle matching
- Set one or both API keys (keys are encrypted at rest and redacted in API/UI responses)

### Library Scan Without Torznab

Seeded Torrent Search (Library Scan) can run with **no enabled Torznab indexers** if Gazelle is configured.

In that mode:

- Only OPS/RED-sourced torrents are processed
- All other torrents are skipped
