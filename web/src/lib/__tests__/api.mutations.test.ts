/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { describe, expect, it } from "vitest"

import { api } from "@/lib/api"
import { makeFilters } from "@/test/mockFilters"
import { server } from "@/test/msw/server"

interface Captured {
  method: string
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

      expect(result).toBeUndefined()

      expect(captured).toBeDefined()
      expect(captured!.method).toBe("POST")
      expect(captured!.url.pathname).toBe("/api/instances/7/torrents/bulk-action")
      expect(captured!.url.search).toBe("")
      expect(captured!.contentType).toContain("application/json")

      expect(captured!.body).toEqual(data)
      const body = captured!.body as typeof data
      expect(body.filters).toEqual(data.filters)
      expect(body.excludeHashes).toEqual(["zzz999"])
      expect(body.selectAll).toBe(true)
      expect(body.category).toBe("movies")
      expect(body.search).toBe("ubuntu")
    })
  })

  describe("renameTorrent", () => {
    it("PUTs { name } to /rename and resolves undefined", async () => {
      let captured: Captured | undefined

      server.use(
        http.put("*/api/instances/:instanceId/torrents/:hash/rename", async ({ request }) => {
          captured = await captureRequest(request)
          return new HttpResponse(null, { status: 204 })
        })
      )

      const result = await api.renameTorrent(3, "deadbeef", "new name")

      expect(result).toBeUndefined()
      expect(captured).toBeDefined()
      expect(captured!.method).toBe("PUT")
      expect(captured!.url.pathname).toBe("/api/instances/3/torrents/deadbeef/rename")
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
