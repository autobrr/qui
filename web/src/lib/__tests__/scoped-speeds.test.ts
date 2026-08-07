/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { resolveFooterSpeeds } from "@/lib/scoped-speeds"

describe("resolveFooterSpeeds", () => {
  it("uses serverState rates for a single-instance view", () => {
    expect(
      resolveFooterSpeeds(false, { totalDownloadSpeed: 999, totalUploadSpeed: 888, totalDownloadData: 777, totalUploadData: 666 }, { dl_info_speed: 100, up_info_speed: 50, dl_info_data: 25, up_info_data: 10 })
    ).toEqual({ downloadSpeed: 100, uploadSpeed: 50, downloadData: 25, uploadData: 10 })
  })

  it("uses aggregate stats totals for an all-instances view (no serverState)", () => {
    // Regression: aggregate views have no serverState, so footer speeds must come
    // from the aggregated stats rather than reading 0.
    expect(
      resolveFooterSpeeds(true, { totalDownloadSpeed: 4096, totalUploadSpeed: 2048, totalDownloadData: 1024, totalUploadData: 512 }, null)
    ).toEqual({ downloadSpeed: 4096, uploadSpeed: 2048, downloadData: 1024, uploadData: 512 })
  })

  it("falls back to 0 when nothing is available", () => {
    expect(resolveFooterSpeeds(true, null, null)).toEqual({ downloadSpeed: 0, uploadSpeed: 0, downloadData: 0, uploadData: 0 })
    expect(resolveFooterSpeeds(false, null, undefined)).toEqual({ downloadSpeed: 0, uploadSpeed: 0, downloadData: 0, uploadData: 0 })
  })

  it("does not leak aggregate totals into a single-instance view", () => {
    expect(
      resolveFooterSpeeds(false, { totalDownloadSpeed: 9999, totalUploadSpeed: 9999, totalDownloadData: 9999, totalUploadData: 9999 }, {})
    ).toEqual({ downloadSpeed: 0, uploadSpeed: 0, downloadData: 0, uploadData: 0 })
  })
})
