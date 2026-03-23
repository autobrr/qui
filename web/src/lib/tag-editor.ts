/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseTorrentTags } from "@/lib/torrent-utils"

export type TagSelectionState = "on" | "off" | "mixed"

export interface TagEditorItem {
  tag: string
  initialState: TagSelectionState
  state: TagSelectionState
}

export interface TagUpdatePlan {
  add: string[]
  remove: string[]
}

const tagSortCollator = new Intl.Collator(undefined, { numeric: true, usage: "sort" })

function normalizeTag(tag: string): string {
  return tag.trim()
}

function buildTagSourceSet(availableTags: string[] | null | undefined, tagValues: string[]): Set<string> {
  const tags = new Set<string>()

  for (const tag of availableTags ?? []) {
    const normalized = normalizeTag(tag)
    if (normalized) {
      tags.add(normalized)
    }
  }

  for (const tagValue of tagValues) {
    for (const tag of parseTorrentTags(tagValue)) {
      tags.add(tag)
    }
  }

  return tags
}

export function sortTags(tags: Iterable<string>): string[] {
  return Array.from(tags).sort(tagSortCollator.compare)
}

export function buildTagEditorItems(
  availableTags: string[] | null | undefined,
  tagValues: string[],
  selectedCount: number
): TagEditorItem[] {
  const tagCounts = new Map<string, number>()

  for (const tagValue of tagValues) {
    const seen = new Set(parseTorrentTags(tagValue))
    for (const tag of seen) {
      tagCounts.set(tag, (tagCounts.get(tag) ?? 0) + 1)
    }
  }

  return sortTags(buildTagSourceSet(availableTags, tagValues)).map((tag) => {
    const count = tagCounts.get(tag) ?? 0

    let initialState: TagSelectionState = "off"
    if ((selectedCount > 0) && (count === selectedCount)) {
      initialState = "on"
    } else if (count > 0) {
      initialState = "mixed"
    }

    return {
      tag,
      initialState,
      state: initialState,
    }
  })
}

export function cycleTagSelectionState(state: TagSelectionState): TagSelectionState {
  switch (state) {
    case "mixed":
      return "on"
    case "on":
      return "off"
    case "off":
    default:
      return "on"
  }
}

export function buildTagUpdatePlan(items: TagEditorItem[]): TagUpdatePlan {
  const add: string[] = []
  const remove: string[] = []

  for (const item of items) {
    if ((item.state === "on") && (item.initialState !== "on")) {
      add.push(item.tag)
      continue
    }

    if ((item.state === "off") && (item.initialState !== "off")) {
      remove.push(item.tag)
    }
  }

  return { add, remove }
}

export function hasTagUpdatePlan(plan: TagUpdatePlan): boolean {
  return (plan.add.length > 0) || (plan.remove.length > 0)
}
