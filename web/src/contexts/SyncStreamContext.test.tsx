/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import {
  createStreamKey,
  isClientConnectionErrorCode,
  STREAM_ERROR_DISCONNECTED,
  STREAM_ERROR_RETRY_EXHAUSTED,
  SyncStreamProvider,
  useActivityStream,
  useSyncStream,
  useSyncStreamManager,
  type StreamParams,
  type StreamState
} from "@/contexts/SyncStreamContext"
import type { TorrentStreamPayload } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render } from "@testing-library/react"
import { useEffect, useState, type ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// SyncStreamProvider now reads a QueryClient (to invalidate queries on activity
// events), so every render must be wrapped in one.
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

// ---------------------------------------------------------------------------
// Controllable EventSource mock
//
// jsdom ships no EventSource, so the provider's `typeof EventSource` guard would
// otherwise short-circuit before ever opening a connection. We install a fully
// controllable fake on globalThis (and window) that records every constructed
// instance and lets each test drive open / error / event emission by hand.
// ---------------------------------------------------------------------------

type SourceEventName = "init" | "update" | "delta" | "stream-error" | "heartbeat" | "activity"

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

  // ----- test driver helpers -------------------------------------------------

  emitOpen() {
    this.readyState = MockEventSource.OPEN
    this.onopen?.(new Event("open"))
  }

  emitError() {
    // Match the browser behaviour: a fatal error transitions the source to
    // CLOSED before onerror fires.
    this.readyState = MockEventSource.CLOSED
    this.onerror?.(new Event("error"))
  }

  emitTransientError() {
    // Browser behaviour on a transient drop: it keeps the source in CONNECTING
    // and auto-retries; onerror still fires but readyState stays CONNECTING.
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
  // Unmount any rendered tree so each provider's document-level listeners
  // (visibilitychange / beforeunload) are detached. Without this, providers from
  // earlier tests linger and respond to a dispatched visibilitychange.
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
  // jsdom's document.visibilityState is read-only and defaults to "visible";
  // tests override it via defineProperty, so restore the default here to avoid
  // leaking a "hidden" getter into later tests.
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => "visible",
  })
})

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

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

// Renders a single useSyncStream subscriber and exposes its latest state plus
// every payload delivered to onMessage. Mounting/unmounting is driven through
// the returned `unmount` so teardown timing can be asserted.
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
  error: null,
  retrying: false,
  retryAttempt: 0,
  nextRetryAt: undefined,
}

// Renders a single useSyncStream subscriber whose params can be swapped while the
// provider stays mounted. Changing sort/search/filters mints a new stream key,
// which is exactly the view-parameter swap that drives openConnection's handoff
// path. Exposes the latest state plus a `swap` to drive new params imperatively.
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

// Renders a subscriber whose mount state can be toggled while the surrounding
// SyncStreamProvider stays mounted. This is required to observe the debounced
// (ENTRY_TEARDOWN_DELAY_MS) teardown path: unmounting the whole tree would also
// unmount the provider, whose own cleanup closes the connection immediately.
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

// Flush the 0ms queueConnectionUpdate debounce so openConnection actually runs.
function flushConnectionQueue() {
  act(() => {
    vi.advanceTimersByTime(0)
  })
}

