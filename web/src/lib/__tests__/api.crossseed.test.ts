/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { http, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"

import { APIError, api } from "@/lib/api"
import { server } from "@/test/msw/server"

// Contract tests for the lib/api.ts "crossseed" method group (issue #1931).
// These call the REAL api client and intercept HTTP via MSW, asserting BOTH
// directions: request serialization (verb/path/query/body) and response
// parsing/normalization into the typed return value. Default handlers in
// handlers.ts match the verb+path but several use mismatched response shapes,
// so every response assertion layers a correct snake_case server.use override.

// Captures the most recent intercepted request so a test can assert the
// outgoing verb, pathname, query params, and JSON body the client serialized.
type Captured = {
  request: Request
  body: string
}

function captureNext(method: "get" | "post", pattern: string, respond: () => Response) {
  const box: { value?: Captured } = {}
  server.use(
    http[method](pattern, async ({ request }) => {
      const clone = request.clone()
      box.value = { request: clone, body: await clone.text() }
      return respond()
    })
  )
  return box
}

describe("api.crossseed contract", () => {
  describe("listTorznabIndexers", () => {
    it("issues GET /torznab/indexers with no body and returns the JSON array verbatim", async () => {
      const indexers = [
        { id: 1, name: "Indexer A" },
        { id: 2, name: "Indexer B" },
      ]
      const captured = captureNext("get", "*/api/torznab/indexers", () =>
        HttpResponse.json(indexers)
      )

      const result = await api.listTorznabIndexers()

      const req = captured.value!.request
      expect(req.method).toBe("GET")
      expect(new URL(req.url).pathname).toBe("/api/torznab/indexers")
      expect(captured.value!.body).toBe("")
      expect(req.headers.get("content-type")).toBe("application/json")
      // No remap/normalization: response.json() returned directly.
      expect(result).toEqual(indexers)
    })
  })

  describe("getCrossSeedSettings", () => {
    it("issues GET /cross-seed/settings and returns the JSON body verbatim", async () => {
      const settings = { enabled: true, instanceId: 7, indexerIds: [3, 4] }
      const captured = captureNext("get", "*/api/cross-seed/settings", () =>
        HttpResponse.json(settings)
      )

      const result = await api.getCrossSeedSettings()

      const req = captured.value!.request
      expect(req.method).toBe("GET")
      expect(new URL(req.url).pathname).toBe("/api/cross-seed/settings")
      expect(captured.value!.body).toBe("")
      expect(result).toEqual(settings)
    })

    it("throws APIError with status and parsed JSON data on a non-2xx response", async () => {
      server.use(
        http.get("*/api/cross-seed/settings", () =>
          HttpResponse.json(
            { error: "settings unavailable", code: "DOWN" },
            { status: 503 }
          )
        )
      )

      // Shared error path for the two no-arg GET methods: extractErrorData
      // reads .error and attaches the full parsed body as APIError.data.
      await expect(api.getCrossSeedSettings()).rejects.toBeInstanceOf(APIError)
      try {
        await api.getCrossSeedSettings()
        throw new Error("expected APIError")
      } catch (err) {
        const apiErr = err as APIError
        expect(apiErr).toBeInstanceOf(APIError)
        expect(apiErr.message).toBe("settings unavailable")
        expect(apiErr.status).toBe(503)
        expect(apiErr.data).toEqual({ error: "settings unavailable", code: "DOWN" })
      }
    })
  })

  describe("analyzeTorrentForCrossSeedSearch", () => {
    it("interpolates instanceId/hash RAW (no encodeURIComponent) and GETs analyze", async () => {
      // "+" is the discriminating char: encodeURIComponent("+") === "%2B",
      // but analyze interpolates the hash RAW, so the "+" stays literal.
      // (Avoid space/"%" which the URL layer normalizes or which break the
      // path-to-regexp matcher.)
      const rawHash = "abc+def+gh"
      const captured = captureNext(
        "get",
        "*/api/cross-seed/torrents/:instanceId/:hash/analyze",
        () => HttpResponse.json({ name: "x" })
      )

      await api.analyzeTorrentForCrossSeedSearch(5, rawHash)

      const req = captured.value!.request
      expect(req.method).toBe("GET")
      // Raw interpolation leaves "+" literal; encodeURIComponent would emit %2B.
      expect(req.url).toContain(`/api/cross-seed/torrents/5/${rawHash}/analyze`)
      expect(req.url).not.toContain("%2B")
      expect(encodeURIComponent(rawHash)).not.toBe(rawHash) // sanity: chars matter
      expect(captured.value!.body).toBe("")
    })

    it("remaps the full snake_case RawTorrentInfo into camelCase CrossSeedTorrentInfo", async () => {
      const rawBody = {
        instance_id: 9,
        instance_name: "Seedbox",
        hash: "HASH123",
        name: "Some.Release.2024",
        category: "movies",
        size: 1024,
        progress: 0.5,
        total_files: 12,
        matching_files: 3,
        file_count: 12,
        content_type: "movie",
        search_type: "imdb",
        search_categories: [2000, 2010],
        required_caps: ["imdbid", "search"],
        disc_layout: true,
        disc_marker: "BDMV",
        available_indexers: [1, 2, 3],
        filtered_indexers: [1, 2],
        excluded_indexers: { "3": "no caps", "bad": "ignored" },
        content_matches: ["file.mkv"],
        content_filtering_completed: true,
      }
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/analyze", () =>
          HttpResponse.json(rawBody)
        )
      )

      const result = await api.analyzeTorrentForCrossSeedSearch(9, "HASH123")

      expect(result).toEqual({
        instanceId: 9,
        instanceName: "Seedbox",
        hash: "HASH123",
        name: "Some.Release.2024",
        category: "movies",
        size: 1024,
        progress: 0.5,
        totalFiles: 12,
        matchingFiles: 3,
        fileCount: 12,
        contentType: "movie",
        searchType: "imdb",
        searchCategories: [2000, 2010],
        requiredCaps: ["imdbid", "search"],
        discLayout: true,
        discMarker: "BDMV",
        availableIndexers: [1, 2, 3],
        filteredIndexers: [1, 2],
        // normalizeExcludedIndexerMap: string keys -> numeric, non-numeric dropped.
        excludedIndexers: { 3: "no caps" },
        contentMatches: ["file.mkv"],
        contentFilteringCompleted: true,
      })
    })
  })

  describe("searchCrossSeedTorrent", () => {
    it("sends NO body when options are empty (conditional body is undefined)", async () => {
      const captured = captureNext(
        "post",
        "*/api/cross-seed/torrents/:instanceId/:hash/search",
        () => HttpResponse.json({ source_torrent: null })
      )

      await api.searchCrossSeedTorrent(1, "h")

      const req = captured.value!.request
      expect(req.method).toBe("POST")
      expect(new URL(req.url).pathname).toBe("/api/cross-seed/torrents/1/h/search")
      // Object.keys(body).length === 0 => body undefined => empty request body.
      expect(captured.value!.body).toBe("")
    })

    it("conditionally serializes only provided options into snake_case body", async () => {
      const captured = captureNext(
        "post",
        "*/api/cross-seed/torrents/:instanceId/:hash/search",
        () => HttpResponse.json({ source_torrent: null })
      )

      await api.searchCrossSeedTorrent(2, "h", {
        query: "  Big Buck Bunny  ",
        limit: 50,
        indexerIds: [4, 5],
        findIndividualEpisodes: false,
        cacheMode: "bypass",
      })

      const sent = JSON.parse(captured.value!.body)
      expect(sent).toEqual({
        query: "Big Buck Bunny", // trimmed
        limit: 50,
        indexer_ids: [4, 5],
        find_individual_episodes: false,
        cache_mode: "bypass",
      })
    })

    it("omits keys that are blank/empty/undefined (whitespace query, empty indexerIds)", async () => {
      const captured = captureNext(
        "post",
        "*/api/cross-seed/torrents/:instanceId/:hash/search",
        () => HttpResponse.json({ source_torrent: null })
      )

      await api.searchCrossSeedTorrent(3, "h", {
        query: "   ",
        indexerIds: [],
      })

      // Whitespace-only query trims to "" (falsy) and empty indexerIds skipped,
      // so no keys are added and no body is sent.
      expect(captured.value!.body).toBe("")
    })

    it("normalizes snake_case source_torrent and results into camelCase", async () => {
      server.use(
        http.post("*/api/cross-seed/torrents/:instanceId/:hash/search", () =>
          HttpResponse.json({
            source_torrent: {
              instance_id: 1,
              instance_name: "Main",
              hash: "abc",
              name: "Source.Name",
              total_files: 4,
            },
            results: [
              {
                indexer: "IndexerX",
                indexer_id: 11,
                title: "Result Title",
                download_url: "http://dl/1",
                info_url: "http://info/1",
                size: 2048,
                seeders: 10,
                leechers: 2,
                category_id: 2000,
                category_name: "Movies",
                publish_date: "2024-01-01",
                download_volume_factor: 1,
                upload_volume_factor: 1,
                guid: "guid-1",
                infohash_v1: "v1hash",
                infohash_v2: "v2hash",
                imdb_id: "tt123",
                tvdb_id: "tv456",
                match_reason: "title",
                match_score: 87,
              },
            ],
            cache: { hit: true },
            partial: true,
            query_degraded: "arr_lookup_failed",
          })
        )
      )

      const result = await api.searchCrossSeedTorrent(1, "abc")

      expect(result.sourceTorrent.name).toBe("Source.Name")
      expect(result.sourceTorrent.instanceId).toBe(1)
      expect(result.sourceTorrent.instanceName).toBe("Main")
      expect(result.sourceTorrent.totalFiles).toBe(4)
      expect(result.results).toEqual([
        {
          indexer: "IndexerX",
          indexerId: 11,
          title: "Result Title",
          downloadUrl: "http://dl/1",
          infoUrl: "http://info/1",
          size: 2048,
          seeders: 10,
          leechers: 2,
          categoryId: 2000,
          categoryName: "Movies",
          publishDate: "2024-01-01",
          downloadVolumeFactor: 1,
          uploadVolumeFactor: 1,
          guid: "guid-1",
          infoHashV1: "v1hash",
          infoHashV2: "v2hash",
          imdbId: "tt123",
          tvdbId: "tv456",
          matchReason: "title",
          matchScore: 87,
        },
      ])
      expect(result.cache).toEqual({ hit: true })
      expect(result.partial).toBe(true)
      expect(result.queryDegraded).toBe("arr_lookup_failed")
    })

    it("falls back source name to trimmed query and match_score to 0 when absent", async () => {
      server.use(
        http.post("*/api/cross-seed/torrents/:instanceId/:hash/search", () =>
          HttpResponse.json({
            source_torrent: null,
            results: [
              {
                indexer: "IndexerY",
                indexer_id: 22,
                title: "No Score",
                download_url: "http://dl/2",
                size: 1,
                seeders: 0,
                leechers: 0,
                category_id: 0,
                category_name: "",
                publish_date: "",
                download_volume_factor: 1,
                upload_volume_factor: 1,
                guid: "guid-2",
                // match_score omitted -> ?? 0
              },
            ],
          })
        )
      )

      const result = await api.searchCrossSeedTorrent(1, "abc", {
        query: "  My Show S01  ",
      })

      // name absent on source_torrent (null) -> trimmedQuery fallback.
      expect(result.sourceTorrent.name).toBe("My Show S01")
      expect(result.results[0].matchScore).toBe(0)
      expect(result.results[0].infoUrl).toBeUndefined()
      // partial absent -> undefined
      expect(result.partial).toBeUndefined()
      expect(result.queryDegraded).toBeUndefined()
    })

    it("maps the arr_no_ids degradation reason", async () => {
      server.use(
        http.post("*/api/cross-seed/torrents/:instanceId/:hash/search", () =>
          HttpResponse.json({
            source_torrent: null,
            results: [],
            query_degraded: "arr_no_ids",
          })
        )
      )

      const result = await api.searchCrossSeedTorrent(1, "abc")

      expect(result.queryDegraded).toBe("arr_no_ids")
    })
  })

  describe("getAsyncFilteringStatus", () => {
    it("GETs async-status with raw interpolation and remaps snake_case state", async () => {
      const captured = captureNext(
        "get",
        "*/api/cross-seed/torrents/:instanceId/:hash/async-status",
        () =>
          HttpResponse.json({
            capabilities_completed: true,
            content_completed: false,
            capability_indexers: [1, 2],
            filtered_indexers: [1],
            excluded_indexers: { "2": "blocked" },
            content_matches: ["a.mkv"],
          })
      )

      const result = await api.getAsyncFilteringStatus(8, "zz")

      const req = captured.value!.request
      expect(req.method).toBe("GET")
      expect(new URL(req.url).pathname).toBe("/api/cross-seed/torrents/8/zz/async-status")
      expect(captured.value!.body).toBe("")
      expect(result).toEqual({
        capabilitiesCompleted: true,
        contentCompleted: false,
        capabilityIndexers: [1, 2],
        filteredIndexers: [1],
        excludedIndexers: { 2: "blocked" },
        contentMatches: ["a.mkv"],
      })
    })

    it("defaults excludedIndexers to {} when the map is absent", async () => {
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/async-status", () =>
          HttpResponse.json({
            capabilities_completed: false,
            content_completed: false,
            capability_indexers: [],
            filtered_indexers: [],
            // excluded_indexers omitted -> normalizeExcludedIndexerMap(undefined)
            // returns undefined, then `|| {}` defaults to empty object.
            content_matches: [],
          })
        )
      )

      const result = await api.getAsyncFilteringStatus(1, "h")
      expect(result.excludedIndexers).toEqual({})
    })
  })

  describe("applyCrossSeedSearchResults", () => {
    it("ALWAYS sends a body and serializes selections + flags into snake_case", async () => {
      const captured = captureNext(
        "post",
        "*/api/cross-seed/torrents/:instanceId/:hash/apply",
        () => HttpResponse.json({ results: [] })
      )

      await api.applyCrossSeedSearchResults(6, "hh", {
        selections: [
          {
            indexerId: 1,
            indexer: "IndexerA",
            downloadUrl: "http://dl/a",
            title: "Sel A",
            guid: "g-a",
          },
          {
            indexerId: 2,
            indexer: "IndexerB",
            downloadUrl: "http://dl/b",
            title: "Sel B",
            // guid omitted -> not serialized
          },
        ],
        useTag: true,
        tagName: "cross-seed",
        startPaused: false,
        findIndividualEpisodes: true,
      })

      const req = captured.value!.request
      expect(req.method).toBe("POST")
      expect(new URL(req.url).pathname).toBe("/api/cross-seed/torrents/6/hh/apply")
      expect(req.headers.get("content-type")).toBe("application/json")
      const sent = JSON.parse(captured.value!.body)
      expect(sent).toEqual({
        selections: [
          {
            indexer_id: 1,
            indexer: "IndexerA",
            download_url: "http://dl/a",
            title: "Sel A",
            guid: "g-a",
          },
          {
            indexer_id: 2,
            indexer: "IndexerB",
            download_url: "http://dl/b",
            title: "Sel B",
          },
        ],
        use_tag: true,
        tag_name: "cross-seed",
        start_paused: false,
        find_individual_episodes: true,
      })
    })

    it("omits optional flags when not provided (body still always sent)", async () => {
      const captured = captureNext(
        "post",
        "*/api/cross-seed/torrents/:instanceId/:hash/apply",
        () => HttpResponse.json({ results: [] })
      )

      await api.applyCrossSeedSearchResults(1, "h", {
        selections: [],
        useTag: false,
      })

      const sent = JSON.parse(captured.value!.body)
      // tag_name / start_paused / find_individual_episodes all omitted.
      expect(sent).toEqual({ selections: [], use_tag: false })
    })

    it("remaps snake_case apply results including nested instance_results and matched_torrent", async () => {
      server.use(
        http.post("*/api/cross-seed/torrents/:instanceId/:hash/apply", () =>
          HttpResponse.json({
            results: [
              {
                title: "Sel A",
                indexer: "IndexerA",
                torrent_name: "Added.Torrent",
                info_hash: "INFOHASH",
                success: true,
                instance_results: [
                  {
                    instance_id: 3,
                    instance_name: "Box3",
                    success: true,
                    status: "added",
                    message: "ok",
                    matched_torrent: {
                      hash: "MH",
                      name: "Matched",
                      progress: 1,
                      size: 500,
                    },
                  },
                  {
                    instance_id: 4,
                    instance_name: "Box4",
                    success: false,
                    status: "failed",
                    // matched_torrent absent -> matchedTorrent undefined
                  },
                ],
                error: "partial",
              },
            ],
          })
        )
      )

      const result = await api.applyCrossSeedSearchResults(1, "h", {
        selections: [],
        useTag: false,
      })

      expect(result).toEqual({
        results: [
          {
            title: "Sel A",
            indexer: "IndexerA",
            torrentName: "Added.Torrent",
            infoHash: "INFOHASH",
            success: true,
            instanceResults: [
              {
                instanceId: 3,
                instanceName: "Box3",
                success: true,
                status: "added",
                message: "ok",
                matchedTorrent: {
                  hash: "MH",
                  name: "Matched",
                  progress: 1,
                  size: 500,
                },
              },
              {
                instanceId: 4,
                instanceName: "Box4",
                success: false,
                status: "failed",
                message: undefined,
                matchedTorrent: undefined,
              },
            ],
            error: "partial",
          },
        ],
      })
    })

    it("throws APIError with status and parsed JSON data on a 409 conflict (POST error path)", async () => {
      server.use(
        http.post("*/api/cross-seed/torrents/:instanceId/:hash/apply", () =>
          HttpResponse.json(
            { error: "conflict", conflicts: ["dupe-hash"] },
            { status: 409 }
          )
        )
      )

      try {
        await api.applyCrossSeedSearchResults(1, "h", { selections: [], useTag: false })
        throw new Error("expected APIError")
      } catch (err) {
        const apiErr = err as APIError
        expect(apiErr).toBeInstanceOf(APIError)
        expect(apiErr.message).toBe("conflict")
        expect(apiErr.status).toBe(409)
        // Full parsed JSON body attached as APIError.data (409 conflict payload).
        expect(apiErr.data).toEqual({ error: "conflict", conflicts: ["dupe-hash"] })
      }
    })
  })
})
