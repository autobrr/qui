/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  anyTorrentHasTag,
  getCommonCategory,
  getCommonSavePath,
  getTorrentDisplayHash,
  getTorrentHashesWithTag,
  getTotalSize,
  normalizeTorrentHash,
  parseTorrentTags,
  resolveTorrentHashes,
  torrentHasTag
} from "@/lib/torrent-utils"
import { makeTorrent } from "@/test/mockTorrent"
import { describe, expect, it } from "vitest"

describe("normalizeTorrentHash", () => {
  it.each([
    [undefined, ""],
    [null, ""],
    ["", ""],
    ["   ", ""],
    [" abc ", "abc"],
    ["abc", "abc"],
  ])("normalizes %p to %p", (input, expected) => {
    expect(normalizeTorrentHash(input)).toBe(expected)
  })
})

describe("resolveTorrentHashes", () => {
  it("prefers primary v1, then fallback v1, then derives from primary hash", () => {
    expect(resolveTorrentHashes({ infohash_v1: "v1-primary" }, { infohash_v1: "v1-fallback" })).toEqual({
      infohashV1: "v1-primary",
      infohashV2: "",
      canonicalHash: "v1-primary",
    })

    expect(resolveTorrentHashes({}, { infohash_v1: "v1-fallback" })).toEqual({
      infohashV1: "v1-fallback",
      infohashV2: "",
      canonicalHash: "v1-fallback",
    })

    expect(resolveTorrentHashes({ hash: "legacy-hash" })).toEqual({
      infohashV1: "legacy-hash",
      infohashV2: "",
      canonicalHash: "legacy-hash",
    })
  })

  it("prefers primary v2, then fallback v2", () => {
    expect(resolveTorrentHashes({ infohash_v2: "v2-primary" }, { infohash_v2: "v2-fallback" }).infohashV2).toBe("v2-primary")
    expect(resolveTorrentHashes({}, { infohash_v2: "v2-fallback" }).infohashV2).toBe("v2-fallback")
  })

  it("does NOT derive v1 from `hash` when hash equals the v2 digest (pure-v2 torrent)", () => {
    expect(
      resolveTorrentHashes({ hash: "v2-digest", infohash_v2: "v2-digest" })
    ).toEqual({
      infohashV1: "",
      infohashV2: "v2-digest",
      canonicalHash: "v2-digest",
    })
  })

  it("returns empty hashes when both sources are empty/missing", () => {
    expect(resolveTorrentHashes()).toEqual({ infohashV1: "", infohashV2: "", canonicalHash: "" })
    expect(resolveTorrentHashes(null, null)).toEqual({ infohashV1: "", infohashV2: "", canonicalHash: "" })
    expect(resolveTorrentHashes({ hash: "", infohash_v1: "", infohash_v2: "" })).toEqual({ infohashV1: "", infohashV2: "", canonicalHash: "" })
  })

  it("falls back through the canonical priority chain", () => {
    expect(resolveTorrentHashes({ hash: "legacy", infohash_v2: "v2" })).toEqual({
      infohashV1: "legacy",
      infohashV2: "v2",
      canonicalHash: "legacy",
    })
  })

  it("trims whitespace from all hash sources", () => {
    expect(
      resolveTorrentHashes({ infohash_v1: "  v1  ", infohash_v2: "  v2  " })
    ).toEqual({
      infohashV1: "v1",
      infohashV2: "v2",
      canonicalHash: "v1",
    })
  })
})

describe("getTorrentDisplayHash", () => {
  it("returns the canonical hash from resolveTorrentHashes", () => {
    expect(getTorrentDisplayHash({ infohash_v1: "v1" })).toBe("v1")
    expect(getTorrentDisplayHash({}, { hash: "legacy" })).toBe("legacy")
    expect(getTorrentDisplayHash()).toBe("")
  })
})

describe("parseTorrentTags", () => {
  it.each<[string | null | undefined, string[]]>([
    [undefined, []],
    [null, []],
    ["", []],
    ["foo", ["foo"]],
    ["foo,bar,baz", ["foo", "bar", "baz"]],
    ["  foo , bar  ", ["foo", "bar"]],
    ["foo,,bar", ["foo", "bar"]],
    [",,,", []],
  ])("parses %p", (input, expected) => {
    expect(parseTorrentTags(input)).toEqual(expected)
  })
})

