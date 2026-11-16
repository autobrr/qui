/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import "@/i18n"
import type { ColumnFilter } from "@/lib/column-filter-utils"
import { EMPTY_SIDEBAR_FILTERS } from "@/lib/filter-views"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import type { CrossInstanceTorrent, TorrentResponse } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import { createElement, type ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { type ListView, listSearchScope, PARTIAL_RETRY_MS, useTorrentResults } from "./useTorrentResults"

const fixture = vi.hoisted(() => ({
  translation: { t: (key: string) => key },
  instances: { instances: [{ id: 1, name: "seedbox", isActive: true }, { id: 2, name: "archive", isActive: true }] },
  layoutRoute: { state: { showInstanceControls: false, instanceId: undefined } },
  location: { pathname: "/dashboard", search: {} },
  incognito: [false, () => {}] as [boolean, (value: boolean) => void],
  unifiedPick: [[], () => {}] as [number[], () => void],
  columnFilters: [[], () => {}] as [unknown[], () => void],
  trackerCustomizations: { data: undefined },
  getCrossInstanceTorrents: vi.fn(),
}))

vi.mock("react-i18next", async importOriginal => ({ ...(await importOriginal<object>()), useTranslation: () => fixture.translation }))
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => () => {}, useLocation: () => fixture.location }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => fixture.instances }))
vi.mock("@/contexts/LayoutRouteContext", () => ({ useLayoutRoute: () => fixture.layoutRoute }))
vi.mock("@/lib/incognito", async importOriginal => ({ ...(await importOriginal<object>()), useIncognitoMode: () => fixture.incognito }))
vi.mock("@/hooks/usePersistedUnifiedInstanceFilter", () => ({ usePersistedUnifiedInstanceFilter: () => fixture.unifiedPick }))
vi.mock("@/hooks/usePersistedFilters", async () => {
  const sidebarFilters = [(await import("@/lib/filter-views")).EMPTY_SIDEBAR_FILTERS, () => {}]
  return { usePersistedFilters: () => sidebarFilters }
})
vi.mock("@/hooks/usePersistedColumnFilters", () => ({ usePersistedColumnFilters: () => fixture.columnFilters }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => fixture.trackerCustomizations }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsMobile: () => false }))
vi.mock("@/lib/api", () => ({ api: { getCrossInstanceTorrents: fixture.getCrossInstanceTorrents } }))

const RATIO_ABOVE_TWO: ColumnFilter[] = [{ columnId: "ratio", operation: "gt", value: "2" }]
const CROSS_SEEDS = "Hash == \"aa\" || Hash == \"bb\""

const view = (changes: Partial<ListView> = {}): ListView => ({
  list: 3,
  sidebarFilters: EMPTY_SIDEBAR_FILTERS,
  columnFilters: [],
  phoneLayout: false,
  unifiedInstanceIds: [1, 2, 3],
  ...changes,
})

