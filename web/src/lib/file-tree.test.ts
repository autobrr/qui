/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentFile } from "@/types"
import { describe, expect, it } from "vitest"
import { buildFileTree, DEFAULT_FILE_SORT, discPathOf, sortFileTree, type FileSort } from "./file-tree"

function file(index: number, name: string, size: number, progress = 0, priority = 1): TorrentFile {
  return { index, name, size, progress, priority, is_seed: false, piece_range: [0, 0], availability: 1 }
}

const files = [
  file(0, "b.mkv", 300, 0.5, 1),
  file(1, "a.mkv", 100, 1, 7),
  file(2, "c10.mkv", 200, 0, 0),
  file(3, "c2.mkv", 50, 0, 6),
  file(4, "Sub/z.srt", 10, 1, 1),
  file(5, "Sub/y.srt", 20, 0, 1),
  file(6, "Big/x.bin", 1000, 0.25, 6),
]

function names(nodes: ReturnType<typeof buildFileTree>["nodes"]): string[] {
  return nodes.map(n => n.name)
}

function sorted(sort: FileSort, incognito = false) {
  return sortFileTree(buildFileTree(files, incognito, "hash").nodes, sort)
}

describe("buildFileTree", () => {
  it("collects folder ids", () => {
    expect(buildFileTree(files, false, "hash").folderIds).toEqual(new Set(["Big", "Sub"]))
  })

  it("aggregates folder size, progress and priority", () => {
    const { nodes } = buildFileTree(files, false, "hash")
    const sub = nodes.find(n => n.id === "Sub")!
    expect(sub.totalSize).toBe(30)
    expect(sub.totalProgress).toBe(10)
    expect(sub.priority).toBe(1)
    expect(sub.totalCount).toBe(2)
    expect(new Set(sub.children?.map(n => n.id))).toEqual(new Set(["Sub/y.srt", "Sub/z.srt"]))
  })

  it("substitutes display names in incognito mode", () => {
    const { nodes } = buildFileTree(files, true, "hash")
    expect(nodes.some(n => n.name === "a.mkv" || n.name === "Sub")).toBe(false)
  })
})

describe("sortFileTree", () => {
  it("default order is folders first, then natural name ascending", () => {
    const { nodes } = buildFileTree(files, false, "hash")
    const out = sortFileTree(nodes, DEFAULT_FILE_SORT)
    expect(names(out)).toEqual(["Big", "Sub", "a.mkv", "b.mkv", "c2.mkv", "c10.mkv"])
    expect(out.find(n => n.id === "Sub")?.children?.map(n => n.id)).toEqual(["Sub/y.srt", "Sub/z.srt"])
    expect(out).not.toBe(nodes)
  })

  it("name sort under incognito orders by the displayed names", () => {
    const fileNames = names(sorted({ column: "name", direction: "asc" }, true).filter(n => n.kind === "file"))
    expect(fileNames).toEqual([...fileNames].sort((a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" })))
  })

  it("keeps folders above files and reverses inside each group on desc", () => {
    expect(names(sorted({ column: "name", direction: "desc" }))).toEqual(["Sub", "Big", "c10.mkv", "c2.mkv", "b.mkv", "a.mkv"])
  })

  it("sorts by size using folder aggregates", () => {
    expect(names(sorted({ column: "size", direction: "asc" }))).toEqual(["Sub", "Big", "c2.mkv", "a.mkv", "c10.mkv", "b.mkv"])
    expect(names(sorted({ column: "size", direction: "desc" }))).toEqual(["Big", "Sub", "b.mkv", "c10.mkv", "a.mkv", "c2.mkv"])
  })

  it("sorts by progress ratio, ties broken by name asc", () => {
    // Sub 1/3, Big 1/4; files: a 1, b 0.5, c2 0, c10 0
    expect(names(sorted({ column: "progress", direction: "desc" }))).toEqual(["Sub", "Big", "a.mkv", "b.mkv", "c2.mkv", "c10.mkv"])
    expect(names(sorted({ column: "progress", direction: "asc" }))).toEqual(["Big", "Sub", "c2.mkv", "c10.mkv", "b.mkv", "a.mkv"])
  })

  it("sorts by numeric priority, mixed folders count as normal", () => {
    const mixed = [...files, file(7, "Big/w.bin", 1, 0, 0)]
    const out = sortFileTree(buildFileTree(mixed, false, "hash").nodes, { column: "priority", direction: "desc" })
    // Big is mixed (6 + 0) -> 1, Sub is 1: tie -> name asc
    expect(names(out)).toEqual(["Big", "Sub", "a.mkv", "c2.mkv", "b.mkv", "c10.mkv"])
  })

  it("sorts children recursively", () => {
    const out = sorted({ column: "size", direction: "desc" })
    const sub = out.find(n => n.id === "Sub")!
    expect(sub.children?.map(n => n.id)).toEqual(["Sub/y.srt", "Sub/z.srt"])
    const asc = sorted({ column: "size", direction: "asc" }).find(n => n.id === "Sub")!
    expect(asc.children?.map(n => n.id)).toEqual(["Sub/z.srt", "Sub/y.srt"])
  })
})

describe("discPathOf", () => {
  const disc = (files: string[]) => buildFileTree(files.map((name, index) => file(index, name, 1)), false, "hash").nodes

  it("marks the folder that holds BDMV as a Disc", () => {
    const [root] = disc(["Movie.2020.BluRay/BDMV/index.bdmv", "Movie.2020.BluRay/CERTIFICATE/id.bdmv"])
    expect(discPathOf(root)).toBe("Movie.2020.BluRay")
    expect(discPathOf(root.children![0])).toBeNull()
  })

  it("marks an .iso file as a Disc regardless of extension case", () => {
    const [root] = disc(["Movie.2020.BluRay/Movie.2020.ISO"])
    expect(discPathOf(root)).toBeNull()
    expect(discPathOf(root.children![0])).toBe("Movie.2020.BluRay/Movie.2020.ISO")
  })

  it("ignores folders and files that are not a Disc", () => {
    const [root] = disc(["Show.S01/Show.S01E01.mkv", "Show.S01/VIDEO_TS/VTS_01_0.IFO"])
    expect(discPathOf(root)).toBeNull()
    expect(discPathOf(root.children![0])).toBeNull()
    expect(discPathOf(root.children![1])).toBeNull()
  })

  it("gives a box set one Disc per BDMV folder", () => {
    const [root] = disc(["Set/Disc 1/BDMV/index.bdmv", "Set/Disc 2/BDMV/index.bdmv", "Set/Extras/BDMV/index.bdmv"])
    expect(discPathOf(root)).toBeNull()
    expect(root.children!.map(discPathOf)).toEqual(["Set/Disc 1", "Set/Disc 2", "Set/Extras"])
  })

  it("addresses a bare BDMV torrent as the torrent root", () => {
    const [bdmv] = disc(["BDMV/index.bdmv"])
    expect(discPathOf(bdmv)).toBe(".")
  })

  it("uses real ids, so incognito display names do not hide a Disc", () => {
    const [root] = buildFileTree([file(0, "Movie/BDMV/index.bdmv", 1)], true, "hash").nodes
    expect(discPathOf(root)).toBe("Movie")
  })
})
