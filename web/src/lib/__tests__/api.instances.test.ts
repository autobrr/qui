/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { http, HttpResponse } from "msw"
import { server } from "@/test/msw/server"
import { api, APIError } from "@/lib/api"

describe("api instances group — contract", () => {
  afterEach(() => {
    server.resetHandlers()
  })

  describe("getInstances", () => {
    it("issues GET /instances with no query/body and returns the JSON array AS-IS", async () => {
      let captured: Request | undefined
      const payload = [
        { id: 1, name: "alpha", connected: true },
        { id: 2, name: "beta", connected: false },
      ]

      server.use(
        http.get("*/api/instances", ({ request }) => {
          captured = request.clone()
          return HttpResponse.json(payload)
        })
      )

      const result = await api.getInstances()

      // Request serialization
      expect(captured).toBeDefined()
      expect(captured!.method).toBe("GET")
      const url = new URL(captured!.url)
      expect(url.pathname).toBe("/api/instances")
      expect(url.search).toBe("")
      expect(captured!.headers.get("content-type")).toBe("application/json")
      // GET requests carry no body.
      expect(captured!.body).toBeNull()

      // Response is returned with no field mapping/transformation.
      expect(result).toEqual(payload)
    })

    it("resolves undefined when the response is 204 No Content (documented edge, not [])", async () => {
      server.use(
        http.get("*/api/instances", () => new HttpResponse(null, { status: 204 }))
      )

      const result = await api.getInstances()
      expect(result).toBeUndefined()
    })

    it("resolves undefined when the response has content-length 0", async () => {
      server.use(
        http.get("*/api/instances", () =>
          new HttpResponse(null, { status: 200, headers: { "content-length": "0" } })
        )
      )

      const result = await api.getInstances()
      expect(result).toBeUndefined()
    })

    it("throws APIError with status + data and the JSON error message on non-2xx", async () => {
      const body = { error: "instances are unavailable", code: "down" }
      server.use(
        http.get("*/api/instances", () => HttpResponse.json(body, { status: 503 }))
      )

      const error = await api.getInstances().catch((e) => e)
      expect(error).toBeInstanceOf(APIError)
      expect((error as APIError).name).toBe("APIError")
      expect((error as APIError).message).toBe("instances are unavailable")
      expect((error as APIError).status).toBe(503)
      expect((error as APIError).data).toEqual(body)
    })

    it("falls back to `message` field when no `error` field is present", async () => {
      const body = { message: "boom from message" }
      server.use(
        http.get("*/api/instances", () => HttpResponse.json(body, { status: 500 }))
      )

      const error = await api.getInstances().catch((e) => e)
      expect(error).toBeInstanceOf(APIError)
      expect((error as APIError).message).toBe("boom from message")
      expect((error as APIError).status).toBe(500)
      expect((error as APIError).data).toEqual(body)
    })

    it("falls back to `HTTP error! status: <status>` when JSON carries no usable message", async () => {
      const body = { something: "else" }
      server.use(
        http.get("*/api/instances", () => HttpResponse.json(body, { status: 418 }))
      )

      const error = await api.getInstances().catch((e) => e)
      expect(error).toBeInstanceOf(APIError)
      expect((error as APIError).message).toBe("HTTP error! status: 418")
      expect((error as APIError).status).toBe(418)
      // Parsed body is still attached as data.
      expect((error as APIError).data).toEqual(body)
    })
  })

  describe("getLocalCrossSeedMatches", () => {
    const rawMatch = {
      instance_id: 7,
      instance_name: "seedbox",
      hash: "abc123",
      name: "Some.Release.2024",
      size: 1234,
      progress: 1,
      save_path: "/data/downloads",
      content_path: "/data/downloads/Some.Release.2024",
      category: "movies",
      tags: "hd,remux",
      state: "uploading",
      tracker: "tracker.example",
      tracker_health: "healthy",
      match_type: "content_path",
    }

    it("issues GET with NO query string when strict defaults to false; interpolates raw path", async () => {
      let captured: Request | undefined
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", ({ request }) => {
          captured = request.clone()
          return HttpResponse.json({ matches: [] })
        })
      )

      await api.getLocalCrossSeedMatches(42, "deadbeef")

      expect(captured).toBeDefined()
      expect(captured!.method).toBe("GET")
      const url = new URL(captured!.url)
      // instanceId + hash interpolated directly into the path (no encodeURIComponent).
      expect(url.pathname).toBe("/api/cross-seed/torrents/42/deadbeef/local-matches")
      // strict=false (default) => no query string at all.
      expect(url.search).toBe("")
      expect(captured!.headers.get("content-type")).toBe("application/json")
    })

    it("appends the hand-built `?strict=true` suffix (NOT URLSearchParams) when strict=true", async () => {
      let captured: Request | undefined
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", ({ request }) => {
          captured = request.clone()
          return HttpResponse.json({ matches: [] })
        })
      )

      await api.getLocalCrossSeedMatches(1, "hashval", true)

      const url = new URL(captured!.url)
      expect(url.pathname).toBe("/api/cross-seed/torrents/1/hashval/local-matches")
      expect(url.search).toBe("?strict=true")
      expect(url.searchParams.get("strict")).toBe("true")
    })

    it("interpolates instanceId and hash without encodeURIComponent (raw)", async () => {
      let capturedPathname: string | undefined
      server.use(
        // Match any local-matches path so we can observe the raw, unencoded value.
        http.get("*/api/cross-seed/torrents/*", ({ request }) => {
          capturedPathname = new URL(request.url).pathname
          return HttpResponse.json({ matches: [] })
        })
      )

      // A hash with a slash would only stay intact if NOT encoded; the client
      // interpolates raw, so the "/" lands literally in the path.
      await api.getLocalCrossSeedMatches(3, "a/b")

      expect(capturedPathname).toBe("/api/cross-seed/torrents/3/a/b/local-matches")
    })

    it("maps snake_case RawLocalMatch to camelCase LocalCrossSeedMatch", async () => {
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", () =>
          HttpResponse.json({ matches: [rawMatch] })
        )
      )

      const result = await api.getLocalCrossSeedMatches(7, "abc123")

      expect(result).toEqual([
        {
          instanceId: 7,
          instanceName: "seedbox",
          hash: "abc123",
          name: "Some.Release.2024",
          size: 1234,
          progress: 1,
          savePath: "/data/downloads",
          contentPath: "/data/downloads/Some.Release.2024",
          category: "movies",
          tags: "hd,remux",
          state: "uploading",
          tracker: "tracker.example",
          trackerHealth: "healthy",
          matchType: "content_path",
        },
      ])
    })

    it("returns [] when matches is empty", async () => {
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", () =>
          HttpResponse.json({ matches: [] })
        )
      )

      const result = await api.getLocalCrossSeedMatches(7, "abc123")
      expect(result).toEqual([])
    })

    it("returns [] when matches is missing from the body", async () => {
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", () =>
          HttpResponse.json({})
        )
      )

      const result = await api.getLocalCrossSeedMatches(7, "abc123")
      expect(result).toEqual([])
    })

    it("preserves an undefined tracker_health as undefined trackerHealth", async () => {
      const withoutHealth = { ...rawMatch, tracker_health: undefined }
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", () =>
          HttpResponse.json({ matches: [withoutHealth] })
        )
      )

      const result = await api.getLocalCrossSeedMatches(7, "abc123")
      expect(result[0].trackerHealth).toBeUndefined()
    })

    it("throws APIError with status + data through the shared request() wrapper", async () => {
      const body = { error: "no such instance" }
      server.use(
        http.get("*/api/cross-seed/torrents/:instanceId/:hash/local-matches", () =>
          HttpResponse.json(body, { status: 404 })
        )
      )

      const error = await api.getLocalCrossSeedMatches(99, "nope").catch((e) => e)
      expect(error).toBeInstanceOf(APIError)
      expect((error as APIError).message).toBe("no such instance")
      expect((error as APIError).status).toBe(404)
      expect((error as APIError).data).toEqual(body)
    })
  })
})