describe("listSearchScope", () => {
  it("asks the instance of a list without filters, with copy groups", () => {
    const scope = listSearchScope(view())

    expect(scope).toEqual({ filters: { ...EMPTY_SIDEBAR_FILTERS, expr: undefined }, filtered: false, instanceIds: [3], groupCopies: true })
  })

  it("sends the sidebar filters of the list, with the column filters in the expression", () => {
    const sidebarFilters = { ...EMPTY_SIDEBAR_FILTERS, status: ["downloading"], expr: "Size > 5" }

    const scope = listSearchScope(view({ sidebarFilters, columnFilters: RATIO_ABOVE_TWO }))

    expect(scope.filters).toEqual({ ...sidebarFilters, expr: "(Ratio > 2) && (Size > 5)" })
    expect(scope.filtered).toBe(true)
  })

  it("counts a column filter alone as a filter", () => {
    const scope = listSearchScope(view({ columnFilters: RATIO_ABOVE_TWO }))

    expect([scope.filters.expr, scope.filtered]).toEqual(["Ratio > 2", true])
  })

  it("leaves the column filters out on the phone layout, which has none", () => {
    const scope = listSearchScope(view({ columnFilters: RATIO_ABOVE_TWO, phoneLayout: true }))

    expect([scope.filters.expr, scope.filtered]).toEqual([undefined, false])
  })

  it("asks the instances of the Unified view", () => {
    expect(listSearchScope(view({ list: ALL_INSTANCES_ID })).instanceIds).toEqual([1, 2, 3])
  })

  it("asks each instance for the copies in Filter Cross-Seeds, one row for each copy", () => {
    const sidebarFilters = { ...EMPTY_SIDEBAR_FILTERS, expr: CROSS_SEEDS }

    const scope = listSearchScope(view({ sidebarFilters }))

    expect(scope).toEqual({ filters: sidebarFilters, filtered: true, instanceIds: undefined, groupCopies: false })
  })

  it("sends the column filters in Filter Cross-Seeds too, where the table applies them in the browser", () => {
    const sidebarFilters = { ...EMPTY_SIDEBAR_FILTERS, expr: CROSS_SEEDS }

    const scope = listSearchScope(view({ sidebarFilters, columnFilters: RATIO_ABOVE_TWO }))

    expect(scope.filters.expr).toBe(`(Ratio > 2) && (${CROSS_SEEDS})`)
  })
})

const torrent = { hash: "aa", name: "Amber.Fields.2024.1080p.WEB-GRP", state: "uploading", size: 1, tracker: "", instanceId: 1, instanceName: "seedbox" } as CrossInstanceTorrent
const answer = (changes: Partial<TorrentResponse>): TorrentResponse => ({ torrents: [], crossInstanceTorrents: [], total: 0, ...changes })
const PARTIAL_EMPTY = answer({ partialResults: true })

describe("useTorrentResults", () => {
  let client: QueryClient

  beforeEach(() => {
    vi.useFakeTimers()
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  })

  afterEach(() => {
    cleanup()
    client.clear()
    vi.useRealTimers()
    fixture.getCrossInstanceTorrents.mockReset()
  })

  async function search(...answers: TorrentResponse[]) {
    for (const response of answers.slice(0, -1)) fixture.getCrossInstanceTorrents.mockResolvedValueOnce(response)
    fixture.getCrossInstanceTorrents.mockResolvedValue(answers.at(-1))
    const wrapper = ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client }, children)
    const hook = renderHook(() => useTorrentResults("amber", () => {}), { wrapper })
    await act(() => vi.advanceTimersByTimeAsync(0))
    return hook
  }

  const waitOneRetry = () => act(() => vi.advanceTimersByTimeAsync(PARTIAL_RETRY_MS))

  // regression: #4229
  it.each([
    ["an instance did not answer and nothing matched", PARTIAL_EMPTY, "commandPalette.searchFailed", false],
    ["an instance did not answer and others matched", answer({ partialResults: true, crossInstanceTorrents: [torrent], total: 1 }), "commandPalette.torrentMatches", false],
    ["each instance answered and nothing matched", answer({}), "commandPalette.noTorrentMatches", true],
  ])("notes the search when %s", async (_name, response, note, noMatch) => {
    const { result } = await search(response)

    expect(result.current.entries[0].note).toBe(note)
    expect(result.current.noMatch).toBe(noMatch)
  })

  // regression: #4229
  it("asks again while the answer is partial, and stops when it is complete", async () => {
    await search(PARTIAL_EMPTY, PARTIAL_EMPTY, answer({}))

    await waitOneRetry()
    await waitOneRetry()
    await waitOneRetry()
    await waitOneRetry()

    expect(fixture.getCrossInstanceTorrents).toHaveBeenCalledTimes(3)
  })

  it("asks nothing more after the palette closes", async () => {
    const { unmount } = await search(PARTIAL_EMPTY)
    await waitOneRetry()
    expect(fixture.getCrossInstanceTorrents).toHaveBeenCalledTimes(2)

    unmount()
    await waitOneRetry()
    await waitOneRetry()

    expect(fixture.getCrossInstanceTorrents).toHaveBeenCalledTimes(2)
  })
})
