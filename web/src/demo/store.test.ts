/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { makeFilters } from "@/test/mockFilters"
import { createStore, matchesStatus, type ListParams } from "./store"
import { makeTorrent } from "@/test/mockTorrent"

const params = (overrides: Partial<ListParams> = {}): ListParams => ({
  page: 0, limit: 300, sort: "added_on", order: "desc", search: "", filters: null, ...overrides,
})

describe("matchesStatus", () => {
  it.each([
    ["downloading", "stalledDL", true],
    ["downloading", "uploading", false],
    ["seeding", "stalledUP", true],
    ["completed", "stoppedUP", true],
    ["stopped", "stoppedDL", true],
    ["paused", "pausedUP", true],
    ["running", "stoppedUP", false],
    ["running", "uploading", true],
    ["active", "stalledUP", false],
    ["inactive", "stalledUP", true],
    ["errored", "missingFiles", true],
    ["error", "error", true],
    ["checking", "checkingResumeData", true],
    ["stalled_uploading", "stalledUP", true],
    ["stalled_downloading", "stalledUP", false],
    ["moving", "moving", true],
  ])("%s matches state %s: %s", (status, state, expected) => {
    const t = makeTorrent({ state, progress: state.endsWith("UP") || state === "uploading" ? 1 : 0.5 })
    expect(matchesStatus(t, status)).toBe(expected)
  })

  it("tracker health statuses read the health field, not the state", () => {
    expect(matchesStatus(makeTorrent({ tracker_health: "unregistered" }), "unregistered")).toBe(true)
    expect(matchesStatus(makeTorrent({ state: "error" }), "unregistered")).toBe(false)
  })
})

