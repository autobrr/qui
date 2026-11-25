/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { formatNarrowAge } from "@/lib/dateTimeUtils"
import { DEFAULT_SORT } from "@/lib/search-results"
import type { InstanceResponse, TorznabSearchResult } from "@/types"

import { SearchResults } from "./SearchResults"

const screenSize = vi.hoisted(() => ({ phone: false, wide: false }))
vi.mock("@/hooks/useMediaQuery", () => ({
  useIsPhone: () => screenSize.phone,
  useIsMobile: () => screenSize.phone,
  useMediaQuery: (query: string) => query === "(min-width: 1440px)" && screenSize.wide,
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  screenSize.phone = false
  screenSize.wide = false
})

function result(edit: Partial<TorznabSearchResult>): TorznabSearchResult {
  return {
    indexer: "Aurora", indexerId: 1, title: "Sample.Film.2023.1080p.WEB-RIG", downloadUrl: "http://127.0.0.1/dl/1.torrent",
    infoUrl: "http://127.0.0.1/info/1", size: 1024, seeders: 9, leechers: 1, categoryId: 2000, categoryName: "Movies",
    publishDate: "2026-11-16T09:00:00Z", downloadVolumeFactor: 1, uploadVolumeFactor: 1, guid: "1",
    ...edit,
  }
}

const first = result({ guid: "1", seeders: 30, title: "Sample.Film.2023.2160p.WEB-RIG" })
const second = result({ guid: "2", seeders: 20, indexer: "Cobalt", indexerId: 3, title: "Sample.Film.2023.1080p.BluRay-RIG" })
const third = result({ guid: "3", seeders: 10, title: "Sample.Film.2023.720p.WEB-RIG" })
const instances = [{ id: 1, name: "Stub 1", connected: true }, { id: 2, name: "Stub 2", connected: false }] as InstanceResponse[]

function renderResults(results: TorznabSearchResult[], onAdd = vi.fn(), onOpenInfo = vi.fn()) {
  render(
    <TooltipProvider delayDuration={0}>
      <SearchResults
        results={results}
        partial={false}
        categoryName={() => "Movies"}
        sort={DEFAULT_SORT}
        onSortChange={() => {}}
        onRefresh={() => {}}
        refreshReady
        instances={instances}
        onAdd={onAdd}
        onDownload={() => {}}
        onOpenInfo={onOpenInfo}
      />
    </TooltipProvider>
  )
  return onAdd
}

const box = (title: string) => screen.getByRole("checkbox", { name: title })
const openMenu = (trigger: HTMLElement) => fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false })

