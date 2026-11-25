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
  availableIndexers?: number[]
  filteredIndexers?: number[]
  excludedIndexers?: Record<number, string>
  contentMatches?: string[]
  gazellePlan?: CrossSeedGazelleLookup[]
}

export type CrossSeedGazelleLookupStatus = "will_ask" | "match" | "no_match" | "not_asked" | "failed"

export type CrossSeedGazelleLookupReason =
  | "cooldown"
  | "not_selected"
  | "source_site"
  | "already_seeded"
  | "not_music"
  | "key_rejected"
  | "timed_out"
  | "error"

export interface CrossSeedGazelleLookup {
  site: string
  name: string
  shortName: string
  indexerId: number
  status: CrossSeedGazelleLookupStatus
  reason?: CrossSeedGazelleLookupReason
  detail?: string
  replacedIndexerIds?: number[]
}

export interface CrossSeedGazelleSite {
  site: string
  name: string
  shortName: string
  indexerId: number
  active: boolean
  replacedIndexerIds: number[]
}

export interface CrossSeedGazelleSiteSummary {
  site: string
  name: string
  asked: number
  matched: number
  failed: number
  notAsked?: Partial<Record<CrossSeedGazelleLookupReason, number>>
}

export type CrossSeedResultClass = "added" | "exists" | "skipped" | "stranded" | "failed"

