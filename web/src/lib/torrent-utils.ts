/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

/**
 * Get common tags from selected torrents (tags that ALL selected torrents have)
 */
export function getCommonTags(torrents: Torrent[]): string[] {
  if (torrents.length === 0) return []

  // Fast path for single torrent
  if (torrents.length === 1) {
    const tags = torrents[0].tags
    return tags ? tags.split(",").map(t => t.trim()).filter(Boolean) : []
  }

  // Initialize with first torrent's tags
  const firstTorrent = torrents[0]
  if (!firstTorrent.tags) return []

  // Use a Set for O(1) lookups
  const firstTorrentTagsSet = new Set(
    firstTorrent.tags.split(",").map(t => t.trim()).filter(Boolean)
  )

  // If first torrent has no tags, no common tags exist
  if (firstTorrentTagsSet.size === 0) return []

  // Convert to array once for iteration
  const firstTorrentTags = Array.from(firstTorrentTagsSet)

  // Use Object as a counter map for better performance with large datasets
  const tagCounts: Record<string, number> = {}
  for (const tag of firstTorrentTags) {
    tagCounts[tag] = 1 // First torrent has this tag
  }

  // Count occurrences of each tag across all torrents
  for (let i = 1; i < torrents.length; i++) {
    const torrent = torrents[i]
    if (!torrent.tags) continue

    // Create a Set of this torrent's tags for O(1) lookups
    const currentTags = new Set(
      torrent.tags.split(",").map(t => t.trim()).filter(Boolean)
    )

    // Only increment count for tags that this torrent has
    for (const tag in tagCounts) {
      if (currentTags.has(tag)) {
        tagCounts[tag]++
      }
    }
  }

  // Return tags that appear in all torrents
  return Object.keys(tagCounts).filter(tag => tagCounts[tag] === torrents.length)
}

/**
 * Get common category from selected torrents (if all have the same category)
 */
export function getCommonCategory(torrents: Torrent[]): string {
  // Early returns for common cases
  if (torrents.length === 0) return ""
  if (torrents.length === 1) return torrents[0].category || ""

  const firstCategory = torrents[0].category || ""

  // Use direct loop instead of every() for early return optimization
  for (let i = 1; i < torrents.length; i++) {
    if ((torrents[i].category || "") !== firstCategory) {
      return "" // Different category found, no need to check the rest
    }
  }

  return firstCategory
}

/**
 * Get common save path from selected torrents (if all have the same path)
 */
export function getCommonSavePath(torrents: Torrent[]): string {
  // Early returns for common cases
  if (torrents.length === 0) return ""
  if (torrents.length === 1) return torrents[0].save_path || ""

  const firstPath = torrents[0].save_path || ""

  // Use direct loop instead of every() for early return optimization
  for (let i = 1; i < torrents.length; i++) {
    if ((torrents[i].save_path || "") !== firstPath) {
      return "" // Different path found, no need to check the rest
    }
  }

  return firstPath
}