/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { RestorePlan, RestoreResult } from "@/types"

export interface RestoreSummaryCounts {
  categories: {
    created: number
    updated: number
    deleted: number
  }
  tags: {
    created: number
    deleted: number
  }
  torrents: {
    added: number
    updated: number
    deleted: number
  }
}

function countItems<T>(items?: T[] | null): number {
  return items?.length ?? 0
}

export function getRestoreSummaryCounts(result: RestoreResult): RestoreSummaryCounts {
  if (result.dryRun) {
    return {
      categories: {
        created: countItems(result.plan?.categories?.create),
        updated: countItems(result.plan?.categories?.update),
        deleted: countItems(result.plan?.categories?.delete),
      },
      tags: {
        created: countItems(result.plan?.tags?.create),
        deleted: countItems(result.plan?.tags?.delete),
      },
      torrents: {
        added: countItems(result.plan?.torrents?.add),
        updated: countItems(result.plan?.torrents?.update),
        deleted: countItems(result.plan?.torrents?.delete),
      },
    }
  }

  return {
    categories: {
      created: countItems(result.applied?.categories?.created),
      updated: countItems(result.applied?.categories?.updated),
      deleted: countItems(result.applied?.categories?.deleted),
    },
    tags: {
      created: countItems(result.applied?.tags?.created),
      deleted: countItems(result.applied?.tags?.deleted),
    },
    torrents: {
      added: countItems(result.applied?.torrents?.added),
      updated: countItems(result.applied?.torrents?.updated),
      deleted: countItems(result.applied?.torrents?.deleted),
    },
  }
}

export function mergeExcludedRows(
  freshPlan: RestorePlan,
  previousPlan: RestorePlan | null,
  excludedHashes: string[]
): RestorePlan {
  if (!previousPlan || excludedHashes.length === 0) return freshPlan

  const excluded = new Set(excludedHashes)
  const mergeList = <T>(
    fresh: T[] | undefined,
    prev: T[] | undefined,
    getHash: (item: T) => string
  ): T[] | undefined => {
    const next = [...(fresh ?? [])]
    const seen = new Set(next.map(getHash))
    for (const item of prev ?? []) {
      const hash = getHash(item)
      if (excluded.has(hash) && !seen.has(hash)) {
        next.push(item)
        seen.add(hash)
      }
    }
    if (next.length === 0) return fresh
    next.sort((a, b) => {
      const ha = getHash(a)
      const hb = getHash(b)
      return ha < hb ? -1 : ha > hb ? 1 : 0
    })
    return next
  }

  return {
    ...freshPlan,
    torrents: {
      add: mergeList(freshPlan.torrents.add, previousPlan.torrents.add, item => item.manifest.hash),
      update: mergeList(freshPlan.torrents.update, previousPlan.torrents.update, item => item.hash),
      delete: mergeList(freshPlan.torrents.delete, previousPlan.torrents.delete, hash => hash),
    },
  }
}

export function humanizeChangeField(field: string): string {
  const mappings: Record<string, string> = {
    sizeBytes: "Size",
    infohash_v1: "Infohash v1",
    infohash_v2: "Infohash v2",
  }
  if (mappings[field]) return mappings[field]
  return field
    .replace(/_/g, " ")
    .replace(/([A-Z])/g, " $1")
    .trim()
    .replace(/^./, char => char.toUpperCase())
}

export function formatChangeValue(value: unknown): string {
  if (value === null || value === undefined) return "—"
  if (Array.isArray(value)) {
    return value.length > 0 ? value.map(item => formatChangeValue(item)).join(", ") : "—"
  }
  if (typeof value === "string") {
    const trimmed = value.trim()
    return trimmed === "" ? "—" : trimmed
  }
  return String(value)
}
