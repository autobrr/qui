/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    getCategories: vi.fn(),
    getTags: vi.fn(),
    getInstancePreferences: vi.fn(),
  },
}))

import { api } from "@/lib/api"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"

const mockedApi = vi.mocked(api, true)

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
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
    mockedApi.getInstancePreferences.mockResolvedValue({} as never)
  })

  afterEach(() => {
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
})
