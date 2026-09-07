/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { usableFreeSpaceSourceType } from "./free-space-source"

describe("usableFreeSpaceSourceType", () => {
  it("keeps a saved qBittorrent path while the capability is unknown", () => {
    expect(usableFreeSpaceSourceType("qbitPath", undefined)).toBe("qbitPath")
  })

  it("keeps a saved qBittorrent path on an instance that supports it", () => {
    expect(usableFreeSpaceSourceType("qbitPath", true)).toBe("qbitPath")
  })

  it("falls back on an instance that cannot report free space at a path", () => {
    expect(usableFreeSpaceSourceType("qbitPath", false)).toBe("qbittorrent")
  })

  it("leaves the other sources alone", () => {
    expect(usableFreeSpaceSourceType("path", false)).toBe("path")
    expect(usableFreeSpaceSourceType("qbittorrent", false)).toBe("qbittorrent")
  })
})
