/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { describe, expect, it } from "vitest"

import { APIError, api } from "@/lib/api"
import { makeFilters } from "@/test/mockFilters"
import { server } from "@/test/msw/server"

// Contract tests for the lib/api.ts "mutations" group (issue #1931). These call
// the REAL api client and intercept HTTP with MSW, asserting both the outgoing
// request serialization (verb, path, query, body, headers) and the parsed
// return value / error contract from the shared request<T>() wrapper.

// Capture the outgoing request so a test can inspect verb / URL / body without
// relying on the default handler's response shape.
interface Captured {
  method: string
  rawUrl: string
  url: URL
  body: unknown
  contentType: string | null
}

function captureRequest(request: Request): Promise<Captured> {
  return (async () => {
    const clone = request.clone()
    const text = await clone.text()
    return {
      method: clone.method,
      rawUrl: clone.url,
      url: new URL(clone.url),
      body: text.length > 0 ? JSON.parse(text) : undefined,
      contentType: clone.headers.get("content-type"),
    }
  })()
}

describe("api mutations group (MSW contract)", () => {
  describe("bulkAction", () => {
    it("POSTs the entire data object verbatim as JSON body with no query params", async () => {
      let captured: Captured | undefined

      server.use(
        http.post("*/api/instances/:instanceId/torrents/bulk-action", async ({ request }) => {
          captured = await captureRequest(request)
          return new HttpResponse(null, { status: 204 })
        })
      )

      const data = {
        hashes: ["abc123", "def456"],
        action: "setCategory" as const,
        category: "movies",
        selectAll: true,
        search: "ubuntu",
        excludeHashes: ["zzz999"],
        filters: makeFilters({ categories: ["movies"] }),
      }

      const result = await api.bulkAction(7, data)

      // Response: default 204 -> request<T>() returns undefined (Promise<void>).
      expect(result).toBeUndefined()

      expect(captured).toBeDefined()
      expect(captured!.method).toBe("POST")
      expect(captured!.url.pathname).toBe("/api/instances/7/torrents/bulk-action")
      // No query params: filters/excludeHashes/selectAll/category/search live in
      // the JSON body, not the query string.
      expect(captured!.url.search).toBe("")
      expect(captured!.contentType).toContain("application/json")

      // The whole object is sent verbatim (no transformation/serialization).
      expect(captured!.body).toEqual(data)
      const body = captured!.body as typeof data
      expect(body.filters).toEqual(data.filters)
      expect(body.excludeHashes).toEqual(["zzz999"])
      expect(body.selectAll).toBe(true)
      expect(body.category).toBe("movies")
      expect(body.search).toBe("ubuntu")
    })

    it("throws APIError with status, message, and full data on a 409 conflict", async () => {
      const conflictPayload = {
        message: "conflict",
        detail: { automations: ["a1", "a2"], conflicting: true },
      }

      server.use(
        http.post("*/api/instances/:instanceId/torrents/bulk-action", () =>
          HttpResponse.json(conflictPayload, { status: 409 })
        )
      )

      let thrown: unknown
      try {
        await api.bulkAction(1, { hashes: ["abc"], action: "delete" })
      } catch (err) {
        thrown = err
      }

      expect(thrown).toBeInstanceOf(APIError)
      const apiErr = thrown as APIError
      // message precedence: errorData.error ?? errorData.message -> "conflict".
      expect(apiErr.message).toBe("conflict")
      expect(apiErr.status).toBe(409)
      // .data is the full parsed JSON conflict payload.
      expect(apiErr.data).toEqual(conflictPayload)
    })
  })

  describe("renameTorrent", () => {
    it("PUTs { name } to /rename with instanceId and unencoded hash in the path", async () => {
      let captured: Captured | undefined
      // Hash with a "+" to prove the path is NOT URL-encoded by the client: the
      // WHATWG URL parser leaves "+" verbatim in a path, whereas if the client
      // called encodeURIComponent(hash) it would become "%2B". (A literal space
      // is always %20-encoded by fetch itself, so it cannot discriminate here.)
      const hash = "abc+def"

      server.use(
        http.put("*/api/instances/:instanceId/torrents/:hash/rename", async ({ request }) => {
          captured = await captureRequest(request)
          return new HttpResponse(null, { status: 204 })
        })
      )

      const result = await api.renameTorrent(3, hash, "new name")

      expect(result).toBeUndefined()
      expect(captured).toBeDefined()
      expect(captured!.method).toBe("PUT")
      // The template literal interpolates hash without encodeURIComponent, so the
      // raw "+" survives verbatim in the request URL rather than as "%2B".
      expect(captured!.rawUrl).toContain(`/instances/3/torrents/${hash}/rename`)
      expect(captured!.rawUrl).not.toContain("%2B")
      expect(captured!.body).toEqual({ name: "new name" })
    })
  })

  describe("renameTorrentFile", () => {
    it("PUTs { oldPath, newPath } to /rename-file and resolves undefined", async () => {
      let captured: Captured | undefined

      server.use(
        http.put("*/api/instances/:instanceId/torrents/:hash/rename-file", async ({ request }) => {
          captured = await captureRequest(request)
          return new HttpResponse(null, { status: 204 })
        })
      )

      const result = await api.renameTorrentFile(5, "deadbeef", "old/a.mkv", "new/b.mkv")

      expect(result).toBeUndefined()
      expect(captured).toBeDefined()
      expect(captured!.method).toBe("PUT")
      expect(captured!.url.pathname).toBe("/api/instances/5/torrents/deadbeef/rename-file")
      expect(captured!.body).toEqual({ oldPath: "old/a.mkv", newPath: "new/b.mkv" })
    })
  })

  describe("renameTorrentFolder", () => {
    it("PUTs { oldPath, newPath } to /rename-folder and resolves undefined", async () => {
      let captured: Captured | undefined

      server.use(
        http.put("*/api/instances/:instanceId/torrents/:hash/rename-folder", async ({ request }) => {
          captured = await captureRequest(request)
          return new HttpResponse(null, { status: 204 })
        })
      )

      const result = await api.renameTorrentFolder(9, "cafef00d", "old/dir", "new/dir")

      expect(result).toBeUndefined()
      expect(captured).toBeDefined()
      expect(captured!.method).toBe("PUT")
      expect(captured!.url.pathname).toBe("/api/instances/9/torrents/cafef00d/rename-folder")
      expect(captured!.body).toEqual({ oldPath: "old/dir", newPath: "new/dir" })
    })
  })
})
