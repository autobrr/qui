/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import {
  FILE_PRIORITY,
  FILE_PRIORITY_LABEL_KEYS,
  FILE_PRIORITY_OPTIONS,
  foldFolderPriority,
  normalizeFilePriority,
  type FolderPriority
} from "@/lib/file-priority"

describe("normalizeFilePriority", () => {
  it("maps 0 to doNotDownload", () => {
    expect(normalizeFilePriority(0)).toBe(FILE_PRIORITY.doNotDownload)
  })

  it("maps 1 to normal", () => {
    expect(normalizeFilePriority(1)).toBe(FILE_PRIORITY.normal)
  })

  it("collapses legacy values 2-5 to normal", () => {
    for (const value of [2, 3, 4, 5]) {
      expect(normalizeFilePriority(value)).toBe(FILE_PRIORITY.normal)
    }
  })

  it("maps 6 to high and 7 to maximum", () => {
    expect(normalizeFilePriority(6)).toBe(FILE_PRIORITY.high)
    expect(normalizeFilePriority(7)).toBe(FILE_PRIORITY.maximum)
  })

  it("clamps out-of-range values to the nearest bucket", () => {
    expect(normalizeFilePriority(-5)).toBe(FILE_PRIORITY.doNotDownload)
    expect(normalizeFilePriority(99)).toBe(FILE_PRIORITY.maximum)
  })
})

describe("foldFolderPriority", () => {
  it("returns the shared value when both sides match", () => {
    expect(foldFolderPriority(FILE_PRIORITY.high, FILE_PRIORITY.high)).toBe(FILE_PRIORITY.high)
  })

  it("returns 'mixed' when the values differ", () => {
    expect(foldFolderPriority(FILE_PRIORITY.normal, FILE_PRIORITY.maximum)).toBe("mixed")
  })

  it("propagates 'mixed' from either side", () => {
    expect(foldFolderPriority("mixed", FILE_PRIORITY.normal)).toBe("mixed")
    expect(foldFolderPriority(FILE_PRIORITY.normal, "mixed")).toBe("mixed")
  })

  it("reduces a list of file priorities correctly", () => {
    const fold = (values: FolderPriority[]) => values.reduce(foldFolderPriority)
    expect(fold([FILE_PRIORITY.high, FILE_PRIORITY.high, FILE_PRIORITY.high])).toBe(FILE_PRIORITY.high)
    expect(fold([FILE_PRIORITY.high, FILE_PRIORITY.normal])).toBe("mixed")
  })
})

describe("priority metadata", () => {
  it("exposes the four selectable options in display order", () => {
    expect(FILE_PRIORITY_OPTIONS).toEqual([0, 1, 6, 7])
  })

  it("has a label key for every option", () => {
    for (const option of FILE_PRIORITY_OPTIONS) {
      expect(FILE_PRIORITY_LABEL_KEYS[option]).toMatch(/^filePriority\./)
    }
  })
})
