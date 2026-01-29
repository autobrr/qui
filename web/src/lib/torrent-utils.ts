/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

type HashSource = {
  hash?: string | null
  infohash_v1?: string | null
  infohash_v2?: string | null
}

export function normalizeTorrentHash(value?: string | null): string {
  if (!value) return ""
  const trimmed = value.trim()
  return trimmed.length > 0 ? trimmed : ""
}

function toHashSource(source?: HashSource | null): HashSource | undefined {
  if (!source) return undefined

  const { hash, infohash_v1, infohash_v2 } = source
  const hasValue = [hash, infohash_v1, infohash_v2].some(value => normalizeTorrentHash(value) !== "")
  if (!hasValue) return undefined

  return { hash, infohash_v1, infohash_v2 }
}

// qBittorrent maps the v1 info-hash into `hash` for legacy torrents; reuse it unless it
// matches the v2 digest (pure v2 torrent).
function deriveLegacyInfohashV1(source?: HashSource | null): string | undefined {
  if (!source) return undefined

  const candidate = normalizeTorrentHash(source.hash)
  if (!candidate) return undefined

  const v2 = normalizeTorrentHash(source.infohash_v2)
  if (v2 && v2 === candidate) return undefined

  return candidate
}

export function resolveTorrentHashes(primary?: HashSource | null, fallback?: HashSource | null) {
  const primarySource = toHashSource(primary)
  const fallbackSource = toHashSource(fallback)

  const infohashV1 = [
    primarySource?.infohash_v1,
    fallbackSource?.infohash_v1,
    deriveLegacyInfohashV1(primarySource),
    deriveLegacyInfohashV1(fallbackSource),
  ].map(normalizeTorrentHash).find(Boolean) || ""

  const infohashV2 = [
    primarySource?.infohash_v2,
    fallbackSource?.infohash_v2,
  ].map(normalizeTorrentHash).find(Boolean) || ""

  const canonicalHash =
    infohashV1 ||
    infohashV2 ||
    normalizeTorrentHash(primarySource?.hash) ||
    normalizeTorrentHash(fallbackSource?.hash)

  return {
    infohashV1,
    infohashV2,
    canonicalHash,
  }
}

export function getTorrentDisplayHash(primary?: HashSource | null, fallback?: HashSource | null): string {
  return resolveTorrentHashes(primary, fallback).canonicalHash
}

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

/**
 * Calculate the total size of selected torrents
 */
export function getTotalSize(torrents: Torrent[]): number {
  if (torrents.length === 0) return 0

  // Use reduce to sum up all torrent sizes
  return torrents.reduce((total, torrent) => total + (torrent.size || 0), 0)
}
