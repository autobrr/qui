/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import type { TorznabIndexer, TorznabRecentSearch, TorznabSearchRequest, TorznabSearchResponse } from "@/types"

import { Search } from "./Search"

const api = vi.hoisted(() => ({
  listTorznabIndexers: vi.fn(),
  getRecentTorznabSearches: vi.fn(),
  searchTorznab: vi.fn(),
}))
vi.mock("@/lib/api", () => ({ api }))

const instances = vi.hoisted(() => ({ instances: [{ id: 1, name: "Stub 1", connected: true }] }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instances }))

vi.mock("@/hooks/useSearchPreferences", async () => {
  const { useState } = await import("react")
  return { useSearchType: () => useState("movies") }
})

vi.mock("@/lib/client-settings", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/client-settings")>(),
  useClientSettingsSettled: () => true,
}))

vi.mock("@/hooks/useMediaQuery", () => ({
  useIsPhone: () => false,
  useIsMobile: () => false,
  useMediaQuery: () => false,
}))

vi.mock("@/components/torrents/AddTorrentDialog", () => ({ AddTorrentDialog: () => null }))


afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function indexer(id: number, enabled = true): TorznabIndexer {
  return {
    id, name: `Indexer ${id}`, base_url: "http://127.0.0.1:1", indexer_id: String(id), backend: "jackett", enabled, priority: 0, timeout_seconds: 30,
    capabilities: [], categories: [], last_test_status: "", created_at: "", updated_at: "",
  }
}

const recent: TorznabRecentSearch = {
  cacheKey: "recent-1", scope: "general", query: "Sample Film 2023", categories: [2000], indexerIds: [1], totalResults: 3,
  cachedAt: "2026-11-20T09:00:00Z", expiresAt: "2026-11-22T09:00:00Z", hitCount: 1,
}

const cache = { hit: true, source: "cache", scope: "general", cachedAt: "2026-11-21T09:00:00Z", expiresAt: "2026-11-22T09:00:00Z" } as TorznabSearchResponse["cache"]

const found: TorznabSearchResponse = {
  total: 1,
  cache,
  results: [{
    indexer: "Indexer 1", indexerId: 1, title: "Sample.Film.2023.1080p.WEB-RIG", downloadUrl: "http://127.0.0.1/dl/1.torrent", size: 1024, seeders: 5, leechers: 1,
    categoryId: 2000, categoryName: "Movies", publishDate: "2026-11-16T09:00:00Z", downloadVolumeFactor: 1, uploadVolumeFactor: 1, guid: "1",
  }],
}

const never = () => new Promise<never>(() => {})

function renderSearch() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <Search />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return client
}

async function searchFor(query: string) {
  const input = screen.getByRole("combobox", { name: "Search Query" })
  fireEvent.change(input, { target: { value: query } })
  await waitFor(() => expect(screen.getByRole("button", { name: "Search" }).hasAttribute("disabled")).toBe(false))
  fireEvent.submit(screen.getByRole("search"))
}

const sent = (call: number): TorznabSearchRequest => api.searchTorznab.mock.calls[call][0]

describe("Search", () => {
  // regression: #4354
  it("offers a recent search only once the indexer list loads, so the pick searches its own indexers", async () => {
    let loadIndexers: (list: TorznabIndexer[]) => void = () => {}
    api.listTorznabIndexers.mockImplementation(() => new Promise(resolve => { loadIndexers = resolve }))
    api.getRecentTorznabSearches.mockResolvedValue([{ ...recent, indexerIds: [2] }])
    api.searchTorznab.mockImplementation(never)
    renderSearch()

    await waitFor(() => expect(api.getRecentTorznabSearches).toHaveBeenCalled())
    await act(async () => {})
    fireEvent.focus(screen.getByRole("combobox", { name: "Search Query" }))
    expect(screen.queryByRole("option", { name: /Sample Film 2023/ })).toBeNull()

    await act(async () => loadIndexers([indexer(1), indexer(2)]))
    fireEvent.click(await screen.findByRole("option", { name: /Sample Film 2023/ }))

    await waitFor(() => expect(api.searchTorznab).toHaveBeenCalledTimes(1))
    expect(sent(0).indexer_ids).toEqual([2])
  })

  // regression: #4354
  it("says when the indexer list fails to load, and loads it again on Try again", async () => {
    api.listTorznabIndexers.mockRejectedValueOnce(new Error("server down")).mockResolvedValue([indexer(1)])
    api.getRecentTorznabSearches.mockResolvedValue([])
    renderSearch()

    expect(await screen.findByText("Failed to load indexers")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))

    expect(await screen.findByRole("combobox", { name: "Search Query" })).toBeTruthy()
    expect(screen.queryByText("Failed to load indexers")).toBeNull()
  })

  // regression: #4354
  it("shows the results without waiting for the recent searches to load again", async () => {
    api.listTorznabIndexers.mockResolvedValue([indexer(1)])
    api.getRecentTorznabSearches.mockResolvedValueOnce([]).mockImplementation(never)
    api.searchTorznab.mockResolvedValue({ results: [], total: 0 })
    renderSearch()

    await searchFor("Sample Film 2023")

    expect(await screen.findByText("No results found for \"Sample Film 2023\"")).toBeTruthy()
  })

  // regression: #4354
  it("offers Refresh when a cached search found nothing", async () => {
    api.listTorznabIndexers.mockResolvedValue([indexer(1)])
    api.getRecentTorznabSearches.mockResolvedValue([])
    api.searchTorznab.mockResolvedValue({ results: [], total: 0, cache })
    renderSearch()

    await searchFor("Sample Film 2023")
    fireEvent.click(await screen.findByRole("button", { name: "Refresh" }))
    fireEvent.click(await screen.findByRole("button", { name: "Refresh now" }))

    await waitFor(() => expect(api.searchTorznab).toHaveBeenCalledTimes(2))
    expect(sent(1).cache_mode).toBe("bypass")
  })

  // regression: #4354
  it("does not bypass the cache again on Try again after a failed refresh", async () => {
    api.listTorznabIndexers.mockResolvedValue([indexer(1)])
    api.getRecentTorznabSearches.mockResolvedValue([])
    api.searchTorznab.mockResolvedValueOnce(found).mockRejectedValueOnce(new Error("indexers timed out")).mockResolvedValue(found)
    renderSearch()

    await searchFor("Sample Film 2023")
    fireEvent.click(await screen.findByRole("button", { name: "Refresh" }))
    fireEvent.click(await screen.findByRole("button", { name: "Refresh now" }))
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }))

    await waitFor(() => expect(api.searchTorznab).toHaveBeenCalledTimes(3))
    expect(sent(1).cache_mode).toBe("bypass")
    expect(sent(2).cache_mode).toBeUndefined()
  })

  // regression: #4354
  it("offers Search all indexers when an enabled indexer was not in the search", async () => {
    api.listTorznabIndexers.mockResolvedValue([indexer(1)])
    api.getRecentTorznabSearches.mockResolvedValue([])
    api.searchTorznab.mockResolvedValue({ results: [], total: 0 })
    const client = renderSearch()

    await searchFor("Sample Film 2023")
    await screen.findByText("No results found for \"Sample Film 2023\"")
    expect(screen.queryByRole("button", { name: "Search all indexers" })).toBeNull()

    client.setQueryData(["torznab", "indexers"], [indexer(1, false), indexer(2)])

    expect(await screen.findByRole("button", { name: "Search all indexers" })).toBeTruthy()
  })
})