export interface CrossSeedInstanceResult {
  instanceId: number
  instanceName: string
  success: boolean
  status: string
  class: CrossSeedResultClass
  message?: string
  crossSeedInstanceId?: number
  crossSeedInstanceName?: string
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

export type CrossSeedQueryDegradedReason = "arr_lookup_failed" | "arr_no_ids"

export interface CrossSeedSearchRejectedCandidate {
  indexer: string
  indexerId: number
  title: string
  size: number
  reason: string
}

export type CrossSeedSearchIndexerStatus = "searched" | "not_covered" | "error" | "excluded"

export interface CrossSeedSearchIndexerOutcome {
  indexerId: number
  status: CrossSeedSearchIndexerStatus
  detail?: string
  candidates: number
}

export interface CrossSeedSearchDecisionTrace {
  sourceSize: number
  tolerancePercent: number
  totalResults: number
  sizeFiltered: number
  releaseFiltered: number
  lateContentFiltered: number
  duplicateFiltered: number
  finalMatches: number
  rejectionCounts?: Record<string, number>
  rejectedCandidates?: CrossSeedSearchRejectedCandidate[]
  indexers?: CrossSeedSearchIndexerOutcome[]
}

export interface CrossSeedTorrentSearchResponse {
  sourceTorrent: CrossSeedTorrentInfo
  results: CrossSeedTorrentSearchResult[]
  cache?: TorznabSearchCacheMetadata
  partial?: boolean
  queryDegraded?: CrossSeedQueryDegradedReason
  gazelleLookups?: CrossSeedGazelleLookup[]
  torznabFailed?: boolean
  decisionTrace?: CrossSeedSearchDecisionTrace
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

export interface ManualCrossSeedProposal {
  hash: string
  name: string
  size: number
  category: string
  effectiveSavePath: string
  overlapBytes: number
  overlapFraction: number
}

export interface ManualCrossSeedProposalsResponse {
  packMode: boolean
  packEpisodeCount: number
  assemblyUnavailableReason: string
  proposalLimit: number
  proposalsTruncated: boolean
  sourceName: string
  sourceSize: number
  sourceFileCount: number
  defaultTags: string[]
  pinnedCategory: string
  proposals: ManualCrossSeedProposal[]
}

export interface ManualAssembleRequest {
  instanceId: number
  torrentData: string
  targetHashes: string[]
  category?: string
  tags?: string[]
}

export interface ManualAssembleResponse {
  ready: boolean
  applied: boolean
  reason: string
  message: string
  targets: { hash: string; name: string; reason: string }[]
  matchedEpisodes: number
  totalEpisodes: number
  coverage: number
  linkedBytes: number
  missingBytes: number
  destination: string
  defaultCategory: string
  linkMode: string
}

export interface ManualCrossSeedApplyResponse {
  success: boolean
  results: CrossSeedInstanceResult[]
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
  crossSeedInstanceId?: number
  crossSeedInstanceName?: string
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
  crossSeedsAdded: number
  candidatesFailed: number
  candidatesSkipped: number
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

export interface CategoryMappingRule {
  categories: string[]
  contentType: string
}

export interface CrossSeedAutomationSettings {
  enabled: boolean
  runIntervalMinutes: number
  targetInstanceIds: number[]
  targetIndexerIds: number[]
  rssSourceCategories: string[]
  rssSourceTags: string[]
  rssSourceExcludeCategories: string[]
  rssSourceExcludeTags: string[]
  webhookSourceCategories: string[]
  webhookSourceTags: string[]
  webhookSourceExcludeCategories: string[]
  webhookSourceExcludeTags: string[]
  findIndividualEpisodes: boolean
  autoResumeMaxDownloadMb: number
  pooledPartialCompletionEnabled: boolean
  useCategoryFromIndexer: boolean
  useCrossCategoryAffix: boolean
  categoryAffixMode: "prefix" | "suffix"
  categoryAffix: string
  useCustomCategory: boolean
  customCategory: string
  runExternalProgramId?: number | null
  categoryMappingRules: CategoryMappingRule[]
  rssAutomationTags: string[]
  seededSearchTags: string[]
  completionSearchTags: string[]
  webhookTags: string[]
  inheritSourceTags: boolean
  skipAutoResumeRss: boolean
  skipAutoResumeSeededSearch: boolean
  skipAutoResumeCompletion: boolean
  skipAutoResumeWebhook: boolean
  skipRecheck: boolean
  rescueTitleMismatches: boolean
  skipPieceBoundarySafetyCheck: boolean
  useHardlinks: boolean
  hardlinkBaseDir: string
  hardlinkDirPreset: "flat" | "by-tracker" | "by-instance"
  gazelleEnabled: boolean
  redactedApiKey: string
  orpheusApiKey: string
  seasonPackEnabled: boolean
  seasonPackAutomationEnabled: boolean
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
  targetInstanceIds?: number[]
  targetIndexerIds?: number[]
  rssSourceCategories?: string[]
  rssSourceTags?: string[]
  rssSourceExcludeCategories?: string[]
  rssSourceExcludeTags?: string[]
  webhookSourceCategories?: string[]
  webhookSourceTags?: string[]
  webhookSourceExcludeCategories?: string[]
  webhookSourceExcludeTags?: string[]
  findIndividualEpisodes?: boolean
  autoResumeMaxDownloadMb?: number
  pooledPartialCompletionEnabled?: boolean
  useCategoryFromIndexer?: boolean
  useCrossCategoryAffix?: boolean
  categoryAffixMode?: "prefix" | "suffix"
  categoryAffix?: string
  useCustomCategory?: boolean
  customCategory?: string
  runExternalProgramId?: number | null
  categoryMappingRules?: CategoryMappingRule[]
  rssAutomationTags?: string[]
  seededSearchTags?: string[]
  completionSearchTags?: string[]
  webhookTags?: string[]
  inheritSourceTags?: boolean
  skipAutoResumeRss?: boolean
  skipAutoResumeSeededSearch?: boolean
  skipAutoResumeCompletion?: boolean
  skipAutoResumeWebhook?: boolean
  skipRecheck?: boolean
  rescueTitleMismatches?: boolean
  skipPieceBoundarySafetyCheck?: boolean
  useHardlinks?: boolean
  hardlinkBaseDir?: string
  hardlinkDirPreset?: "flat" | "by-tracker" | "by-instance"
  gazelleEnabled?: boolean
  redactedApiKey?: string
  orpheusApiKey?: string
  seasonPackEnabled?: boolean
  seasonPackAutomationEnabled?: boolean
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
  excludeCategories?: string[]
  excludeTags?: string[]
}

export interface CrossSeedSearchSettings {
  instanceId?: number | null
  categories: string[]
  tags: string[]
  excludeCategories: string[]
  excludeTags: string[]
  indexerIds: number[]
  intervalSeconds: number
  cooldownMinutes: number
  maxAddedAgeDays: number
  skipIndividualEpisodes: boolean
  torznabEnabled: boolean
  scheduleEnabled: boolean
  scheduleIntervalHours: number
  scheduleMaxSearches: number
  scheduleMaxAgeDays: number
  createdAt?: string
  updatedAt?: string
}

export type CrossSeedSearchSettingsPatch = Partial<Omit<CrossSeedSearchSettings, "createdAt" | "updatedAt">>

export type CrossSeedSearchResultStatus = "added" | "skipped" | "failed"

export interface CrossSeedSearchResult {
  torrentHash: string
  torrentName: string
  indexerName: string
  releaseTitle: string
  status: CrossSeedSearchResultStatus
  message?: string
  processedAt: string
  crossSeedInstanceId?: number
  crossSeedInstanceName?: string
}

export type CrossSeedSearchRunKind = "scheduled" | "manual"

export interface CrossSeedSearchRun {
  id: number
  instanceId: number
  kind: CrossSeedSearchRunKind
  status: string
  startedAt: string
  completedAt?: string
  totalTorrents: number
  processed: number
  torrentsWithCrossSeeds: number
  crossSeedsAdded: number
  torrentsFailed: number
  torrentsSkipped: number
  message?: string
  errorMessage?: string
  filters: CrossSeedSearchFilters
  indexerIds: number[]
  intervalSeconds: number
  cooldownMinutes: number
  results: CrossSeedSearchResult[]
  gazelleSummary?: CrossSeedGazelleSiteSummary[]
  searchLimit?: number
  waitingTorrents?: number
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
  nextScheduledRunAt?: string
  effectiveIntervalSeconds?: number
}

export interface SeasonPackRun {
  id: number
  torrentName: string
  phase: "check" | "apply" | "resume"
  status: "ready" | "ready_no_threshold" | "skipped" | "applied" | "failed"
  reason: string
  message: string
  instanceId?: number
  addedInstanceId?: number
  matchedEpisodes: number
  totalEpisodes: number
  coverage: number
  linkMode?: string
  createdAt: string
}

export interface CrossSeedRouteRow {
  baseDir: string
  mappedPath: string
}

export interface CrossSeedRoute {
  sourceInstanceId: number
  crossSeedInstanceId: number
  rows: CrossSeedRouteRow[]
}

export interface CrossSeedRouteStatus extends CrossSeedRoute {
  rows: (CrossSeedRouteRow & { routedTorrents: number | null })[]
  brokenRule?: string
}

export type CrossSeedRouteTestRowResult = "sees" | "seesFolderNotMarker" | "cannotSeeFolder" | "cannotWrite"

export interface CrossSeedRouteTestResult {
  crossSeedUnavailable?: string
  rows: { baseDir: string; listedPath: string; result: CrossSeedRouteTestRowResult; error?: string }[]
}
