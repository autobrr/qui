/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { getRestoreSummaryCounts, mergeExcludedRows } from "@/lib/backup-restore"
import type { RestoreResult } from "@/types"

function makeResult(dryRun: boolean): RestoreResult {
  return {
    mode: "complete",
    runId: 7,
    instanceId: 1,
    dryRun,
    plan: {
      mode: "complete",
      runId: 7,
      instanceId: 1,
      categories: {
        create: [{ name: "movies", savePath: "/media/movies" }],
        update: [{ name: "tv", currentPath: "/old/tv", desiredPath: "/media/tv" }],
        delete: ["unused"],
      },
      tags: {
        create: [{ name: "4k" }, { name: "hd" }],
        delete: ["legacy"],
      },
      torrents: {
        add: [
          { manifest: { hash: "aaa", name: "Alpha", sizeBytes: 100 } },
          { manifest: { hash: "bbb", name: "Beta", sizeBytes: 200 } },
        ],
        update: [
          {
            hash: "ccc",
            current: { hash: "ccc", name: "Gamma", category: "old", tags: [] },
            desired: { hash: "ccc", name: "Gamma", category: "movies", sizeBytes: 300 },
            changes: [{ field: "category", supported: true, current: "old", desired: "movies" }],
          },
        ],
        delete: ["ddd", "eee", "fff"],
      },
    },
    applied: dryRun
      ? {
        categories: {},
        tags: {},
        torrents: {},
      }
      : {
        categories: { created: ["movies"] },
        tags: { created: ["4k"] },
        torrents: { added: ["aaa"], updated: ["ccc"], deleted: ["ddd"] },
      },
  }
}

describe("getRestoreSummaryCounts", () => {
  it("uses planned counts for dry-run results when applied is empty", () => {
    expect(getRestoreSummaryCounts(makeResult(true))).toEqual({
      categories: { created: 1, updated: 1, deleted: 1 },
      tags: { created: 2, deleted: 1 },
      torrents: { added: 2, updated: 1, deleted: 3 },
    })
  })

  it("uses applied counts for live restore results", () => {
    expect(getRestoreSummaryCounts(makeResult(false))).toEqual({
      categories: { created: 1, updated: 0, deleted: 0 },
      tags: { created: 1, deleted: 0 },
      torrents: { added: 1, updated: 1, deleted: 1 },
    })
  })
})

describe("mergeExcludedRows", () => {
  it("preserves excluded rows from the previous plan while keeping fresh rows from the dry-run plan", () => {
    const previousPlan = makeResult(true).plan
    const freshPlan = {
      ...previousPlan,
      torrents: {
        add: [{ manifest: { hash: "bbb", name: "Beta", sizeBytes: 200 } }],
        update: [],
        delete: ["eee", "fff", "zzz"],
      },
    }

    const merged = mergeExcludedRows(freshPlan, previousPlan, ["aaa", "ccc", "ddd"])
    expect(merged.torrents.add?.map(item => item.manifest.hash)).toEqual(["aaa", "bbb"])
    expect(merged.torrents.update?.map(item => item.hash)).toEqual(["ccc"])
    expect(merged.torrents.delete).toEqual(["ddd", "eee", "fff", "zzz"])
  })

  it("returns the fresh plan unchanged when there are no exclusions", () => {
    const freshPlan = makeResult(true).plan
    expect(mergeExcludedRows(freshPlan, freshPlan, [])).toBe(freshPlan)
  })
})
