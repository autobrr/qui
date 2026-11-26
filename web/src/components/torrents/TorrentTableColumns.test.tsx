/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TooltipProvider } from "@/components/ui/tooltip"
import i18n from "@/i18n"
import { getLinuxTags } from "@/lib/incognito"
import type { Torrent } from "@/types"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactElement } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { createColumns, type TableViewMode } from "./TorrentTableColumns"

function iconColumnSizes(viewMode: TableViewMode) {
  return createColumns({ incognitoMode: false, viewMode }, i18n.getFixedT(null, "torrents"))
    .filter(col => col.id === "status_icon" || col.id === "tracker_icon")
    .map(({ size, minSize, maxSize }) => ({ size, minSize, maxSize }))
}

describe("icon columns", () => {
  it.each([
    ["normal", 40],
    ["dense", 32],
  ] as const)("%s mode sizes both icon columns to icon plus padding", (viewMode, width) => {
    const sizes = iconColumnSizes(viewMode)
    expect(sizes).toEqual([
      { size: width, minSize: width, maxSize: width },
      { size: width, minSize: width, maxSize: width },
    ])
  })
})

describe("reannounce column", () => {
  afterEach(cleanup)

  function reannounceCell(state: string, reannounce: number | undefined): string {
    const column = createColumns({ incognitoMode: false }, i18n.getFixedT(null, "torrents"))
      .find(col => "accessorKey" in col && col.accessorKey === "reannounce")!
    const cell = column.cell as (ctx: { row: { original: Torrent } }) => ReactElement
    return render(cell({ row: { original: { state, reannounce } as Torrent } })).container.textContent ?? ""
  }

  it("shows a dash when the row does not carry reannounce", () => {
    expect(reannounceCell("uploading", undefined)).toBe("—")
  })

  it("shows a dash for a stopped torrent", () => {
    expect(reannounceCell("stoppedUP", 0)).toBe("—")
  })

  it("shows now for 0 on an active torrent", () => {
    expect(reannounceCell("uploading", 0)).toBe("now")
  })

  it("shows minutes for an active torrent", () => {
    expect(reannounceCell("stalledUP", 840)).toBe("14m")
  })
})

describe("popularity and private columns", () => {
  afterEach(cleanup)

  function cellText(accessorKey: "popularity" | "private", original: Partial<Torrent>): string {
    const column = createColumns({ incognitoMode: false }, i18n.getFixedT(null, "torrents"))
      .find(col => "accessorKey" in col && col.accessorKey === accessorKey)!
    const cell = column.cell as (ctx: { row: { original: Torrent } }) => ReactElement
    return render(cell({ row: { original: original as Torrent } })).container.textContent ?? ""
  }

  it("shows a dash when the row does not carry popularity", () => {
    expect(cellText("popularity", {})).toBe("—")
  })

  it("shows a popularity of 0 as a value", () => {
    expect(cellText("popularity", { popularity: 0 })).toBe("0.00")
  })

  it("shows a dash when the row does not carry private", () => {
    expect(cellText("private", {})).toBe("—")
  })

  it("shows the private value", () => {
    expect(cellText("private", { private: true })).toBe("Yes")
    cleanup()
    expect(cellText("private", { private: false })).toBe("No")
  })
})

describe("name column", () => {
  afterEach(cleanup)

  function nameCell(original: Partial<Torrent>): string {
    const column = createColumns({ incognitoMode: false }, i18n.getFixedT(null, "torrents"))
      .find(col => "accessorKey" in col && col.accessorKey === "name")!
    const cell = column.cell as (ctx: { row: { original: Torrent } }) => ReactElement
    return render(cell({ row: { original: { name: "Example.Show.S01E01-GRP", ...original } as Torrent } }), { wrapper: TooltipProvider }).container.textContent ?? ""
  }

  it("shows the badge of a helper torrent", () => {
    expect(nameCell({ qui_helper: "transfer" })).toBe("Transfer helperExample.Show.S01E01-GRP")
    expect(nameCell({ qui_helper: "share" })).toBe("Share helperExample.Show.S01E01-GRP")
  })

  it("shows no badge on a torrent that is no helper", () => {
    expect(nameCell({ tags: "qui-transfer" })).toBe("Example.Show.S01E01-GRP")
  })
})

describe("cut-off cell text", () => {
  afterEach(cleanup)
  beforeEach(() => {
    delete (window as Window & { ontouchstart?: unknown }).ontouchstart
  })

  function layout(scrollWidth: number) {
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(scrollWidth)
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(50)
  }

  function focusCell(accessorKey: "name" | "category" | "tags", text: string, incognitoMode = false) {
    const column = createColumns({ incognitoMode }, i18n.getFixedT(null, "torrents"))
      .find(col => "accessorKey" in col && col.accessorKey === accessorKey)!
    const cell = column.cell as (ctx: { row: { original: Torrent } }) => ReactElement
    const original = { hash: "9f8e7d6c5b4a3210", name: "Example.Show.S01E01-GRP", category: "tv-shows", tags: "seed, keep" } as Torrent
    render(cell({ row: { original } }), { wrapper: TooltipProvider })
    fireEvent.pointerEnter(screen.getByText(text))
    fireEvent.focus(screen.getByText(text))
    return screen.queryByRole("tooltip")?.textContent
  }

  it.each([
    ["name", "Example.Show.S01E01-GRP"],
    ["category", "tv-shows"],
    ["tags", "seed, keep"],
  ] as const)("shows the full %s when it is cut off", (accessorKey, text) => {
    layout(200)
    expect(focusCell(accessorKey, text)).toContain(text)
  })

  it.each([
    ["name", "Example.Show.S01E01-GRP"],
    ["category", "tv-shows"],
    ["tags", "seed, keep"],
  ] as const)("shows no tooltip when the %s fits", (accessorKey, text) => {
    layout(50)
    expect(focusCell(accessorKey, text)).toBeUndefined()
  })

  it("measures no cell until the pointer enters it", () => {
    layout(200)
    const column = createColumns({ incognitoMode: false }, i18n.getFixedT(null, "torrents"))
      .find(col => "accessorKey" in col && col.accessorKey === "tags")!
    const cell = column.cell as (ctx: { row: { original: Torrent } }) => ReactElement
    render(cell({ row: { original: { hash: "9f8e7d6c5b4a3210", tags: "seed, keep" } as Torrent } }), { wrapper: TooltipProvider })

    expect(document.querySelector("[data-slot=truncated-text]")).toBeNull()
  })

  it("shows the fake tags in the tooltip in incognito mode", () => {
    layout(200)
    const fake = getLinuxTags("9f8e7d6c5b4a3210")
    expect(focusCell("tags", fake, true)).toContain(fake)
  })
})
