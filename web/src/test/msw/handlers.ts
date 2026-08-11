/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"

import type { TorrentResponse } from "@/types"
import { makeServerState } from "@/test/mockServerState"
import { makeTorrent } from "@/test/mockTorrent"

// MSW handlers mirror the request/response shapes of the @/lib/api client for
// the endpoints exercised by the torrent / cross-seed hook suites (issue #1931).
//
// In jsdom the api client resolves a relative `/api/...` URL against the page
// origin, so every pattern is anchored with a leading `*` to stay agnostic of
// the configured base URL (window.__QUI_BASE_URL__). Tests that need a specific
// payload or an error path should layer a per-file override via
// `server.use(...)` rather than mutating these defaults.

const emptyTorrentResponse = (): TorrentResponse => ({
  torrents: [makeTorrent()],
  total: 1,
  serverState: makeServerState(),
  counts: undefined,
  stats: undefined,
})

export const handlers = [
  // --- Torrents list -------------------------------------------------------
  http.get("*/api/instances/:instanceId/torrents", () =>
    HttpResponse.json(emptyTorrentResponse())
  ),

  http.get("*/api/torrents/cross-instance", () =>
    HttpResponse.json(emptyTorrentResponse())
  ),

  // --- Bulk + per-torrent mutations ----------------------------------------
  http.post("*/api/instances/:instanceId/torrents/bulk-action", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.put("*/api/instances/:instanceId/torrents/:hash/rename", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.put("*/api/instances/:instanceId/torrents/:hash/rename-file", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.put("*/api/instances/:instanceId/torrents/:hash/rename-folder", () =>
    new HttpResponse(null, { status: 204 })
  ),

  // --- Export (binary .torrent blob) ---------------------------------------
  http.get("*/api/instances/:instanceId/torrents/:hash/export", () =>
    new HttpResponse(new Blob(["d8:announce"], { type: "application/x-bittorrent" }), {
      status: 200,
      headers: {
        "Content-Type": "application/x-bittorrent",
        "Content-Disposition": "attachment; filename=\"test.torrent\"",
      },
    })
  ),

  // --- Instances -----------------------------------------------------------
  http.get("*/api/instances", () => HttpResponse.json([])),

  // --- Torznab / cross-seed settings ---------------------------------------
  http.get("*/api/torznab/indexers", () => HttpResponse.json([])),

  http.get("*/api/cross-seed/settings", () =>
    HttpResponse.json({ findIndividualEpisodes: false })
  ),

  // --- Cross-seed per-torrent search workflow ------------------------------
  http.get("*/api/cross-seed/torrents/:instanceId/:hash/analyze", ({ params }) =>
    HttpResponse.json({
      instance_id: Number(params.instanceId),
      hash: params.hash,
      name: "test-torrent",
      content_filtering_completed: true,
    })
  ),

  http.post("*/api/cross-seed/torrents/:instanceId/:hash/search", () =>
    HttpResponse.json({ source_torrent: null, results: [], cache: undefined })
  ),

  http.get("*/api/cross-seed/torrents/:instanceId/:hash/async-status", () =>
    HttpResponse.json({
      capabilities_completed: true,
      content_completed: true,
      capability_indexers: [],
      filtered_indexers: [],
      excluded_indexers: {},
      content_matches: [],
    })
  ),

  http.post("*/api/cross-seed/torrents/:instanceId/:hash/apply", () =>
    HttpResponse.json({ results: [] })
  ),

  http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", () =>
    HttpResponse.json({ matches: [] })
  ),
]
