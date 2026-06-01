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
    updateInstancePreferences: vi.fn(),
  },
}))

import { api } from "@/lib/api"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useInstanceMetadata, type InstanceMetadata } from "@/hooks/useInstanceMetadata"

const mockedApi = vi.mocked(api, true)

function makeWrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

describe("useInstancePreferences metadata interaction", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockedApi.getCategories.mockResolvedValue({
      movies: { name: "movies", savePath: "/data/movies" },
    } as never)
    mockedApi.getTags.mockResolvedValue(["linux", "iso"] as never)
    mockedApi.getInstancePreferences.mockResolvedValue({ listen_port: 6881 } as never)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  // Regression: fetching preferences before any metadata exists must not seed an
  // instance-metadata entry with empty categories/tags. That used to make
  // useInstanceMetadata treat metadata as complete and skip its fallback, leaving
  // category/tag selectors permanently empty.
  it("fetching preferences first does not poison metadata, so the categories/tags fallback still runs", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = makeWrapper(client)

    // 1) Preferences hook runs first (e.g. a preferences panel opened before the
    //    torrents view hydrated the metadata cache).
    const prefs = renderHook(() => useInstancePreferences(1), { wrapper })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10)
    })

    expect(mockedApi.getInstancePreferences).toHaveBeenCalledWith(1)
    expect(prefs.result.current.preferences).toEqual({ listen_port: 6881 })

    // It must not have fabricated a metadata entry with empty categories/tags.
    const metadataAfterPrefs = client.getQueryData<InstanceMetadata>(["instance-metadata", 1])
    expect(metadataAfterPrefs?.categories).toBeUndefined()
    expect(metadataAfterPrefs?.tags).toBeUndefined()

    // 2) Metadata hook mounts later. Because the metadata entry has no preferences,
    //    its fallback fetches categories + tags (+ preferences) directly.
    const meta = renderHook(() => useInstanceMetadata(1), { wrapper })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500) // fallback delay (400ms) + settle
    })

    expect(mockedApi.getCategories).toHaveBeenCalledWith(1)
    expect(mockedApi.getTags).toHaveBeenCalledWith(1)
    expect(meta.result.current.data?.tags).toEqual(["linux", "iso"])
    expect(Object.keys(meta.result.current.data?.categories ?? {})).toContain("movies")
  })

  // The merge path: when a metadata entry already exists (e.g. hydrated by the
  // torrent stream), the preferences hook enriches it in place without clobbering
  // the already-present categories/tags.
  it("merges fetched preferences into an existing metadata entry", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData<InstanceMetadata>(["instance-metadata", 1], {
      categories: { movies: { name: "movies", savePath: "/data/movies" } },
      tags: ["linux"],
    })
    const wrapper = makeWrapper(client)

    renderHook(() => useInstancePreferences(1), { wrapper })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10)
    })

    const md = client.getQueryData<InstanceMetadata>(["instance-metadata", 1])
    expect(md?.preferences).toEqual({ listen_port: 6881 })
    expect(Object.keys(md?.categories ?? {})).toContain("movies")
    expect(md?.tags).toEqual(["linux"])
  })
})
