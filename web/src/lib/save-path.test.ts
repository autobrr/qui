/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { resolveDestinationPath } from "./save-path"
import type { Category } from "@/types/torrents"

const categories: Record<string, Category> = {
  movies: { name: "movies", savePath: "/mnt/movies" },
  tv: { name: "tv", savePath: "" },
}

describe("resolveDestinationPath", () => {
  it("uses the entered save path when auto management is off", () => {
    expect(resolveDestinationPath({
      autoTMM: false,
      savePath: "/mnt/manual",
      category: "movies",
      categories,
      defaultSavePath: "/downloads",
    })).toBe("/mnt/manual")
  })

  it("falls back to the default save path when nothing was entered", () => {
    expect(resolveDestinationPath({
      autoTMM: false,
      savePath: "  ",
      category: "",
      categories,
      defaultSavePath: "/downloads",
    })).toBe("/downloads")
  })

  it("uses the category save path under auto management", () => {
    expect(resolveDestinationPath({
      autoTMM: true,
      savePath: "/mnt/ignored",
      category: "movies",
      categories,
      defaultSavePath: "/downloads",
    })).toBe("/mnt/movies")
  })

  it("uses the implicit category path when the category has none", () => {
    expect(resolveDestinationPath({
      autoTMM: true,
      savePath: "",
      category: "tv",
      categories,
      defaultSavePath: "/downloads/",
    })).toBe("/downloads/tv")
  })

  it("keeps the separator a Windows host uses", () => {
    expect(resolveDestinationPath({
      autoTMM: true,
      savePath: "",
      category: "tv",
      categories,
      defaultSavePath: "D:\\downloads",
    })).toBe("D:\\downloads\\tv")
  })

  it("treats the no-category sentinel as the default save path", () => {
    expect(resolveDestinationPath({
      autoTMM: true,
      savePath: "",
      category: "__none__",
      categories,
      defaultSavePath: "/downloads",
    })).toBe("/downloads")
  })

  it("returns nothing when the destination cannot be resolved", () => {
    expect(resolveDestinationPath({
      autoTMM: true,
      savePath: "",
      category: "tv",
      categories,
      defaultSavePath: undefined,
    })).toBe("")
  })
})
