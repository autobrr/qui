/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import type { TorznabRecentSearch } from "@/types"

import { SearchForm } from "./SearchForm"

vi.mock("@/hooks/useMediaQuery", () => ({
  useIsPhone: () => false,
  useIsMobile: () => false,
  useMediaQuery: () => false,
}))

afterEach(cleanup)

function recent(cacheKey: string, query: string): TorznabRecentSearch {
  return { cacheKey, scope: "general", query, categories: [2000], indexerIds: [1], totalResults: 2, cachedAt: "2026-11-20T09:00:00Z", expiresAt: "2026-11-22T09:00:00Z", hitCount: 1 }
}

function Form({ onRecent }: { onRecent: (search: TorznabRecentSearch) => void }) {
  const [query, setQuery] = useState("")
  return (
    <SearchForm
      query={query}
      onQueryChange={setQuery}
      type="movies"
      onTypeChange={() => {}}
      typeReady
      params={{}}
      onParamsChange={() => {}}
      indexers={[]}
      selectedIndexerIds={new Set()}
      onIndexersChange={() => {}}
      recent={[recent("a", "Other Film 2021"), recent("b", "Sample Beta 2023")]}
      pending={false}
      canSubmit={false}
      onSubmit={() => {}}
      onRecent={onRecent}
    />
  )
}

describe("SearchForm", () => {
  // regression: #4354
  it("highlights no recent search after the query is cleared, so Enter runs none", () => {
    const onRecent = vi.fn()
    render(<Form onRecent={onRecent} />)
    const input = screen.getByRole("combobox", { name: "Search Query" })

    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: "bet" } })
    fireEvent.keyDown(input, { key: "ArrowDown" })
    fireEvent.click(screen.getByRole("button", { name: "Clear the query" }))

    expect(screen.getAllByRole("option").every(option => option.getAttribute("aria-selected") === "false")).toBe(true)
    fireEvent.keyDown(input, { key: "Enter" })
    expect(onRecent).not.toHaveBeenCalled()
  })
})
