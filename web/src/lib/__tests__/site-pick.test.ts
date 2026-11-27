/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { sitePickChange, sitePickForSave, sitePickFromIds, sitePickOptions, sitePickSelected } from "@/lib/site-pick"
import type { CrossSeedGazelleSite, TorznabIndexer } from "@/types"

function indexer(id: number, name: string, enabled = true): TorznabIndexer {
  return {
    id,
    name,
    base_url: "http://indexer.example.invalid",
    indexer_id: String(id),
    backend: "native",
    enabled,
    priority: 0,
    timeout_seconds: 30,
    capabilities: [],
    categories: [],
    last_test_status: "",
    created_at: "",
    updated_at: "",
  }
}

const indexers = [indexer(7, "Music Indexer"), indexer(8, "Redacted"), indexer(9, "Off Indexer", false)]

const sites: CrossSeedGazelleSite[] = [
  { site: "redacted.sh", name: "Redacted (API)", shortName: "RED", indexerId: -1001, active: true, replacedIndexerIds: [8] },
  { site: "orpheus.network", name: "Orpheus (API)", shortName: "OPS", indexerId: -1002, active: true, replacedIndexerIds: [] },
]

const labels = (options: ReturnType<typeof sitePickOptions>) => options.map(option => option.label)

describe("sitePickOptions", () => {
  it("lists the RED/OPS sites where the Trigger asks them, in place of the Torznab indexers they replace", () => {
    for (const trigger of ["manual", "completion", "library"] as const) {
      expect(labels(sitePickOptions(trigger, indexers, sites))).toEqual(["Music Indexer", "Redacted (API)", "Orpheus (API)"])
    }
  })

  it("lists only Torznab indexers for RSS, the replaced ones too", () => {
    expect(labels(sitePickOptions("rss", indexers, sites))).toEqual(["Music Indexer", "Redacted"])
  })

  it("lists only the RED/OPS sites when Torznab is off", () => {
    expect(labels(sitePickOptions("library", indexers, sites, { torznabOn: false }))).toEqual(["Redacted (API)", "Orpheus (API)"])
  })
})

describe("sitePickChange", () => {
  it("turns a picked RED/OPS site into its host and a Torznab indexer into its id", () => {
    const options = sitePickOptions("completion", indexers, sites)
    const pick = sitePickChange({ indexerIds: [], gazelleSites: [] }, ["7", "-1001"], options, sites)
    expect(pick).toEqual({ indexerIds: [7], gazelleSites: ["redacted.sh"] })
    expect(sitePickSelected(pick, options, sites)).toEqual(["7", "-1001"])
  })

  it("keeps the Torznab picks that the picker hides while Torznab is off", () => {
    const options = sitePickOptions("library", indexers, sites, { torznabOn: false })
    const pick = sitePickChange({ indexerIds: [7], gazelleSites: ["redacted.sh"] }, ["-1002"], options, sites)
    expect(pick).toEqual({ indexerIds: [7], gazelleSites: ["orpheus.network"] })
  })

  it("drops a hidden pick of a Torznab indexer that a RED/OPS site replaces", () => {
    const options = sitePickOptions("completion", indexers, sites)
    const pick = sitePickChange({ indexerIds: [8], gazelleSites: [] }, ["-1002"], options, sites)
    expect(pick).toEqual({ indexerIds: [], gazelleSites: ["orpheus.network"] })
  })
})

describe("sitePickForSave", () => {
  it("keeps the stored RED/OPS sites while the list of sites has not loaded", () => {
    const options = sitePickOptions("library", indexers, [])
    expect(sitePickForSave({ indexerIds: [7], gazelleSites: ["redacted.sh", "orpheus.network"] }, options, [])).toEqual({
      indexerIds: [7],
      gazelleSites: ["redacted.sh", "orpheus.network"],
    })
  })
})

describe("sitePickFromIds", () => {
  it("reads the RED/OPS sites of the picked ids from the gazelle plan of a search", () => {
    const plan = [{ site: "orpheus.network", name: "Orpheus (API)", indexerId: -1002 }]
    expect(sitePickFromIds([7, -1002, -1001], plan)).toEqual({ indexerIds: [7], gazelleSites: ["orpheus.network"] })
  })
})
