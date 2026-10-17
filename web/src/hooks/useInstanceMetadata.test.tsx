/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    getCategories: vi.fn(),
    getTags: vi.fn(),
  },
}))

import { api } from "@/lib/api"
import { useInstanceMetadata, type InstanceMetadata } from "@/hooks/useInstanceMetadata"

const mockedApi = vi.mocked(api, true)

function makeWrapper(metadata?: InstanceMetadata) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  if (metadata) {
    queryClient.setQueryData(["instance-metadata", 1], metadata)
  }
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe("useInstanceMetadata", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockedApi.getCategories.mockResolvedValue({
      movies: { name: "movies", savePath: "/data/movies" },
    } as never)
    mockedApi.getTags.mockResolvedValue(["linux", "iso"])
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it("fetches categories and tags via the fallback when no stream has hydrated the cache", async () => {
    const { result } = renderHook(() => useInstanceMetadata(1), { wrapper: makeWrapper() })

    // The fallback fires after the default 400ms delay; advancing flushes its
    // Promise.all + the resulting query-cache update.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })

    expect(mockedApi.getCategories).toHaveBeenCalledWith(1)
    expect(mockedApi.getTags).toHaveBeenCalledWith(1)
    expect(result.current.data?.tags).toEqual(["linux", "iso"])
    expect(Object.keys(result.current.data?.categories ?? {})).toContain("movies")
  })
  it("does not fetch categories and tags when the torrent list already put them in the cache", async () => {
    const metadata: InstanceMetadata = { categories: {}, tags: ["linux"] }
    const { result } = renderHook(() => useInstanceMetadata(1), { wrapper: makeWrapper(metadata) })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })

    expect(mockedApi.getCategories).not.toHaveBeenCalled()
    expect(mockedApi.getTags).not.toHaveBeenCalled()
    expect(result.current.data).toBe(metadata)
    expect(result.current.isLoading).toBe(false)
  })

  it("treats the empty entry of another screen as not loaded", async () => {
    const wrapper = makeWrapper()
    renderHook(() => useInstanceMetadata(1, { fallbackDelayMs: Infinity }), { wrapper })
    const { result } = renderHook(() => useInstanceMetadata(1), { wrapper })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })

    expect(mockedApi.getCategories).toHaveBeenCalledWith(1)
    expect(result.current.data?.tags).toEqual(["linux", "iso"])
  })
  it("counts an instance with no categories and no tags as loaded", async () => {
    mockedApi.getCategories.mockResolvedValue({} as never)
    mockedApi.getTags.mockResolvedValue([])
    const { result } = renderHook(() => useInstanceMetadata(1), { wrapper: makeWrapper() })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500)
    })

    expect(result.current.isLoading).toBe(false)
    expect(mockedApi.getCategories).toHaveBeenCalledTimes(1)
  })
})
