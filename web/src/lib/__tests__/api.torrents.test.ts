/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { APIError, api } from "@/lib/api"
import { makeFilters } from "@/test/mockFilters"
import { makeServerState } from "@/test/mockServerState"
import { makeTorrent } from "@/test/mockTorrent"
import { server } from "@/test/msw/server"
import type { TorrentResponse } from "@/types"

// Contract tests for the lib/api.ts "torrents" method group (issue #1931).
// These call the REAL api client (no @/lib/api mock) and intercept HTTP with
// MSW, asserting BOTH the outgoing request serialization (verb / path / query)
// and the response parsing / normalization into the typed return value, plus
// the real error contract (APIError, not a plain Error).

// Capture the outgoing Request so a test can assert its serialization. Each
// override clones the request because the body/url stream can only be read once.
let capturedRequest: Request | undefined

beforeEach(() => {
  capturedRequest = undefined
})

afterEach(() => {
  vi.restoreAllMocks()
})

const okTorrentResponse = (overrides: Partial<TorrentResponse> = {}): TorrentResponse => ({
  torrents: [makeTorrent()],
  total: 1,
  serverState: makeServerState(),
  ...overrides,
})

const captureTorrents = (body: TorrentResponse = okTorrentResponse()) =>
  http.get("*/api/instances/:instanceId/torrents", ({ request }) => {
    capturedRequest = request.clone()
    return HttpResponse.json(body)
  })

const captureCrossInstance = (body: TorrentResponse = okTorrentResponse()) =>
  http.get("*/api/torrents/cross-instance", ({ request }) => {
    capturedRequest = request.clone()
    return HttpResponse.json(body)
  })

const capturedUrl = (): URL => {
  if (!capturedRequest) {
    throw new Error("no request captured")
  }
  return new URL(capturedRequest.url)
}

