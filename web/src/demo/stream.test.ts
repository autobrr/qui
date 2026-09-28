/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createStore } from "./store"
import { createDemoEventSource, DEMO_HEARTBEAT_MS, DEMO_TICK_MS } from "./stream"

describe("DemoEventSource", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("speaks the multiplexed stream contract", () => {
    const store = createStore({ seed: 1, counts: [20, 5] })
    const DemoEventSource = createDemoEventSource(store)
    const key = JSON.stringify({ instanceId: 1, instanceIds: null, page: 0, limit: 3, sort: "added_on", order: "desc", search: "", filters: null })
    const streams = [{ key, instanceId: 1, instanceIds: null, page: 0, limit: 3, sort: "added_on", order: "desc", search: "", filters: null }]
    const source = new DemoEventSource(`/demo/api/stream?streams=${encodeURIComponent(JSON.stringify(streams))}`, { withCredentials: true })

    const events: Array<{ type: string; payload: { version?: { major: number; minor: number }; meta?: { streamKey?: string }; data?: { torrents: unknown[] } } }> = []
    for (const type of ["init", "update", "heartbeat"]) {
      source.addEventListener(type, e => events.push({ type, payload: JSON.parse((e as MessageEvent).data) }))
    }
    const opened = vi.fn()
    source.onopen = opened

    expect(source.readyState).toBe(0)
    vi.advanceTimersByTime(0)
    expect(source.readyState).toBe(1)
    expect(opened).toHaveBeenCalledTimes(1)
    expect(events).toHaveLength(1)
    expect(events[0].type).toBe("init")
    expect(events[0].payload.meta?.streamKey).toBe(key)
    expect(events[0].payload.version).toEqual({ major: 1, minor: 1 })
    expect(events[0].payload.data?.torrents).toHaveLength(3)

    const tick = vi.spyOn(store, "tick")
    vi.advanceTimersByTime(DEMO_TICK_MS)
    expect(tick).toHaveBeenCalledTimes(1)
    expect(events[1].type).toBe("update")
    expect(events[1].payload.version).toEqual({ major: 1, minor: 2 })

    vi.advanceTimersByTime(DEMO_HEARTBEAT_MS - DEMO_TICK_MS)
    expect(events.filter(e => e.type === "heartbeat")).toHaveLength(1)

    source.close()
    expect(source.readyState).toBe(2)
    const count = events.length
    vi.advanceTimersByTime(DEMO_HEARTBEAT_MS * 2)
    expect(events).toHaveLength(count)
  })
})
