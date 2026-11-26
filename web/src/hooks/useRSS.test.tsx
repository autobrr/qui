/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { FeedsUpdatePayload, RSSEventHandlers } from "@/lib/rss-events"
import type { RSSItems } from "@/types"
import { rssKeys, useRSSFeeds } from "./useRSS"

const { mockApi, rssEventState } = vi.hoisted(() => ({
  mockApi: {
    getRSSItems: vi.fn(() => Promise.resolve({})),
  },
  rssEventState: {
    handlers: undefined as RSSEventHandlers | undefined,
    connect: vi.fn(),
    disconnect: vi.fn(),
  },
}))

vi.mock("@/lib/api", () => ({ api: mockApi }))
vi.mock("@/lib/rss-events", () => ({
  RSSEventSource: class {
    constructor(_instanceId: number, handlers: RSSEventHandlers) {
      rssEventState.handlers = handlers
    }

    connect() {
      rssEventState.connect()
    }

    disconnect() {
      rssEventState.disconnect()
    }
  },
}))

function syntheticRSSItems(feedCount: number, articleCount: number): RSSItems {
  return Object.fromEntries(Array.from({ length: feedCount }, (_, feedIndex) => [
    `Feed ${feedIndex}`,
    {
      uid: `feed-${feedIndex}`,
      url: `https://example.invalid/feed/${feedIndex}`,
      hasError: false,
      isLoading: false,
      articles: Array.from({ length: articleCount }, (_, articleIndex) => ({
        id: `feed-${feedIndex}-article-${articleIndex}`,
        date: "2026-08-25T12:00:00Z",
        title: `Synthetic article ${articleIndex}`,
        description: "Synthetic RSS article content used to exercise a realistically sized payload.",
        isRead: false,
      })),
    },
  ]))
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  rssEventState.handlers = undefined
})

describe("useRSSFeeds", () => {
  it("stores a large SSE update without invalidating the same query", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries")
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )

    renderHook(() => useRSSFeeds(1), { wrapper })
    await waitFor(() => {
      expect(mockApi.getRSSItems).toHaveBeenCalledExactlyOnceWith(1, true)
    })

    const items = syntheticRSSItems(200, 50)
    act(() => {
      rssEventState.handlers?.onFeedsUpdate?.({ instanceId: 1, items, timestamp: 0 } as FeedsUpdatePayload)
    })

    expect(Object.keys(queryClient.getQueryData<RSSItems>(rssKeys.feeds(1)) ?? {})).toHaveLength(200)
    expect(invalidateSpy).not.toHaveBeenCalled()
  })
})
