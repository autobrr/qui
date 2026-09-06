/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import type { Torrent } from "@/types"
import { getToggleSelectionState, resolveStreamRow } from "./torrent-utils"

describe("getToggleSelectionState", () => {
  const cases: Array<{ name: string; values: boolean[]; stateUnknown: boolean; allEnabled: boolean; mixed: boolean }> = [
    { name: "all enabled offers the disable direction", values: [true, true], stateUnknown: false, allEnabled: true, mixed: false },
    { name: "all disabled offers the enable direction", values: [false, false], stateUnknown: false, allEnabled: false, mixed: false },
    { name: "disagreeing values are mixed", values: [true, false, true], stateUnknown: false, allEnabled: false, mixed: true },
    { name: "an empty selection is neither enabled nor mixed", values: [], stateUnknown: false, allEnabled: false, mixed: false },
    // Select-all over more torrents than are loaded: the loaded rows can agree by
    // chance, so a direction read from them would be applied to unseen torrents.
    { name: "unloaded rows make an all-enabled window mixed", values: [true, true], stateUnknown: true, allEnabled: true, mixed: true },
    { name: "unloaded rows make an all-disabled window mixed", values: [false, false], stateUnknown: true, allEnabled: false, mixed: true },
    { name: "unloaded rows with nothing loaded are mixed", values: [], stateUnknown: true, allEnabled: false, mixed: true },
  ]

  for (const { name, values, stateUnknown, allEnabled, mixed } of cases) {
    it(name, () => {
      expect(getToggleSelectionState(values, stateUnknown)).toEqual({ allEnabled, mixed })
    })
  }
})

describe("resolveStreamRow", () => {
  const previous = { hash: "aa11", name: "Hybrid.Release.S02E03.2160p.WEB-GRPB" } as Torrent
  const cases: Array<{ name: string; data: { torrents: Torrent[]; total: number }; want: Torrent | null }> = [
    { name: "a changed row replaces the previous one", data: { torrents: [{ ...previous, progress: 1 }], total: 1 }, want: { ...previous, progress: 1 } },
    // The panel asked for the v2 hash; the server answered with the v1-keyed row.
    { name: "a row keyed by another hash still lands", data: { torrents: [{ ...previous, hash: "aa11" }], total: 1 }, want: previous },
    { name: "an unchanged delta keeps the previous row", data: { torrents: [], total: 1 }, want: previous },
    { name: "a removed torrent drops the row", data: { torrents: [], total: 0 }, want: null },
  ]

  for (const { name, data, want } of cases) {
    it(name, () => {
      expect(resolveStreamRow(previous, data)).toEqual(want)
    })
  }
})
