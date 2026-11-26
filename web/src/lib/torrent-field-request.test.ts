/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { buildTorrentFieldRequest, type TorrentFieldScope } from "./torrent-field-request"

const filters = {
  status: ["downloading"],
  excludeStatus: [],
  categories: ["tv"],
  excludeCategories: [],
  tags: [],
  excludeTags: [],
  trackers: [],
  excludeTrackers: [],
}

const singleInstanceScope: TorrentFieldScope = {
  isCrossInstance: false,
  sort: "name",
  order: "asc",
  search: "iso",
  filters,
  excludeHashes: ["ddd"],
  excludeTargets: [{ instanceId: 1, hash: "ddd" }],
}

const crossInstanceScope: TorrentFieldScope = {
  ...singleInstanceScope,
  isCrossInstance: true,
  instanceIds: [1, 2],
}

const selection = {
  hashes: ["aaa", "bbb"],
  targets: [
    { instanceId: 1, hash: "aaa" },
    { instanceId: 2, hash: "bbb" },
  ],
}

describe("buildTorrentFieldRequest", () => {
  it("resolves a single-instance selection by hashes, not filter scope", () => {
    expect(buildTorrentFieldRequest(singleInstanceScope, selection)).toEqual({
      hashes: ["aaa", "bbb"],
    })
  })

  it("resolves a cross-instance selection by instance/hash targets", () => {
    expect(buildTorrentFieldRequest(crossInstanceScope, selection)).toEqual({
      targets: selection.targets,
      instanceIds: [1, 2],
    })
  })

  it("resolves select-all by the active filter scope with exclusions", () => {
    expect(buildTorrentFieldRequest(crossInstanceScope)).toEqual({
      sort: "name",
      order: "asc",
      search: "iso",
      filters,
      excludeHashes: ["ddd"],
      excludeTargets: [{ instanceId: 1, hash: "ddd" }],
      instanceIds: [1, 2],
    })
  })

  it("drops cross-instance-only params from a single-instance filter scope", () => {
    expect(buildTorrentFieldRequest(singleInstanceScope)).toEqual({
      sort: "name",
      order: "asc",
      search: "iso",
      filters,
      excludeHashes: ["ddd"],
      excludeTargets: undefined,
      instanceIds: undefined,
    })
  })
})
