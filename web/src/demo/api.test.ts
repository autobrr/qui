/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it, vi } from "vitest"
import { createDemoFetch } from "./api"
import { createStore } from "./store"

const store = createStore({ seed: 3, counts: [30, 10] })
const fallback = vi.fn(async () => new Response("outside", { status: 200 }))
const fetch = createDemoFetch(store, "/demo/api", fallback as unknown as typeof globalThis.fetch)
const hash = () => store.instances[0].torrents[0].hash

describe("createDemoFetch", () => {
  it.each([
    ["GET", "/auth/me"],
    ["GET", "/instances"],
    ["GET", "/instances/1/capabilities"],
    ["GET", "/instances/1/preferences"],
    ["GET", "/instances/1/torrents?page=0&limit=5"],
    ["GET", "/torrents/cross-instance?instanceIds=1,2&limit=5"],
    ["GET", "/instances/1/categories"],
    ["GET", "/instances/1/tags"],
    ["GET", "/themes"],
    ["GET", "/themes/settings"],
    ["GET", "/client-settings"],
    ["GET", "/filter-views"],
    ["GET", "/tracker-icons"],
    ["GET", "/version/latest"],
  ])("%s %s answers JSON", async (method, path) => {
    const res = await fetch(`http://localhost/demo/api${path}`, { method })
    expect(res.status).toBe(200)
    expect(res.headers.get("content-type")).toBe("application/json")
    await expect(res.json()).resolves.toBeDefined()
  })

  it.each(["properties", "trackers", "files", "peers", "pieces", "webseeds"])("serves torrent %s", async (part) => {
    const res = await fetch(`http://localhost/demo/api/instances/1/torrents/${hash()}/${part}`)
    expect(res.status).toBe(200)
    await expect(res.json()).resolves.toBeDefined()
  })

  it("lists with the same params the api client sends", async () => {
    const filters = encodeURIComponent(JSON.stringify({ status: ["completed"], categories: [], tags: [] }))
    const res = await fetch(`http://localhost/demo/api/instances/1/torrents?page=0&limit=3&sort=size&order=asc&filters=${filters}`)
    const body = await res.json()
    expect(body.torrents).toHaveLength(3)
    expect(body.torrents.every((t: { progress: number }) => t.progress === 1)).toBe(true)
    expect(body.counts.total).toBe(30)
  })

  it("mutates through bulk-action and answers 204", async () => {
    const h = hash()
    const res = await fetch("http://localhost/demo/api/instances/1/torrents/bulk-action", {
      method: "POST", body: JSON.stringify({ hashes: [h], action: "pause" }),
    })
    expect(res.status).toBe(204)
    expect(store.find(1, h)!.state).toMatch(/^stopped/)
  })

  it("copies fields for a selection", async () => {
    const h = hash()
    const res = await fetch("http://localhost/demo/api/instances/1/torrents/field", {
      method: "POST", body: JSON.stringify({ field: "hash", hashes: [h] }),
    })
    await expect(res.json()).resolves.toEqual({ values: [h], total: 1 })
  })

  it("adds a torrent from a magnet form", async () => {
    const form = new FormData()
    form.set("urls", "magnet:?xt=urn:btih:abc&dn=fresh%20iso")
    form.set("category", "films")
    const res = await fetch("http://localhost/demo/api/instances/1/torrents", { method: "POST", body: form })
    await expect(res.json()).resolves.toMatchObject({ added: 1 })
    expect(store.instances[0].torrents[0].name).toBe("fresh iso")
  })

  it("keys trackers by domain like the real endpoint", async () => {
    const res = await fetch("http://localhost/demo/api/instances/1/trackers")
    const body = await res.json() as Record<string, string>
    expect(Object.keys(body).every(k => !k.includes("/"))).toBe(true)
    expect(Object.values(body).every(v => v.startsWith("https://"))).toBe(true)
  })

  it("answers a miss with JSON 404, never HTML or a throw", async () => {
    const res = await fetch("http://localhost/demo/api/backups")
    expect(res.status).toBe(404)
    expect(res.headers.get("content-type")).toBe("application/json")
    await expect(res.json()).resolves.toEqual({ error: "not available in the demo" })
  })

  it("passes non-API requests to the real fetch", async () => {
    await fetch("http://localhost/demo/assets/x.js")
    expect(fallback).toHaveBeenCalledTimes(1)
  })
})
