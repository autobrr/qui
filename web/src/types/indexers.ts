/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface TorznabIndexer {
  id: number
  name: string
  base_url: string
  indexer_id: string
  basic_username?: string
  backend: "jackett" | "prowlarr" | "native"
  enabled: boolean
  priority: number
  timeout_seconds: number
  capabilities: string[]
  categories: TorznabIndexerCategory[]
  last_test_at?: string
  last_test_status: string
  last_test_error?: string
  created_at: string
  updated_at: string
}

/** Response from create/update indexer endpoints, may include warnings for partial failures */
export interface IndexerResponse extends TorznabIndexer {
  warnings?: string[]
}

export interface TorznabIndexerCategory {
  indexer_id: number
  category_id: number
  category_name: string
  parent_category_id?: number
}

export interface TorznabIndexerError {
  id: number
  indexer_id: number
  error_message: string
  error_code: string
  occurred_at: string
  resolved_at?: string
  error_count: number
}

export interface TorznabIndexerLatencyStats {
  indexer_id: number
  operation_type: string
  total_requests: number
  successful_requests: number
  avg_latency_ms?: number
  min_latency_ms?: number
  max_latency_ms?: number
  success_rate_pct: number
  last_measured_at: string
}

export interface TorznabIndexerHealth {
  indexer_id: number
  indexer_name: string
  enabled: boolean
  last_test_status: string
  errors_last_24h: number
  unresolved_errors: number
  avg_latency_ms?: number
  success_rate_pct?: number
  requests_last_7d?: number
  last_measured_at?: string
}

// Activity/Scheduler types
export interface SchedulerTaskStatus {
  jobId: number
  taskId: number
  indexerId: number
  indexerName: string
  priority: string
  createdAt: string
  isRss: boolean
}

export interface SchedulerJobStatus {
  jobId: number
  totalTasks: number
  completedTasks: number
}

export interface SchedulerStatus {
  queuedTasks: SchedulerTaskStatus[]
  inFlightTasks: SchedulerTaskStatus[]
  activeJobs: SchedulerJobStatus[]
  queueLength: number
  workerCount: number
  workersInUse: number
}

export interface IndexerCooldownStatus {
  indexerId: number
  indexerName: string
  cooldownEnd: string
  reason?: string
}

export interface IndexerActivityStatus {
  scheduler?: SchedulerStatus
  cooldownIndexers: IndexerCooldownStatus[]
}

export interface SearchHistoryEntry {
  id: number
  jobId: number
  taskId: number
  indexerId: number
  indexerName: string
  query?: string
  releaseName?: string
  params?: Record<string, string>
  categories?: number[]
  contentType?: string
  priority: string
  searchMode?: string
  status: "success" | "error" | "skipped" | "rate_limited"
  resultCount: number
  startedAt: string
  completedAt: string
  durationMs: number
  errorMessage?: string
  // Cross-seed outcome tracking
  outcome?: "added" | "failed" | "no_match" | ""
  addedCount?: number
}

export interface SearchHistoryResponse {
  entries: SearchHistoryEntry[]
  total: number
  source: string
}

export interface TorznabIndexerFormData {
  name: string
  base_url: string
  indexer_id?: string
  api_key: string
  basic_username?: string
  basic_password?: string
  backend?: "jackett" | "prowlarr" | "native"
  enabled?: boolean
  priority?: number
  timeout_seconds?: number
  capabilities?: string[]
  categories?: TorznabIndexerCategory[]
}

export interface TorznabIndexerUpdate {
  name?: string
  base_url?: string
  api_key?: string
  indexer_id?: string
  basic_username?: string
  basic_password?: string
  backend?: "jackett" | "prowlarr" | "native"
  enabled?: boolean
  priority?: number
  timeout_seconds?: number
  capabilities?: string[]
  categories?: TorznabIndexerCategory[]
}

export interface TorznabSearchRequest {
  query?: string
  categories?: number[]
  imdb_id?: string
  tvdb_id?: string
  year?: number
  season?: number
  episode?: number
  artist?: string
  album?: string
  limit?: number
  offset?: number
  indexer_ids?: number[]
  cache_mode?: "bypass"
}

export interface TorznabSearchResponse {
  results: TorznabSearchResult[]
  total: number
  cache?: TorznabSearchCacheMetadata
}

export interface TorznabSearchCacheMetadata {
  hit: boolean
  scope: string
  source: string
  cachedAt: string
  expiresAt: string
  lastUsed?: string
}

export interface TorznabSearchCacheStats {
  entries: number
  totalHits: number
  approxSizeBytes: number
  oldestCachedAt?: string
  newestCachedAt?: string
  lastUsedAt?: string
  enabled: boolean
  ttlMinutes: number
}

export interface TorznabRecentSearch {
  cacheKey: string
  scope: string
  query: string
  categories: number[]
  indexerIds: number[]
  totalResults: number
  cachedAt: string
  lastUsedAt?: string
  expiresAt: string
  hitCount: number
}

export interface TorznabSearchResult {
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
  imdbId?: string
  tvdbId?: string
  source?: string
  collection?: string
  group?: string
}

export interface JackettIndexer {
  id: string
  name: string
  description: string
  type: string
  configured: boolean
  backend?: "jackett" | "prowlarr" | "native"
  caps?: string[]
  categories?: TorznabIndexerCategory[]
}

export interface DiscoverJackettRequest {
  base_url: string
  api_key: string
  basic_username?: string
  basic_password?: string
}

export interface DiscoverJackettResponse {
  indexers: JackettIndexer[]
  warnings?: string[]
}
