/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorznabSearchCacheMetadata } from "./indexers"

export interface CrossSeedTorrentInfo {
  instanceId?: number
  instanceName?: string
  hash?: string
  name: string
  category?: string
  size?: number
  progress?: number
  totalFiles?: number
  matchingFiles?: number
  fileCount?: number
  contentType?: string
  searchType?: string
  searchCategories?: number[]
  requiredCaps?: string[]
  discLayout?: boolean
  discMarker?: string
  // Pre-filtering information for UI context menu
  availableIndexers?: number[]
  filteredIndexers?: number[]
  excludedIndexers?: Record<number, string>
  contentMatches?: string[]
  // Async filtering status
  contentFilteringCompleted?: boolean
}

export interface AsyncIndexerFilteringState {
  capabilitiesCompleted: boolean
  contentCompleted: boolean
  capabilityIndexers: number[]
  filteredIndexers: number[]
  excludedIndexers: Record<number, string>
  contentMatches: string[]
}

export interface CrossSeedInstanceResult {
  instanceId: number
  instanceName: string
  success: boolean
  status: string
  message?: string
  matchedTorrent?: {
    hash: string
    name: string
    progress: number
    size: number
  }
}

export interface CrossSeedTorrentSearchResult {
  indexer: string
  indexerId: number
  title: string
  downloadUrl: string
  infoUrl?: string
  size: number
  seeders: number
  leechers: number
  categoryId: number
  categoryName: string
  publishDate: string
  downloadVolumeFactor: number
  uploadVolumeFactor: number
  guid: string
  infoHashV1?: string
  infoHashV2?: string
  imdbId?: string
  tvdbId?: string
  matchReason?: string
  matchScore: number
}

export interface CrossSeedTorrentSearchResponse {
  sourceTorrent: CrossSeedTorrentInfo
  results: CrossSeedTorrentSearchResult[]
  cache?: TorznabSearchCacheMetadata
}

export interface CrossSeedTorrentSearchSelection {
  indexerId: number
  indexer: string
  downloadUrl: string
  title: string
  guid?: string
}

export interface CrossSeedApplyResult {
  title: string
  indexer: string
  torrentName?: string
  infoHash?: string
  success: boolean
  instanceResults?: CrossSeedInstanceResult[]
  error?: string
}

export interface CrossSeedApplyResponse {
  results: CrossSeedApplyResult[]
}

export interface CrossSeedBlocklistEntry {
  instanceId: number
  infoHash: string
  note?: string
  createdAt: string
}

export interface CrossSeedRunResult {
  instanceId: number
  instanceName: string
  indexerName?: string
  success: boolean
  status: string
  message?: string
  matchedTorrentHash?: string
  matchedTorrentName?: string
}

export interface CrossSeedRun {
  id: number
  triggeredBy: string
  mode: "auto" | "manual"
  status: "pending" | "running" | "success" | "partial" | "failed"
  startedAt: string
  completedAt?: string
  totalFeedItems: number
  candidatesFound: number
  torrentsAdded: number
  torrentsFailed: number
  torrentsSkipped: number
  message?: string
  errorMessage?: string
  results?: CrossSeedRunResult[]
  createdAt: string
}

export interface SeasonPackCategoryRule {
  resolution: string
  source: string
  category: string
}

export interface CrossSeedAutomationSettings {
  enabled: boolean
  runIntervalMinutes: number
  startPaused: boolean
  category?: string | null
  targetInstanceIds: number[]
  targetIndexerIds: number[]
  // RSS source filtering: filter which local torrents to search when checking RSS feeds
  rssSourceCategories: string[]
  rssSourceTags: string[]
  rssSourceExcludeCategories: string[]
  rssSourceExcludeTags: string[]
  // Webhook source filtering: filter which local torrents to search when checking webhook requests
  webhookSourceCategories: string[]
  webhookSourceTags: string[]
  webhookSourceExcludeCategories: string[]
  webhookSourceExcludeTags: string[]
  findIndividualEpisodes: boolean
  sizeMismatchTolerancePercent: number
  useCategoryFromIndexer: boolean
  useCrossCategoryAffix: boolean
  categoryAffixMode: "prefix" | "suffix"
  categoryAffix: string
  useCustomCategory: boolean
  customCategory: string
  runExternalProgramId?: number | null
  // Source-specific tagging
  rssAutomationTags: string[]
  seededSearchTags: string[]
  completionSearchTags: string[]
  webhookTags: string[]
  inheritSourceTags: boolean
  // Skip auto-resume settings per source mode
  skipAutoResumeRss: boolean
  skipAutoResumeSeededSearch: boolean
  skipAutoResumeCompletion: boolean
  skipAutoResumeWebhook: boolean
  skipRecheck: boolean
  skipPieceBoundarySafetyCheck: boolean
  // Hardlink mode settings
  useHardlinks: boolean
  hardlinkBaseDir: string
  hardlinkDirPreset: "flat" | "by-tracker" | "by-instance"
  // Gazelle (OPS/RED) cross-seed settings
  gazelleEnabled: boolean
  redactedApiKey: string
  orpheusApiKey: string
  // Season pack settings
  seasonPackEnabled: boolean
  seasonPackSkipRepackCompare: boolean
  seasonPackSimplifyHdrCompare: boolean
  seasonPackSimplifyWebCompare: boolean
  seasonPackSkipYearCompare: boolean
  seasonPackCoverageThreshold: number
  seasonPackTags: string[]
  seasonPackCategory: string
  seasonPackCategoryRules: SeasonPackCategoryRule[]
  seasonPackTvdbApiKey?: string
  seasonPackTvdbPin?: string
  createdAt?: string
  updatedAt?: string
}

