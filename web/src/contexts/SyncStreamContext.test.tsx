/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { resolveStreamRow } from "@/lib/torrent-utils"
import { isClientConnectionErrorCode, isStreamUsable, STREAM_ERROR_DISCONNECTED, STREAM_ERROR_RETRY_EXHAUSTED, streamFallbackPollInterval } from "@/lib/sync-stream-state"
import {
  createStreamKey,
  SyncStreamProvider,
  useActivityStream,
  useSyncStream,
  useSyncStreamManager,
  type StreamParams,
  type StreamState
} from "@/contexts/SyncStreamContext"
import type { TorrentStreamPayload } from "@/types"
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query"
import { act, cleanup, render } from "@testing-library/react"
import { useEffect, useMemo, useState, type ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

function TestProviders({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient({
    defaultOptions: { queries: { retry: false } },
  }))
  return (
    <QueryClientProvider client={client}>
      <SyncStreamProvider>{children}</SyncStreamProvider>
    </QueryClientProvider>
  )
}

type SourceEventName = "connection" | "init" | "update" | "delta" | "stream-error" | "heartbeat" | "activity"

class MockEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2

  static instances: MockEventSource[] = []

  readonly url: string
  readonly withCredentials: boolean
  readyState = MockEventSource.CONNECTING
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null

  private listeners: Record<string, Set<(event: MessageEvent | Event) => void>> = {}
  closed = false

  constructor(url: string, init?: { withCredentials?: boolean }) {
    this.url = url
    this.withCredentials = Boolean(init?.withCredentials)
    MockEventSource.instances.push(this)
  }

  addEventListener(type: string, listener: (event: MessageEvent | Event) => void) {
    if (!this.listeners[type]) {
      this.listeners[type] = new Set()
    }
    this.listeners[type].add(listener)
  }

  removeEventListener(type: string, listener: (event: MessageEvent | Event) => void) {
    this.listeners[type]?.delete(listener)
  }

  close() {
    this.closed = true
    this.readyState = MockEventSource.CLOSED
  }

  emitOpen() {
    this.readyState = MockEventSource.OPEN
    this.onopen?.(new Event("open"))
  }

  emitError() {
    this.readyState = MockEventSource.CLOSED
    this.onerror?.(new Event("error"))
  }

  emitTransientError() {
    this.readyState = MockEventSource.CONNECTING
    this.onerror?.(new Event("error"))
  }

  emit(type: SourceEventName, data?: unknown) {
    const event = new MessageEvent(type, {
      data: data === undefined ? undefined : JSON.stringify(data),
    })
    this.listeners[type]?.forEach(listener => listener(event))
  }

  hasListener(type: SourceEventName) {
    return (this.listeners[type]?.size ?? 0) > 0
  }
}

let originalEventSource: typeof globalThis.EventSource | undefined

beforeEach(() => {
  vi.useFakeTimers()
  MockEventSource.instances = []
  originalEventSource = globalThis.EventSource
  ;(globalThis as { EventSource?: unknown }).EventSource = MockEventSource as unknown as typeof EventSource
  ;(window as { EventSource?: unknown }).EventSource = MockEventSource as unknown as typeof EventSource
})

afterEach(() => {
  act(() => {
    cleanup()
  })
  if (originalEventSource === undefined) {
    delete (globalThis as { EventSource?: unknown }).EventSource
    delete (window as { EventSource?: unknown }).EventSource
  } else {
    ;(globalThis as { EventSource?: unknown }).EventSource = originalEventSource
    ;(window as { EventSource?: unknown }).EventSource = originalEventSource
  }
  vi.clearAllTimers()
  vi.useRealTimers()
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => "visible",
  })
})

const BASE_PARAMS: StreamParams = {
  instanceId: 1,
  page: 1,
  limit: 50,
  sort: "added_on",
  order: "desc",
}

interface HarnessControls {
  getState: () => StreamState
  payloads: TorrentStreamPayload[]
}

function makeParams(overrides: Partial<StreamParams> = {}): StreamParams {
  return { ...BASE_PARAMS, ...overrides }
}

function renderSubscriber(params: StreamParams | null, enabled = true) {
  const controls: HarnessControls = {
    getState: () => DEFAULT,
    payloads: [],
  }

  function Subscriber() {
    const state = useSyncStream(params, {
      enabled,
      onMessage: payload => {
        controls.payloads.push(payload)
      },
    })
    controls.getState = () => state
    return null
  }

  const wrapper = ({ children }: { children: ReactNode }) => (
    <TestProviders>{children}</TestProviders>
  )

  let utils: ReturnType<typeof render>
  act(() => {
    utils = render(<Subscriber />, { wrapper })
  })

  return {
    controls,
    unmount: () => act(() => utils.unmount()),
  }
}

const DEFAULT: StreamState = {
  connected: false,
  initialized: false,
  dataStalled: false,
  error: null,
  retrying: false,
  retryAttempt: 0,
  nextRetryAt: undefined,
}

function renderSwappableSubscriber(initialParams: StreamParams) {
  const controls: HarnessControls = {
    getState: () => DEFAULT,
    payloads: [],
  }
  let swapParams: (params: StreamParams) => void = () => {}

  function Subscriber({ params }: { params: StreamParams }) {
    const state = useSyncStream(params, {
      enabled: true,
      onMessage: payload => {
        controls.payloads.push(payload)
      },
    })
    controls.getState = () => state
    return null
  }

  function Root() {
    const [params, setParams] = useState(initialParams)
    swapParams = setParams
    return (
      <TestProviders>
        <Subscriber params={params} />
      </TestProviders>
    )
  }

  act(() => {
    render(<Root />)
  })

  return {
    controls,
    swap: (params: StreamParams) => act(() => swapParams(params)),
  }
}

function renderToggleableSubscriber(params: StreamParams) {
  let setMounted: (mounted: boolean) => void = () => {}

  function Subscriber() {
    useSyncStream(params, { enabled: true })
    return null
  }

  function Root() {
    const [mounted, setMountedState] = useState(true)
    setMounted = setMountedState
    return <TestProviders>{mounted ? <Subscriber /> : null}</TestProviders>
  }

  act(() => {
    render(<Root />)
  })

  return {
    unmountSubscriber: () => act(() => setMounted(false)),
  }
}

function flushConnectionQueue() {
  act(() => {
    vi.advanceTimersByTime(0)
  })
}

