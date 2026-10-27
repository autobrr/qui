/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// EventSource stand-in for the demo build. Serves the multiplexed /api/stream
// contract that SyncStreamContext speaks: a `connection` event, one `init` per
// subscribed stream key, then a full `update` per key every tick with the minor
// version incremented, and a `heartbeat` often enough to keep the stale
// watchdog quiet. changeDemoSubscriptions serves POST /api/stream/subscriptions.

import type { TorrentFilters } from "@/types/torrents"
import type { DemoStore } from "./store"

export interface StreamEntry {
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

// The open demo sources by connection id.
const openSources = new Map<string, { change: (add: StreamEntry[], remove: string[]) => void }>()
let nextConnectionId = 0

// changeDemoSubscriptions removes first, and an added key replaces the stream
// with the same key. It returns false when no open source has the id.
export function changeDemoSubscriptions(connectionId: string, add: StreamEntry[], remove: string[]): boolean {
  const source = openSources.get(connectionId)
  source?.change(add, remove)
  return source !== undefined
}

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
    private readonly connectionId = `demo-conn-${++nextConnectionId}`
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
      openSources.set(this.connectionId, this)
      this.emit("connection", { type: "connection", connectionId: this.connectionId })
      this.emitAll("init")
      this.tickTimer = setInterval(() => {
        store.tick()
        this.minor++
        this.emitAll("update")
      }, DEMO_TICK_MS)
      this.heartbeatTimer = setInterval(() => this.emit("heartbeat", { type: "heartbeat", timestamp: new Date().toISOString() }), DEMO_HEARTBEAT_MS)
    }

    change(add: StreamEntry[], remove: string[]) {
      const drop = new Set([...remove, ...add.map(s => s.key)])
      this.streams = [...this.streams.filter(s => !drop.has(s.key)), ...add]
      this.emitAll("init", add)
    }

    private emitAll(type: "init" | "update", streams = this.streams) {
      for (const s of streams) {
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
      openSources.delete(this.connectionId)
      if (this.tickTimer) clearInterval(this.tickTimer)
      if (this.heartbeatTimer) clearInterval(this.heartbeatTimer)
      this.tickTimer = null
      this.heartbeatTimer = null
    }
  }
}