export interface CrossSeedAutomationSettingsPatch {
  enabled?: boolean
  runIntervalMinutes?: number
  startPaused?: boolean
  category?: string | null
  targetInstanceIds?: number[]
  targetIndexerIds?: number[]
  // RSS source filtering: filter which local torrents to search when checking RSS feeds
  rssSourceCategories?: string[]
  rssSourceTags?: string[]
  rssSourceExcludeCategories?: string[]
  rssSourceExcludeTags?: string[]
  // Webhook source filtering: filter which local torrents to search when checking webhook requests
  webhookSourceCategories?: string[]
  webhookSourceTags?: string[]
  webhookSourceExcludeCategories?: string[]
  webhookSourceExcludeTags?: string[]
  findIndividualEpisodes?: boolean
  sizeMismatchTolerancePercent?: number
  useCategoryFromIndexer?: boolean
  useCrossCategoryAffix?: boolean
  categoryAffixMode?: "prefix" | "suffix"
  categoryAffix?: string
  useCustomCategory?: boolean
  customCategory?: string
  runExternalProgramId?: number | null
  // Source-specific tagging
  rssAutomationTags?: string[]
  seededSearchTags?: string[]
  completionSearchTags?: string[]
  webhookTags?: string[]
  inheritSourceTags?: boolean
  // Skip auto-resume settings per source mode
  skipAutoResumeRss?: boolean
  skipAutoResumeSeededSearch?: boolean
  skipAutoResumeCompletion?: boolean
  skipAutoResumeWebhook?: boolean
  skipRecheck?: boolean
  skipPieceBoundarySafetyCheck?: boolean
  // Hardlink mode settings
  useHardlinks?: boolean
  hardlinkBaseDir?: string
  hardlinkDirPreset?: "flat" | "by-tracker" | "by-instance"
  // Gazelle (OPS/RED) cross-seed settings
  gazelleEnabled?: boolean
  redactedApiKey?: string
  orpheusApiKey?: string
  // Season pack settings
  seasonPackEnabled?: boolean
  seasonPackSkipRepackCompare?: boolean
  seasonPackSimplifyHdrCompare?: boolean
  seasonPackSimplifyWebCompare?: boolean
  seasonPackSkipYearCompare?: boolean
  seasonPackCoverageThreshold?: number
  seasonPackTags?: string[]
  seasonPackCategory?: string
  seasonPackCategoryRules?: SeasonPackCategoryRule[]
  seasonPackTvdbApiKey?: string
  seasonPackTvdbPin?: string
}

export interface CrossSeedAutomationStatus {
  settings: CrossSeedAutomationSettings
  lastRun?: CrossSeedRun | null
  nextRunAt?: string
  running: boolean
}

export interface CrossSeedSearchFilters {
  categories: string[]
  tags: string[]
}

export interface CrossSeedSearchSettings {
  instanceId?: number | null
  categories: string[]
  tags: string[]
  indexerIds: number[]
  intervalSeconds: number
  cooldownMinutes: number
  createdAt?: string
  updatedAt?: string
}

export interface CrossSeedSearchSettingsPatch {
  instanceId?: number | null
  categories?: string[]
  tags?: string[]
  indexerIds?: number[]
  intervalSeconds?: number
  cooldownMinutes?: number
}

export type CrossSeedSearchResultStatus = "added" | "skipped" | "failed"

export interface CrossSeedSearchResult {
  torrentHash: string
  torrentName: string
  indexerName: string
  releaseTitle: string
  status: CrossSeedSearchResultStatus
  message?: string
  processedAt: string
}

export interface CrossSeedSearchRun {
  id: number
  instanceId: number
  status: string
  startedAt: string
  completedAt?: string
  totalTorrents: number
  processed: number
  torrentsAdded: number
  torrentsFailed: number
  torrentsSkipped: number
  message?: string
  errorMessage?: string
  filters: CrossSeedSearchFilters
  indexerIds: number[]
  intervalSeconds: number
  cooldownMinutes: number
  results: CrossSeedSearchResult[]
  createdAt: string
}

export interface CrossSeedSearchCandidate {
  torrentHash: string
  torrentName: string
  category?: string
  tags: string[]
}

export interface CrossSeedSearchStatus {
  running: boolean
  run?: CrossSeedSearchRun
  currentTorrent?: CrossSeedSearchCandidate
  recentResults: CrossSeedSearchResult[]
  nextRunAt?: string
  effectiveIntervalSeconds?: number
}

export interface SeasonPackRun {
  id: number
  torrentName: string
  phase: "check" | "apply"
  status: "ready" | "skipped" | "applied" | "failed"
  reason: string
  message: string
  instanceId?: number
  matchedEpisodes: number
  totalEpisodes: number
  coverage: number
  linkMode?: string
  createdAt: string
}
