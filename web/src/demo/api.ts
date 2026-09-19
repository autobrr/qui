/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Fake API for the demo build: a fetch router over the in-memory store.
// Every response is JSON with a content-type, never HTML and never a thrown
// error, because ssoSafeFetch in lib/api.ts treats both as an expired SSO
// session and navigates the page away. No 401 is ever returned for the same
// reason: a 401 outside /auth/me sends the app to /login.

import type { TorrentFilters } from "@/types/torrents"
import { CAPABILITIES, PREFERENCES, trackerDomain, type BulkActionBody, type DemoStore, type ListParams } from "./store"

// The built-in themes the Go registry embeds, so the picker and the default
// look match a real install. Name and id follow internal/themes/registry.go.
const themeCSS = import.meta.glob("../../../internal/themes/assets/*.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>
const builtinThemes = Object.entries(themeCSS).map(([file, css]) => {
  const name = /@name:\s*(.+?)\s*(?:\n|\*)/.exec(css)?.[1] ?? file.replace(/.*\/|\.css$/g, "")
  return {
    id: name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""),
    name,
    description: /@description:\s*(.+?)\s*(?:\n|\*)/.exec(css)?.[1],
    premium: false,
    css,
  }
}).sort((a, b) => (a.id === "minimal" ? -1 : b.id === "minimal" ? 1 : a.name.localeCompare(b.name)))

type Handler = (ctx: { params: Record<string, string>; url: URL; request: Request }) => Promise<Response> | Response

interface Route {
  method: string
  pattern: RegExp
  keys: string[]
  handler: Handler
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
}

function noContent(): Response {
  return new Response(null, { status: 204 })
}

function compile(path: string): { pattern: RegExp; keys: string[] } {
  const keys: string[] = []
  const source = path.replace(/:([a-zA-Z]+)/g, (_, key: string) => {
    keys.push(key)
    return "([^/]+)"
  })
  return { pattern: new RegExp(`^${source}$`), keys }
}

export function parseListParams(url: URL): ListParams {
  const p = url.searchParams
  let filters: TorrentFilters | null = null
  const raw = p.get("filters")
  if (raw) {
    try {
      filters = JSON.parse(raw) as TorrentFilters
    } catch {
      filters = null
    }
  }
  return {
    page: Number(p.get("page") ?? 0),
    limit: Number(p.get("limit") ?? 300),
    sort: p.get("sort") ?? "added_on",
    order: p.get("order") === "asc" ? "asc" : "desc",
    search: p.get("search") ?? "",
    filters,
  }
}

async function bodyJSON<T>(request: Request): Promise<T> {
  try {
    return (await request.json()) as T
  } catch {
    return {} as T
  }
}

