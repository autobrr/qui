/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { EconomyAnalysis, EconomyScore, EconomyStats } from "@/types"

export const generateFakeEconomyStats = (): EconomyStats => ({
  totalTorrents: 2847,
  totalStorage: 5432109876543,
  deduplicatedStorage: 4555566765456,
  storageSavings: 876543210987,
  averageEconomyScore: 67.8,
  highValueTorrents: 425,
  rareContentCount: 156,
  wellSeededOldContent: 1234,
})

export const generateFakeEconomyScores = (count: number = 25): EconomyScore[] =>
  Array.from({ length: count }, (_, i) => ({
    hash: `fake${i}hash${"0".repeat(32)}`,
    name: `Sample.Torrent.${i + 1}.2024.1080p.BluRay.x264-GROUP`,
    size: Math.floor(Math.random() * 10000000000),
    seeds: Math.floor(Math.random() * 100),
    peers: Math.floor(Math.random() * 50),
    ratio: Math.random() * 10,
    age: Math.floor(Math.random() * 365),
    economyScore: Math.floor(Math.random() * 100),
    storageValue: Math.floor(Math.random() * 100),
    rarityBonus: Math.floor(Math.random() * 50),
    deduplicationFactor: Math.random() * 2,
    reviewPriority: Math.floor(Math.random() * 10),
    tracker: ["Tracker1", "Tracker2", "Tracker3"][Math.floor(Math.random() * 3)],
    state: ["completed", "downloading", "seeding"][Math.floor(Math.random() * 3)],
    category: ["Movies", "TV", "Music", "Books"][Math.floor(Math.random() * 4)],
    lastActivity: Date.now() - Math.floor(Math.random() * 86400000),
  }))

export const generateFakeEconomyData = (): EconomyAnalysis => {
  const fakeStats = generateFakeEconomyStats()
  const fakeScores = generateFakeEconomyScores(25)

  return {
    scores: fakeScores,
    stats: fakeStats,
    topValuable: fakeScores.slice(0, 5),
    duplicates: {
      "Sample.Movie.2024": ["fake1hash", "fake2hash"],
      "Another.Movie.2024": ["fake3hash", "fake4hash"],
    },
    optimizations: [
      {
        type: "duplicate_cleanup",
        title: "Remove Duplicate Torrents",
        description: "Found 45 duplicate torrents that can be safely removed",
        priority: "high" as const,
        savings: 876543210987,
        impact: 85,
        torrents: ["fake1hash", "fake2hash"],
        category: "storage" as const,
      },
    ],
    storageOptimization: {
      totalPotentialSavings: 876543210987,
      deduplicationSavings: 456789012345,
      oldContentCleanupSavings: 234567890123,
      ratioOptimizationSavings: 123456789012,
      unusedContentSavings: 62123456789,
    },
    reviewTorrents: {
      torrents: fakeScores,
      groups: [fakeScores.slice(0, 5), fakeScores.slice(5, 10)],
      torrentGroups: [
        {
          id: "group1",
          torrents: fakeScores.slice(0, 3),
          primaryTorrent: fakeScores[0],
          groupType: "duplicate" as const,
          totalSize: 15000000000,
          deduplicatedSize: 5000000000,
          potentialSavings: 10000000000,
          recommendedAction: "keep_best" as const,
          priority: 8,
        },
      ],
      groupingEnabled: true,
      pagination: {
        page: 1,
        pageSize: 25,
        totalItems: 300,
        totalPages: 12,
        hasNextPage: true,
        hasPrevPage: false,
      },
    },
    reviewThreshold: 50,
  }
}