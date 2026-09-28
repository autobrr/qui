/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// EventSource stand-in for the demo build. Serves the multiplexed /api/stream
// contract that SyncStreamContext speaks: one `init` per subscribed stream key,
// then a full `update` per key every tick with the minor version incremented,
// and a `heartbeat` often enough to keep the stale watchdog quiet.

import type { TorrentFilters } from "@/types/torrents"
import type { DemoStore } from "./store"

interface StreamEntry {
  key: string
  instanceId: number
  instanceIds: number[] | null
  page: number
  limit: number
  sort: string
  order: "asc" | "desc"
  search: string
  filters: TorrentFilters | null
}

export const DEMO_TICK_MS = 2000
export const DEMO_HEARTBEAT_MS = 5000

export function createDemoEventSource(store: DemoStore) {
  return class DemoEventSource extends EventTarget {
    static readonly CONNECTING = 0
    static readonly OPEN = 1
    static readonly CLOSED = 2

    readonly url: string
    readonly withCredentials: boolean
    readyState = 0
    onopen: ((ev: Event) => void) | null = null

    private streams: StreamEntry[] = []
    private minor = 1
    private tickTimer: ReturnType<typeof setInterval> | null = null
    private heartbeatTimer: ReturnType<typeof setInterval> | null = null
    constructor(url: string | URL, init?: EventSourceInit) {
      super()
      this.url = String(url)
      this.withCredentials = init?.withCredentials ?? false
      const parsed = new URL(this.url, window.location.href)
      try {
        this.streams = JSON.parse(parsed.searchParams.get("streams") ?? "[]") as StreamEntry[]
      } catch {
        this.streams = []
      }
      // Listeners attach after construction, so the first frames wait a task.
      setTimeout(() => this.open(), 0)
    }

    private open() {
      if (this.readyState === DemoEventSource.CLOSED) return
      this.readyState = DemoEventSource.OPEN
      this.onopen?.(new Event("open"))
      this.emitAll("init")
      this.tickTimer = setInterval(() => {
        store.tick()
        this.minor++
        this.emitAll("update")
      }, DEMO_TICK_MS)
      this.heartbeatTimer = setInterval(() => this.emit("heartbeat", { type: "heartbeat", timestamp: new Date().toISOString() }), DEMO_HEARTBEAT_MS)
    }

    private emitAll(type: "init" | "update") {
      for (const s of this.streams) {
        const ids = s.instanceIds && s.instanceIds.length > 0 ? s.instanceIds : s.instanceId > 0 ? [s.instanceId] : store.instances.map(i => i.id)
        const cross = s.instanceId <= 0 || (s.instanceIds?.length ?? 0) > 0
        const data = store.query(ids, { page: s.page, limit: s.limit, sort: s.sort, order: s.order, search: s.search ?? "", filters: s.filters ?? null }, cross)
        this.emit(type, {
          type,
          data,
          version: { major: 1, minor: this.minor },
          meta: { instanceId: s.instanceId, streamKey: s.key, timestamp: new Date().toISOString(), fullUpdate: true },
        })
      }
    }

    private emit(type: string, payload: unknown) {
      if (this.readyState !== DemoEventSource.OPEN) return
      this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(payload) }))
    }

    close() {
      this.readyState = DemoEventSource.CLOSED
      if (this.tickTimer) clearInterval(this.tickTimer)
      if (this.heartbeatTimer) clearInterval(this.heartbeatTimer)
      this.tickTimer = null
      this.heartbeatTimer = null
    }
  }
}
