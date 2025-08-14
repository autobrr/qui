/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from '@/types'

/**
 * Get common tags from selected torrents (tags that ALL selected torrents have)
 */
export function getCommonTags(torrents: Torrent[]): string[] {
  if (torrents.length === 0) return []
  
  // Get tags from first torrent
  const firstTorrentTags = torrents[0].tags
    ? (Array.isArray(torrents[0].tags) 
        ? torrents[0].tags 
        : torrents[0].tags.split(',').map((t: string) => t.trim())
      ).filter((t: string) => t)
    : []
  
  // If only one torrent, return its tags
  if (torrents.length === 1) return firstTorrentTags
  
  // Find common tags across all torrents
  return firstTorrentTags.filter((tag: string) => 
    torrents.every(torrent => {
      const torrentTags = torrent.tags
        ? (Array.isArray(torrent.tags)
            ? torrent.tags
            : torrent.tags.split(',').map((t: string) => t.trim())
          )
        : []
      return torrentTags.includes(tag)
    })
  )
}

/**
 * Get common category from selected torrents (if all have the same category)
 */
export function getCommonCategory(torrents: Torrent[]): string {
  if (torrents.length === 0) return ''
  
  const firstCategory = torrents[0].category || ''
  
  // Check if all torrents have the same category
  const allSameCategory = torrents.every(t => (t.category || '') === firstCategory)
  
  return allSameCategory ? firstCategory : ''
}