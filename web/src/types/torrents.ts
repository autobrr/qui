/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AppPreferences, QBittorrentAppInfo } from "./app"
import type { InstanceError } from "./instances"

export interface TorrentTracker {
  url: string
  status: number
  num_peers: number
  num_seeds: number
  num_leeches: number
  num_downloaded: number
  msg: string
}

export interface TorrentProperties {
  addition_date: number
  comment: string
  completion_date: number
  created_by: string
  creation_date: number
  dl_limit: number
  dl_speed: number
  dl_speed_avg: number
  download_path: string
  eta: number
  hash: string
  infohash_v1: string
  infohash_v2: string
  is_private: boolean
  last_seen: number
  name: string
  nb_connections: number
  nb_connections_limit: number
  peers: number
  peers_total: number
  piece_size: number
  pieces_have: number
  pieces_num: number
  reannounce: number
  save_path: string
  seeding_time: number
  seeds: number
  seeds_total: number
  share_ratio: number
  time_elapsed: number
  total_downloaded: number
  total_downloaded_session: number
  total_size: number
  total_uploaded: number
  total_uploaded_session: number
  total_wasted: number
  up_limit: number
  up_speed: number
  up_speed_avg: number
}

export interface TorrentFile {
  availability: number
  index: number
  is_seed?: boolean
  name: string
  piece_range: number[]
  priority: number
  progress: number
  size: number
}

export interface TorrentFileMediaInfoField {
  name: string
  value: string
}

export interface TorrentFileMediaInfoStream {
  kind: string
  fields: TorrentFileMediaInfoField[]
}

export interface TorrentFileMediaInfoResponse {
  fileIndex: number
  relativePath: string
  streams: TorrentFileMediaInfoStream[]
  rawJSON: string
}

export interface Torrent {
  added_on: number
  amount_left: number
  auto_tmm: boolean
  availability: number
  category: string
  completed: number
  completion_on: number
  content_path: string
  dl_limit: number
  dlspeed: number
  download_path: string
  downloaded: number
  downloaded_session: number
  eta: number
  f_l_piece_prio: boolean
  force_start: boolean
  hash: string
  infohash_v1: string
  infohash_v2: string
  popularity: number
  private: boolean
  last_activity: number
  max_ratio: number
  max_seeding_time: number
  max_inactive_seeding_time?: number
  name: string
  num_complete: number
  num_incomplete: number
  num_leechs: number
  num_seeds: number
  priority: number
  progress: number
  ratio: number
  ratio_limit: number
  reannounce: number
  save_path: string
  seeding_time: number
  seeding_time_limit: number
  inactive_seeding_time_limit?: number
  share_limit_action?: string
  share_limits_mode?: string
  seen_complete: number
  seq_dl: boolean
  size: number
  state: string
  super_seeding: boolean
  tags: string
  time_active: number
  total_size: number
  tracker: string
  trackers_count: number
  trackers?: TorrentTracker[]
  tracker_health?: "unregistered" | "tracker_down" | "tracker_error"
  up_limit: number
  uploaded: number
  uploaded_session: number
  upspeed: number
}

export interface DuplicateTorrentMatch {
  hash: string
  infohash_v1?: string
  infohash_v2?: string
  name: string
  matched_hashes?: string[]
}

export interface TorrentStats {
  total: number
  downloading: number
  seeding: number
  paused: number
  error: number
  totalDownloadSpeed?: number
  totalUploadSpeed?: number
  totalDownloadData?: number
  totalUploadData?: number
  totalSize?: number
  totalRemainingSize?: number
  totalSeedingSize?: number
}

export interface CacheMetadata {
  source: "cache" | "fresh"
  age: number
  isStale: boolean
  nextRefresh?: string
}

export interface TrackerTransferStats {
  uploaded: number
  downloaded: number
  uploadedSession: number
  downloadedSession: number
  totalSize: number
  count: number
}

/**
 * Sidebar count groups returned with a torrent list response.
 *
 * Tracker keys are normalized tracker domains. qBittorrent pseudo tracker
 * labels such as DHT, PeX, and LSD are omitted rather than folded into Unknown.
 */
export interface TorrentCounts {
  status: Record<string, number>
  categories: Record<string, number>
  categorySizes?: Record<string, number>
  tags: Record<string, number>
  tagSizes?: Record<string, number>
  trackers: Record<string, number>
  trackerTransfers?: Record<string, TrackerTransferStats>
  total: number
}

/**
 * Manual torrent filters accepted by list endpoints and stream subscriptions.
 * Tracker filters use the same normalized domain keys as TorrentCounts.trackers.
 */
export interface TorrentFilters {
  status: string[]
  excludeStatus: string[]
  categories: string[]
  excludeCategories: string[]
  expandedCategories?: string[]
  expandedExcludeCategories?: string[]
  tags: string[]
  excludeTags: string[]
  trackers: string[]
  excludeTrackers: string[]
  expr?: string
}

/**
 * A named snapshot of TorrentFilters, saved server-side and shared across
 * instances. The backend stores filters as an opaque blob, so treat them as
 * partial and normalize with toViewFilters before use.
 */
export interface FilterView {
  id: number
  name: string
  filters: Partial<TorrentFilters>
  createdAt: string
  updatedAt: string
}

