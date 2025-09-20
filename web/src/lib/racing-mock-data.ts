/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { RacingDashboard, RacingTorrent, TrackerStats } from "@/types"

const trackers = ["TL", "IPT", "RED", "GGN", "MTV", "BTN", "PTP", "MAM"]
const categories = ["movies", "tv-shows", "music", "games", "ebooks", "software"]

function generateMockTorrent(index: number, type: "fast" | "high-ratio" | "low-ratio"): RacingTorrent {
  const tracker = trackers[index % trackers.length]
  const category = categories[index % categories.length]

  let ratio: number
  let completionTime: number

  switch (type) {
    case "fast":
      ratio = 2 + Math.random() * 8
      completionTime = 30 + Math.random() * 270 // 30s to 5 minutes
      break
    case "high-ratio":
      ratio = 10 + Math.random() * 90
      completionTime = 300 + Math.random() * 3300 // 5 minutes to 1 hour
      break
    case "low-ratio":
      ratio = Math.random() * 0.8
      completionTime = 3600 + Math.random() * 7200 // 1-3 hours
      break
  }

  const now = new Date()
  const addedTime = new Date(now.getTime() - Math.random() * 7 * 24 * 60 * 60 * 1000) // Last 7 days
  const completedTime = new Date(addedTime.getTime() + completionTime * 1000)

  return {
    hash: `mock_hash_${index}`,
    name: `Sample.Torrent.${category}.${index + 1}.${tracker}.1080p.BluRay.x264-GROUP`,
    size: (1 + Math.random() * 49) * 1073741824, // 1-50 GB
    tracker: tracker,
    trackerDomain: `${tracker.toLowerCase()}.example.com`,
    ratio: Number(ratio.toFixed(2)),
    completionTime: Math.floor(completionTime),
    addedOn: addedTime.toISOString(),
    completedOn: completedTime.toISOString(),
    state: "seeding",
    category: category,
    tags: "racing, mock",
    instanceId: 1,
    instanceName: "Demo Instance",
  }
}

function generateMockTrackerStats(): TrackerStats {
  const byTracker: Record<string, any> = {}

  trackers.forEach((tracker) => {
    byTracker[`${tracker}_1`] = {
      totalTorrents: 100 + Math.floor(Math.random() * 900),
      completedTorrents: 50 + Math.floor(Math.random() * 450),
      averageRatio: Number((1 + Math.random() * 9).toFixed(2)),
      averageCompletionTime: 300 + Math.floor(Math.random() * 3300),
      instanceId: 1,
      instanceName: "Demo Instance",
    }
  })

  return {
    totalTorrents: 2847,
    completedTorrents: 2156,
    averageRatio: 4.73,
    averageCompletionTime: 1823,
    byTracker,
  }
}

export function generateMockRacingDashboard(): RacingDashboard {
  const topFastest: RacingTorrent[] = []
  const topRatios: RacingTorrent[] = []
  const bottomRatios: RacingTorrent[] = []

  // Generate 10 of each type
  for (let i = 0; i < 10; i++) {
    topFastest.push(generateMockTorrent(i, "fast"))
    topRatios.push(generateMockTorrent(i + 10, "high-ratio"))
    bottomRatios.push(generateMockTorrent(i + 20, "low-ratio"))
  }

  // Sort appropriately
  topFastest.sort((a, b) => (a.completionTime || 0) - (b.completionTime || 0))
  topRatios.sort((a, b) => b.ratio - a.ratio)
  bottomRatios.sort((a, b) => a.ratio - b.ratio)

  return {
    topFastest,
    topRatios,
    bottomRatios,
    trackerStats: generateMockTrackerStats(),
    lastUpdated: new Date().toISOString(),
  }
}