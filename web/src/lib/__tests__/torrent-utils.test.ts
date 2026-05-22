/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  anyTorrentHasTag,
  getCommonCategory,
  getCommonSavePath,
  getCommonTags,
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

// Intent: trimmed string input, "" for any null/undefined/blank. Catches
// callers that previously passed unsanitized values into expression builders
// and tracked-identity comparisons.
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

// Intent: cross-seed and hybrid v1/v2 torrents must resolve consistently
// across primary/fallback sources. qBittorrent stores the v1 hash in `hash`
// for legacy torrents, but a pure-v2 torrent puts the v2 digest there —
// derivation must NOT treat that as the v1 hash. The canonical-hash priority
// (v1 → v2 → primary.hash → fallback.hash) defines what we use as a stable
// identity for the torrent across instances.
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

    // Legacy: hash holds the v1 digest, no explicit infohash_v1 field.
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
    // On a pure-v2 torrent, qBittorrent puts the v2 hash in both `hash` and
    // `infohash_v2`. We must not return the v2 hash as v1 — that would lie
    // about the torrent's protocol.
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
    // v2 wins over a legacy `hash` when no v1 is available
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

// Intent: thin wrapper used by display code when it only needs one
// identifier. Re-runs resolveTorrentHashes so behavior stays in sync.
describe("getTorrentDisplayHash", () => {
  it("returns the canonical hash from resolveTorrentHashes", () => {
    expect(getTorrentDisplayHash({ infohash_v1: "v1" })).toBe("v1")
    expect(getTorrentDisplayHash({}, { hash: "legacy" })).toBe("legacy")
    expect(getTorrentDisplayHash()).toBe("")
  })
})

// Intent: turn the comma-separated tags string into a clean array. Used
// everywhere we need to reason about individual tags (intersection, has-tag
// checks). Catches anyone who removes the trim or empty-string filter.
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

// Intent: per-torrent tag membership. The bulk-tag dialog drives off this
// to pre-select tags that exist on the focused torrent. Catches anyone who
// removes the trim or accepts empty-string tag queries.
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

// Intent: "does any of these torrents have this tag" — drives the cross-seed
// warning, blocklist, and other "any" affordances. Empty list short-circuits
// to false so callers don't need to guard.
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

// Intent: collect the hashes that have a given tag — used to scope bulk
// operations (e.g. block-cross-seed) to the subset that actually carries
// the cross-seed marker.
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

  it("returns empty when nothing matches", () => {
    const torrents = [makeTorrent({ hash: "h1", tags: "windows" })]
    expect(getTorrentHashesWithTag(torrents, "linux")).toEqual([])
  })
})

// Intent: intersection of tags across multiple torrents — drives the bulk
// tag-editor's "on" state (a tag is "on" only if every selected torrent
// already has it). Catches anyone who breaks the intersection into a union.
describe("getCommonTags", () => {
  it("returns empty for empty list", () => {
    expect(getCommonTags([])).toEqual([])
  })

  it("returns the parsed tags for a single torrent", () => {
    expect(getCommonTags([makeTorrent({ tags: "  a , b  " })])).toEqual(["a", "b"])
  })

  it("returns empty when the first torrent has no tags", () => {
    expect(getCommonTags([makeTorrent({ tags: "" }), makeTorrent({ tags: "a,b" })])).toEqual([])
  })

  it("returns only tags present on every torrent", () => {
    const torrents = [
      makeTorrent({ tags: "a,b,c" }),
      makeTorrent({ tags: "b,c,d" }),
      makeTorrent({ tags: "c,b" }),
    ]
    // Order follows the first torrent's tag order.
    expect(getCommonTags(torrents)).toEqual(["b", "c"])
  })

  it("returns empty when no tag is shared by all torrents", () => {
    const torrents = [
      makeTorrent({ tags: "a,b" }),
      makeTorrent({ tags: "c,d" }),
    ]
    expect(getCommonTags(torrents)).toEqual([])
  })

  it("skips torrents with no tags when counting (treats them as 'has none')", () => {
    const torrents = [
      makeTorrent({ tags: "a,b" }),
      makeTorrent({ tags: "" }),
      makeTorrent({ tags: "a" }),
    ]
    // The empty-tags torrent never increments any counter, so no tag reaches
    // count === torrents.length. Result: empty common set.
    expect(getCommonTags(torrents)).toEqual([])
  })
})

// Intent: common-category check for the bulk "Set category" dialog. Returns
// "" the moment we find any disagreement so the UI shows a neutral input
// instead of pre-filling with one torrent's category.
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

// Intent: same pattern as getCommonCategory but for save_path. Drives the
// pre-fill of the bulk "Set location" dialog.
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

// Intent: aggregate selection size for the bulk-action UI ("Delete 12
// torrents (4.5 GiB)"). Treats missing/zero sizes as zero so it can't
// throw on partial data.
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

  it("treats missing size as 0", () => {
    expect(
      getTotalSize([
        makeTorrent({ size: 100 }),
        makeTorrent({ size: 0 }),
      ])
    ).toBe(100)
  })
})