describe("SyncStreamContext", () => {
  describe("single subscriber lifecycle", () => {
    it("opens exactly one EventSource at the batch URL and closes it on teardown", () => {
      const { unmountSubscriber } = renderToggleableSubscriber(BASE_PARAMS)

      expect(MockEventSource.instances).toHaveLength(0)
      flushConnectionQueue()

      expect(MockEventSource.instances).toHaveLength(1)
      const source = MockEventSource.instances[0]

      const expectedUrl = api.getTorrentsStreamBatchUrl([
        {
          key: createStreamKey(BASE_PARAMS),
          instanceId: BASE_PARAMS.instanceId,
          page: BASE_PARAMS.page,
          limit: BASE_PARAMS.limit,
          sort: BASE_PARAMS.sort,
          order: BASE_PARAMS.order,
          search: "",
          filters: null,
        },
      ])
      expect(source.url).toBe(expectedUrl)
      expect(source.withCredentials).toBe(true)

      expect(source.hasListener("init")).toBe(true)
      expect(source.hasListener("update")).toBe(true)
      expect(source.hasListener("stream-error")).toBe(true)
      expect(source.hasListener("heartbeat")).toBe(true)

      unmountSubscriber()
      expect(source.closed).toBe(false)

      act(() => {
        vi.advanceTimersByTime(201)
      })

      expect(source.closed).toBe(true)
      expect(MockEventSource.instances).toHaveLength(1)
    })

    it("sends the summary option on the wire", () => {
      renderSubscriber({ ...BASE_PARAMS, summary: true })
      flushConnectionQueue()

      const url = new URL(MockEventSource.instances[0].url, "http://localhost")
      const streams = JSON.parse(url.searchParams.get("streams") ?? "[]")
      expect(streams[0].summary).toBe(true)
    })

    it("gives a grouped subscription its own key and sends groupCopies only on it", () => {
      const grouped = { ...BASE_PARAMS, groupCopies: true }
      expect(createStreamKey(grouped)).not.toBe(createStreamKey(BASE_PARAMS))
      expect(createStreamKey({ ...BASE_PARAMS, groupCopies: false })).toBe(createStreamKey(BASE_PARAMS))
      expect(createStreamKey(BASE_PARAMS)).not.toContain("groupCopies")

      renderSubscriber(BASE_PARAMS)
      renderSubscriber(grouped)
      flushConnectionQueue()

      const [flat, groupedStream] = MockEventSource.instances.map(source =>
        JSON.parse(new URL(source.url, "http://localhost").searchParams.get("streams") ?? "[]")[0])
      expect(flat.key).toBe(createStreamKey(BASE_PARAMS))
      expect("groupCopies" in flat).toBe(false)
      expect(groupedStream.key).toBe(createStreamKey(grouped))
      expect(groupedStream.groupCopies).toBe(true)
    })

    it("marks the entry connected once an update payload arrives", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]

      const key = createStreamKey(BASE_PARAMS)
      act(() => {
        source.emitOpen()
        source.emit("update", {
          type: "update",
          version: { major: 1, minor: 1 },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
        })
      })

      expect(controls.getState().connected).toBe(true)
      expect(controls.getState().error).toBeNull()
      expect(controls.payloads).toHaveLength(1)
      expect(controls.payloads[0].type).toBe("update")
    })

    it("restores a listener's row baseline after a stream error without reconnecting", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]
      const key = createStreamKey(BASE_PARAMS)
      const row = { hash: "a", name: "Synthetic torrent", ratio: 2 }
      act(() => {
        source.emitOpen()
        source.emit("init", {
          type: "init", version: { major: 1, minor: 1 },
          meta: { streamKey: key }, data: { torrents: [row], total: 1 },
        })
        source.emit("stream-error", {
          type: "stream-error", error: "Temporary sync failure", meta: { streamKey: key },
        })
      })
      expect(controls.getState().connected).toBe(false)
      expect(controls.getState().initialized).toBe(true)
      act(() => source.emit("delta", {
        type: "delta", version: { major: 1, minor: 2 },
        delta: { baseVersion: { major: 1, minor: 1 } },
        meta: { streamKey: key }, data: { torrents: [], total: 1 },
      }))
      expect(controls.payloads.map(payload => payload.type)).toEqual(["init", "stream-error", "init"])
      expect(resolveStreamRow(null, controls.payloads[2].data!)).toEqual(row)
      expect(controls.payloads[2].version).toEqual({ major: 1, minor: 2 })
      expect(controls.payloads[2].delta).toBeUndefined()
      expect(source.closed).toBe(false)
      expect(controls.getState().connected).toBe(true)
      expect(controls.getState().retryAttempt).toBe(0)
      act(() => {
        source.emit("stream-error", {
          type: "stream-error", error: "Temporary sync failure", meta: { streamKey: key },
        })
        source.emit("delta", {
          type: "delta", version: { major: 1, minor: 4 },
          delta: { baseVersion: { major: 1, minor: 3 } },
          meta: { streamKey: key }, data: { torrents: [], total: 0 },
        })
      })
      expect(source.closed).toBe(true)
      expect(controls.getState().retryAttempt).toBe(1)
    })

    it("coalesces recovery when a delta does not continue the accepted version", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]
      const key = createStreamKey(BASE_PARAMS)

      act(() => {
        source.emitOpen()
        source.emit("init", {
          type: "init",
          version: { major: 3, minor: 4 },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
          data: { torrents: [], total: 0 },
        })
        source.emit("delta", {
          type: "delta",
          version: { major: 3, minor: 6 },
          delta: { baseVersion: { major: 3, minor: 5 } },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
          data: { torrents: [], total: 0 },
        })
        source.emit("delta", {
          type: "delta",
          version: { major: 3, minor: 7 },
          delta: { baseVersion: { major: 3, minor: 6 } },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
          data: { torrents: [], total: 0 },
        })
      })

      expect(controls.payloads).toHaveLength(1)
      expect(controls.getState().connected).toBe(false)
      expect(controls.getState().initialized).toBe(false)

      act(() => vi.advanceTimersByTime(4000))
      expect(source.closed).toBe(true)
      expect(MockEventSource.instances).toHaveLength(2)

      const replacement = MockEventSource.instances[1]
      act(() => {
        replacement.emitOpen()
        replacement.emit("init", {
          type: "init",
          version: { major: 3, minor: 7 },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
          data: { torrents: [], total: 0 },
        })
      })

      expect(controls.getState().initialized).toBe(true)
      expect(controls.payloads.map(payload => payload.type)).toEqual(["init", "init"])
    })
  })

  describe("reconnect backoff", () => {
    it("preserves recovery deadlines across remounts and visibility changes", () => {
      let state = DEFAULT
      let remount = () => {}
      function Subscriber() {
        state = useSyncStream(BASE_PARAMS)
        return null
      }
      function Root() {
        const [revision, setRevision] = useState(0)
        remount = () => setRevision(value => value + 1)
        return <Subscriber key={revision} />
      }
      render(<TestProviders><Root /></TestProviders>)
      flushConnectionQueue()
      const key = createStreamKey(BASE_PARAMS)
      for (const delay of [4000, 8000]) {
        const source = MockEventSource.instances.at(-1)!
        act(() => {
          source.emitOpen()
          source.emit("init", { type: "init", meta: { streamKey: key }, data: { torrents: [] } })
        })
        const deadline = state.nextRetryAt
        const count = MockEventSource.instances.length
        act(() => {
          vi.advanceTimersByTime(1000)
          remount()
        })
        flushConnectionQueue()
        act(() => {
          setVisibility("hidden")
          setVisibility("visible")
        })
        expect(state.nextRetryAt).toBe(deadline)
        expect(MockEventSource.instances).toHaveLength(count)
        act(() => vi.advanceTimersByTime(delay - 1001))
        expect(MockEventSource.instances).toHaveLength(count)
        act(() => vi.advanceTimersByTime(1))
        expect(MockEventSource.instances).toHaveLength(count + 1)
      }
    })

    it("invalidates every subscription while shared recovery waits", () => {
      const paramsB = makeParams({ instanceId: 2 })
      let stateA = DEFAULT
      let stateB = DEFAULT
      function Subscribers() {
        stateA = useSyncStream(BASE_PARAMS)
        stateB = useSyncStream(paramsB)
        return null
      }
      render(<TestProviders><Subscribers /></TestProviders>)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
        for (const params of [BASE_PARAMS, paramsB]) {
          source.emit("init", {
            type: "init", version: { major: 1, minor: 1 },
            meta: { streamKey: createStreamKey(params) }, data: { torrents: [] },
          })
        }
      })
      expect(stateA.initialized).toBe(true)
      expect(stateB.initialized).toBe(true)
      act(() => source.emit("delta", {
        type: "delta", version: { major: 1, minor: 3 },
        delta: { baseVersion: { major: 1, minor: 2 } },
        meta: { streamKey: createStreamKey(BASE_PARAMS) }, data: { torrents: [] },
      }))
      expect(source.closed).toBe(true)
      expect(stateA.connected).toBe(false)
      expect(stateB.connected).toBe(false)
      expect(stateA.initialized).toBe(false)
      expect(stateB.initialized).toBe(false)
      expect(stateB.retryAttempt).toBe(1)
      act(() => vi.advanceTimersByTime(4000))
      const replacement = MockEventSource.instances.at(-1)!
      act(() => {
        replacement.emitOpen()
        replacement.emit("init", {
          type: "init", version: { major: 1, minor: 3 },
          meta: { streamKey: createStreamKey(BASE_PARAMS) }, data: { torrents: [] },
        })
      })
      expect(stateA.initialized).toBe(true)
      expect(stateB.initialized).toBe(false)
      expect(stateA.retryAttempt).toBe(1)
      act(() => replacement.emit("init", {
        type: "init", version: { major: 1, minor: 2 },
        meta: { streamKey: createStreamKey(paramsB) }, data: { torrents: [] },
      }))
      expect(stateB.initialized).toBe(true)
      expect(stateB.retryAttempt).toBe(0)
    })

    it("keeps a server-error subscription polling while shared recovery waits", () => {
      const paramsB = makeParams({ instanceId: 2 })
      let stateB = DEFAULT
      function Subscribers() {
        useSyncStream(BASE_PARAMS)
        stateB = useSyncStream(paramsB)
        return null
      }
      render(<TestProviders><Subscribers /></TestProviders>)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
        source.emit("init", {
          type: "init", version: { major: 1, minor: 1 },
          meta: { streamKey: createStreamKey(BASE_PARAMS) }, data: { torrents: [] },
        })
        source.emit("stream-error", {
          type: "stream-error", error: "instance unreachable",
          meta: { streamKey: createStreamKey(paramsB) },
        })
      })
      expect(streamFallbackPollInterval(stateB, 3000)).toBe(false)
      act(() => source.emit("delta", {
        type: "delta", version: { major: 1, minor: 3 },
        delta: { baseVersion: { major: 1, minor: 2 } },
        meta: { streamKey: createStreamKey(BASE_PARAMS) }, data: { torrents: [] },
      }))
      expect(source.closed).toBe(true)
      expect(streamFallbackPollInterval(stateB, 3000)).toBe(3000)
    })

    it("resets recovery using only subscriptions sent to the source", () => {
      let state = DEFAULT
      function Subscribers() {
        state = useSyncStream(BASE_PARAMS)
        useSyncStream(makeParams({ instanceId: 0 }))
        return null
      }
      render(<TestProviders><Subscribers /></TestProviders>)
      flushConnectionQueue()
      const key = createStreamKey(BASE_PARAMS)
      act(() => {
        const source = MockEventSource.instances[0]
        source.emitOpen()
        source.emit("init", { type: "init", meta: { streamKey: key }, data: { torrents: [] } })
      })
      expect(state.retryAttempt).toBe(1)
      act(() => vi.advanceTimersByTime(4000))
      act(() => {
        const source = MockEventSource.instances.at(-1)!
        source.emitOpen()
        source.emit("init", {
          type: "init", version: { major: 1, minor: 1 },
          meta: { streamKey: key }, data: { torrents: [] },
        })
      })
      expect(state.retryAttempt).toBe(0)
      expect(state.initialized).toBe(true)
    })

    it("backs off rejected frames across socket opens and resets after a valid baseline", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const key = createStreamKey(BASE_PARAMS)

      for (const delay of [4000, 8000, 16000, 30000]) {
        const source = MockEventSource.instances.at(-1)!
        act(() => {
          source.emitOpen()
          source.emit("init", { type: "init", meta: { streamKey: key }, data: { torrents: [] } })
        })
        expect(controls.getState().nextRetryAt).toBe(Date.now() + delay)
        const count = MockEventSource.instances.length
        act(() => vi.advanceTimersByTime(delay - 1))
        expect(MockEventSource.instances).toHaveLength(count)
        act(() => vi.advanceTimersByTime(1))
        expect(MockEventSource.instances).toHaveLength(count + 1)
      }

      const source = MockEventSource.instances.at(-1)!
      act(() => {
        source.emitOpen()
        source.emit("init", {
          type: "init", version: { major: 1, minor: 1 },
          meta: { streamKey: key }, data: { torrents: [] },
        })
      })
      expect(controls.getState().retryAttempt).toBe(0)
      expect(controls.getState().initialized).toBe(true)
    })

    it("doubles the delay each attempt and caps at RETRY_MAX_DELAY_MS", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const nowSpy = vi.spyOn(Date, "now").mockReturnValue(1_000_000)

      const expectedDelays = [4000, 8000, 16000, 30000, 30000, 30000]

      for (let i = 0; i < expectedDelays.length; i++) {
        const source = MockEventSource.instances[MockEventSource.instances.length - 1]

        act(() => {
          source.emitError()
        })

        const state = controls.getState()
        expect(state.retrying).toBe(true)
        expect(state.retryAttempt).toBe(i + 1)
        expect(state.nextRetryAt).toBe(1_000_000 + expectedDelays[i])

        act(() => {
          vi.advanceTimersByTime(expectedDelays[i])
        })
      }

      nowSpy.mockRestore()
    })

    it("surfaces a fatal error after MAX_RETRY_ATTEMPTS failures", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const delays = [4000, 8000, 16000, 30000, 30000, 30000]

      for (let i = 0; i < delays.length; i++) {
        const source = MockEventSource.instances[MockEventSource.instances.length - 1]
        act(() => {
          source.emitError()
        })

        if (i < delays.length - 1) {
          expect(controls.getState().error).toBe(STREAM_ERROR_DISCONNECTED)
          expect(controls.getState().error).not.toBe(STREAM_ERROR_RETRY_EXHAUSTED)
          act(() => {
            vi.advanceTimersByTime(delays[i])
          })
        }
      }

      expect(controls.getState().retryAttempt).toBe(6)
      expect(controls.getState().error).toBe(STREAM_ERROR_RETRY_EXHAUSTED)
      expect(isClientConnectionErrorCode(controls.getState().error)).toBe(true)
    })
  })

  describe("heartbeat watchdog", () => {
    it("tears down and reconnects when no event arrives within STREAM_STALE_TIMEOUT_MS", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
        source.emit("heartbeat")
      })
      expect(MockEventSource.instances).toHaveLength(1)

      act(() => {
        vi.advanceTimersByTime(STALE - 1)
      })
      expect(source.closed).toBe(false)

      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(source.closed).toBe(true)
      expect(controls.getState().retrying).toBe(true)
      expect(controls.getState().retryAttempt).toBe(1)
    })

    it("does NOT kill a slow init whose first event lands after 15s but within the grace", () => {
      renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
      })

      act(() => {
        vi.advanceTimersByTime(30_000)
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      act(() => {
        source.emit("init", { type: "init", version: { major: 1, minor: 1 }, meta: { instanceId: 1, timestamp: "now", streamKey: createStreamKey(BASE_PARAMS) }, data: { torrents: [], total: 0 } })
      })
      act(() => {
        vi.advanceTimersByTime(STALE - 1)
      })
      expect(source.closed).toBe(false)
      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(source.closed).toBe(true)
    })

    it("does NOT reconnect while heartbeats keep resetting the watchdog", () => {
      renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
      })

      for (let i = 0; i < 5; i++) {
        act(() => {
          vi.advanceTimersByTime(10_000)
          source.emit("heartbeat")
        })
        expect(source.closed).toBe(false)
      }

      expect(MockEventSource.instances).toHaveLength(1)
      expect(source.closed).toBe(false)
    })
  })

  describe("per-stream data watchdog", () => {
    it.each(["visible", "hidden"] as const)("stalls streams independently while %s", visibility => {
      const paramsA = makeParams({ sort: "added_on" })
      const paramsB = makeParams({ sort: "size" })
      let stateA = DEFAULT
      let stateB = DEFAULT
      function Subscribers() {
        stateA = useSyncStream(paramsA)
        stateB = useSyncStream(paramsB)
        return null
      }
      render(<TestProviders><Subscribers /></TestProviders>)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
        for (const params of [paramsA, paramsB]) {
          source.emit("init", {
            type: "init",
            version: { major: 1, minor: 1 },
            meta: { streamKey: createStreamKey(params) },
            data: { torrents: [], total: 0 },
          })
        }
        setVisibility(visibility)
      })
      expect(stateA.initialized).toBe(true)
      expect(stateB.initialized).toBe(true)

      for (let elapsed = 10_000; elapsed <= 120_000; elapsed += 10_000) {
        act(() => {
          vi.advanceTimersByTime(10_000)
          source.emit("update", {
            type: "update",
            version: { major: 1, minor: 1 + elapsed / 10_000 },
            meta: { streamKey: createStreamKey(paramsB) },
            data: { torrents: [], total: 0 },
          })
        })
        expect(stateA.dataStalled).toBe(elapsed === 120_000)
        expect(stateB.dataStalled).toBe(false)
      }

      for (let elapsed = 0; elapsed < 60_000; elapsed += 10_000) {
        act(() => {
          vi.advanceTimersByTime(10_000)
          source.emit("heartbeat")
        })
      }
      act(() => {
        source.emit("delta", {
          type: "delta",
          version: { major: 1, minor: 2 },
          delta: { baseVersion: { major: 1, minor: 1 } },
          meta: { streamKey: createStreamKey(paramsA) },
          data: { torrents: [], total: 0 },
        })
      })
      expect(stateA.dataStalled).toBe(false)
      expect(stateB.dataStalled).toBe(false)
      for (let elapsed = 10_000; elapsed <= 120_000; elapsed += 10_000) {
        act(() => {
          vi.advanceTimersByTime(10_000)
          source.emit("heartbeat")
        })
        expect(stateA.dataStalled).toBe(elapsed === 120_000)
        expect(stateB.dataStalled).toBe(elapsed >= 60_000)
      }
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)
    })

    it.each([90_000, 130_000])("bounds the replacement baseline grace (%i ms delay)", delay => {
      const paramsA = makeParams({ sort: "added_on" })
      const paramsB = makeParams({ sort: "size" })
      let stateA = DEFAULT
      let stateB = DEFAULT
      let showSecond: (show: boolean) => void = () => undefined

      function Root() {
        const [show, setShow] = useState(false)
        showSecond = setShow
        stateA = useSyncStream(paramsA)
        stateB = useSyncStream(show ? paramsB : null, { enabled: show })
        return null
      }

      act(() => {
        render(
          <TestProviders>
            <Root />
          </TestProviders>
        )
      })
      flushConnectionQueue()

      const first = MockEventSource.instances[0]
      act(() => {
        first.emitOpen()
        first.emit("init", {
          type: "init",
          version: { major: 1, minor: 1 },
          meta: { instanceId: 1, timestamp: "now", streamKey: createStreamKey(paramsA) },
          data: { torrents: [], total: 0 },
        })
        vi.advanceTimersByTime(14_000)
        showSecond(true)
      })
      flushConnectionQueue()

      expect(first.closed).toBe(true)
      expect(MockEventSource.instances).toHaveLength(2)
      const replacement = MockEventSource.instances[1]
      act(() => {
        replacement.emitOpen()
        replacement.emit("init", {
          type: "init",
          version: { major: 1, minor: 1 },
          meta: { streamKey: createStreamKey(paramsB) },
          data: { torrents: [], total: 0 },
        })
        vi.advanceTimersByTime(1_000)
      })

      expect(stateA.dataStalled).toBe(false)
      expect(replacement.closed).toBe(false)

      act(() => {
        for (let elapsed = 0; elapsed < delay; elapsed += 10_000) {
          vi.advanceTimersByTime(10_000)
          replacement.emit("update", {
            type: "update",
            version: { major: 1, minor: 2 + elapsed / 10_000 },
            meta: { streamKey: createStreamKey(paramsB) },
            data: { torrents: [], total: 0 },
          })
        }
      })
      expect(stateA.dataStalled).toBe(delay >= 120_000)
      expect(stateB.initialized).toBe(true)
      expect(stateB.dataStalled).toBe(false)
      act(() => {
        setVisibility("hidden")
        setVisibility("visible")
      })
      expect(stateA.dataStalled).toBe(delay >= 120_000)
      act(() => {
        replacement.emit("init", {
          type: "init",
          version: { major: 1, minor: 2 },
          meta: { instanceId: 1, timestamp: "now", streamKey: createStreamKey(paramsA) },
          data: { torrents: [], total: 0 },
        })
      })
      expect(stateA.dataStalled).toBe(false)
      for (let elapsed = 10_000; elapsed <= 120_000; elapsed += 10_000) {
        act(() => {
          vi.advanceTimersByTime(10_000)
          replacement.emit("heartbeat")
        })
        expect(stateA.dataStalled).toBe(elapsed === 120_000)
      }
      expect(replacement.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(2)
    })

    it("does not renew a silent stream's grace on later connection replacements", () => {
      const paramsA = makeParams({ sort: "added_on" })
      const paramsB = makeParams({ sort: "size" })
      const paramsC = makeParams({ sort: "name" })
      let stateA = DEFAULT
      let showB: (show: boolean) => void = () => undefined
      let showC: (show: boolean) => void = () => undefined

      function Root() {
        const [b, setB] = useState(false)
        const [c, setC] = useState(false)
        showB = setB
        showC = setC
        stateA = useSyncStream(paramsA)
        useSyncStream(b ? paramsB : null, { enabled: b })
        useSyncStream(c ? paramsC : null, { enabled: c })
        return null
      }

      render(<TestProviders><Root /></TestProviders>)
      flushConnectionQueue()
      const first = MockEventSource.instances[0]
      act(() => {
        first.emitOpen()
        first.emit("init", {
          type: "init",
          version: { major: 1, minor: 1 },
          meta: { streamKey: createStreamKey(paramsA) },
          data: { torrents: [], total: 0 },
        })
        vi.advanceTimersByTime(14_000)
        showB(true)
      })
      flushConnectionQueue()
      const second = MockEventSource.instances[1]
      act(() => {
        second.emitOpen()
        second.emit("init", {
          type: "init",
          version: { major: 1, minor: 1 },
          meta: { streamKey: createStreamKey(paramsB) },
          data: { torrents: [], total: 0 },
        })
        for (let elapsed = 0; elapsed < 90_000; elapsed += 10_000) {
          vi.advanceTimersByTime(10_000)
          second.emit("heartbeat")
        }
        showC(true)
      })
      expect(stateA.dataStalled).toBe(false)
      flushConnectionQueue()
      const third = MockEventSource.instances[2]
      act(() => {
        third.emitOpen()
        third.emit("init", {
          type: "init",
          version: { major: 1, minor: 1 },
          meta: { streamKey: createStreamKey(paramsC) },
          data: { torrents: [], total: 0 },
        })
        for (let elapsed = 0; elapsed < 20_000; elapsed += 10_000) {
          vi.advanceTimersByTime(10_000)
          third.emit("heartbeat")
        }
        vi.advanceTimersByTime(9_999)
        third.emit("heartbeat")
      })
      expect(stateA.dataStalled).toBe(false)

      act(() => vi.advanceTimersByTime(1_000))
      expect(stateA.dataStalled).toBe(true)
      expect(third.closed).toBe(false)
    })
  })

  describe("visibility stale watchdog", () => {
    it("re-arms a fresh watchdog on refocus instead of killing a healthy OPEN connection", () => {
      renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
        source.emit("heartbeat")
      })

      act(() => {
        setVisibility("hidden")
      })

      act(() => {
        vi.advanceTimersByTime(STALE * 3)
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      act(() => {
        setVisibility("visible")
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      act(() => {
        vi.advanceTimersByTime(STALE - 1)
      })
      expect(source.closed).toBe(false)
      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(source.closed).toBe(true)
    })

    it("does NOT re-arm the watchdog when a heartbeat arrives while hidden", () => {
      renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
      })

      act(() => {
        setVisibility("hidden")
      })

      act(() => {
        source.emit("heartbeat", { type: "heartbeat", meta: { timestamp: "now" } })
        vi.advanceTimersByTime(STALE * 3)
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      act(() => {
        setVisibility("visible")
      })
      act(() => {
        vi.advanceTimersByTime(STALE - 1)
      })
      expect(source.closed).toBe(false)
      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(source.closed).toBe(true)
    })

    it("still force-reconnects on refocus when the source is CLOSED", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const first = MockEventSource.instances[0]
      act(() => {
        first.emitOpen()
        first.emitError()
      })
      expect(controls.getState().retrying).toBe(true)

      const countAfterError = MockEventSource.instances.length

      act(() => {
        setVisibility("visible")
      })
      flushConnectionQueue()

      expect(MockEventSource.instances.length).toBeGreaterThan(countAfterError)
      expect(controls.getState().retryAttempt).toBe(0)
    })
  })

  describe("transient onerror guard", () => {
    it("ignores a transient onerror (CONNECTING) without closing, disconnecting, or scheduling a reconnect", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]

      act(() => {
        source.emitOpen()
        source.emit("update", {
          type: "update",
          version: { major: 1, minor: 1 },
          meta: { instanceId: 1, timestamp: "now", streamKey: createStreamKey(BASE_PARAMS) },
        })
      })
      expect(controls.getState().connected).toBe(true)

      act(() => {
        source.emitTransientError()
      })

      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)
      expect(controls.getState().connected).toBe(true)
      expect(controls.getState().error).toBeNull()
      expect(controls.getState().retrying).toBe(false)
      expect(controls.getState().retryAttempt).toBe(0)
    })

    it("escalates when onerror is terminal (CLOSED)", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]

      act(() => {
        source.emitOpen()
      })
      act(() => {
        source.emitError()
      })

      expect(source.closed).toBe(true)
      expect(controls.getState().connected).toBe(false)
      expect(controls.getState().error).toBe(STREAM_ERROR_DISCONNECTED)
      expect(controls.getState().retrying).toBe(true)
      expect(controls.getState().retryAttempt).toBe(1)
    })

    it("still escalates a connection stuck in CONNECTING via the initial stale grace", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]

      act(() => {
        source.emitOpen()
      })
      act(() => {
        source.emitTransientError()
      })
      expect(source.closed).toBe(false)

      act(() => {
        vi.advanceTimersByTime(INITIAL_GRACE - 1)
      })
      expect(source.closed).toBe(false)
      act(() => {
        vi.advanceTimersByTime(1)
      })

      expect(source.closed).toBe(true)
      expect(controls.getState().retrying).toBe(true)
      expect(controls.getState().retryAttempt).toBe(1)
    })

    it("escalates a cold-start connection that never opens once the stale watchdog elapses", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]

      act(() => {
        source.emitTransientError()
      })
      expect(source.closed).toBe(false)
      expect(controls.getState().retrying).toBe(false)

      act(() => {
        vi.advanceTimersByTime(INITIAL_GRACE)
      })
      expect(source.closed).toBe(true)
      expect(controls.getState().retrying).toBe(true)
      expect(controls.getState().retryAttempt).toBe(1)
    })
  })

  describe("view-parameter swap handoff", () => {
    function emitInitFor(source: MockEventSource, params: StreamParams) {
      act(() => {
        source.emit("init", {
          type: "init",
          version: { major: 1, minor: 1 },
          meta: { instanceId: params.instanceId, timestamp: "now", streamKey: createStreamKey(params) },
        })
      })
    }

    it("stays connected across rapid swaps whose init arrives within normal latency", () => {
      const { controls, swap } = renderSwappableSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const first = MockEventSource.instances[0]
      act(() => {
        first.emitOpen()
      })
      emitInitFor(first, BASE_PARAMS)
      expect(controls.getState().connected).toBe(true)

      const sorts = ["name", "size", "added_on", "progress"]
      for (const sort of sorts) {
        const nextParams = makeParams({ sort })
        swap(nextParams)
        flushConnectionQueue()

        const source = MockEventSource.instances[MockEventSource.instances.length - 1]
        act(() => {
          source.emitOpen()
          vi.advanceTimersByTime(100)
        })
        emitInitFor(source, nextParams)

        expect(controls.getState().connected).toBe(true)
        expect(controls.getState().error).toBeNull()
      }
    })

    it("keeps the stream connected when the socket opened but the first snapshot is slow", () => {
      const { controls, swap } = renderSwappableSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const first = MockEventSource.instances[0]
      act(() => {
        first.emitOpen()
      })
      emitInitFor(first, BASE_PARAMS)
      expect(controls.getState().connected).toBe(true)

      const nextParams = makeParams({ sort: "size" })
      swap(nextParams)
      flushConnectionQueue()

      const second = MockEventSource.instances[MockEventSource.instances.length - 1]
      expect(second).not.toBe(first)

      act(() => {
        second.emitOpen()
      })

      act(() => {
        vi.advanceTimersByTime(GRACE + 500)
      })
      expect(controls.getState().connected).toBe(true)
      expect(controls.getState().error).toBeNull()

      const live = MockEventSource.instances[MockEventSource.instances.length - 1]
      emitInitFor(live, nextParams)
      expect(controls.getState().connected).toBe(true)
      expect(controls.payloads[controls.payloads.length - 1].type).toBe("init")
    })

    it("still flips disconnected when the replacement socket never opens within grace", () => {
      const { controls, swap } = renderSwappableSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const first = MockEventSource.instances[0]
      act(() => {
        first.emitOpen()
      })
      emitInitFor(first, BASE_PARAMS)
      expect(controls.getState().connected).toBe(true)

      swap(makeParams({ sort: "size" }))
      flushConnectionQueue()
      expect(controls.getState().connected).toBe(true)

      act(() => {
        vi.advanceTimersByTime(GRACE)
      })
      expect(controls.getState().connected).toBe(false)
    })
  })

  describe("instanceId <= 0 guard", () => {
    it("does not open an EventSource for an invalid instanceId", () => {
      const { controls } = renderSubscriber(makeParams({ instanceId: 0 }))
      flushConnectionQueue()

      expect(MockEventSource.instances).toHaveLength(0)
      expect(controls.getState().connected).toBe(false)
    })
  })

  describe("signature dedup", () => {
    it("reuses a single EventSource for two subscribers with identical params", () => {
      const controlsA: HarnessControls = { getState: () => DEFAULT, payloads: [] }
      const controlsB: HarnessControls = { getState: () => DEFAULT, payloads: [] }

      function TwoSubscribers() {
        const stateA = useSyncStream(makeParams(), {
          onMessage: p => controlsA.payloads.push(p),
        })
        const stateB = useSyncStream(makeParams(), {
          onMessage: p => controlsB.payloads.push(p),
        })
        controlsA.getState = () => stateA
        controlsB.getState = () => stateB
        return null
      }

      act(() => {
        render(
          <TestProviders>
            <TwoSubscribers />
          </TestProviders>
        )
      })
      flushConnectionQueue()

      expect(MockEventSource.instances).toHaveLength(1)

      const source = MockEventSource.instances[0]
      const key = createStreamKey(makeParams())
      act(() => {
        source.emitOpen()
        source.emit("update", {
          type: "update",
          version: { major: 1, minor: 1 },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
        })
      })

      expect(controlsA.payloads).toHaveLength(1)
      expect(controlsB.payloads).toHaveLength(1)
      expect(controlsA.getState().connected).toBe(true)
      expect(controlsB.getState().connected).toBe(true)
    })

    it.each([false, true])("replays healthy snapshots on remount and waits for recovery after errors, cross=%s", async cross => {
      const params = makeParams(cross ? { instanceId: 0, instanceIds: [1] } : {})
      const field = cross ? "cross_instance_torrents" : "torrents"
      const row = { hash: "a", name: "Original", ...(cross ? { instance_id: 1, instance_name: "Synthetic" } : {}) }
      const controlsA: HarnessControls = { getState: () => DEFAULT, payloads: [] }
      const controlsB: HarnessControls = { getState: () => DEFAULT, payloads: [] }
      let setMounted: (mounted: boolean) => void = () => {}
      function Subscriber({ controls }: { controls: HarnessControls }) {
        const state = useSyncStream(params, {
          onMessage: payload => controls.payloads.push(payload),
        })
        controls.getState = () => state
        return null
      }
      function Root() {
        const [mounted, updateMounted] = useState(true)
        setMounted = updateMounted
        return (
          <TestProviders>
            <Subscriber controls={controlsA} />
            {mounted ? <Subscriber controls={controlsB} /> : null}
          </TestProviders>
        )
      }
      render(<Root />)
      flushConnectionQueue()
      const key = createStreamKey(params)
      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
        source.emit("init", {
          type: "init", version: { major: 4, minor: 1 }, meta: { streamKey: key },
          data: { [field]: [row], total: 1, counts: { total: 1 }, activeTaskCount: 1 },
        })
        source.emit("delta", {
          type: "delta", version: { major: 4, minor: 2 },
          delta: { baseVersion: { major: 4, minor: 1 } }, meta: { streamKey: key },
          data: { [field]: [{ ...row, name: "Updated" }], total: 1, activeTaskCount: 1 },
        })
      })
      act(() => setMounted(false))
      await act(async () => setMounted(true))
      flushConnectionQueue()
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)
      expect(controlsA.getState().initialized).toBe(true)
      expect(controlsA.payloads.map(payload => payload.type)).toEqual(["init", "delta"])
      expect(controlsB.payloads.map(payload => payload.type)).toEqual(["init", "delta", "init"])
      expect(controlsB.payloads.at(-1)).toMatchObject({
        version: { major: 4, minor: 2 },
        data: { [field]: [{ ...row, name: "Updated" }], total: 1, counts: { total: 1 } },
      })

      act(() => setMounted(false))
      act(() => source.emit("stream-error", {
        type: "stream-error", error: "Temporary sync failure", meta: { streamKey: key },
      }))
      await act(async () => setMounted(true))
      flushConnectionQueue()
      expect(controlsB.payloads).toHaveLength(3)
      expect(controlsB.getState()).toMatchObject({ connected: false, initialized: true, error: "Temporary sync failure" })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      act(() => source.emit("delta", {
        type: "delta", version: { major: 4, minor: 3 },
        delta: { baseVersion: { major: 4, minor: 2 } }, meta: { streamKey: key },
        data: { [field]: [], total: 1, activeTaskCount: 0 },
      }))
      expect(controlsB.payloads).toHaveLength(4)
      expect(controlsB.payloads.at(-1)).toMatchObject({
        type: "init", version: { major: 4, minor: 3 },
        data: { [field]: [{ ...row, name: "Updated" }], total: 1, counts: { total: 1 }, activeTaskCount: 0 },
      })
      expect(controlsB.getState()).toMatchObject({ connected: true, initialized: true, error: null, retryAttempt: 0 })
      expect(MockEventSource.instances).toHaveLength(1)
    })

    it("opens distinct EventSources for differing params", () => {
      function TwoSubscribers() {
        useSyncStream(makeParams({ page: 1 }), {})
        useSyncStream(makeParams({ page: 2 }), {})
        return null
      }

      act(() => {
        render(
          <TestProviders>
            <TwoSubscribers />
          </TestProviders>
        )
      })
      flushConnectionQueue()

      expect(MockEventSource.instances).toHaveLength(1)
      const decoded = decodeStreamsParam(MockEventSource.instances[0].url)
      expect(decoded).toHaveLength(2)
      expect(decoded.map(s => s.page).sort()).toEqual([1, 2])
    })
  })
})

