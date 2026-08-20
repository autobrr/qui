/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { describe, expect, it } from "vitest"

import { APIError, api } from "@/lib/api"
import { server } from "@/test/msw/server"

// Contract tests for the lib/api.ts "export" method group (issue #1931).
//
// These call the REAL api client (no @/lib/api mock) and intercept HTTP with
// MSW so we assert BOTH directions: request serialization (verb, encoded path,
// absence of body/query) and response parsing into the typed return value.
//
// exportTorrent is special: it does NOT go through the shared request<T>()
// wrapper, so on a non-2xx it throws a PLAIN Error (no .status), unlike the
// APIError thrown elsewhere. That divergence is the key contract under test.

describe("api.exportTorrent — request serialization", () => {
  it("issues GET to the percent-encoded export path with no body or query", async () => {
    let captured: Request | undefined

    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", ({ request }) => {
        captured = request.clone()
        return new HttpResponse("d8:announce", {
          status: 200,
          headers: {
            "Content-Type": "application/x-bittorrent",
            "Content-Disposition": "attachment; filename=\"ubuntu.torrent\"",
          },
        })
      })
    )

    // A hash containing '/' proves encodeURIComponent is applied (contrast with
    // the crossseed methods which interpolate the raw hash).
    await api.exportTorrent(7, "ab/cd ef")

    expect(captured).toBeDefined()
    const req = captured as Request
    expect(req.method).toBe("GET")

    const url = new URL(req.url)
    expect(url.pathname).toBe("/api/instances/7/torrents/ab%2Fcd%20ef/export")
    // No query string is appended.
    expect(url.search).toBe("")
    // GET carries no request body: no Content-Type request header is serialized.
    expect(req.headers.get("Content-Type")).toBeNull()
    // ssoSafeFetch injects the XHR marker header.
    expect(req.headers.get("X-Requested-With")).toBe("XMLHttpRequest")
  })
})

describe("api.exportTorrentsArchive", () => {
  it("posts archive targets and returns the ZIP filename", async () => {
    let captured: Request | undefined
    const targets = [{
      instanceId: 7,
      instanceName: "Home",
      hash: "deadbeef",
      category: "Movies",
    }]

    server.use(
      http.post("*/api/torrents/export", ({ request }) => {
        captured = request.clone()
        return new HttpResponse("zipdata", {
          status: 200,
          headers: { "Content-Disposition": "attachment; filename=qui-torrents.zip" },
        })
      })
    )

    const result = await api.exportTorrentsArchive(targets)

    expect(captured?.method).toBe("POST")
    expect(await captured?.json()).toEqual({ targets })
    expect(await result.blob.text()).toBe("zipdata")
    expect(result.filename).toBe("qui-torrents.zip")
  })
})

describe("api.exportTorrent — response parsing", () => {
  it("returns a Blob and the filename parsed from a quoted content-disposition", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse("binarydata", {
          status: 200,
          headers: {
            "Content-Type": "application/x-bittorrent",
            "Content-Disposition": "attachment; filename=\"my file.torrent\"",
          },
        })
      )
    )

    const result = await api.exportTorrent(1, "deadbeef")

    // Assert the Blob structurally: the jsdom/undici Blob constructor differs
    // from the global one vitest's `toBeInstanceOf` checks against, so verify
    // the Blob contract (numeric size + readable body) instead.
    expect(typeof result.blob.size).toBe("number")
    expect(await result.blob.text()).toBe("binarydata")
    expect(result.filename).toBe("my file.torrent")
  })

  it("parses an unquoted filename= content-disposition (optional-quote branch)", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse("x", {
          status: 200,
          headers: {
            // No surrounding quotes — exercises the optional-quote regex branch.
            "Content-Disposition": "attachment; filename=plain.torrent",
          },
        })
      )
    )

    const result = await api.exportTorrent(1, "deadbeef")

    expect(typeof result.blob.size).toBe("number")
    expect(result.filename).toBe("plain.torrent")
  })

  it("decodeURIComponent()s a filename*=UTF-8'' content-disposition", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse("x", {
          status: 200,
          headers: {
            // %E2%98%83 is the snowman ☃ — proves decodeURIComponent runs.
            "Content-Disposition": "attachment; filename*=UTF-8''na%CC%88me%E2%98%83.torrent",
          },
        })
      )
    )

    const result = await api.exportTorrent(1, "deadbeef")

    expect(typeof result.blob.size).toBe("number")
    expect(result.filename).toBe(decodeURIComponent("na%CC%88me%E2%98%83.torrent"))
    expect(result.filename).toContain("☃")
  })

  it("returns filename === null when the content-disposition header is absent", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse("x", { status: 200 })
      )
    )

    const result = await api.exportTorrent(1, "deadbeef")

    expect(typeof result.blob.size).toBe("number")
    expect(result.filename).toBeNull()
  })

  it("returns filename === null when the content-disposition is unparseable", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse("x", {
          status: 200,
          headers: {
            // No filename / filename* token at all.
            "Content-Disposition": "attachment",
          },
        })
      )
    )

    const result = await api.exportTorrent(1, "deadbeef")

    expect(result.filename).toBeNull()
  })
})

describe("api.exportTorrent — error contract (plain Error, NOT APIError)", () => {
  it("throws a plain Error with NO .status for a JSON error body, using errorData.error", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        HttpResponse.json({ error: "torrent not found" }, { status: 404 })
      )
    )

    let thrown: unknown
    try {
      await api.exportTorrent(1, "missinghash")
    } catch (err) {
      thrown = err
    }

    expect(thrown).toBeInstanceOf(Error)
    // KEY contract difference: it is a PLAIN Error, not the APIError thrown by request<T>().
    expect(thrown instanceof APIError).toBe(false)
    expect((thrown as { status?: unknown }).status).toBeUndefined()
    expect((thrown as Error).message).toBe("torrent not found")
  })

  it("falls back to `HTTP error! status: <status>` when the error body is empty", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse(null, { status: 500 })
      )
    )

    let thrown: unknown
    try {
      await api.exportTorrent(1, "deadbeef")
    } catch (err) {
      thrown = err
    }

    expect(thrown).toBeInstanceOf(Error)
    expect(thrown instanceof APIError).toBe(false)
    expect((thrown as { status?: unknown }).status).toBeUndefined()
    expect((thrown as Error).message).toBe("HTTP error! status: 500")
  })

  it("annotates an HTML error body rather than surfacing raw markup", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse("<html><body>Bad Gateway</body></html>", {
          status: 502,
          headers: { "Content-Type": "text/html" },
        })
      )
    )

    let thrown: unknown
    try {
      await api.exportTorrent(1, "deadbeef")
    } catch (err) {
      thrown = err
    }

    expect(thrown).toBeInstanceOf(Error)
    expect(thrown instanceof APIError).toBe(false)
    expect((thrown as { status?: unknown }).status).toBeUndefined()
    expect((thrown as Error).message).toBe("HTTP error! status: 502 (server returned HTML error page)")
  })
})