export interface FilterViewInput {
  name: string
  filters: Partial<TorrentFilters>
}

/**
 * Real-time instance auth/decryption and connection metadata emitted on torrent
 * stream snapshots.
 */
export interface InstanceMeta {
  connected: boolean
  hasDecryptionError: boolean
  recentErrors?: InstanceError[]
  /** Normalized qBittorrent connection status, or "disabled" for inactive instances. */
  connectionStatus?: string
}

/**
 * Torrent list response shared by REST and stream snapshots.
 *
 * `preferences` is tri-state: omitted leaves existing frontend preference caches
 * unchanged, a value replaces them, and `null` clears stale cached preferences.
 */
export interface TorrentResponse {
  torrents: Torrent[]
  crossInstanceTorrents?: CrossInstanceTorrent[]
  cross_instance_torrents?: CrossInstanceTorrent[]  // Backend uses snake_case
  total: number
  activeTaskCount?: number
  stats?: TorrentStats
  counts?: TorrentCounts
  categories?: Record<string, Category>
  tags?: string[]
  serverState?: ServerState
  appInfo?: QBittorrentAppInfo
  /** qBittorrent preferences for this instance; `null` explicitly clears cached preferences. */
  preferences?: AppPreferences | null
  useSubcategories?: boolean
  cacheMetadata?: CacheMetadata
  hasMore?: boolean
  trackerHealthSupported?: boolean
  isCrossInstance?: boolean
  instanceMeta?: InstanceMeta  // Real-time instance health from SSE
  /** Frontend-assembled only: number of pages combined into this loaded-window response. */
  windowPageCount?: number
}

export interface AddTorrentFailedURL {
  url: string
  error: string
}

export interface AddTorrentFailedFile {
  filename: string
  error: string
}

export interface AddTorrentResponse {
  message: string
  added: number
  failed: number
  failedURLs?: AddTorrentFailedURL[]
  failedFiles?: AddTorrentFailedFile[]
}

export interface CrossInstanceTorrent extends Torrent {
  instanceId: number
  instanceName: string
}

export interface TorrentStreamMeta {
  instanceId: number
  rid?: number
  fullUpdate?: boolean
  timestamp: string
  // lastSuccessfulSync is when the instance's data last actually updated (RFC3339).
  // Present on stream-error frames so the UI can show how stale the retained rows are
  // without that age resetting on every failed attempt. Omitted when unknown.
  lastSuccessfulSync?: string
  retryInSeconds?: number
  streamKey?: string
}

export interface TorrentStreamVersion {
  major: number
  minor: number
}

// TorrentStreamDelta reconciles a delta frame's page-0 window against the previous
// frame. The added/changed rows ride in the frame's `data.torrents` (or
// `cross_instance_torrents`); `order` is the full page key sequence, present only
// when membership or ordering changed (otherwise changed rows apply in place). Keys
// are the torrent hash for single-instance streams and "<instanceId>:<hash>" for
// cross-instance streams.
export interface TorrentStreamDelta {
  order?: string[]
  baseVersion: TorrentStreamVersion
}

export interface TorrentStreamPayload {
  type: "init" | "update" | "delta" | "stream-error" | "heartbeat"
  data?: TorrentResponse
  delta?: TorrentStreamDelta
  meta?: TorrentStreamMeta
  version?: TorrentStreamVersion
  error?: string
}

// Simplified MainData - only used for Dashboard server stats
export interface MainData {
  rid: number
  serverState?: ServerState
  server_state?: ServerState
}

export interface Category {
  name: string
  savePath: string
}

export interface ServerState {
  connection_status: string
  dht_nodes: number
  dl_info_data: number
  dl_info_speed: number
  dl_rate_limit: number
  up_info_data: number
  up_info_speed: number
  up_rate_limit: number
  queueing: boolean
  use_alt_speed_limits: boolean
  use_subcategories?: boolean
  refresh_interval: number
  alltime_dl?: number
  alltime_ul?: number
  total_wasted_session?: number
  global_ratio?: string
  total_peer_connections?: number
  free_space_on_disk?: number
  average_time_queue?: number
  queued_io_jobs?: number
  read_cache_hits?: string
  read_cache_overload?: string
  total_buffers_size?: number
  total_queued_size?: number
  write_cache_overload?: string
  last_external_address_v4?: string
  last_external_address_v6?: string
}

export interface TransferInfo {
  connection_status: string
  dht_nodes: number
  dl_info_data: number
  dl_info_speed: number
  dl_rate_limit: number
  up_info_data: number
  up_info_speed: number
  up_rate_limit: number
}

export interface TorrentPeer {
  ip?: string
  port?: number
  connection?: string
  flags?: string
  flags_desc?: string
  client?: string
  progress: number
  dl_speed?: number
  up_speed?: number
  downloaded?: number
  uploaded?: number
  relevance?: number
  files?: string
  country?: string
  country_code?: string
  peer_id_client?: string
}

export interface SortedPeer extends TorrentPeer {
  key: string
}

export interface TorrentPeersResponse {
  peers?: Record<string, TorrentPeer>
  peers_removed?: string[]
  rid: number
  full_update: boolean
  show_flags?: boolean
}

export interface SortedPeersResponse extends TorrentPeersResponse {
  sorted_peers?: SortedPeer[]
}

export interface WebSeed {
  url: string
}
