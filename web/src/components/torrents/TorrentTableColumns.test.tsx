/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "@/i18n"
import type { Torrent } from "@/types"
import { cleanup, render } from "@testing-library/react"
import type { ReactElement } from "react"
import { afterEach, describe, expect, it } from "vitest"

import { createColumns, type TableViewMode } from "./TorrentTableColumns"

function iconColumnSizes(viewMode: TableViewMode) {
  return createColumns({ incognitoMode: false, viewMode }, i18n.getFixedT(null, "torrents"))
    .filter(col => col.id === "status_icon" || col.id === "tracker_icon")
    .map(({ size, minSize, maxSize }) => ({ size, minSize, maxSize }))
}

describe("icon columns", () => {
  // 16px icon plus the row padding of the view mode: px-3 (12px) normal, px-2 (8px) dense.
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
