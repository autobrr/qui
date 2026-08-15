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
    getInstancePreferencesWithMeta: vi.fn(),
    updateInstancePreferences: vi.fn(),
  },
}))

import { api } from "@/lib/api"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useInstanceMetadata, type InstanceMetadata } from "@/hooks/useInstanceMetadata"
import type { AppPreferences } from "@/types"

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
    mockedApi.getInstancePreferencesWithMeta.mockResolvedValue({
      preferences: { listen_port: 6881 },
      cachedAt: null,
    } as never)
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

    expect(mockedApi.getInstancePreferencesWithMeta).toHaveBeenCalledWith(1)
    expect(prefs.result.current.preferences).toEqual({ listen_port: 6881 })
    expect(prefs.result.current.cachedAt).toBeNull()

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

  // Graceful degradation: when the backend serves cached preferences (X-Qui-Cached-At
  // header present), the hook surfaces the parsed timestamp via cachedAt so the dialog
  // can warn the user.
  it("surfaces the cachedAt timestamp when the backend serves cached preferences", async () => {
    const cachedAt = new Date("2026-06-25T10:00:00Z")
    mockedApi.getInstancePreferencesWithMeta.mockResolvedValue({
      preferences: { listen_port: 6881 },
      cachedAt,
    } as never)

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = makeWrapper(client)

    const prefs = renderHook(() => useInstancePreferences(1), { wrapper })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10)
    })

    expect(prefs.result.current.preferences).toEqual({ listen_port: 6881 })
    expect(prefs.result.current.cachedAt).toEqual(cachedAt)
  })

  // A successful preferences write proves qBittorrent is reachable again, so the stale
  // "showing cached settings" marker must clear instead of sticking from the earlier
  // degraded load (the fetch-once query would otherwise never re-run to clear it).
  it("clears the cachedAt marker after a successful preferences update", async () => {
    const cachedAt = new Date("2026-06-25T10:00:00Z")
    mockedApi.getInstancePreferencesWithMeta.mockResolvedValue({
      preferences: { listen_port: 6881 },
      cachedAt,
    } as never)
    mockedApi.updateInstancePreferences.mockResolvedValue({ listen_port: 6882 } as never)

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = makeWrapper(client)

    const prefs = renderHook(() => useInstancePreferences(1), { wrapper })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10)
    })
    expect(prefs.result.current.cachedAt).toEqual(cachedAt)

    await act(async () => {
      prefs.result.current.updatePreferences({ listen_port: 6882 })
      await vi.advanceTimersByTimeAsync(10)
    })

    expect(mockedApi.updateInstancePreferences).toHaveBeenCalledWith(1, { listen_port: 6882 })
    expect(prefs.result.current.cachedAt).toBeNull()
  })

  // After the metadata short-circuit was removed: an explicit refetch must hit the
  // backend even when metadata already carries preferences, so staleness is re-derived
  // instead of returning cached metadata with a cleared cachedAt.
  it("refetch hits the backend even when metadata already has preferences", async () => {
    const cachedAt = new Date("2026-06-25T11:00:00Z")
    mockedApi.getInstancePreferencesWithMeta.mockResolvedValue({
      preferences: { listen_port: 7000 },
      cachedAt,
    } as never)

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData<InstanceMetadata>(["instance-metadata", 1], {
      categories: { movies: { name: "movies", savePath: "/data/movies" } },
      tags: ["linux"],
      preferences: { listen_port: 6881 } as AppPreferences,
    })
    const wrapper = makeWrapper(client)

    const prefs = renderHook(() => useInstancePreferences(1), { wrapper })

    // The query is disabled while metadata supplies preferences, so nothing fetched yet.
    expect(mockedApi.getInstancePreferencesWithMeta).not.toHaveBeenCalled()

    await act(async () => {
      await prefs.result.current.refetch()
      await vi.advanceTimersByTimeAsync(10)
    })

    expect(mockedApi.getInstancePreferencesWithMeta).toHaveBeenCalledWith(1)
    expect(prefs.result.current.cachedAt).toEqual(cachedAt)
  })
})