describe("api.getTorrents", () => {
  describe("request serialization", () => {
    it("interpolates instanceId into the path and issues a GET with JSON content-type", async () => {
      server.use(captureTorrents())

      await api.getTorrents(42, {})

      expect(capturedRequest?.method).toBe("GET")
      expect(capturedUrl().pathname).toMatch(/\/api\/instances\/42\/torrents$/)
      expect(capturedRequest?.headers.get("content-type")).toBe("application/json")
    })

    it("only emits page/limit when defined and serializes them as strings", async () => {
      server.use(captureTorrents())

      await api.getTorrents(1, { page: 0, limit: 50 })

      const params = capturedUrl().searchParams
      expect(params.get("page")).toBe("0")
      expect(params.get("limit")).toBe("50")
    })

    it("omits page/limit keys entirely when undefined", async () => {
      server.use(captureTorrents())

      await api.getTorrents(1, {})

      const params = capturedUrl().searchParams
      expect(params.has("page")).toBe(false)
      expect(params.has("limit")).toBe(false)
    })

    it("only emits sort/order/search when truthy", async () => {
      server.use(captureTorrents())

      await api.getTorrents(1, { sort: "name", order: "desc", search: "ubuntu" })

      const params = capturedUrl().searchParams
      expect(params.get("sort")).toBe("name")
      expect(params.get("order")).toBe("desc")
      expect(params.get("search")).toBe("ubuntu")
    })

    it("omits falsy sort/order/search (empty strings) from the query", async () => {
      server.use(captureTorrents())

      await api.getTorrents(1, { sort: "", search: "" })

      const params = capturedUrl().searchParams
      expect(params.has("sort")).toBe(false)
      expect(params.has("order")).toBe(false)
      expect(params.has("search")).toBe(false)
    })

    it("serializes filters as JSON.stringify under the 'filters' key (URL-decoded round-trips)", async () => {
      server.use(captureTorrents())

      const filters = makeFilters({ status: ["downloading"], expr: "size > 1" })
      await api.getTorrents(1, { filters })

      const raw = capturedUrl().searchParams.get("filters")
      expect(raw).not.toBeNull()
      // URLSearchParams decodes the value for us; it must equal the JSON source.
      expect(JSON.parse(raw as string)).toEqual(filters)
    })

    it("serializes preferCached=true as prefer=stale (param name 'prefer', literal 'stale')", async () => {
      server.use(captureTorrents())

      await api.getTorrents(1, { preferCached: true })

      const params = capturedUrl().searchParams
      expect(params.get("prefer")).toBe("stale")
      expect(params.has("preferCached")).toBe(false)
    })

    it("omits the prefer key when preferCached is falsy or absent", async () => {
      server.use(captureTorrents())

      await api.getTorrents(1, { preferCached: false })
      expect(capturedUrl().searchParams.has("prefer")).toBe(false)

      capturedRequest = undefined
      await api.getTorrents(1, {})
      expect(capturedUrl().searchParams.has("prefer")).toBe(false)
    })
  })

  describe("response parsing", () => {
    it("returns response.json() verbatim with caller-visible total and hasMore", async () => {
      const body = okTorrentResponse({
        torrents: [makeTorrent({ name: "alpha" })],
        total: 7,
        hasMore: true,
      })
      server.use(captureTorrents(body))

      const result = await api.getTorrents(1, {})

      expect(result.total).toBe(7)
      expect(result.hasMore).toBe(true)
      expect(result.torrents).toHaveLength(1)
      expect(result.torrents[0].name).toBe("alpha")
    })

    it("resolves to undefined for an empty 204 response (cast as TorrentResponse)", async () => {
      server.use(
        http.get("*/api/instances/:instanceId/torrents", () =>
          new HttpResponse(null, { status: 204 })
        )
      )

      const result = await api.getTorrents(1, {})

      expect(result).toBeUndefined()
    })
  })

  describe("error contract", () => {
    it("throws an APIError with .status and .data from a non-2xx JSON body", async () => {
      server.use(
        http.get("*/api/instances/:instanceId/torrents", () =>
          HttpResponse.json({ error: "boom", conflict: true }, { status: 409 })
        )
      )

      try {
        await api.getTorrents(1, {})
        expect.unreachable("expected getTorrents to throw")
      } catch (error) {
        expect(error).toBeInstanceOf(APIError)
        const apiError = error as APIError
        expect(apiError.name).toBe("APIError")
        expect(apiError.message).toBe("boom")
        expect(apiError.status).toBe(409)
        expect(apiError.data).toEqual({ error: "boom", conflict: true })
      }
    })

    it("falls back to 'HTTP error! status: <status>' for an empty error body", async () => {
      server.use(
        http.get("*/api/instances/:instanceId/torrents", () =>
          new HttpResponse(null, { status: 500 })
        )
      )

      await expect(api.getTorrents(1, {})).rejects.toMatchObject({
        name: "APIError",
        status: 500,
        message: "HTTP error! status: 500",
      })
    })
  })
})