const STALE = 15000
const INITIAL_GRACE = 60000
const GRACE = 1200

function setVisibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => state,
  })
  document.dispatchEvent(new Event("visibilitychange"))
}

function decodeStreamsParam(url: string): Array<{ page: number }> {
  const query = url.split("?")[1] ?? ""
  const params = new URLSearchParams(query)
  const raw = params.get("streams")
  if (!raw) {
    return []
  }
  return JSON.parse(decodeURIComponent(raw)) as Array<{ page: number }>
}

describe("SyncStreamContext activity channel", () => {
  it("opens an activity-only connection (no streams) and invalidates on activity events", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(client, "invalidateQueries")
    const listener = vi.fn()

    function ActivityConsumer() {
      useActivityStream()
      const { subscribeActivity } = useSyncStreamManager()
      useEffect(() => subscribeActivity(listener), [subscribeActivity])
      return null
    }

    act(() => {
      render(
        <QueryClientProvider client={client}>
          <SyncStreamProvider>
            <ActivityConsumer />
          </SyncStreamProvider>
        </QueryClientProvider>
      )
    })
    flushConnectionQueue()

    expect(MockEventSource.instances).toHaveLength(1)
    const source = MockEventSource.instances[0]
    expect(source.url).toContain("activity=1")
    expect(decodeStreamsParam(source.url)).toHaveLength(0)
    expect(source.hasListener("activity")).toBe(true)

    act(() => {
      source.emitOpen()
      source.emit("activity", {
        type: "activity",
        activity: { kind: "backup.run", instanceId: 7, timestamp: "now" },
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["instance-backups", 7] })
    expect(listener).toHaveBeenCalledWith({ kind: "backup.run", instanceId: 7, timestamp: "now" })
  })

  it("keeps the connection alive on activity heartbeats and ignores malformed activity payloads", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(client, "invalidateQueries")

    function ActivityConsumer() {
      useActivityStream()
      return null
    }

    act(() => {
      render(
        <QueryClientProvider client={client}>
          <SyncStreamProvider>
            <ActivityConsumer />
          </SyncStreamProvider>
        </QueryClientProvider>
      )
    })
    flushConnectionQueue()
    const source = MockEventSource.instances[0]

    act(() => {
      source.emitOpen()
      source.emit("activity", { type: "activity" })
      source.emit("heartbeat", { type: "heartbeat", meta: { timestamp: "now" } })
    })

    expect(invalidateSpy).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(STALE - 1)
    })
    expect(MockEventSource.instances).toHaveLength(1)
    expect(source.closed).toBe(false)
  })

  it("reconciles all activity-backed queries on reconnect, without an activity event", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(client, "invalidateQueries")

    function ActivityConsumer() {
      useActivityStream()
      return null
    }

    act(() => {
      render(
        <QueryClientProvider client={client}>
          <SyncStreamProvider>
            <ActivityConsumer />
          </SyncStreamProvider>
        </QueryClientProvider>
      )
    })
    flushConnectionQueue()

    const first = MockEventSource.instances[0]
    act(() => {
      first.emitOpen()
    })
    expect(invalidateSpy).not.toHaveBeenCalled()

    act(() => {
      first.emitError()
    })
    act(() => {
      vi.advanceTimersByTime(4000)
    })

    const second = MockEventSource.instances[MockEventSource.instances.length - 1]
    expect(second).not.toBe(first)
    expect(second.url).toContain("activity=1")

    act(() => {
      second.emitOpen()
    })

    const invalidatedKeys = invalidateSpy.mock.calls.map(
      ([arg]) => (arg as { queryKey: unknown }).queryKey
    )
    expect(invalidatedKeys).toContainEqual(["instance-backups"])
    expect(invalidatedKeys).toContainEqual(["dir-scan"])
    expect(invalidatedKeys).toContainEqual(["orphan-scan"])
    expect(invalidatedKeys).toContainEqual(["tracker-icons"])
  })

  it("reconciles on the FIRST successful open if it followed a failed attempt", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(client, "invalidateQueries")

    function ActivityConsumer() {
      useActivityStream()
      return null
    }

    act(() => {
      render(
        <QueryClientProvider client={client}>
          <SyncStreamProvider>
            <ActivityConsumer />
          </SyncStreamProvider>
        </QueryClientProvider>
      )
    })
    flushConnectionQueue()

    const first = MockEventSource.instances[0]
    act(() => {
      first.emitError()
    })
    expect(invalidateSpy).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(4000)
    })
    const second = MockEventSource.instances[MockEventSource.instances.length - 1]
    expect(second).not.toBe(first)

    act(() => {
      second.emitOpen()
    })

    const invalidatedKeys = invalidateSpy.mock.calls.map(
      ([arg]) => (arg as { queryKey: unknown }).queryKey
    )
    expect(invalidatedKeys).toContainEqual(["instance-backups"])
    expect(invalidatedKeys).toContainEqual(["tracker-icons"])
  })
})

