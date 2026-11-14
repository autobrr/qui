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

  // qBittorrent: a subcategory with no path saves under its parent's folder, and a relative path counts from the default folder.
  it.each([
    ["tv/anime", { tv: { name: "tv", savePath: "/media/tv" }, "tv/anime": { name: "tv/anime", savePath: "" } }, "/data", "/media/tv/anime"],
    ["tv/anime/new", { tv: { name: "tv", savePath: "" } }, "/data", "/data/tv/anime/new"],
    ["books", { books: { name: "books", savePath: "library/books" } }, "/data", "/data/library/books"],
    ["tv/anime", { tv: { name: "tv", savePath: "E:\\media\\tv" } }, "D:\\data", "E:\\media\\tv\\anime"],
    ["tv", { tv: { name: "tv", savePath: "\\\\nas\\tv" } }, "D:\\data", "\\\\nas\\tv"],
  ])("resolves %s as qBittorrent does", (category, cats, defaultSavePath, expected) => {
    expect(resolveDestinationPath({
      autoTMM: true,
      savePath: "",
      category,
      categories: cats as Record<string, Category>,
      defaultSavePath,
    })).toBe(expected)
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
