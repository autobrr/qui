---
sidebar_position: 9
title: OPS/RED (Gazelle)
description: Cross-seed between Orpheus and Redacted with their Gazelle JSON APIs, with or without Torznab.
---

# OPS/RED (Gazelle)

qui cross-seeds between Orpheus (OPS) and Redacted (RED) using the trackers' Gazelle JSON APIs.

:::tip TL;DR
- For the best OPS/RED cross-seed coverage, enable Gazelle and set **both** API keys.
- If you set **only one** key, Gazelle matching still works, but coverage is **partial**:
  - OPS-sourced torrents need the **RED** key because qui queries the opposite site
  - RED-sourced torrents need the **OPS** key because qui queries the opposite site
- Library Scan (Seeded Torrent Search) can run in Gazelle-only mode without Torznab. Use an interval of **10+ seconds**.
:::

## What it does

If you enable Gazelle matching:

- If a torrent comes from OPS or RED, qui queries **only the opposite site** (RED -> OPS, OPS -> RED).
- If a torrent comes from another source, qui checks it against the Gazelle sites you configured.
- Torznab can run in parallel. If you configure **both** Gazelle keys, qui excludes OPS/RED Torznab indexers from per-torrent searches (manual, completion, library scan). If you configure only one key, qui keeps Torznab as a fallback.
- Completion searches are the exception. If a completed torrent comes from OPS or RED, qui searches only the opposite site when you configured its key. It skips Torznab for that torrent.
- If Torznab is unavailable, qui still returns a successful empty result for a torrent that Gazelle handled. If a local prefilter proves the target tracker content is already present, qui sends no remote Gazelle request.

Gazelle support gives qui music-specific handling for OPS/RED. The tracker-native APIs search by Gazelle release metadata and source-specific infohashes. Direct Gazelle API use lets Gazelle-only scans run without a Torznab backend. qui paces API requests and tracks key coverage separately from Torznab indexer rules.

### Content gate for non-OPS/RED torrents

Before qui spends an API call on a non-OPS/RED torrent, it reads the torrent's file extensions. If most of the torrent's bytes are not audio, e-book, or comic files, qui skips the Gazelle query for that torrent. If a torrent is already on OPS or RED, it skips this gate because the source tracker proves that the content fits.

## When it applies

qui detects OPS and RED sources by reading the announce URL:

- RED announce host: `flacsfor.me`
- OPS announce host: `home.opsfet.ch`

These map to the Gazelle API sites:

- RED API host: `redacted.sh`
- OPS API host: `orpheus.network`

## Keys and coverage

You can configure one key or both. What qui queries depends on what you seed.

- If a torrent is sourced from **OPS**, qui searches for it on **RED**. That requires a **RED key**.
- If a torrent is sourced from **RED**, qui searches for it on **OPS**. That requires an **OPS key**.

If you set only one key, expect this behavior:

- Mixed OPS+RED libraries: some torrents return "no match" because qui cannot query the needed opposite site.
- Non-OPS/RED torrents: qui queries whichever Gazelle sites you configured, one or both.

## What happens if Gazelle is not configured

If you disable Gazelle or set no API keys:

- qui falls back to Torznab (Jackett/Prowlarr) where available
- Gazelle-only modes (Torznab disabled) cannot run

## How it matches

In order:

1. Infohash match with the Gazelle-style `info["source"]` swap logic (see [nemorosa](https://github.com/KyokoMiki/nemorosa))
2. Filename search plus exact total size
3. Filename search plus filelist verification

If the target tracker is down or returns an error, qui treats the torrent as **no match** and continues the run.

## Configuration

UI: **Cross-Seed > Rules > Gazelle (OPS/RED)**

- Enable Gazelle matching
- Set one or both API keys
- qui encrypts the keys at rest and redacts them in API and UI responses

## Common issues

### "torznab disabled but gazelle not configured"

You tried to run in Gazelle-only mode with Torznab disabled, but qui has no usable Gazelle client.

Fix:

- Enable Gazelle
- Set at least one API key
- If you changed `sessionSecret` (or `QUI__SESSION_SECRET`), enter the keys again. qui cannot decrypt the old encrypted values.
- For the best OPS/RED coverage, set **both** keys

### Only one key set

This configuration works, but coverage is partial.

For example, if you set only the RED key:

- qui can check OPS-sourced torrents against RED
- qui cannot check RED-sourced torrents against OPS

## Rate limiting

qui rate-limits requests to OPS/RED and shares the limit across the whole qui process. Multiple qBittorrent instances do not multiply API pressure.

Gazelle and Torznab also differ in how qui applies time-based search constraints:

- If you enable Torznab, Library Scan keeps the per-torrent interval floor of 60 seconds used for indexer searches.
- If you disable Torznab and configure Gazelle, Library Scan can use a lower interval floor because requests go directly to the tracker APIs instead of through Torznab indexers.
- qui stamps the Gazelle cooldown for a torrent when it sends a Gazelle lookup. It also stamps it when Gazelle was enabled for the run but had nothing to look up for that torrent. Causes: the content gate, the local prefilter, or the target hash already present. A search that fails before any lookup does not stamp it.
- qui stamps Torznab cooldowns per indexer, and only for indexers that completed the search. An indexer that was rate limited or failed stays eligible on the next run.
- After each search attempt, qui copies the representative torrent's Gazelle and per-indexer cooldown stamps to its duplicate torrents.

### Library Scan without Torznab

If you configure Gazelle, Seeded Torrent Search (Library Scan) can run with **no enabled Torznab indexers**.

In that mode:

- qui still processes all source torrents
- Matches come only from the configured Gazelle sites (RED/OPS)
- You can lower the Library Scan interval below the Torznab floor, down to a minimum of 5 seconds. Actual request pacing still respects the shared OPS/RED API rate limits.
- Use 10 seconds or more to reduce API pressure. The interval is per-torrent pacing, and each torrent can trigger multiple API calls.
