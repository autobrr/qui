/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const { useSyncStreamMock } = vi.hoisted(() => ({ useSyncStreamMock: vi.fn() }))

vi.mock("@/contexts/SyncStreamContext", () => ({
  useSyncStream: useSyncStreamMock,
}))

vi.mock("@/hooks/useInstances", () => ({
  useInstances: () => ({ instances: [{ id: 1, isActive: true }] }),
}))

vi.mock("@/hooks/useInstanceCapabilities", () => ({
  useInstanceCapabilities: () => ({ data: undefined }),
}))

vi.mock("@/lib/api", () => ({
  api: {
    getTorrents: vi.fn().mockResolvedValue({ torrents: [], total: 0, hasMore: false }),
    getCrossInstanceTorrents: vi.fn().mockResolvedValue({ torrents: [], total: 0, hasMore: false }),
  },
}))

import { STREAM_HIDDEN_PAUSE_DELAY_MS, useTorrentsList } from "@/hooks/useTorrentsList"

const DEFAULT_STREAM_STATE = {
  connected: false,
  error: null,
  lastMeta: undefined,
  retrying: false,
  retryAttempt: 0,
  nextRetryAt: undefined,
}

// The single-instance torrent-list stream multiplexes a heavy limit:300 snapshot.
// These helpers read back the `enabled` flag the hook hands to useSyncStream so we
// can assert when the list subscription is paused.
function lastStreamEnabled(): boolean | undefined {
  const calls = useSyncStreamMock.mock.calls
  if (calls.length === 0) {
    return undefined
  }
  const options = calls[calls.length - 1][1] as { enabled?: boolean } | undefined
  return options?.enabled
}

let hidden = false

function setHidden(next: boolean) {
  hidden = next
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"))
  })
}

// One QueryClient per test, reused across rerenders (renderHook's rerender()
// re-invokes the wrapper, so an inline client would reset the cache each time).
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe("useTorrentsList background stream gating", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    hidden = false
    useSyncStreamMock.mockReturnValue(DEFAULT_STREAM_STATE)
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => hidden,
    })
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it("keeps the list stream enabled while the tab is visible", () => {
    renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: createWrapper() })

    expect(lastStreamEnabled()).toBe(true)
  })

  it("pauses the list stream once the tab has been hidden past the delay", () => {
    const { rerender } = renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: createWrapper() })

    expect(lastStreamEnabled()).toBe(true)

    setHidden(true)
    act(() => {
      vi.advanceTimersByTime(STREAM_HIDDEN_PAUSE_DELAY_MS)
    })
    rerender()

    expect(lastStreamEnabled()).toBe(false)
  })

  it("resumes the list stream immediately when the tab becomes visible again", () => {
    const { rerender } = renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: createWrapper() })

    setHidden(true)
    act(() => {
      vi.advanceTimersByTime(STREAM_HIDDEN_PAUSE_DELAY_MS)
    })
    rerender()
    expect(lastStreamEnabled()).toBe(false)

    setHidden(false)
    rerender()

    expect(lastStreamEnabled()).toBe(true)
  })
})