describe("torrentHasTag", () => {
  it("returns true when the tag is present (after trimming)", () => {
    const t = makeTorrent({ tags: "linux,iso" })
    expect(torrentHasTag(t, "linux")).toBe(true)
    expect(torrentHasTag(t, "  linux  ")).toBe(true)
  })

  it("returns false for missing or empty tag queries", () => {
    expect(torrentHasTag(makeTorrent({ tags: "linux" }), "windows")).toBe(false)
    expect(torrentHasTag(makeTorrent({ tags: "linux" }), "")).toBe(false)
    expect(torrentHasTag(makeTorrent({ tags: "linux" }), "   ")).toBe(false)
    expect(torrentHasTag(makeTorrent({ tags: "" }), "linux")).toBe(false)
  })

  it("is case-sensitive (qBittorrent tags are case-sensitive)", () => {
    expect(torrentHasTag(makeTorrent({ tags: "Linux" }), "linux")).toBe(false)
  })
})

describe("anyTorrentHasTag", () => {
  it("returns false for empty list", () => {
    expect(anyTorrentHasTag([], "linux")).toBe(false)
  })

  it("returns true when at least one torrent has the tag", () => {
    const torrents = [makeTorrent({ tags: "a" }), makeTorrent({ tags: "linux,b" })]
    expect(anyTorrentHasTag(torrents, "linux")).toBe(true)
  })

  it("returns false when no torrent has the tag", () => {
    const torrents = [makeTorrent({ tags: "a" }), makeTorrent({ tags: "b" })]
    expect(anyTorrentHasTag(torrents, "linux")).toBe(false)
  })
})

describe("getTorrentHashesWithTag", () => {
  it("returns empty for empty list", () => {
    expect(getTorrentHashesWithTag([], "linux")).toEqual([])
  })

  it("returns hashes only for torrents that have the tag", () => {
    const torrents = [
      makeTorrent({ hash: "h1", tags: "linux" }),
      makeTorrent({ hash: "h2", tags: "windows" }),
      makeTorrent({ hash: "h3", tags: "linux,iso" }),
    ]
    expect(getTorrentHashesWithTag(torrents, "linux")).toEqual(["h1", "h3"])
  })
})

describe("getCommonCategory", () => {
  it("returns empty for empty list", () => {
    expect(getCommonCategory([])).toBe("")
  })

  it("returns the category for a single torrent", () => {
    expect(getCommonCategory([makeTorrent({ category: "movies" })])).toBe("movies")
    expect(getCommonCategory([makeTorrent({ category: "" })])).toBe("")
  })

  it("returns the shared category when all match", () => {
    expect(
      getCommonCategory([
        makeTorrent({ category: "movies" }),
        makeTorrent({ category: "movies" }),
        makeTorrent({ category: "movies" }),
      ])
    ).toBe("movies")
  })

  it("returns empty as soon as any torrent disagrees", () => {
    expect(
      getCommonCategory([
        makeTorrent({ category: "movies" }),
        makeTorrent({ category: "tv" }),
      ])
    ).toBe("")
  })
})

describe("getCommonSavePath", () => {
  it("returns empty for empty list", () => {
    expect(getCommonSavePath([])).toBe("")
  })

  it("returns the path for a single torrent", () => {
    expect(getCommonSavePath([makeTorrent({ save_path: "/downloads" })])).toBe("/downloads")
  })

  it("returns the shared path when all match", () => {
    expect(
      getCommonSavePath([
        makeTorrent({ save_path: "/a" }),
        makeTorrent({ save_path: "/a" }),
      ])
    ).toBe("/a")
  })

  it("returns empty when paths diverge", () => {
    expect(
      getCommonSavePath([
        makeTorrent({ save_path: "/a" }),
        makeTorrent({ save_path: "/b" }),
      ])
    ).toBe("")
  })
})

describe("getTotalSize", () => {
  it("returns 0 for empty list", () => {
    expect(getTotalSize([])).toBe(0)
  })

  it("sums sizes across torrents", () => {
    expect(
      getTotalSize([
        makeTorrent({ size: 100 }),
        makeTorrent({ size: 200 }),
        makeTorrent({ size: 50 }),
      ])
    ).toBe(350)
  })
})