export function createRoutes(store: DemoStore): Route[] {
  const routes: Route[] = []
  const on = (method: string, path: string, handler: Handler) => {
    const { pattern, keys } = compile(path)
    routes.push({ method, pattern, keys, handler })
  }
  const id = (params: Record<string, string>) => Number(params.instanceId)

  on("GET", "/auth/me", () => json({ id: 1, username: "demo", auth_method: "demo" }))
  on("GET", "/auth/check-setup", () => json({ setupRequired: false }))
  on("POST", "/auth/logout", () => noContent())

  on("GET", "/themes", () => json({ themes: builtinThemes }))
  on("GET", "/themes/settings", () => json(null))
  on("PUT", "/themes/settings", async ({ request }) => json(await bodyJSON(request)))
  on("GET", "/themes/custom", () => json({ directory: "", themes: [] }))
  on("GET", "/client-settings", () => json({}))
  on("PUT", "/client-settings", () => noContent())
  on("GET", "/filter-views", () => json([]))
  on("GET", "/tracker-icons", () => json({}))
  on("GET", "/license/licensed", () => json({ licensed: false }))
  on("GET", "/version/latest", () => json(null))

  on("GET", "/instances", () => json(store.instanceResponses()))
  on("GET", "/instances/:instanceId/capabilities", () => json(CAPABILITIES))
  on("GET", "/instances/:instanceId/preferences", () => json(PREFERENCES))
  on("GET", "/instances/:instanceId/transfer-info", ({ params }) => {
    const s = store.instance(id(params))?.serverState
    return json(s ? {
      connection_status: s.connection_status, dht_nodes: s.dht_nodes, dl_info_data: s.dl_info_data,
      dl_info_speed: s.dl_info_speed, dl_rate_limit: s.dl_rate_limit, up_info_data: s.up_info_data,
      up_info_speed: s.up_info_speed, up_rate_limit: s.up_rate_limit,
    } : null)
  })
  on("GET", "/instances/:instanceId/torrent-creator/count", () => json({ count: 0 }))
  on("GET", "/instances/:instanceId/categories", ({ params }) => json(store.instance(id(params))?.categories ?? {}))
  on("GET", "/instances/:instanceId/tags", ({ params }) => json([...(store.instance(id(params))?.tags ?? [])].sort()))
  on("GET", "/instances/:instanceId/trackers", ({ params }) => {
    const out: Record<string, string> = {}
    for (const t of store.instance(id(params))?.torrents ?? []) out[trackerDomain(t)] ??= t.tracker
    return json(out)
  })

  on("GET", "/instances/:instanceId/torrents", ({ params, url }) => json(store.query([id(params)], parseListParams(url), false)))
  on("GET", "/torrents/cross-instance", ({ url }) => {
    const ids = (url.searchParams.get("instanceIds") ?? "").split(",").map(Number).filter(n => n > 0)
    return json(store.query(ids.length ? ids : store.instances.map(i => i.id), parseListParams(url), true))
  })
  on("POST", "/instances/:instanceId/torrents/field", async ({ params, request }) => {
    const body = await bodyJSON<BulkActionBody & { field: string; sort?: string; order?: "asc" | "desc" }>(request)
    const ids = body.instanceIds?.length ? body.instanceIds : [id(params)]
    const rows = store.query(ids, {
      page: 0, limit: Number.MAX_SAFE_INTEGER, sort: body.sort ?? "added_on", order: body.order ?? "desc",
      search: body.search ?? "", filters: body.filters ?? null,
    }, ids.length > 1)
    const list = ids.length > 1 ? rows.crossInstanceTorrents ?? [] : rows.torrents
    const wanted = body.selectAll ? null : new Set([...(body.hashes ?? []), ...(body.targets ?? []).map(t => t.hash)])
    const excluded = new Set([...(body.excludeHashes ?? []), ...(body.excludeTargets ?? []).map(t => t.hash)])
    const values = list
      .filter(t => (wanted ? wanted.has(t.hash) : true) && !excluded.has(t.hash))
      .map(t => {
        switch (body.field) {
          case "hash": return t.hash
          case "full_path": return t.content_path
          case "tags": return t.tags
          case "magnet_uri": return `magnet:?xt=urn:btih:${t.hash}&dn=${encodeURIComponent(t.name)}`
          default: return t.name
        }
      })
    return json({ values, total: values.length })
  })
  on("POST", "/instances/:instanceId/torrents/bulk-action", async ({ params, request }) => {
    store.bulkAction(id(params), await bodyJSON<BulkActionBody>(request))
    return noContent()
  })
  on("POST", "/instances/:instanceId/torrents", async ({ params, request }) => {
    const form = await request.formData().catch(() => null)
    const urls = String(form?.get("urls") ?? "").split("\n").map(s => s.trim()).filter(Boolean)
    const files = (form?.getAll("torrent") ?? []).filter((f): f is File => f instanceof File)
    const names = [
      ...urls.map(u => new URLSearchParams(u.split("?")[1] ?? "").get("dn") ?? "magnet-download"),
      ...files.map(f => f.name.replace(/\.torrent$/i, "")),
    ]
    const category = String(form?.get("category") ?? "")
    const tags = String(form?.get("tags") ?? "").split(",").map(s => s.trim()).filter(Boolean)
    const paused = form?.get("paused") === "true"
    const autoTMM = form?.get("autoTMM") !== "false"
    const savePath = String(form?.get("savepath") ?? "")
    for (const name of names) store.addTorrent(id(params), name, category, tags, paused, autoTMM, savePath)
    return json({ message: "ok", added: names.length, failed: 0 })
  })

  on("PUT", "/instances/:instanceId/torrents/:hash/rename", async ({ params, request }) => {
    const body = await bodyJSON<{ name?: string }>(request)
    if (body.name) store.rename(id(params), params.hash, body.name)
    return noContent()
  })
  const renamePath: Handler = async ({ params, request }) => {
    const body = await bodyJSON<{ oldPath?: string; newPath?: string }>(request)
    if (body.oldPath && body.newPath) store.renamePath(id(params), params.hash, body.oldPath, body.newPath)
    return noContent()
  }
  on("PUT", "/instances/:instanceId/torrents/:hash/rename-file", renamePath)
  on("PUT", "/instances/:instanceId/torrents/:hash/rename-folder", renamePath)
  on("PUT", "/instances/:instanceId/torrents/:hash/files", async ({ params, request }) => {
    const body = await bodyJSON<{ indices?: number[]; priority?: number }>(request)
    store.setFilePriority(id(params), params.hash, body.indices ?? [], body.priority ?? 1)
    return noContent()
  })
  on("POST", "/instances/:instanceId/torrents/add-peers", () => noContent())
  on("POST", "/instances/:instanceId/torrents/ban-peers", async ({ params, request }) => {
    const body = await bodyJSON<{ peers?: string[] }>(request)
    store.banPeers(id(params), body.peers ?? [])
    return noContent()
  })
  on("POST", "/instances/:instanceId/alternative-speed-limits/toggle", ({ params }) => json({ enabled: store.toggleAltSpeedLimits(id(params)) }))

  const detail = (part: keyof NonNullable<ReturnType<DemoStore["details"]>>) => ({ params }: { params: Record<string, string> }) => {
    const d = store.details(id(params), params.hash)
    return d ? json(d[part]) : json({ error: "torrent not found" }, 404)
  }
  on("GET", "/instances/:instanceId/torrents/:hash/properties", detail("properties"))
  on("GET", "/instances/:instanceId/torrents/:hash/trackers", detail("trackers"))
  on("GET", "/instances/:instanceId/torrents/:hash/files", detail("files"))
  on("GET", "/instances/:instanceId/torrents/:hash/peers", detail("peers"))
  on("GET", "/instances/:instanceId/torrents/:hash/pieces", detail("pieces"))
  on("GET", "/instances/:instanceId/torrents/:hash/webseeds", () => json([]))
  on("GET", "/instances/:instanceId/torrents/:hash/disc-scans", () => json([]))
  on("GET", "/cross-seed/torrents/:instanceId/:hash/local-matches", () => json([]))

  const upsertCategory: Handler = async ({ params, request }) => {
    const body = await bodyJSON<{ name?: string; savePath?: string }>(request)
    if (body.name) store.addCategory(id(params), body.name, body.savePath ?? "")
    return noContent()
  }
  on("POST", "/instances/:instanceId/categories", upsertCategory)
  on("PUT", "/instances/:instanceId/categories", upsertCategory)
  on("DELETE", "/instances/:instanceId/categories", async ({ params, request }) => {
    const body = await bodyJSON<{ categories?: string[] }>(request)
    store.removeCategories(id(params), body.categories ?? [])
    return noContent()
  })
  on("POST", "/instances/:instanceId/tags", async ({ params, request }) => {
    const body = await bodyJSON<{ tags?: string[] }>(request)
    store.addTags(id(params), body.tags ?? [])
    return noContent()
  })
  on("DELETE", "/instances/:instanceId/tags", async ({ params, request }) => {
    const body = await bodyJSON<{ tags?: string[] }>(request)
    store.removeTags(id(params), body.tags ?? [])
    return noContent()
  })

  return routes
}

export function createDemoFetch(store: DemoStore, apiBase: string, fallback: typeof fetch): typeof fetch {
  const routes = createRoutes(store)
  return async (input, init) => {
    const request = new Request(input, init)
    const url = new URL(request.url)
    if (!url.pathname.startsWith(apiBase)) return fallback(input, init)
    const path = url.pathname.slice(apiBase.length).replace(/\/$/, "") || "/"
    for (const route of routes) {
      if (route.method !== request.method) continue
      const match = route.pattern.exec(path)
      if (!match) continue
      const params: Record<string, string> = {}
      route.keys.forEach((key, i) => { params[key] = decodeURIComponent(match[i + 1]) })
      return route.handler({ params, url, request })
    }
    return json({ error: "not available in the demo" }, 404)
  }
}