describe("SyncStreamContext", () => {
  describe("single subscriber lifecycle", () => {
    it("opens exactly one EventSource at the batch URL and closes it on teardown", () => {
      const { unmountSubscriber } = renderToggleableSubscriber(BASE_PARAMS)

      // queueConnectionUpdate batches the open behind a 0ms timeout.
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

      // All four server event listeners are attached.
      expect(source.hasListener("init")).toBe(true)
      expect(source.hasListener("update")).toBe(true)
      expect(source.hasListener("stream-error")).toBe(true)
      expect(source.hasListener("heartbeat")).toBe(true)

      // Unmounting the subscriber removes the listener, but entry teardown is
      // debounced by ENTRY_TEARDOWN_DELAY_MS (200ms) so the source stays open.
      unmountSubscriber()
      expect(source.closed).toBe(false)

      // The teardown timer fires at the 200ms boundary and itself queues a 0ms
      // connection update; advancing just past the boundary lets that nested
      // timer run so ensureConnection sees zero entries and closes the source.
      act(() => {
        vi.advanceTimersByTime(201) // ENTRY_TEARDOWN_DELAY_MS + queued update
      })

      expect(source.closed).toBe(true)
      expect(MockEventSource.instances).toHaveLength(1)
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
        // Frame 5 was missed. The next aggregate-only frame proves the gap even
        // though it carries no changed rows.
        source.emit("delta", {
          type: "delta",
          version: { major: 3, minor: 6 },
          delta: { baseVersion: { major: 3, minor: 5 } },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
          data: { torrents: [], total: 0 },
        })
        // A second bad frame before the queued reconnect runs must not create a
        // second replacement connection.
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

      // attempt 1 -> 4000, 2 -> 8000, 3 -> 16000, 4 -> 30000 (cap),
      // 5 -> 30000, 6 -> 30000.
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

        // Fire the scheduled reconnect so the next attempt can be scheduled.
        act(() => {
          vi.advanceTimersByTime(expectedDelays[i])
        })
        // ensureConnection reopens via the same synchronous path here (no
        // queueConnectionUpdate), so a fresh source already exists.
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
          // Not yet at the cap: error is the generic disconnect code.
          expect(controls.getState().error).toBe(STREAM_ERROR_DISCONNECTED)
          expect(controls.getState().error).not.toBe(STREAM_ERROR_RETRY_EXHAUSTED)
          act(() => {
            vi.advanceTimersByTime(delays[i])
          })
        }
      }

      expect(controls.getState().retryAttempt).toBe(6) // MAX_RETRY_ATTEMPTS
      // The error is a stable machine code (not English prose) so the UI can map it
      // to localized streamStatus.* copy instead of leaking English to non-en locales.
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
        // The first event switches the watchdog from the init grace to the 15s
        // steady-state budget this test exercises.
        source.emit("heartbeat")
      })
      expect(MockEventSource.instances).toHaveLength(1)

      // 14.999s: still alive.
      act(() => {
        vi.advanceTimersByTime(STALE - 1)
      })
      expect(source.closed).toBe(false)

      // Crossing 15s trips the watchdog -> closeConnection + scheduleReconnect.
      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(source.closed).toBe(true)
      expect(controls.getState().retrying).toBe(true)
      expect(controls.getState().retryAttempt).toBe(1)
    })

    it("does NOT kill a slow init whose first event lands after 15s but within the grace", () => {
      // The init is a single large SSE event that can take longer than the 15s
      // steady-state budget to drain on a big instance; the pre-first-event grace
      // must keep the connection alive so it is not torn down into a reconnect storm.
      renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen()
      })

      // 30s with no event yet: past the old 15s watchdog, still within the grace.
      act(() => {
        vi.advanceTimersByTime(30_000)
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      // The init finally lands; from here the normal 15s budget applies.
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

      // Emit a heartbeat every 10s for 50s. The 15s watchdog never elapses
      // because each heartbeat resets it.
      for (let i = 0; i < 5; i++) {
        act(() => {
          vi.advanceTimersByTime(10_000)
          source.emit("heartbeat")
        })
        expect(source.closed).toBe(false)
      }

      // Only ever one source: no reconnect happened.
      expect(MockEventSource.instances).toHaveLength(1)
      expect(source.closed).toBe(false)
    })
  })

  describe("visibility stale watchdog", () => {
    it("re-arms a fresh watchdog on refocus instead of killing a healthy OPEN connection", () => {
      renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()

      const source = MockEventSource.instances[0]
      act(() => {
        source.emitOpen() // readyState = OPEN
        // First event -> steady-state 15s watchdog (this test asserts that budget).
        source.emit("heartbeat")
      })

      // Tab goes hidden: the stale timer is cleared (timing is meaningless while throttled).
      act(() => {
        setVisibility("hidden")
      })

      // Simulate a throttled background tab: advance WELL past the stale window. With the
      // old code an overdue timer would have fired; with the hidden-clear it must not.
      act(() => {
        vi.advanceTimersByTime(STALE * 3)
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      // Refocus: connection is still OPEN, so we must re-arm a FRESH watchdog and NOT
      // tear down / reconnect the healthy connection.
      act(() => {
        setVisibility("visible")
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      // The fresh watchdog has the full STREAM_STALE_TIMEOUT_MS again: just under it,
      // still alive; crossing it trips a reconnect (proving the timer was actually re-armed).
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
        source.emitOpen() // arms the 15s stale watchdog, readyState = OPEN
      })

      act(() => {
        setVisibility("hidden")
      })

      // Background heartbeats keep arriving. They must NOT resurrect the watchdog
      // while hidden: a re-armed timer under background throttling false-fires and
      // flips the stream to REST polling. Emit one, then blow well past the window.
      act(() => {
        source.emit("heartbeat", { type: "heartbeat", meta: { timestamp: "now" } })
        vi.advanceTimersByTime(STALE * 3)
      })
      expect(source.closed).toBe(false)
      expect(MockEventSource.instances).toHaveLength(1)

      // Refocus re-arms a fresh watchdog: still alive just under it, trips just over.
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
        first.emitError() // network error -> source CLOSED, closeConnection
      })
      // The error path schedules a reconnect; the visibility path then resets retry
      // state and reopens immediately.
      expect(controls.getState().retrying).toBe(true)

      const countAfterError = MockEventSource.instances.length

      act(() => {
        setVisibility("visible")
      })
      flushConnectionQueue()

      // A new source was opened by the visibility reconnect path.
      expect(MockEventSource.instances.length).toBeGreaterThan(countAfterError)
      // Retry state was reset (resetRetry:true).
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

      // The native EventSource retry is left to proceed: nothing is torn down.
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
        source.emitError() // terminal: readyState=CLOSED before onerror fires
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
        source.emitOpen() // arms the watchdog (init grace, no event yet)
      })
      act(() => {
        source.emitTransientError() // CONNECTING: does not escalate
      })
      expect(source.closed).toBe(false)

      // No event ever arrives, so the longer pre-first-event grace applies; just
      // under it the connection is still alive, and crossing it escalates.
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

      // Server unreachable from mount: the browser keeps retrying (CONNECTING) and
      // never fires onopen, so handleSourceError defers to the native retry...
      act(() => {
        source.emitTransientError()
      })
      expect(source.closed).toBe(false)
      expect(controls.getState().retrying).toBe(false)

      // ...but a connection that never opens must still escalate via the stale
      // watchdog armed at connection creation (not only in onopen), so qui's own
      // backoff and offline state engage instead of waiting silently forever. No
      // event ever arrives, so the longer pre-first-event grace governs the timing.
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

      // Each swap mints a new stream key, tearing down the old EventSource and
      // opening a new one. The new socket opens and delivers init well within the
      // grace window, so connected must never flip false during the handoff.
      const sorts = ["name", "size", "added_on", "progress"]
      for (const sort of sorts) {
        const nextParams = makeParams({ sort })
        swap(nextParams)
        flushConnectionQueue()

        const source = MockEventSource.instances[MockEventSource.instances.length - 1]
        // Socket opens, then a fast init resolves it -- still inside the grace window.
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

      // Swap to a new key (cold filter): teardown + reopen.
      const nextParams = makeParams({ sort: "size" })
      swap(nextParams)
      flushConnectionQueue()

      const second = MockEventSource.instances[MockEventSource.instances.length - 1]
      expect(second).not.toBe(first)

      // The replacement socket opens promptly, but its first snapshot is slow.
      act(() => {
        second.emitOpen()
      })

      // Advance PAST the base grace window with no init yet. Because the socket has
      // demonstrably opened, onopen refreshed the handoff and the stream must not
      // flip offline / re-enable polling.
      act(() => {
        vi.advanceTimersByTime(GRACE + 500)
      })
      expect(controls.getState().connected).toBe(true)
      expect(controls.getState().error).toBeNull()

      // The slow init finally arrives on the live source and resolves the handoff
      // cleanly. (Tearing down the prior view's entry may have reopened a fresh
      // source meanwhile, so deliver init on whichever source is current.)
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

      // Swap to a new key, but never open the replacement socket nor deliver init.
      swap(makeParams({ sort: "size" }))
      flushConnectionQueue()
      expect(controls.getState().connected).toBe(true) // still in the grace window

      // The grace window elapses with no onopen and no init: the existing fixed-window
      // behaviour must still flip the stream offline so REST polling resumes.
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

    it("does not open an EventSource for a negative instanceId", () => {
      renderSubscriber(makeParams({ instanceId: -5 }))
      flushConnectionQueue()

      expect(MockEventSource.instances).toHaveLength(0)
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

      // Identical StreamParams collapse to one entry / signature -> one source.
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

      // Both subscribers share the same entry and both receive the payload.
      expect(controlsA.payloads).toHaveLength(1)
      expect(controlsB.payloads).toHaveLength(1)
      expect(controlsA.getState().connected).toBe(true)
      expect(controlsB.getState().connected).toBe(true)
    })

    it("requests one shared init when a listener remounts without a baseline", () => {
      const controlsA: HarnessControls = { getState: () => DEFAULT, payloads: [] }
      const controlsB: HarnessControls = { getState: () => DEFAULT, payloads: [] }
      let setMounted: (mounted: boolean) => void = () => {}

      function Subscriber({ controls }: { controls: HarnessControls }) {
        const state = useSyncStream(makeParams(), {
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

      act(() => {
        render(<Root />)
      })
      flushConnectionQueue()

      const key = createStreamKey(makeParams())
      const first = MockEventSource.instances[0]
      act(() => {
        first.emitOpen()
        first.emit("init", {
          type: "init",
          version: { major: 4, minor: 1 },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
          data: { torrents: [], total: 0 },
        })
      })
      expect(controlsA.payloads).toHaveLength(1)
      expect(controlsB.payloads).toHaveLength(1)

      act(() => setMounted(false))
      act(() => setMounted(true))
      expect(controlsA.getState().initialized).toBe(false)
      flushConnectionQueue()

      expect(first.closed).toBe(true)
      expect(MockEventSource.instances).toHaveLength(2)
      const replacement = MockEventSource.instances[1]
      act(() => {
        replacement.emitOpen()
        replacement.emit("init", {
          type: "init",
          version: { major: 4, minor: 1 },
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
          data: { torrents: [], total: 0 },
        })
      })

      // The already-mounted listener receives the same replacement baseline; it
      // is not left on a different sequence when the remounted listener recovers.
      expect(controlsA.payloads.map(payload => payload.type)).toEqual(["init", "init"])
      expect(controlsB.payloads.map(payload => payload.type)).toEqual(["init", "init"])
      expect(controlsA.getState().initialized).toBe(true)
      expect(controlsB.getState().initialized).toBe(true)
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

      // Two distinct entries -> still a single multiplexed source, but the URL
      // carries both stream payloads.
      expect(MockEventSource.instances).toHaveLength(1)
      const decoded = decodeStreamsParam(MockEventSource.instances[0].url)
      expect(decoded).toHaveLength(2)
      expect(decoded.map(s => s.page).sort()).toEqual([1, 2])
    })
  })
})

const STALE = 15000 // STREAM_STALE_TIMEOUT_MS (applies once the first event has arrived)
const INITIAL_GRACE = 60000 // STREAM_INITIAL_TIMEOUT_MS (applies until the first event)
const GRACE = 1200 // HANDOFF_GRACE_PERIOD_MS

// jsdom defaults document.visibilityState to "visible" and makes it read-only,
// so override it via a getter and fire the real visibilitychange event.
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

    // A single EventSource opens with activity=1 and no streams param.
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
      // Malformed activity payload must not throw or invalidate.
      source.emit("activity", { type: "activity" })
      // A heartbeat keeps the watchdog alive (no reconnect) without invalidating.
      source.emit("heartbeat", { type: "heartbeat", meta: { timestamp: "now" } })
    })

    expect(invalidateSpy).not.toHaveBeenCalled()

    // Advancing past the stale window without any event would reconnect; the
    // heartbeat above reset it, so the original source is still the only one.
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
    // First open arms reconnect reconciliation but must NOT invalidate: the
    // activity-backed queries just mounted and fetched.
    act(() => {
      first.emitOpen()
    })
    expect(invalidateSpy).not.toHaveBeenCalled()

    // Drop the connection and let the backoff timer reopen it. The new per-session
    // activity topic cannot replay anything published while we were down.
    act(() => {
      first.emitError()
    })
    act(() => {
      vi.advanceTimersByTime(4000) // RETRY_BASE_DELAY_MS
    })

    const second = MockEventSource.instances[MockEventSource.instances.length - 1]
    expect(second).not.toBe(first)
    expect(second.url).toContain("activity=1")

    act(() => {
      second.emitOpen()
    })

    // No activity event was emitted; reconciliation came purely from the reconnect.
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

    // The first attempt fails before it ever opens (events could be missed during
    // this pre-first-open window).
    const first = MockEventSource.instances[0]
    act(() => {
      first.emitError()
    })
    expect(invalidateSpy).not.toHaveBeenCalled()

    // Backoff reopens. This is the first *successful* open, but because it followed
    // a failure it must still reconcile rather than trust mount-time data.
    act(() => {
      vi.advanceTimersByTime(4000) // RETRY_BASE_DELAY_MS
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
    // Dynamic backend text must still be displayed verbatim, so it is not a client code.
    expect(isClientConnectionErrorCode("instance unreachable: connection refused")).toBe(false)
    expect(isClientConnectionErrorCode(null)).toBe(false)
    expect(isClientConnectionErrorCode(undefined)).toBe(false)
    expect(isClientConnectionErrorCode("")).toBe(false)
  })
})
