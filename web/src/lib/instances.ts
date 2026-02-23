/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const ALL_INSTANCES_ID = 0
export const UNIFIED_INSTANCE_IDS_SEARCH_PARAM = "instanceIds"

export function isAllInstancesScope(instanceId: number): boolean {
  return instanceId === ALL_INSTANCES_ID
}

export function parseUnifiedInstanceIds(value: unknown): number[] {
  if (typeof value !== "string" || value.trim() === "") {
    return []
  }

  const seen = new Set<number>()
  const parsed: number[] = []

  value.split(",").forEach((entry) => {
    const parsedId = Number.parseInt(entry.trim(), 10)
    if (!Number.isFinite(parsedId) || parsedId <= 0 || seen.has(parsedId)) {
      return
    }
    seen.add(parsedId)
    parsed.push(parsedId)
  })

  return parsed
}

export function normalizeUnifiedInstanceIds(instanceIds: readonly number[], activeInstanceIds: readonly number[]): number[] {
  if (activeInstanceIds.length === 0) {
    return []
  }

  const activeUniqueSorted = Array.from(new Set(activeInstanceIds.filter(id => id > 0))).sort((left, right) => left - right)
  if (activeUniqueSorted.length === 0) {
    return []
  }

  const activeSet = new Set(activeUniqueSorted)
  const selectedFilteredSorted = Array.from(
    new Set(instanceIds.filter(id => activeSet.has(id)))
  ).sort((left, right) => left - right)

  if (selectedFilteredSorted.length === 0) {
    return []
  }

  if (
    selectedFilteredSorted.length === activeUniqueSorted.length &&
    selectedFilteredSorted.every((id, index) => id === activeUniqueSorted[index])
  ) {
    return []
  }

  return selectedFilteredSorted
}

export function resolveUnifiedInstanceIds(rawInstanceIds: unknown, activeInstanceIds: readonly number[]): number[] {
  const activeUniqueSorted = Array.from(new Set(activeInstanceIds.filter(id => id > 0))).sort((left, right) => left - right)
  if (activeUniqueSorted.length === 0) {
    return []
  }

  const normalizedSelection = normalizeUnifiedInstanceIds(parseUnifiedInstanceIds(rawInstanceIds), activeUniqueSorted)
  if (normalizedSelection.length === 0) {
    return activeUniqueSorted
  }

  return normalizedSelection
}

export function encodeUnifiedInstanceIds(instanceIds: readonly number[]): string | undefined {
  if (instanceIds.length === 0) {
    return undefined
  }
  return instanceIds.join(",")
}
