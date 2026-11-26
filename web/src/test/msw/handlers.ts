/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"

import type { TorrentResponse } from "@/types"
import { makeServerState } from "@/test/mockServerState"
import { makeTorrent } from "@/test/mockTorrent"

const emptyTorrentResponse = (): TorrentResponse => ({
  torrents: [makeTorrent()],
  total: 1,
  serverState: makeServerState(),
  counts: undefined,
  stats: undefined,
})

export const handlers = [
  http.get("*/api/instances/:instanceId/torrents", () =>
    HttpResponse.json(emptyTorrentResponse())
  ),

  http.get("*/api/torrents/cross-instance", () =>
    HttpResponse.json(emptyTorrentResponse())
  ),

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

  http.get("*/api/instances/:instanceId/torrents/:hash/export", () =>
    new HttpResponse(new Blob(["d8:announce"], { type: "application/x-bittorrent" }), {
      status: 200,
      headers: {
        "Content-Type": "application/x-bittorrent",
        "Content-Disposition": "attachment; filename=\"test.torrent\"",
      },
    })
  ),

  http.get("*/api/instances", () => HttpResponse.json([])),

  http.get("*/api/torznab/indexers", () => HttpResponse.json([])),

  http.get("*/api/cross-seed/settings", () =>
    HttpResponse.json({ findIndividualEpisodes: false })
  ),

  http.get("*/api/cross-seed/torrents/:instanceId/:hash/analyze", ({ params }) =>
    HttpResponse.json({
      instance_id: Number(params.instanceId),
      hash: params.hash,
      name: "test-torrent",
    })
  ),

  http.post("*/api/cross-seed/torrents/:instanceId/:hash/search", () =>
    HttpResponse.json({ source_torrent: null, results: [], cache: undefined })
  ),

  http.post("*/api/cross-seed/torrents/:instanceId/:hash/apply", () =>
    HttpResponse.json({ results: [] })
  ),

  http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", () =>
    HttpResponse.json({ matches: [] })
  ),

  http.post("*/api/instances/:instanceId/torrents/delete-cross-seeds", () =>
    HttpResponse.json({ matches: [], unchecked_instances: [] })
  ),
]
