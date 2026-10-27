/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "@/i18n"
import { describe, expect, it } from "vitest"

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