describe("createStore", () => {
  const store = createStore({ seed: 7, counts: [400, 100] })

  it("builds two instances of the requested sizes", () => {
    expect(store.instances.map(i => [i.id, i.name, i.torrents.length])).toEqual([[1, "seedbox", 400], [2, "home", 100]])
    expect(store.instanceResponses().every(i => i.isActive && i.connected)).toBe(true)
  })

  it("is deterministic for a seed", () => {
    const again = createStore({ seed: 7, counts: [400, 100] })
    expect(again.instances[0].torrents.map(t => t.hash)).toEqual(store.instances[0].torrents.map(t => t.hash))
  })

  it("pages, sorts, and reports totals", () => {
    const first = store.query([1], params({ limit: 50, sort: "size", order: "asc" }))
    const second = store.query([1], params({ limit: 50, sort: "size", order: "asc", page: 1 }))
    expect(first.torrents).toHaveLength(50)
    expect(first.total).toBe(400)
    expect(first.hasMore).toBe(true)
    expect(first.torrents[49].size).toBeLessThanOrEqual(second.torrents[0].size)
    const last = store.query([1], params({ limit: 50, page: 7 }))
    expect(last.hasMore).toBe(false)
    expect(store.query([1], params({ limit: 50, page: 8 })).torrents).toHaveLength(0)
  })

  it("counts match the filtered results for every status", () => {
    const counts = store.query([1], params()).counts!
    for (const [status, count] of Object.entries(counts.status)) {
      const filtered = store.query([1], params({ filters: makeFilters({ status: [status] }) }))
      expect(filtered.total, status).toBe(count)
    }
  })

  it("filters categories including subcategories, tags including untagged, and trackers", () => {
    const all = store.instances[0].torrents
    const distributions = store.query([1], params({ filters: makeFilters({ categories: ["distributions"] }) }))
    expect(distributions.total).toBe(all.filter(t => t.category === "distributions" || t.category.startsWith("distributions/")).length)
    expect(distributions.total).toBeGreaterThan(all.filter(t => t.category === "distributions").length)

    const untagged = store.query([1], params({ filters: makeFilters({ tags: [""] }) }))
    expect(untagged.total).toBe(all.filter(t => t.tags === "").length)
    expect(untagged.total).toBeGreaterThan(0)

    const stable = store.query([1], params({ filters: makeFilters({ tags: ["stable"], excludeTags: ["lts"] }) }))
    expect(stable.torrents.every(t => t.tags.includes("stable") && !t.tags.includes("lts"))).toBe(true)

    const domain = "linuxtracker.example"
    const byTracker = store.query([1], params({ filters: makeFilters({ trackers: [domain] }) }))
    expect(byTracker.total).toBe(store.query([1], params()).counts!.trackers[domain])
    expect(byTracker.total).toBeGreaterThan(0)
  })

  it("searches by name and looks up by hash", () => {
    const hits = store.query([1], params({ search: "DEBIAN-13" }))
    expect(hits.total).toBeGreaterThan(0)
    expect(hits.torrents.every(t => t.name.toLowerCase().includes("debian-13"))).toBe(true)
    const hash = all()[3].hash
    const one = store.query([1], params({ limit: 1, filters: makeFilters({ hashes: [hash] }) }))
    expect(one.torrents.map(t => t.hash)).toEqual([hash])
  })

  it("serves the all-instances view with instance columns", () => {
    const cross = store.query([1, 2], params({ limit: 500 }), true)
    expect(cross.isCrossInstance).toBe(true)
    expect(cross.total).toBe(500)
    expect(cross.torrents).toHaveLength(0)
    expect(new Set(cross.crossInstanceTorrents!.map(t => t.instanceName))).toEqual(new Set(["seedbox", "home"]))
  })

  it("ticks downloads forward and finishes them", () => {
    const t = all().find(x => x.state === "downloading")!
    t.amount_left = 1
    t.downloaded = t.size - 1
    store.tick()
    expect(t.progress).toBe(1)
    expect(t.state).toBe("uploading")
    expect(t.completion_on).toBeGreaterThan(0)
    expect(store.instances[0].serverState.up_info_speed).toBeGreaterThan(0)
  })

  it.each([
    ["pause", {}, (t: ReturnType<typeof all>[number]) => t.state === "stoppedUP" || t.state === "stoppedDL"],
    ["resume", {}, (t: ReturnType<typeof all>[number]) => t.state === "uploading" || t.state === "downloading"],
    ["setCategory", { category: "films" }, (t: ReturnType<typeof all>[number]) => t.category === "films" && t.save_path === "/data/torrents/films"],
    ["addTags", { tags: "keep, new-tag" }, (t: ReturnType<typeof all>[number]) => t.tags.includes("keep") && t.tags.includes("new-tag")],
    ["setTags", { tags: "only" }, (t: ReturnType<typeof all>[number]) => t.tags === "only"],
    ["removeTags", { tags: "only" }, (t: ReturnType<typeof all>[number]) => t.tags === ""],
    ["setUploadLimit", { uploadLimit: 512 }, (t: ReturnType<typeof all>[number]) => t.up_limit === 512 * 1024],
    ["setLocation", { location: "/mnt/other" }, (t: ReturnType<typeof all>[number]) => t.save_path === "/mnt/other"],
  ] as const)("%s writes the row", (action, extra, check) => {
    const t = all()[10]
    store.bulkAction(1, { hashes: [t.hash], action, ...extra })
    expect(check(t)).toBe(true)
  })

  it("adds a tag to the instance tag list on addTags", () => {
    expect(store.instances[0].tags.has("new-tag")).toBe(true)
  })

  it("deletes by hash, by selectAll with filters, and by targets on another instance", () => {
    const before = all().length
    store.bulkAction(1, { hashes: [all()[0].hash], action: "delete" })
    expect(all()).toHaveLength(before - 1)

    const stopped = store.query([1], params({ filters: makeFilters({ status: ["stopped"] }) })).total
    expect(stopped).toBeGreaterThan(0)
    store.bulkAction(1, { action: "delete", selectAll: true, filters: makeFilters({ status: ["stopped"] }) })
    expect(store.query([1], params({ filters: makeFilters({ status: ["stopped"] }) })).total).toBe(0)

    const homeHash = store.instances[1].torrents[0].hash
    store.bulkAction(0, { action: "delete", targets: [{ instanceId: 2, hash: homeHash }] })
    expect(store.find(2, homeHash)).toBeUndefined()
  })

  it("adds a torrent that starts downloading and renames it", () => {
    store.addTorrent(1, "new-download", "films", ["keep"], false, true, "")
    const added = all()[0]
    expect(added.name).toBe("new-download")
    expect(added.state).toBe("downloading")
    expect(added.progress).toBe(0)
    store.rename(1, added.hash, "renamed")
    expect(added.name).toBe("renamed")
    expect(added.content_path).toBe("/data/torrents/films/renamed")
  })

  it("gives batched adds distinct hashes", () => {
    store.addTorrent(1, "one", "", [], false, true, "")
    store.addTorrent(1, "two", "", [], false, true, "")
    expect(all()[0].hash).not.toBe(all()[1].hash)
  })

  it("keeps the auto TMM choice and a custom save path", () => {
    store.addTorrent(1, "manual", "films", [], true, false, "/mnt/other")
    expect(all()[0]).toMatchObject({ auto_tmm: false, save_path: "/mnt/other", content_path: "/mnt/other/manual" })
    store.addTorrent(1, "blank", "films", [], true, false, "")
    expect(all()[0]).toMatchObject({ auto_tmm: false, save_path: "/data/torrents/films" })
  })

  it("honours the enable flag on forceStart and toggleSequentialDownload", () => {
    const t = all()[8]
    store.bulkAction(1, { hashes: [t.hash], action: "forceStart", enable: true })
    expect(t.force_start).toBe(true)
    expect(t.state).toMatch(/^forced/)
    store.bulkAction(1, { hashes: [t.hash], action: "forceStart", enable: false })
    expect(t.force_start).toBe(false)
    expect(t.state).toMatch(/^(uploading|downloading)$/)
    store.bulkAction(1, { hashes: [t.hash], action: "toggleSequentialDownload", enable: true })
    store.bulkAction(1, { hashes: [t.hash], action: "toggleSequentialDownload", enable: true })
    expect(t.seq_dl).toBe(true)
  })

  it("edits the tracker URL of rows that carry the old one", () => {
    const t = all()[9]
    const other = all().find(x => x.tracker !== t.tracker)!
    store.bulkAction(1, { hashes: [t.hash, other.hash], action: "editTrackers", trackerOldURL: t.tracker, trackerNewURL: "https://new.example/announce" })
    expect(t.tracker).toBe("https://new.example/announce")
    expect(other.tracker).not.toBe("https://new.example/announce")
    expect(store.details(1, t.hash)!.trackers.at(-1)!.url).toBe("https://new.example/announce")
  })

  it("overlays file priorities and banned peers on derived details", () => {
    const t = all().find(x => x.num_seeds + x.num_leechs > 0)!
    store.setFilePriority(1, t.hash, [0], 0)
    const before = store.details(1, t.hash)!
    const key = before.peers.sorted_peers![0].key
    store.banPeers(1, [key])
    const d = store.details(1, t.hash)!
    expect(d.files[0].priority).toBe(0)
    expect(d.peers.sorted_peers!.some(p => p.key === key)).toBe(false)
    expect(d.peers.peers![key]).toBeUndefined()
  })

  it("moves rows through the queue and renumbers it from 1", () => {
    const queued = () => all().filter(t => t.priority > 0).sort((a, b) => a.priority - b.priority)
    const [first, second, third] = queued()
    store.bulkAction(1, { hashes: [third.hash], action: "topPriority" })
    expect(queued().slice(0, 3)).toEqual([third, first, second])
    store.bulkAction(1, { hashes: [third.hash], action: "decreasePriority" })
    expect(queued().slice(0, 3)).toEqual([first, third, second])
    store.bulkAction(1, { hashes: [third.hash], action: "increasePriority" })
    expect(queued()[0]).toBe(third)
    store.bulkAction(1, { hashes: [third.hash], action: "bottomPriority" })
    expect(queued().at(-1)).toBe(third)
    expect(queued().map(t => t.priority)).toEqual(queued().map((_, i) => i + 1))
  })

  it("toggles the alternative speed limits flag", () => {
    expect(store.toggleAltSpeedLimits(1)).toBe(true)
    expect(store.instances[0].serverState.use_alt_speed_limits).toBe(true)
    expect(store.toggleAltSpeedLimits(1)).toBe(false)
  })

  it("overlays comment and path renames on derived details", () => {
    const t = all()[6]
    store.bulkAction(1, { hashes: [t.hash], action: "setComment", comment: "hello" })
    const first = store.details(1, t.hash)!.files[0].name
    store.renamePath(1, t.hash, first, "moved/first.bin")
    const d = store.details(1, t.hash)!
    expect(d.properties.comment).toBe("hello")
    expect(d.files[0].name).toBe("moved/first.bin")
    store.renamePath(1, t.hash, "moved", "elsewhere")
    expect(store.details(1, t.hash)!.files[0].name).toBe("elsewhere/first.bin")
  })

  it("derives stable details from the hash", () => {
    const t = all()[5]
    const a = store.details(1, t.hash)!
    const b = store.details(1, t.hash)!
    expect(a.files.map(f => f.name)).toEqual(b.files.map(f => f.name))
    expect(a.files.reduce((sum, f) => sum + f.size, 0)).toBe(t.size)
    expect(a.properties.pieces_num).toBe(a.pieces.length)
    expect(a.trackers.at(-1)!.url).toBe(t.tracker)
    expect(store.details(1, "nope")).toBeUndefined()
  })

  function all() {
    return store.instances[0].torrents
  }
})