describe("api.getCrossInstanceTorrents", () => {
  describe("request serialization", () => {
    it("targets the top-level /torrents/cross-instance path with a GET", async () => {
      server.use(captureCrossInstance())

      await api.getCrossInstanceTorrents({})

      expect(capturedRequest?.method).toBe("GET")
      expect(capturedUrl().pathname).toMatch(/\/api\/torrents\/cross-instance$/)
    })

    it("emits page/limit/sort/order/search/filters with the same rules as getTorrents", async () => {
      server.use(captureCrossInstance())

      const filters = makeFilters({ tags: ["x"] })
      await api.getCrossInstanceTorrents({
        page: 2,
        limit: 25,
        sort: "ratio",
        order: "asc",
        search: "iso",
        filters,
      })

      const params = capturedUrl().searchParams
      expect(params.get("page")).toBe("2")
      expect(params.get("limit")).toBe("25")
      expect(params.get("sort")).toBe("ratio")
      expect(params.get("order")).toBe("asc")
      expect(params.get("search")).toBe("iso")
      expect(JSON.parse(params.get("filters") as string)).toEqual(filters)
    })

    it("never emits a prefer/preferCached param (this method has no preferCached option)", async () => {
      server.use(captureCrossInstance())

      await api.getCrossInstanceTorrents({ page: 1 })

      const params = capturedUrl().searchParams
      expect(params.has("prefer")).toBe(false)
      expect(params.has("preferCached")).toBe(false)
    })

    it("emits instanceIds as a comma-joined string when truthy and non-empty", async () => {
      server.use(captureCrossInstance())

      await api.getCrossInstanceTorrents({ instanceIds: [1, 2, 3] })

      // URLSearchParams.get URL-decodes, so the %2C separators come back as commas.
      expect(capturedUrl().searchParams.get("instanceIds")).toBe("1,2,3")
    })

    it("omits instanceIds when undefined or an empty array", async () => {
      server.use(captureCrossInstance())

      await api.getCrossInstanceTorrents({ instanceIds: [] })
      expect(capturedUrl().searchParams.has("instanceIds")).toBe(false)

      capturedRequest = undefined
      await api.getCrossInstanceTorrents({})
      expect(capturedUrl().searchParams.has("instanceIds")).toBe(false)
    })
  })

  describe("response normalization", () => {
    it("promotes snake_case instance identity and writes the same array to both casings", async () => {
      const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {})
      const snakeRow = {
        ...makeTorrent({ name: "snake" }),
        instance_id: 5,
        instance_name: "seedbox",
      }
      server.use(
        captureCrossInstance(
          okTorrentResponse({
            cross_instance_torrents: [snakeRow] as unknown as TorrentResponse["cross_instance_torrents"],
          })
        )
      )

      const result = await api.getCrossInstanceTorrents({})

      expect(result.crossInstanceTorrents).toHaveLength(1)
      expect(result.crossInstanceTorrents?.[0].instanceId).toBe(5)
      expect(result.crossInstanceTorrents?.[0].instanceName).toBe("seedbox")
      // The identical normalized array is written onto BOTH fields.
      expect(result.cross_instance_torrents).toBe(result.crossInstanceTorrents)
      expect(errorSpy).not.toHaveBeenCalled()
    })

    it("drops rows missing instance identity entirely rather than rendering them", async () => {
      const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {})
      const validRow = {
        ...makeTorrent({ name: "kept" }),
        instance_id: 9,
        instance_name: "main",
      }
      // No instanceId/instanceName and no snake_case identity => dropped.
      const orphanRow = { ...makeTorrent({ name: "orphan" }) }
      server.use(
        captureCrossInstance(
          okTorrentResponse({
            cross_instance_torrents: [validRow, orphanRow] as TorrentResponse["cross_instance_torrents"],
          })
        )
      )

      const result = await api.getCrossInstanceTorrents({})

      expect(result.crossInstanceTorrents).toHaveLength(1)
      expect(result.crossInstanceTorrents?.[0].instanceId).toBe(9)
      expect(result.crossInstanceTorrents?.[0].instanceName).toBe("main")
      expect(errorSpy).toHaveBeenCalledTimes(1)
    })

    it("returns camelCase rows unchanged when every row already has instance identity", async () => {
      const camelRows = [
        { ...makeTorrent({ name: "a" }), instanceId: 1, instanceName: "one" },
        { ...makeTorrent({ name: "b" }), instanceId: 2, instanceName: "two" },
      ]
      server.use(
        captureCrossInstance(
          okTorrentResponse({
            total: 2,
            crossInstanceTorrents: camelRows as TorrentResponse["crossInstanceTorrents"],
          })
        )
      )

      const result = await api.getCrossInstanceTorrents({})

      expect(result.crossInstanceTorrents).toHaveLength(2)
      expect(result.crossInstanceTorrents?.map(t => t.instanceId)).toEqual([1, 2])
      expect(result.total).toBe(2)
      // Pass-through reuses the parsed array for both casings.
      expect(result.cross_instance_torrents).toBe(result.crossInstanceTorrents)
    })
  })

  describe("error contract", () => {
    it("throws an APIError before normalization runs on a non-2xx response", async () => {
      server.use(
        http.get("*/api/torrents/cross-instance", () =>
          HttpResponse.json({ error: "cross boom" }, { status: 502 })
        )
      )

      await expect(api.getCrossInstanceTorrents({})).rejects.toMatchObject({
        name: "APIError",
        status: 502,
        message: "cross boom",
      })
    })
  })
})