describe("isClientConnectionErrorCode", () => {
  it("recognizes client connection-state codes so the UI shows localized copy", () => {
    expect(isClientConnectionErrorCode(STREAM_ERROR_DISCONNECTED)).toBe(true)
    expect(isClientConnectionErrorCode(STREAM_ERROR_RETRY_EXHAUSTED)).toBe(true)
  })

  it("treats backend payload error text and empty values as non-client errors", () => {
    expect(isClientConnectionErrorCode("instance unreachable: connection refused")).toBe(false)
    expect(isClientConnectionErrorCode(null)).toBe(false)
    expect(isClientConnectionErrorCode(undefined)).toBe(false)
    expect(isClientConnectionErrorCode("")).toBe(false)
  })
})

describe("stream fallback poll interval", () => {
  it("slows a stalled view, stops on a server error, and polls again after a dropped connection", async () => {
    const poll = vi.fn(async () => null)
    function Poller() {
      const state = useSyncStream(BASE_PARAMS, { enabled: true })
      useQuery({
        queryKey: ["fallback-poll"],
        queryFn: poll,
        enabled: !isStreamUsable(state),
        refetchInterval: streamFallbackPollInterval(state, 3000),
      })
      return null
    }
    act(() => {
      render(<Poller />, { wrapper: TestProviders })
    })
    flushConnectionQueue()
    const source = MockEventSource.instances[0]
    const key = createStreamKey(BASE_PARAMS)
    const countPolls = async (ms: number, heartbeats = true) => {
      poll.mockClear()
      for (let left = ms; left > 0; left -= 10_000) {
        await act(() => vi.advanceTimersByTimeAsync(Math.min(left, 10_000)))
        if (heartbeats) act(() => source.emit("heartbeat"))
      }
      return poll.mock.calls.length
    }

    act(() => {
      source.emitOpen()
      source.emit("init", {
        type: "init", version: { major: 1, minor: 1 },
        meta: { streamKey: key }, data: { torrents: [], total: 0 },
      })
    })
    await countPolls(120_000)
    expect(await countPolls(29_000)).toBe(0)
    expect(await countPolls(2_000)).toBe(1)

    act(() => source.emit("stream-error", {
      type: "stream-error", error: "Temporary sync failure", meta: { streamKey: key },
    }))
    expect(await countPolls(60_000)).toBe(0)

    act(() => source.emitError())
    expect(await countPolls(9_500, false)).toBe(3)
  })
})

