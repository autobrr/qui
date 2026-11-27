/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, delay, http } from "msw"
import { afterEach, describe, expect, it, vi } from "vitest"

import { api } from "@/lib/api"
import { failedRequests } from "@/lib/failed-requests"
import { server } from "@/test/msw/server"
import "@/i18n"

afterEach(() => {
  sessionStorage.clear()
  vi.restoreAllMocks()
})

describe("failed requests of the browser session", () => {
  it("keeps a failed call of the API client with its method, path, status and error", async () => {
    server.use(http.get("*/api/instances/7/torrents", () => HttpResponse.json({ error: "instance offline" }, { status: 502 })))

    await expect(api.request("/instances/7/torrents?search=secret")).rejects.toThrow("instance offline")

    await vi.waitFor(() => {
      expect(failedRequests().at(-1)).toMatchObject({ method: "GET", path: "/api/instances/7/torrents", status: 502, error: "instance offline" })
    })
    expect(Number.isNaN(Date.parse(failedRequests().at(-1)!.time))).toBe(false)
  })

  it("keeps a failed call that skips request() and calls the fetch wrapper itself", async () => {
    server.use(http.post("*/api/problem-reports", () => HttpResponse.json({ error: "too_large" }, { status: 413 })))

    await expect(api.sendProblemReport({} as never, { anonymous: true })).rejects.toThrow("too_large")

    await vi.waitFor(() => {
      expect(failedRequests().at(-1)).toMatchObject({ method: "POST", path: "/api/problem-reports", status: 413, error: "too_large" })
    })
  })

  it("answers its caller before it reads the error of a failed call", async () => {
    let finishBody = () => {}
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        finishBody = () => {
          controller.enqueue(new TextEncoder().encode(JSON.stringify({ error: "disk full" })))
          controller.close()
        }
      },
    })
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(body, { status: 500, headers: { "Content-Type": "application/json" } }))

    await expect(api.downloadLogFile("qui.log")).rejects.toThrow()
    finishBody()

    await vi.waitFor(() => {
      expect(failedRequests().at(-1)).toMatchObject({ path: "/api/logs/files/qui.log", status: 500, error: "disk full" })
    })
  })

  it("keeps the status of a sign-in page that a proxy sent in place of an answer", async () => {
    server.use(http.get("*/api/instances", () => new HttpResponse("<!doctype html><html><body>Sign in</body></html>", {
      status: 403,
      headers: { "Content-Type": "text/html" },
    })))
    sessionStorage.setItem("qui_sso_recovery_attempted", "1")

    await expect(api.getInstances()).rejects.toThrow()

    expect(failedRequests().at(-1)).toMatchObject({ method: "GET", path: "/api/instances", status: 403 })
  })

  it("keeps a call that got no answer", async () => {
    server.use(http.get("*/api/version", () => HttpResponse.error()))

    await expect(api.getVersion()).rejects.toThrow()

    const last = failedRequests().at(-1)
    expect(last).toMatchObject({ method: "GET", path: "/api/version" })
    expect(last?.status).toBeUndefined()
    expect(last?.error).not.toBe("")
  })

  it("keeps no call that its caller cancelled", async () => {
    server.use(http.get("*/api/slow", async () => {
      await delay("infinite")
      return HttpResponse.json({})
    }))
    const before = failedRequests()
    const controller = new AbortController()

    const call = api.request("/slow", { signal: controller.signal })
    controller.abort()

    await expect(call).rejects.toThrow()
    expect(failedRequests()).toEqual(before)
  })

  it("keeps a call that timed out", async () => {
    server.use(http.get("*/api/slow", async () => {
      await delay("infinite")
      return HttpResponse.json({})
    }))
    const controller = new AbortController()

    const call = api.request("/slow", { signal: controller.signal })
    controller.abort(new DOMException("The operation timed out.", "TimeoutError"))

    await expect(call).rejects.toThrow()
    expect(failedRequests().at(-1)).toMatchObject({ method: "GET", path: "/api/slow" })
  })

  it("keeps no successful call", async () => {
    server.use(http.get("*/api/version", () => HttpResponse.json({ version: "1.2.3" })))
    const before = failedRequests()

    await api.getVersion()

    expect(failedRequests()).toEqual(before)
  })

  it("keeps only the last 20", async () => {
    server.use(http.get("*/api/fail/:n", () => HttpResponse.json({ error: "nope" }, { status: 500 })))

    for (let n = 0; n < 25; n++) {
      await expect(api.request(`/fail/${n}`)).rejects.toThrow()
    }

    await vi.waitFor(() => {
      expect(failedRequests().at(-1)?.path).toBe("/api/fail/24")
    })
    const paths = failedRequests().map((request) => request.path)
    expect(paths).toHaveLength(20)
    expect(paths[0]).toBe("/api/fail/5")
  })
})
