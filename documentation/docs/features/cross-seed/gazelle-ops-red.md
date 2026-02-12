---
sidebar_position: 25
title: OPS/RED (Gazelle)
description: Cross-seed using Orpheus/Redacted Gazelle APIs, optionally alongside Torznab.
---

# OPS/RED (Gazelle)

qui can cross-seed between Orpheus (OPS) and Redacted (RED) using the trackers' Gazelle JSON APIs.

This path is **Gazelle-aware**:

- Gazelle checks RED/OPS when API keys are configured
- OPS/RED source torrents search only the opposite site (RED -> OPS, OPS -> RED)
- Non-OPS/RED source torrents can still be checked against RED and OPS
- Torznab can run in parallel, but OPS/RED Torznab indexers are excluded when Gazelle is configured

## When It Applies

OPS/RED source detection still matters for the swap-hash fast path. qui detects source site from announce/tracker URL:

- RED announce host: `flacsfor.me`
- OPS announce host: `home.opsfet.ch`

If the source is OPS/RED, qui targets only the opposite site.
If the source is not OPS/RED, qui can still query whichever Gazelle sites you configured.

## What Happens If Gazelle Isn't Configured

If Gazelle is disabled or no API keys are set, qui falls back to Torznab like it did before.

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
- If only one key is set, only that tracker can be queried via Gazelle

## Rate Limiting

Requests to OPS/RED are rate-limited and **shared across the whole qui process**, so running multiple qBittorrent instances does not multiply API pressure.

### Library Scan Without Torznab

Seeded Torrent Search (Library Scan) can run with **no enabled Torznab indexers** if Gazelle is configured.

In that mode:

- All source torrents are still processed
- Matches come only from configured Gazelle sites (RED/OPS)
- You can lower the Library Scan interval below 60 seconds (minimum 1 second), but actual request pacing still respects the shared OPS/RED API rate limits