describe("subscription changes on an open stream", () => {
  const MAIN = makeParams({ page: 0, limit: 300 })
  const SPEEDS = makeParams({ page: 0, limit: 1, summary: true })
  const details = (hash: string) => makeParams({ page: 0, limit: 1, filters: { hashes: [hash] } as StreamParams["filters"] })

  function renderTableWithDetails() {
    let setHash: (hash: string | null) => void = () => {}
    let remountExtra: () => void = () => {}
    let showSpeeds: () => void = () => {}
    function Extra() {
      useSyncStream(MAIN, {})
      return null
    }
    function Speeds() {
      useSyncStream(SPEEDS, {})
      return null
    }
    function Details({ hash }: { hash: string }) {
      useSyncStream(useMemo(() => details(hash), [hash]), {})
      return null
    }
    function Root() {
      const [hash, updateHash] = useState<string | null>(null)
      const [extra, updateExtra] = useState(0)
      const [speeds, updateSpeeds] = useState(false)
      setHash = updateHash
      remountExtra = () => updateExtra(n => n + 1)
      showSpeeds = () => updateSpeeds(true)
      useSyncStream(MAIN, {})
      return <>{hash ? <Details hash={hash} /> : null}{extra > 0 ? <Extra key={extra} /> : null}{speeds ? <Speeds /> : null}</>
    }
    act(() => {
      render(<Root />, { wrapper: TestProviders })
    })
    flushConnectionQueue()
    const source = MockEventSource.instances[0]
    act(() => {
      source.emitOpen()
      source.emit("connection", { type: "connection", connectionId: "qui-conn-7" })
      source.emit("init", {
        type: "init", version: { major: 1, minor: 1 },
        meta: { streamKey: createStreamKey(MAIN) }, data: { torrents: [], total: 0 },
      })
    })
    return {
      source,
      select: (hash: string | null) => {
        act(() => setHash(hash))
        flushConnectionQueue()
      },
      remountMainView: () => {
        act(() => remountExtra())
        flushConnectionQueue()
      },
      addSpeedsView: () => {
        act(() => showSpeeds())
        flushConnectionQueue()
      },
    }
  }

  const streamKeysOf = (url: string) => decodeStreamsParam(url).map(s => (s as { key?: string }).key).sort()

  async function settle() {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
  }

  afterEach(() => vi.restoreAllMocks())

  it("adds and removes a view with a change request and keeps the stream open", async () => {
    const change = vi.spyOn(api, "changeStreamSubscriptions").mockResolvedValue(undefined)
    const { source, select } = renderTableWithDetails()

    select("aaa")
    await settle()
    expect(change).toHaveBeenCalledTimes(1)
    expect(change).toHaveBeenLastCalledWith("qui-conn-7", [expect.objectContaining({ key: createStreamKey(details("aaa")), limit: 1 })], [], expect.any(AbortSignal))

    select(null)
    await settle()
    expect(change).toHaveBeenCalledTimes(2)
    expect(change).toHaveBeenLastCalledWith("qui-conn-7", [], [createStreamKey(details("aaa"))], expect.any(AbortSignal))

    expect(MockEventSource.instances).toEqual([source])
    expect(source.closed).toBe(false)
  })

  it.each([false, true])("switches from one torrent to another with one change request, gap=%s", async gap => {
    const change = vi.spyOn(api, "changeStreamSubscriptions").mockResolvedValue(undefined)
    const { source, select, remountMainView, addSpeedsView } = renderTableWithDetails()
    select("aaa")
    await settle()
    act(() => source.emit("init", {
      type: "init", version: { major: 2, minor: 1 },
      meta: { streamKey: createStreamKey(details("aaa")) }, data: { torrents: [], total: 0 },
    }))
    change.mockClear()

    if (gap) {
      select(null)
      remountMainView()
    }
    select("bbb")
    remountMainView()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50)
    })
    expect(change).toHaveBeenCalledTimes(1)
    expect(change).toHaveBeenLastCalledWith(
      "qui-conn-7",
      [expect.objectContaining({ key: createStreamKey(details("bbb")) })],
      [createStreamKey(details("aaa"))],
      expect.any(AbortSignal)
    )

    addSpeedsView()
    await settle()
    expect(change).toHaveBeenCalledTimes(2)
    expect(change).toHaveBeenLastCalledWith("qui-conn-7", [expect.objectContaining({ key: createStreamKey(SPEEDS) })], [], expect.any(AbortSignal))
    expect(MockEventSource.instances).toEqual([source])
  })

  it("adds a view back when it mounts again after a change removed it", async () => {
    const change = vi.spyOn(api, "changeStreamSubscriptions").mockResolvedValue(undefined)
    const { select, addSpeedsView } = renderTableWithDetails()
    select("aaa")
    await settle()
    change.mockClear()

    select(null)
    addSpeedsView()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50)
    })
    expect(change).toHaveBeenLastCalledWith("qui-conn-7", [expect.objectContaining({ key: createStreamKey(SPEEDS) })], [createStreamKey(details("aaa"))], expect.any(AbortSignal))

    select("aaa")
    await settle()
    expect(change).toHaveBeenCalledTimes(2)
    expect(change).toHaveBeenLastCalledWith("qui-conn-7", [expect.objectContaining({ key: createStreamKey(details("aaa")) })], [], expect.any(AbortSignal))
  })

  it("reopens the stream when a change request fails, with the current views in the URL", async () => {
    vi.spyOn(api, "changeStreamSubscriptions").mockRejectedValue(new Error("connection not found"))
    const { source, select } = renderTableWithDetails()

    select("aaa")
    await settle()
    expect(source.closed).toBe(true)
    expect(MockEventSource.instances).toHaveLength(2)
    expect(streamKeysOf(MockEventSource.instances[1].url)).toEqual([createStreamKey(MAIN), createStreamKey(details("aaa"))].sort())
  })

  it("gives the first init its grace after the connection event", () => {
    function Root() {
      useSyncStream(MAIN, {})
      return null
    }
    act(() => {
      render(<Root />, { wrapper: TestProviders })
    })
    flushConnectionQueue()
    const source = MockEventSource.instances[0]
    act(() => {
      source.emitOpen()
      source.emit("connection", { type: "connection", connectionId: "qui-conn-7" })
    })
    act(() => {
      vi.advanceTimersByTime(INITIAL_GRACE - 1)
    })
    expect(source.closed).toBe(false)
  })

  it("gives the first init after a native retry its grace", () => {
    function Root() {
      useSyncStream(MAIN, {})
      return null
    }
    act(() => {
      render(<Root />, { wrapper: TestProviders })
    })
    flushConnectionQueue()
    const source = MockEventSource.instances[0]
    act(() => {
      source.emitOpen()
      source.emit("init", {
        type: "init", version: { major: 1, minor: 1 },
        meta: { streamKey: createStreamKey(MAIN) }, data: { torrents: [], total: 0 },
      })
      source.emitTransientError()
      source.emitOpen()
    })
    act(() => {
      vi.advanceTimersByTime(INITIAL_GRACE - 1)
    })
    expect(source.closed).toBe(false)
  })

  it("sends the views that changed to the new server connection after a native retry", async () => {
    const change = vi.spyOn(api, "changeStreamSubscriptions").mockResolvedValue(undefined)
    const { source, select } = renderTableWithDetails()
    select("aaa")
    await settle()
    change.mockClear()

    act(() => source.emit("connection", { type: "connection", connectionId: "qui-conn-8" }))
    await settle()
    expect(change).toHaveBeenCalledTimes(1)
    expect(change).toHaveBeenLastCalledWith("qui-conn-8", [expect.objectContaining({ key: createStreamKey(details("aaa")) })], [], expect.any(AbortSignal))
    expect(MockEventSource.instances).toEqual([source])
  })

  it("lists the current views in the URL of the stream after a reconnect", async () => {
    vi.spyOn(api, "changeStreamSubscriptions").mockResolvedValue(undefined)
    const { source, select } = renderTableWithDetails()
    select("aaa")
    await settle()
    select("bbb")
    await settle()

    act(() => source.emitError())
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(MockEventSource.instances).toHaveLength(2)
    expect(streamKeysOf(MockEventSource.instances[1].url)).toEqual([createStreamKey(MAIN), createStreamKey(details("bbb"))].sort())
  })

  it.each([false, true])("reopens the stream when a change request gets no answer, retry=%s", async retry => {
    const change = vi.spyOn(api, "changeStreamSubscriptions").mockImplementation((_id, _add, _remove, signal) =>
      new Promise((_, reject) => signal?.addEventListener("abort", () => reject(signal.reason))))
    const { source, select } = renderTableWithDetails()
    select("aaa")
    if (retry) {
      act(() => source.emit("connection", { type: "connection", connectionId: "qui-conn-8" }))
    }
    for (let elapsed = 0; elapsed <= INITIAL_GRACE; elapsed += 5000) {
      act(() => source.emit("heartbeat"))
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000)
      })
    }
    expect(source.closed).toBe(true)
    expect(streamKeysOf(MockEventSource.instances[1].url)).toEqual([createStreamKey(MAIN), createStreamKey(details("aaa"))].sort())
    expect((change.mock.calls[0][3]?.reason as Error | undefined)?.name).toBe("TimeoutError")
  })

  it("keeps the stream while the server holds a change until the first inits are sent", async () => {
    const change = vi.spyOn(api, "changeStreamSubscriptions").mockImplementation((_id, _add, _remove, signal) =>
      new Promise((resolve, reject) => {
        window.setTimeout(resolve, STALE + 5000)
        signal?.addEventListener("abort", () => reject(signal.reason))
      }))
    const { source, select } = renderTableWithDetails()
    select("aaa")
    for (let elapsed = 0; elapsed <= STALE + 5000; elapsed += 5000) {
      act(() => source.emit("heartbeat"))
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000)
      })
    }
    expect(change).toHaveBeenCalledTimes(1)
    expect(source.closed).toBe(false)
    expect(MockEventSource.instances).toEqual([source])
  })

  it("gives the init of a view that joins the open stream its grace", async () => {
    vi.spyOn(api, "changeStreamSubscriptions").mockResolvedValue(undefined)
    const { source, select } = renderTableWithDetails()
    select("aaa")
    await settle()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(STALE + 5000)
    })
    expect(source.closed).toBe(false)

    act(() => source.emit("init", {
      type: "init", version: { major: 2, minor: 1 },
      meta: { streamKey: createStreamKey(details("aaa")) }, data: { torrents: [], total: 0 },
    }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(STALE + 1)
    })
    expect(source.closed).toBe(true)
  })

  it("does not hold changes on a new stream behind a stuck request of the old one", async () => {
    const change = vi.spyOn(api, "changeStreamSubscriptions").mockReturnValueOnce(new Promise(() => {}))
    const { source, select } = renderTableWithDetails()
    select("aaa")
    await settle()

    act(() => source.emitError())
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    const next = MockEventSource.instances[1]
    act(() => {
      next.emitOpen()
      next.emit("connection", { type: "connection", connectionId: "qui-conn-9" })
    })
    change.mockResolvedValue(undefined)
    select("bbb")
    await settle()
    expect(change).toHaveBeenLastCalledWith(
      "qui-conn-9",
      [expect.objectContaining({ key: createStreamKey(details("bbb")) })],
      [createStreamKey(details("aaa"))],
      expect.any(AbortSignal)
    )
  })
})