describe("SearchResults", () => {
  it("adds the ticked results to the instance that the user picks", () => {
    const onAdd = renderResults([first, second, third])

    fireEvent.click(screen.getByText(first.title))
    fireEvent.click(screen.getByText(third.title), { shiftKey: true })
    const bar = screen.getByRole("region", { name: "Selected results" })
    expect(within(bar).getByText("3 selected")).toBeTruthy()

    fireEvent.click(box(second.title))
    openMenu(within(bar).getByRole("button", { name: /^Add/ }))
    fireEvent.click(screen.getByRole("menuitem", { name: /Stub 2/ }))

    expect(onAdd).toHaveBeenCalledTimes(1)
    expect(onAdd.mock.calls[0][0].map((picked: TorznabSearchResult) => picked.guid)).toEqual(["1", "3"])
    expect(onAdd.mock.calls[0][1]).toBe(2)
  })

  it("ticks the range from the row ticked before, also when React renders both ticks at once", () => {
    renderResults([first, second, third])

    act(() => {
      fireEvent.click(screen.getByText(first.title))
      fireEvent.click(screen.getByText(third.title), { shiftKey: true })
    })

    expect(within(screen.getByRole("region", { name: "Selected results" })).getByText("3 selected")).toBeTruthy()
  })

  it("shows all results again and drops the selection on a tap of the filtered count", () => {
    renderResults([first, second, third])

    fireEvent.click(box(first.title))
    fireEvent.change(screen.getByRole("textbox", { name: "Filter results..." }), { target: { value: "WEB" } })
    fireEvent.click(box(third.title))
    expect(screen.getByRole("region", { name: "Selected results" }).textContent).toContain("2 selected")
    fireEvent.click(screen.getByRole("button", { name: /2 of 3 results/ }))

    expect(screen.getByText("3 results")).toBeTruthy()
    expect(screen.getAllByRole("row").length).toBe(4)
    expect(screen.queryByRole("region", { name: "Selected results" })).toBeNull()
    expect(screen.getAllByRole("checkbox").every(checkbox => checkbox.getAttribute("aria-checked") === "false")).toBe(true)
  })

  it("keeps the other row actions in a menu below 1440 px, shows each as a button from 1440 px, and a click on one leaves the row unticked", () => {
    renderResults([first])
    expect(screen.queryByRole("button", { name: "Open on indexer" })).toBeNull()
    openMenu(screen.getByRole("button", { name: "More actions" }))
    expect(screen.getAllByRole("menuitem").map(item => item.textContent)).toEqual(["Download .torrent", "Open on indexer", "Copy title"])
    cleanup()

    screenSize.wide = true
    const onOpenInfo = vi.fn()
    renderResults([first, result({ guid: "4", infoUrl: undefined, title: "Sample.Film.2023.480p.WEB-RIG" })], undefined, onOpenInfo)

    expect(screen.queryByRole("button", { name: "More actions" })).toBeNull()
    expect(screen.getAllByRole("button", { name: "Download .torrent" }).length).toBe(2)
    expect(screen.getAllByRole("button", { name: "Copy title" }).length).toBe(2)
    expect(screen.getAllByRole("button", { name: "Open on indexer" }).length).toBe(1)

    fireEvent.click(screen.getByRole("button", { name: "Open on indexer" }))
    expect(onOpenInfo).toHaveBeenCalledWith(first)
    expect(box(first.title).getAttribute("aria-checked")).toBe("false")
  })

  it("names each row button, and the exact date of a result, in a tooltip once a mouse enters the row", async () => {
    screenSize.wide = true
    renderResults([first])
    const row = screen.getAllByRole("row")[1]
    fireEvent.pointerOver(row, { pointerType: "mouse" })

    for (const name of ["Add", "Download .torrent", "Open on indexer", "Copy title"]) {
      const button = screen.getByRole("button", { name })
      fireEvent.pointerEnter(button, { pointerType: "mouse" })
      fireEvent.pointerMove(button, { pointerType: "mouse" })
      await waitFor(() => expect(screen.getByRole("tooltip").textContent).toBe(name))
    }

    const cell = within(row).getAllByText(formatNarrowAge(Date.now() - Date.parse(first.publishDate))).at(-1)!
    fireEvent.pointerEnter(cell, { pointerType: "mouse" })
    fireEvent.pointerMove(cell, { pointerType: "mouse" })
    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toContain("2026"))
  })

  it("names a row button in a tooltip also when Chrome reports the mouse as coming from the parent of a button that the row above rebuilt", async () => {
    screenSize.wide = true
    renderResults([first, second])
    const [, above, below] = screen.getAllByRole("row")
    const rebuilt = within(above).getByRole("button", { name: "Copy title" })
    const actions = rebuilt.parentElement!
    fireEvent.pointerOver(rebuilt, { pointerType: "mouse" })
    expect(rebuilt.isConnected).toBe(false)

    fireEvent.pointerOver(within(below).getByRole("button", { name: "Copy title" }), { pointerType: "mouse", relatedTarget: actions })
    const button = within(below).getByRole("button", { name: "Copy title" })
    fireEvent.pointerEnter(button, { pointerType: "mouse" })
    fireEvent.pointerMove(button, { pointerType: "mouse" })
    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toBe("Copy title"))
  })

  it("shows the exact date of a result on a tap of its age, and the tap leaves the row unticked", async () => {
    renderResults([first])
    const age = screen.getAllByText(formatNarrowAge(Date.now() - Date.parse(first.publishDate)))[0]

    fireEvent.pointerDown(age, { pointerType: "touch" })
    fireEvent.click(age)

    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toContain("2026"))
    expect(box(first.title).getAttribute("aria-checked")).toBe("false")
  })

  it("names a focused row button in a tooltip after the first Tab press", async () => {
    screenSize.wide = true
    renderResults([first])

    fireEvent.keyDown(document.body, { key: "Tab" })
    const button = screen.getByRole("button", { name: "Copy title" })
    fireEvent.keyDown(button, { key: "Tab" })
    fireEvent.focus(button)

    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toBe("Copy title"))
  })

  it("offers no action that a result cannot do, and a tap on its actions leaves the row unticked", () => {
    const noPage = result({ infoUrl: undefined })
    const onAdd = renderResults([noPage])

    expect(screen.queryByRole("button", { name: "Open on indexer" })).toBeNull()
    openMenu(screen.getByRole("button", { name: "More actions" }))
    const items = screen.getAllByRole("menuitem").map(item => item.textContent)
    expect(items).toEqual(["Download .torrent", "Copy title"])
    expect(screen.queryAllByRole("menuitem").some(item => item.getAttribute("aria-disabled") === "true")).toBe(false)

    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    const add = screen.getAllByRole("button", { name: "Add" }).at(-1)!
    fireEvent.click(add)
    openMenu(add)
    fireEvent.click(screen.getByRole("menuitem", { name: /Stub 1/ }))
    expect(onAdd).toHaveBeenCalledWith([noPage], 1)
    expect(box(noPage.title).getAttribute("aria-checked")).toBe("false")
    fireEvent.click(add.closest("td")!)
    expect(box(noPage.title).getAttribute("aria-checked")).toBe("false")
    expect(screen.queryByRole("region", { name: "Selected results" })).toBeNull()
  })
})
