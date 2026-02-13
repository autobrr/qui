---
sidebar_position: 25
title: OPS/RED (Gazelle)
description: Cross-seed using Orpheus/Redacted Gazelle APIs, optionally alongside Torznab.
---

# OPS/RED (Gazelle)

qui can cross-seed between Orpheus (OPS) and Redacted (RED) using the trackers' Gazelle JSON APIs.

:::tip TL;DR
- Want the best OPS <-> RED cross-seed coverage: enable Gazelle and set **both** API keys.
- If you set **only one** key, Gazelle matching still works, but coverage is **partial**:
  - OPS-sourced torrents need the **RED** key (because qui queries the opposite site)
  - RED-sourced torrents need the **OPS** key (because qui queries the opposite site)
- "Library Scan" (Seeded Torrent Search) can run in Gazelle-only mode without Torznab, but run it sparingly and use an interval of **10+ seconds**.
:::

## What It Does

When Gazelle matching is enabled:

- OPS/RED source torrents query **only the opposite site** (RED -> OPS, OPS -> RED)
- Non-OPS/RED source torrents can still be checked against whichever Gazelle sites you configured
- Torznab can run in parallel, but for per-torrent searches (manual/completion/library scan) OPS/RED Torznab indexers are excluded when Gazelle is configured

## When It Applies

OPS/RED source detection is based on the announce/tracker URL:

- RED announce host: `flacsfor.me`
- OPS announce host: `home.opsfet.ch`

These map to the Gazelle API sites:

- RED API host: `redacted.sh`
- OPS API host: `orpheus.network`

## Keys And Coverage (ELI5)

You can configure one key or both. What qui can query depends on what you seed.

- If a torrent is sourced from **OPS**, qui tries to find it on **RED**. That requires a **RED key**.
- If a torrent is sourced from **RED**, qui tries to find it on **OPS**. That requires an **OPS key**.

If you only set one key, expect this:

- Mixed OPS+RED libraries: some torrents will be "no match" simply because qui cannot query the needed opposite site.
- Non-OPS/RED torrents: qui will query whichever Gazelle sites you configured (one or both).

## What Happens If Gazelle Isn't Configured

If Gazelle is disabled or no API keys are set:

- qui falls back to Torznab (Jackett/Prowlarr) where available
- Gazelle-only modes (Torznab disabled) cannot run

## How It Matches

In order:

1. Infohash match using Gazelle-style `info["source"]` swap logic (see [nemorosa](https://github.com/KyokoMiki/nemorosa))
2. Filename search + exact total size
3. Filename search + filelist verification (size multiset)

If the target tracker is down or errors, the torrent is treated as **no match** and the run continues (best-effort).

## Configuration

UI: **Cross-Seed -> Rules -> Gazelle (OPS/RED)**

- Enable Gazelle matching
- Set one or both API keys
- Keys are encrypted at rest and redacted in API/UI responses

## Common Issues

### "torznab disabled but gazelle not configured"

You tried to run in Gazelle-only mode (Torznab disabled), but qui has no usable Gazelle client.

Fix:

- Enable Gazelle
- Set at least one API key
- For best OPS <-> RED coverage, set **both** keys

### Only One Key Set

This is supported, but coverage is partial.

Example: only RED key is set.

- OPS-sourced torrents can be checked against RED
- RED-sourced torrents cannot be checked against OPS

## Rate Limiting

Requests to OPS/RED are rate-limited and **shared across the whole qui process**, so running multiple qBittorrent instances does not multiply API pressure.

### Library Scan Without Torznab

Seeded Torrent Search (Library Scan) can run with **no enabled Torznab indexers** if Gazelle is configured.

In that mode:

- All source torrents are still processed
- Matches come only from configured Gazelle sites (RED/OPS)
- You can lower the Library Scan interval below 60 seconds (minimum 5 seconds), but actual request pacing still respects the shared OPS/RED API rate limits
- Recommended: 10+ seconds to reduce API pressure (interval is per-torrent pacing; each torrent can trigger multiple API calls)
