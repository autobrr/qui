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
  type StreamParams,
  type StreamState
} from "@/contexts/SyncStreamContext"
import type { TorrentStreamPayload } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, render } from "@testing-library/react"
import { useState, type ReactNode } from "react"
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

type SourceEventName = "init" | "update" | "stream-error" | "heartbeat" | "activity"

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
  if (originalEventSource === undefined) {
    delete (globalThis as { EventSource?: unknown }).EventSource
    delete (window as { EventSource?: unknown }).EventSource
  } else {
    ;(globalThis as { EventSource?: unknown }).EventSource = originalEventSource
    ;(window as { EventSource?: unknown }).EventSource = originalEventSource
  }
  vi.clearAllTimers()
  vi.useRealTimers()
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
  error: null,
  retrying: false,
  retryAttempt: 0,
  nextRetryAt: undefined,
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
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
        })
      })

      expect(controls.getState().connected).toBe(true)
      expect(controls.getState().error).toBeNull()
      expect(controls.payloads).toHaveLength(1)
      expect(controls.payloads[0].type).toBe("update")
    })
  })

  describe("reconnect backoff", () => {
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
        source.emitOpen() // arms the 15s stale watchdog
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

  describe("transient onerror guard", () => {
    it("ignores a transient onerror (CONNECTING) without closing, disconnecting, or scheduling a reconnect", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]

      act(() => {
        source.emitOpen()
        source.emit("update", {
          type: "update",
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

    it("still escalates a connection stuck in CONNECTING via the 15s stale watchdog", () => {
      const { controls } = renderSubscriber(BASE_PARAMS)
      flushConnectionQueue()
      const source = MockEventSource.instances[0]

      act(() => {
        source.emitOpen() // arms the 15s stale watchdog
      })
      act(() => {
        source.emitTransientError() // CONNECTING: does not escalate
      })
      expect(source.closed).toBe(false)

      act(() => {
        vi.advanceTimersByTime(STALE) // 15000
      })

      expect(source.closed).toBe(true)
      expect(controls.getState().retrying).toBe(true)
      expect(controls.getState().retryAttempt).toBe(1)
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
          meta: { instanceId: 1, timestamp: "now", streamKey: key },
        })
      })

      // Both subscribers share the same entry and both receive the payload.
      expect(controlsA.payloads).toHaveLength(1)
      expect(controlsB.payloads).toHaveLength(1)
      expect(controlsA.getState().connected).toBe(true)
      expect(controlsB.getState().connected).toBe(true)
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

const STALE = 15000 // STREAM_STALE_TIMEOUT_MS

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
