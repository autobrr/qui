/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface User {
  id?: number
  username: string
  createdAt?: string
  updatedAt?: string
  auth_method?: string
}

export interface AuthResponse {
  user: User
  message?: string
}

// Generic warning response for operations that succeed with caveats
export interface WarningResponse {
  warning?: string
}

export interface Instance {
  id: number
  name: string
  host: string
  username: string
  basicUsername?: string
  tlsSkipVerify: boolean
  hasLocalFilesystemAccess: boolean
  // Hardlink mode settings (per-instance)
  useHardlinks: boolean
  hardlinkBaseDir: string
  hardlinkDirPreset: "flat" | "by-tracker" | "by-instance"
  // Reflink mode (copy-on-write) - mutually exclusive with hardlink mode
  useReflinks: boolean
  // Fallback to regular mode when reflink/hardlink fails
  fallbackToRegularMode: boolean
  sortOrder: number
  isActive: boolean
  reannounceSettings: InstanceReannounceSettings
}

export interface InstanceFormData {
  name: string
  host: string
  username?: string
  password?: string
  basicUsername?: string
  basicPassword?: string
  tlsSkipVerify: boolean
  hasLocalFilesystemAccess: boolean
  // Hardlink mode settings (per-instance)
  useHardlinks?: boolean
  hardlinkBaseDir?: string
  hardlinkDirPreset?: "flat" | "by-tracker" | "by-instance"
  // Reflink mode (copy-on-write) - mutually exclusive with hardlink mode
  useReflinks?: boolean
  // Fallback to regular mode when reflink/hardlink fails
  fallbackToRegularMode?: boolean
  reannounceSettings: InstanceReannounceSettings
}

export interface InstanceReannounceSettings {
  enabled: boolean
  initialWaitSeconds: number
  reannounceIntervalSeconds: number
  maxAgeSeconds: number
  maxRetries: number
  aggressive: boolean
  monitorAll: boolean
  excludeCategories: boolean
  categories: string[]
  excludeTags: boolean
  tags: string[]
  excludeTrackers: boolean
  trackers: string[]
}

// Reannounce settings constraints - shared across components
export const REANNOUNCE_CONSTRAINTS = {
  MIN_INITIAL_WAIT: 5,
  MIN_INTERVAL: 5,
  MIN_MAX_AGE: 60,
  MIN_MAX_RETRIES: 1,
  MAX_MAX_RETRIES: 50,
  DEFAULT_MAX_RETRIES: 50,
} as const

export interface InstanceCrossSeedCompletionSettings {
  instanceId: number
  enabled: boolean
  categories: string[]
  tags: string[]
  excludeCategories: string[]
  excludeTags: string[]
  indexerIds: number[]
}

/**
 * A torrent match found by the backend using proper release metadata parsing (rls library).
 */
export interface LocalCrossSeedMatch {
  instanceId: number
  instanceName: string
  hash: string
  name: string
  size: number
  progress: number
  savePath: string
  contentPath: string
  category: string
  tags: string
  state: string
  tracker: string
  trackerHealth?: string
  matchType: "content_path" | "name" | "release"
}

export interface InstanceReannounceActivity {
  instanceId: number
  hash: string
  torrentName?: string
  trackers?: string
  outcome: "skipped" | "failed" | "succeeded"
  reason?: string
  timestamp: string
}

export interface InstanceReannounceCandidate {
  instanceId: number
  hash: string
  torrentName?: string
  trackers?: string
  timeActiveSeconds?: number
  category?: string
  tags?: string
  state: "watching" | "reannouncing" | "cooldown"
  hasTrackerProblem: boolean
  waitingForInitial: boolean
}

export interface InstanceError {
  id: number
  instanceId: number
  errorType: string
  errorMessage: string
  occurredAt: string
}

// Condition field types for expression-based automations
export type ConditionField =
  // String fields
  | "NAME"
  | "HASH"
  | "INFOHASH_V1"
  | "INFOHASH_V2"
  | "MAGNET_URI"
  | "CATEGORY"
  | "TAGS"
  | "SAVE_PATH"
  | "CONTENT_PATH"
  | "DOWNLOAD_PATH"
  | "CREATED_BY"
  | "TRACKERS"
  | "CONTENT_TYPE"
  | "EFFECTIVE_NAME"
  | "RLS_SOURCE"
  | "RLS_RESOLUTION"
  | "RLS_CODEC"
  | "RLS_HDR"
  | "RLS_AUDIO"
  | "RLS_CHANNELS"
  | "RLS_GROUP"
  | "STATE"
  | "TRACKER"
  | "COMMENT"
  // Numeric fields (bytes)
  | "SIZE"
  | "TOTAL_SIZE"
  | "COMPLETED"
  | "DOWNLOADED"
  | "DOWNLOADED_SESSION"
  | "UPLOADED"
  | "UPLOADED_SESSION"
  | "AMOUNT_LEFT"
  | "FREE_SPACE"
  // Time fields (duration seconds and timestamp-backed ages)
  | "ADDED_ON"
  | "COMPLETION_ON"
  | "LAST_ACTIVITY"
  | "SEEN_COMPLETE"
  | "ETA"
  | "REANNOUNCE"
  | "SEEDING_TIME"
  | "TIME_ACTIVE"
  | "MAX_SEEDING_TIME"
  | "MAX_INACTIVE_SEEDING_TIME"
  | "SEEDING_TIME_LIMIT"
  | "INACTIVE_SEEDING_TIME_LIMIT"
  // Legacy age aliases (duration type)
  | "ADDED_ON_AGE"
  | "COMPLETION_ON_AGE"
  | "LAST_ACTIVITY_AGE"
  // Numeric fields (float64)
  | "RATIO"
  | "RATIO_LIMIT"
  | "MAX_RATIO"
  | "PROGRESS"
  | "AVAILABILITY"
  | "POPULARITY"
  // Numeric fields (speeds)
  | "DL_SPEED"
  | "UP_SPEED"
  | "DL_LIMIT"
  | "UP_LIMIT"
  // Numeric fields (counts/misc)
  | "NUM_SEEDS"
  | "NUM_LEECHS"
  | "NUM_COMPLETE"
  | "NUM_INCOMPLETE"
  | "TRACKERS_COUNT"
  | "PRIORITY"
  | "GROUP_SIZE"
  // Boolean fields
  | "PRIVATE"
  | "AUTO_MANAGED"
  | "FIRST_LAST_PIECE_PRIO"
  | "FORCE_START"
  | "SEQUENTIAL_DOWNLOAD"
  | "SUPER_SEEDING"
  | "IS_UNREGISTERED"
  | "HAS_MISSING_FILES"
  | "IS_GROUPED"
  // Enum-like fields
  | "HARDLINK_SCOPE"

export type ConditionOperator =
  // Logical operators (for groups)
  | "AND"
  | "OR"
  // Comparison operators
  | "EQUAL"
  | "NOT_EQUAL"
  | "CONTAINS"
  | "NOT_CONTAINS"
  | "STARTS_WITH"
  | "ENDS_WITH"
  | "GREATER_THAN"
  | "GREATER_THAN_OR_EQUAL"
  | "LESS_THAN"
  | "LESS_THAN_OR_EQUAL"
  | "BETWEEN"
  | "MATCHES"
  // Cross-category lookup operators (NAME field only)
  | "EXISTS_IN"
  | "CONTAINS_IN"

export interface RuleCondition {
  /** UI-only stable identifier (not persisted server-side) */
  clientId?: string
  field?: ConditionField
  operator: ConditionOperator
  groupId?: string
  value?: string
  minValue?: number
  maxValue?: number
  regex?: boolean
  negate?: boolean
  conditions?: RuleCondition[]
}

export interface SpeedLimitAction {
  enabled: boolean
  uploadKiB?: number
  downloadKiB?: number
  condition?: RuleCondition
}

export interface ShareLimitsAction {
  enabled: boolean
  ratioLimit?: number
  seedingTimeMinutes?: number
  condition?: RuleCondition
}

export interface PauseAction {
  enabled: boolean
  condition?: RuleCondition
}

export interface ResumeAction {
  enabled: boolean
  condition?: RuleCondition
}

export interface RecheckAction {
  enabled: boolean
  condition?: RuleCondition
}

export interface ReannounceAction {
  enabled: boolean
  condition?: RuleCondition
}

export interface GroupDefinition {
  id: string
  keys: string[]
  ambiguousPolicy?: "verify_overlap" | "skip"
  minFileOverlapPercent?: number
}

export interface GroupingConfig {
  defaultGroupId?: string
  groups?: GroupDefinition[]
}

export interface DeleteAction {
  enabled: boolean
  mode?: "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds" | "deleteWithFilesIncludeCrossSeeds"
  includeHardlinks?: boolean // Only valid when mode is "deleteWithFilesIncludeCrossSeeds"
  groupId?: string
  atomic?: "all"
  condition?: RuleCondition
}

export interface TagAction {
  enabled: boolean
  tags: string[]
  mode: "full" | "add" | "remove"
  deleteFromClient?: boolean
  useTrackerAsTag?: boolean
  useDisplayName?: boolean
  condition?: RuleCondition
}

export interface CategoryAction {
  enabled: boolean
  category: string
  includeCrossSeeds?: boolean
  groupId?: string
  blockIfCrossSeedInCategories?: string[]
  condition?: RuleCondition
}

export interface MoveAction {
  enabled: boolean
  path: string
  blockIfCrossSeed?: boolean
  groupId?: string
  atomic?: "all"
  condition?: RuleCondition
}

export interface ExternalProgramAction {
  enabled: boolean
  programId: number
  condition?: RuleCondition
}

export interface ActionConditions {
  schemaVersion: string
  grouping?: GroupingConfig
  speedLimits?: SpeedLimitAction
  shareLimits?: ShareLimitsAction
  pause?: PauseAction
  resume?: ResumeAction
  recheck?: RecheckAction
  reannounce?: ReannounceAction
  delete?: DeleteAction
  // Legacy single-tag action (still accepted for existing automations)
  tag?: TagAction
  // Preferred multi-tag actions
  tags?: TagAction[]
  category?: CategoryAction
  move?: MoveAction
  externalProgram?: ExternalProgramAction
}

export type FreeSpaceSource =
  | { type: "qbittorrent" }
  | { type: "path"; path: string }

export type FreeSpaceSourceType = FreeSpaceSource["type"]

export interface Automation {
  id: number
  instanceId: number
  name: string
  trackerPattern: string
  trackerDomains?: string[]
  conditions: ActionConditions
  freeSpaceSource?: FreeSpaceSource
  enabled: boolean
  dryRun: boolean
  sortOrder: number
  intervalSeconds?: number | null // null = use global default (15 minutes)
  createdAt?: string
  updatedAt?: string
}

export interface AutomationInput {
  name: string
  trackerPattern?: string
  trackerDomains?: string[]
  conditions: ActionConditions
  freeSpaceSource?: FreeSpaceSource
  enabled?: boolean
  dryRun?: boolean
  sortOrder?: number
  intervalSeconds?: number | null // null = use global default (15 minutes)
}

export type PreviewView = "needed" | "eligible"

export interface AutomationPreviewInput extends AutomationInput {
  previewLimit?: number
  previewOffset?: number
  previewView?: PreviewView
}

export interface AutomationDryRunResult {
  status: string
  activityIds?: number[]
  activities?: AutomationActivity[]
}

export interface AutomationActivity {
  id: number
  instanceId: number
  hash: string
  torrentName?: string
  trackerDomain?: string
  action: "deleted_ratio" | "deleted_seeding" | "deleted_unregistered" | "deleted_condition" | "delete_failed" | "limit_failed" | "tags_changed" | "category_changed" | "speed_limits_changed" | "share_limits_changed" | "paused" | "resumed" | "rechecked" | "reannounced" | "moved" | "external_program" | "dry_run_no_match"
  ruleId?: number
  ruleName?: string
  outcome: "success" | "failed" | "dry-run"
  reason?: string
  details?: {
    ratio?: number
    ratioLimit?: number
    seedingMinutes?: number
    seedingLimitMinutes?: number
    filesKept?: boolean
    deleteMode?: "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds" | "deleteWithFilesIncludeCrossSeeds"
    limitKiB?: number
    count?: number
    type?: string
    programId?: number
    programName?: string
    // Tag activity details
    added?: Record<string, number>   // tag -> count of torrents
    removed?: Record<string, number> // tag -> count of torrents
    // Category activity details
    categories?: Record<string, number> // category -> count of torrents
    // Speed/share limit activity details
    limits?: Record<string, number> // "upload:1024" -> count, or "2.00:1440" -> count
    // Move activity details
    paths?: Record<string, number> // path -> count of torrents
  }
  createdAt: string
}

export interface AutomationActivityRunItem {
  hash: string
  name: string
  trackerDomain?: string
  tagsAdded?: string[]
  tagsRemoved?: string[]
  category?: string
  movePath?: string
  size?: number
  ratio?: number
  addedOn?: number
  uploadLimitKiB?: number
  downloadLimitKiB?: number
  ratioLimit?: number
  seedingMinutes?: number
}

export interface AutomationActivityRun {
  total: number
  items: AutomationActivityRunItem[]
}

export interface AutomationPreviewTorrent {
  name: string
  hash: string
  size: number
  ratio: number
  seedingTime: number
  tracker: string
  category: string
  tags: string
  state: string
  addedOn: number
  uploaded: number
  downloaded: number
  contentPath?: string
  isUnregistered?: boolean
  isCrossSeed?: boolean
  isHardlinkCopy?: boolean // Included via hardlink expansion (not ContentPath match)
  hardlinkScope?: string // none, torrents_only, outside_qbittorrent
  // Additional fields for dynamic columns
  numSeeds: number
  numComplete: number
  numLeechs: number
  numIncomplete: number
  progress: number
  availability: number
  timeActive: number
  lastActivity: number
  completionOn: number
  totalSize: number
}

export interface AutomationPreviewResult {
  totalMatches: number
  crossSeedCount?: number
  examples: AutomationPreviewTorrent[]
  warnings?: string[] // Warnings explaining why certain operations were skipped
}

export interface RegexValidationError {
  path: string
  message: string
  pattern: string
  field: string
  operator: string
}

export interface RegexValidationResult {
  valid: boolean
  errors: RegexValidationError[]
}

export interface InstanceResponse extends Instance {
  connected: boolean
  hasDecryptionError: boolean
  recentErrors?: InstanceError[]
  connectionStatus?: string
}

export interface InstanceCapabilities {
  supportsTorrentCreation: boolean
  supportsTorrentExport: boolean
  supportsSetTags: boolean
  supportsTrackerHealth: boolean
  supportsTrackerEditing: boolean
  supportsRenameTorrent: boolean
  supportsRenameFile: boolean
  supportsRenameFolder: boolean
  supportsFilePriority: boolean
  supportsSubcategories: boolean
  supportsTorrentTmpPath: boolean
  supportsPathAutocomplete: boolean
  supportsFreeSpacePathSource: boolean
  supportsSetRSSFeedURL: boolean
  webAPIVersion?: string
}

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
  magnet_uri: string
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
  tracker_health?: "unregistered" | "tracker_down"
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
  totalSize: number
  count: number
}

export interface TrackerCustomization {
  id: number
  displayName: string
  domains: string[]
  includedInStats?: string[]
  createdAt: string
  updatedAt: string
}

export interface TrackerCustomizationInput {
  displayName: string
  domains: string[]
  includedInStats?: string[]
}

export interface DashboardSettings {
  id: number
  userId: number
  sectionVisibility: Record<string, boolean>
  sectionOrder: string[]
  sectionCollapsed: Record<string, boolean>
  trackerBreakdownSortColumn: string
  trackerBreakdownSortDirection: string
  trackerBreakdownItemsPerPage: number
  createdAt: string
  updatedAt: string
}

export interface DashboardSettingsInput {
  sectionVisibility?: Record<string, boolean>
  sectionOrder?: string[]
  sectionCollapsed?: Record<string, boolean>
  trackerBreakdownSortColumn?: string
  trackerBreakdownSortDirection?: string
  trackerBreakdownItemsPerPage?: number
}

export interface LogExclusions {
  id: number
  patterns: string[]
  createdAt: string
  updatedAt: string
}

export interface LogExclusionsInput {
  patterns: string[]
}

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

export interface TorrentResponse {
  torrents: Torrent[]
  crossInstanceTorrents?: CrossInstanceTorrent[]
  cross_instance_torrents?: CrossInstanceTorrent[]  // Backend uses snake_case
  total: number
  stats?: TorrentStats
  counts?: TorrentCounts
  categories?: Record<string, Category>
  tags?: string[]
  serverState?: ServerState
  useSubcategories?: boolean
  cacheMetadata?: CacheMetadata
  hasMore?: boolean
  trackerHealthSupported?: boolean
  isCrossInstance?: boolean
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
  ip: string
  port: number
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

export type BackupRunKind = "manual" | "hourly" | "daily" | "weekly" | "monthly" | "import"

export type BackupRunStatus = "pending" | "running" | "success" | "failed" | "canceled"

export interface BackupSettings {
  instanceId: number
  enabled: boolean
  hourlyEnabled: boolean
  dailyEnabled: boolean
  weeklyEnabled: boolean
  monthlyEnabled: boolean
  keepHourly: number
  keepDaily: number
  keepWeekly: number
  keepMonthly: number
  includeCategories: boolean
  includeTags: boolean
  createdAt?: string
  updatedAt?: string
}

export interface BackupRun {
  id: number
  instanceId: number
  kind: BackupRunKind
  status: BackupRunStatus
  requestedBy: string
  requestedAt: string
  startedAt?: string
  completedAt?: string
  manifestPath?: string | null
  totalBytes: number
  torrentCount: number
  categoryCounts?: Record<string, number>
  categories?: Record<string, BackupCategorySnapshot>
  tags?: string[]
  errorMessage?: string | null
  progressCurrent?: number
  progressTotal?: number
  progressPercentage?: number
}

export interface BackupRunsResponse {
  runs: BackupRun[]
  hasMore: boolean
}

export interface BackupManifestItem {
  hash: string
  name: string
  category?: string | null
  sizeBytes: number
  infohashV1?: string | null
  infohashV2?: string | null
  tags?: string[]
  torrentBlob?: string
}

export interface BackupCategorySnapshot {
  savePath?: string | null
}

export interface BackupManifest {
  instanceId: number
  kind: BackupRunKind
  generatedAt: string
  torrentCount: number
  categories?: Record<string, BackupCategorySnapshot>
  tags?: string[]
  items: BackupManifestItem[]
}

export type RestoreMode = "incremental" | "overwrite" | "complete"

export interface RestorePlanCategorySpec {
  name: string
  savePath?: string | null
}

export interface RestorePlanCategoryUpdate {
  name: string
  currentPath: string
  desiredPath: string
}

export interface RestoreDiffChange {
  field: string
  supported: boolean
  current?: unknown
  desired?: unknown
  message?: string
}

export interface RestorePlanTorrentSpec {
  manifest: BackupManifestItem
}

export interface RestorePlanTorrentUpdate {
  hash: string
  current: {
    hash: string
    name: string
    category: string
    tags: string[]
    trackerUrls?: string[]
    infoHashV1?: string
    infoHashV2?: string
    sizeBytes?: number
  }
  desired: BackupManifestItem & { torrentBlob?: string }
  changes: RestoreDiffChange[]
}

export interface RestorePlan {
  mode: RestoreMode
  runId: number
  instanceId: number
  categories: {
    create?: RestorePlanCategorySpec[]
    update?: RestorePlanCategoryUpdate[]
    delete?: string[]
  }
  tags: {
    create?: { name: string }[]
    delete?: string[]
  }
  torrents: {
    add?: RestorePlanTorrentSpec[]
    update?: RestorePlanTorrentUpdate[]
    delete?: string[]
  }
}

export interface RestoreAppliedCategories {
  created?: string[]
  updated?: string[]
  deleted?: string[]
}

export interface RestoreAppliedTags {
  created?: string[]
  deleted?: string[]
}

export interface RestoreAppliedTorrents {
  added?: string[]
  updated?: string[]
  deleted?: string[]
}

export interface RestoreAppliedTotals {
  categories: RestoreAppliedCategories
  tags: RestoreAppliedTags
  torrents: RestoreAppliedTorrents
}

export interface RestoreErrorItem {
  operation: string
  target: string
  message: string
}

export interface RestoreResult {
  mode: RestoreMode
  runId: number
  instanceId: number
  dryRun: boolean
  plan: RestorePlan
  applied: RestoreAppliedTotals
  warnings?: string[]
  errors?: RestoreErrorItem[]
}

export interface AppPreferences {
  // Core limits and speeds (fully supported)
  dl_limit: number
  up_limit: number
  alt_dl_limit: number
  alt_up_limit: number

  // Queue management (fully supported)
  queueing_enabled: boolean
  max_active_downloads: number
  max_active_torrents: number
  max_active_uploads: number
  max_active_checking_torrents: number

  // Network settings (fully supported)
  listen_port: number
  random_port: boolean // Deprecated in qBittorrent but functional
  upnp: boolean
  upnp_lease_duration: number

  // Connection protocol & interface (fully supported)
  bittorrent_protocol: number
  utp_tcp_mixed_mode: number

  // Network interface fields - displayed as read-only in UI
  // TODO: These fields are configurable in qBittorrent API but go-qbittorrent library
  // lacks the required endpoints for proper dropdown selection:
  // - /api/v2/app/networkInterfaceList
  // - /api/v2/app/networkInterfaceAddressList
  // Currently shown as read-only inputs displaying actual qBittorrent values
  current_network_interface: string // Shows current interface (empty = auto-detect)
  current_interface_address: string // Shows current interface IP address

  announce_ip: string
  reannounce_when_address_changed: boolean

  // Connection limits
  max_connec: number
  max_connec_per_torrent: number
  max_uploads: number
  max_uploads_per_torrent: number
  enable_multi_connections_from_same_ip: boolean

  // Advanced network
  outgoing_ports_min: number
  outgoing_ports_max: number
  limit_lan_peers: boolean
  limit_tcp_overhead: boolean
  limit_utp_rate: boolean
  peer_tos: number
  socket_backlog_size: number
  send_buffer_watermark: number
  send_buffer_low_watermark: number
  send_buffer_watermark_factor: number
  max_concurrent_http_announces: number
  request_queue_size: number
  stop_tracker_timeout: number

  // Seeding limits
  max_ratio_enabled: boolean
  max_ratio: number
  max_seeding_time_enabled: boolean
  max_seeding_time: number

  // Paths and file management
  save_path: string
  temp_path: string
  temp_path_enabled: boolean
  auto_tmm_enabled: boolean
  save_resume_data_interval: number

  // Startup behavior
  start_paused_enabled: boolean // NOTE: Not supported by qBittorrent API - handled via localStorage

  // BitTorrent protocol (fully supported)
  dht: boolean
  pex: boolean
  lsd: boolean
  encryption: number
  anonymous_mode: boolean

  // Proxy settings (fully supported)
  proxy_type: number | string // Note: number (pre-4.5.x), string (post-4.6.x)
  proxy_ip: string
  proxy_port: number
  proxy_username: string
  proxy_password: string
  proxy_auth_enabled: boolean
  proxy_peer_connections: boolean
  proxy_torrents_only: boolean
  proxy_hostname_lookup: boolean

  // Security & filtering
  ip_filter_enabled: boolean
  ip_filter_path: string
  ip_filter_trackers: boolean
  banned_IPs: string
  block_peers_on_privileged_ports: boolean
  resolve_peer_countries: boolean

  // Performance & disk I/O (mostly supported)
  async_io_threads: number
  hashing_threads: number
  file_pool_size: number
  disk_cache: number
  disk_cache_ttl: number
  disk_queue_size: number
  disk_io_type: number
  disk_io_read_mode: number // Limited API support
  disk_io_write_mode: number
  checking_memory_use: number
  memory_working_set_limit: number // May not be settable via API
  enable_coalesce_read_write: boolean

  // Upload behavior (partial support)
  upload_choking_algorithm: number
  upload_slots_behavior: number

  // Peer management
  peer_turnover: number
  peer_turnover_cutoff: number
  peer_turnover_interval: number

  // Embedded tracker
  enable_embedded_tracker: boolean
  embedded_tracker_port: number
  embedded_tracker_port_forwarding: boolean

  // Scheduler
  scheduler_enabled: boolean
  schedule_from_hour: number
  schedule_from_min: number
  schedule_to_hour: number
  schedule_to_min: number
  scheduler_days: number

  // Web UI (read-only reference)
  web_ui_port: number
  web_ui_username: string
  use_https: boolean
  web_ui_address: string
  web_ui_ban_duration: number
  web_ui_clickjacking_protection_enabled: boolean
  web_ui_csrf_protection_enabled: boolean
  web_ui_custom_http_headers: string
  web_ui_domain_list: string
  web_ui_host_header_validation_enabled: boolean
  web_ui_https_cert_path: string
  web_ui_https_key_path: string
  web_ui_max_auth_fail_count: number
  web_ui_reverse_proxies_list: string
  web_ui_reverse_proxy_enabled: boolean
  web_ui_secure_cookie_enabled: boolean
  web_ui_session_timeout: number
  web_ui_upnp: boolean
  web_ui_use_custom_http_headers_enabled: boolean

  // Additional commonly used fields
  add_trackers_enabled: boolean
  add_trackers: string
  announce_to_all_tiers: boolean
  announce_to_all_trackers: boolean

  // File management and content layout
  torrent_content_layout: string
  incomplete_files_ext: boolean
  preallocate_all: boolean
  excluded_file_names_enabled: boolean
  excluded_file_names: string

  // Category behavior
  category_changed_tmm_enabled: boolean
  save_path_changed_tmm_enabled: boolean
  use_category_paths_in_manual_mode: boolean
  // Subcategory behavior
  use_subcategories?: boolean

  // Torrent behavior
  torrent_changed_tmm_enabled: boolean
  torrent_stop_condition: string

  // Miscellaneous
  alternative_webui_enabled: boolean
  alternative_webui_path: string
  auto_delete_mode: number
  autorun_enabled: boolean
  autorun_on_torrent_added_enabled: boolean
  autorun_on_torrent_added_program: string
  autorun_program: string
  bypass_auth_subnet_whitelist: string
  bypass_auth_subnet_whitelist_enabled: boolean
  bypass_local_auth: boolean
  dont_count_slow_torrents: boolean
  export_dir: string
  export_dir_fin: string
  idn_support_enabled: boolean
  locale: string
  performance_warning: boolean
  recheck_completed_torrents: boolean
  refresh_interval: number
  resume_data_storage_type: string
  slow_torrent_dl_rate_threshold: number
  slow_torrent_inactive_timer: number
  slow_torrent_ul_rate_threshold: number
  ssrf_mitigation: boolean
  validate_https_tracker_certificate: boolean

  // RSS settings
  rss_auto_downloading_enabled: boolean
  rss_download_repack_proper_episodes: boolean
  rss_max_articles_per_feed: number
  rss_processing_enabled: boolean
  rss_refresh_interval: number
  rss_smart_episode_filters: string

  // Dynamic DNS
  dyndns_domain: string
  dyndns_enabled: boolean
  dyndns_password: string
  dyndns_service: number
  dyndns_username: string

  // Mail notifications
  mail_notification_auth_enabled: boolean
  mail_notification_email: string
  mail_notification_enabled: boolean
  mail_notification_password: string
  mail_notification_sender: string
  mail_notification_smtp: string
  mail_notification_ssl_enabled: boolean
  mail_notification_username: string

  // Scan directories (structured as empty object in go-qbittorrent)
  scan_dirs: Record<string, unknown>

  // Add catch-all for any additional fields from the API
  [key: string]: unknown
}

// qBittorrent application information
export interface QBittorrentBuildInfo {
  qt: string
  libtorrent: string
  boost: string
  openssl: string
  bitness: number
  platform?: string
}

export interface QBittorrentAppInfo {
  version: string
  webAPIVersion?: string
  buildInfo?: QBittorrentBuildInfo
}

// Torrent Creation Types
export type TorrentFormat = "v1" | "v2" | "hybrid"
export type TorrentCreationStatus = "Queued" | "Running" | "Finished" | "Failed"

export interface TorrentCreationParams {
  sourcePath: string
  torrentFilePath?: string
  private?: boolean
  format?: TorrentFormat
  optimizeAlignment?: boolean
  paddedFileSizeLimit?: number
  pieceSize?: number
  comment?: string
  source?: string
  trackers?: string[]
  urlSeeds?: string[]
  startSeeding?: boolean
}

export interface TorrentCreationTask {
  taskID: string
  sourcePath: string
  torrentFilePath?: string
  pieceSize: number
  private: boolean
  format?: TorrentFormat
  optimizeAlignment?: boolean
  paddedFileSizeLimit?: number
  status: TorrentCreationStatus
  comment?: string
  source?: string
  trackers?: string[]
  urlSeeds?: string[]
  timeAdded: string
  timeStarted?: string
  timeFinished?: string
  progress?: number
  errorMessage?: string
}

export interface TorrentCreationTaskResponse {
  taskID: string
}

// External Program Types
export interface PathMapping {
  from: string
  to: string
}

export interface ExternalProgram {
  id: number
  name: string
  path: string
  args_template: string
  enabled: boolean
  use_terminal: boolean
  path_mappings: PathMapping[]
  created_at: string
  updated_at: string
}

export interface ExternalProgramCreate {
  name: string
  path: string
  args_template: string
  enabled: boolean
  use_terminal: boolean
  path_mappings: PathMapping[]
}

export interface ExternalProgramUpdate {
  name: string
  path: string
  args_template: string
  enabled: boolean
  use_terminal: boolean
  path_mappings: PathMapping[]
}

export interface ExternalProgramExecute {
  program_id: number
  instance_id: number
  hashes: string[]
}

export interface ExternalProgramExecuteResult {
  hash: string
  success: boolean
  stdout?: string
  stderr?: string
  error?: string
}

export interface ExternalProgramExecuteResponse {
  results: ExternalProgramExecuteResult[]
}

export interface NotificationEventDefinition {
  type: string
  label: string
  description: string
}

export interface NotificationTarget {
  id: number
  name: string
  url: string
  enabled: boolean
  eventTypes: string[]
  createdAt: string
  updatedAt: string
}

export interface NotificationTargetRequest {
  name: string
  url: string
  enabled: boolean
  eventTypes: string[]
}

export interface NotificationTestRequest {
  title?: string
  message?: string
}

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

export interface CrossSeedSearchResult {
  torrentHash: string
  torrentName: string
  indexerName: string
  releaseTitle: string
  added: boolean
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
}
// Orphan Scan types
export type OrphanScanRunStatus =
  | "pending"
  | "scanning"
  | "preview_ready"
  | "deleting"
  | "completed"
  | "failed"
  | "canceled"

export type OrphanScanTriggerType = "manual" | "scheduled"

export type OrphanScanFileStatus = "pending" | "deleted" | "skipped" | "failed"

export type OrphanScanPreviewSort = "size_desc" | "directory_size_desc"

export interface OrphanScanSettings {
  id?: number
  instanceId: number
  enabled: boolean
  gracePeriodMinutes: number
  ignorePaths: string[]
  scanIntervalHours: number
  previewSort: OrphanScanPreviewSort
  maxFilesPerRun: number
  autoCleanupEnabled: boolean
  autoCleanupMaxFiles: number
  createdAt?: string
  updatedAt?: string
}

export interface OrphanScanSettingsUpdate {
  enabled?: boolean
  gracePeriodMinutes?: number
  ignorePaths?: string[]
  scanIntervalHours?: number
  previewSort?: OrphanScanPreviewSort
  maxFilesPerRun?: number
  autoCleanupEnabled?: boolean
  autoCleanupMaxFiles?: number
}

export interface OrphanScanRun {
  id: number
  instanceId: number
  status: OrphanScanRunStatus
  triggeredBy: OrphanScanTriggerType
  scanPaths: string[]
  filesFound: number
  filesDeleted: number
  foldersDeleted: number
  bytesReclaimed: number
  truncated: boolean
  errorMessage?: string | null
  startedAt: string
  completedAt?: string | null
}

export interface OrphanScanFile {
  id: number
  runId: number
  filePath: string
  fileSize: number
  modifiedAt?: string | null
  status: OrphanScanFileStatus
  errorMessage?: string | null
}

export interface OrphanScanRunWithFiles extends OrphanScanRun {
  files: OrphanScanFile[]
}

// Log Settings Types
export interface LogSettings {
  level: string
  path: string
  maxSize: number
  maxBackups: number
  configPath?: string
  locked?: Record<string, string>
}

export interface LogSettingsUpdate {
  level?: string
  path?: string
  maxSize?: number
  maxBackups?: number
}

// Directory Scanner Types
export type DirScanMatchMode = "strict" | "flexible"

export type DirScanFileStatus =
  | "pending"
  | "matched"
  | "no_match"
  | "error"
  | "already_seeding"
  | "in_qbittorrent"

export type DirScanRunStatus =
  | "queued"
  | "scanning"
  | "searching"
  | "injecting"
  | "success"
  | "failed"
  | "canceled"

export interface DirScanSettings {
  id: number
  enabled: boolean
  matchMode: DirScanMatchMode
  sizeTolerancePercent: number
  minPieceRatio: number
  maxSearcheesPerRun: number
  allowPartial: boolean
  skipPieceBoundarySafetyCheck: boolean
  startPaused: boolean
  category: string
  tags: string[]
  createdAt: string
  updatedAt: string
}

export interface DirScanSettingsUpdate {
  enabled?: boolean
  matchMode?: DirScanMatchMode
  sizeTolerancePercent?: number
  minPieceRatio?: number
  maxSearcheesPerRun?: number
  allowPartial?: boolean
  skipPieceBoundarySafetyCheck?: boolean
  startPaused?: boolean
  category?: string
  tags?: string[]
}

export interface DirScanDirectory {
  id: number
  path: string
  qbitPathPrefix?: string
  category?: string
  tags: string[]
  enabled: boolean
  arrInstanceId?: number
  targetInstanceId: number
  scanIntervalMinutes: number
  lastScanAt?: string
  createdAt: string
  updatedAt: string
}

export interface DirScanDirectoryCreate {
  path: string
  qbitPathPrefix?: string
  category?: string
  tags?: string[]
  enabled?: boolean
  arrInstanceId?: number
  targetInstanceId: number
  scanIntervalMinutes?: number
}

export interface DirScanDirectoryUpdate {
  path?: string
  qbitPathPrefix?: string
  category?: string
  tags?: string[]
  enabled?: boolean
  arrInstanceId?: number
  targetInstanceId?: number
  scanIntervalMinutes?: number
}

export interface DirScanRun {
  id: number
  directoryId: number
  status: DirScanRunStatus
  triggeredBy: string
  filesFound: number
  filesSkipped: number
  matchesFound: number
  torrentsAdded: number
  errorMessage?: string
  startedAt: string
  completedAt?: string
}

export type DirScanRunInjectionStatus = "added" | "failed"

export interface DirScanRunInjection {
  id: number
  runId: number
  directoryId: number
  status: DirScanRunInjectionStatus
  searcheeName: string
  torrentName: string
  infoHash: string
  contentType: string
  indexerName?: string
  trackerDomain?: string
  trackerDisplayName?: string
  linkMode?: string
  savePath?: string
  category?: string
  tags: string[]
  errorMessage?: string
  createdAt: string
}

export interface DirScanFile {
  id: number
  directoryId: number
  filePath: string
  fileSize: number
  fileModTime: string
  status: DirScanFileStatus
  matchedTorrentHash?: string
  matchedIndexerId?: number
  lastProcessedAt?: string
}

// RSS Types

export interface RSSArticle {
  id: string
  date: string
  title: string
  author?: string
  description?: string
  torrentURL?: string
  link?: string
  isRead: boolean
}

export interface RSSFeed {
  uid: string
  url: string
  title?: string
  lastBuildDate?: string
  hasError: boolean
  isLoading: boolean
  articles?: RSSArticle[]
}

// Hierarchical structure returned by qBittorrent
export interface RSSItems {
  [key: string]: RSSFeed | RSSItems
}

// Helper type to distinguish feeds from folders
export function isRSSFeed(item: RSSFeed | RSSItems): item is RSSFeed {
  return "url" in item && typeof item.url === "string"
}

export interface RSSRuleTorrentParams {
  category?: string
  tags?: string[]
  save_path?: string
  download_path?: string
  content_layout?: string
  operating_mode?: string
  skip_checking?: boolean
  upload_limit?: number
  download_limit?: number
  seeding_time_limit?: number
  inactive_seeding_time_limit?: number
  share_limit_action?: string
  ratio_limit?: number
  stopped?: boolean
  stop_condition?: string
  use_auto_tmm?: boolean
  use_download_path?: boolean
  add_to_top_of_queue?: boolean
}

export interface RSSAutoDownloadRule {
  enabled: boolean
  priority: number
  useRegex: boolean
  mustContain: string
  mustNotContain: string
  episodeFilter?: string
  affectedFeeds: string[]
  lastMatch?: string
  ignoreDays: number
  smartFilter: boolean
  previouslyMatchedEpisodes?: string[]
  torrentParams?: RSSRuleTorrentParams
  // Legacy fields
  addPaused?: boolean | null
  savePath?: string
  assignedCategory?: string
  torrentContentLayout?: string
}

export type RSSRules = Record<string, RSSAutoDownloadRule>

export type RSSMatchingArticles = Record<string, string[]>

// Request types for RSS API
export interface AddRSSFolderRequest {
  path: string
}

export interface AddRSSFeedRequest {
  url: string
  path?: string
}

export interface SetRSSFeedURLRequest {
  path: string
  url: string
}

export interface MoveRSSItemRequest {
  itemPath: string
  destPath?: string // Optional: empty moves item to root
}

export interface RemoveRSSItemRequest {
  path: string
}

export interface RefreshRSSItemRequest {
  itemPath: string
}

export interface MarkRSSAsReadRequest {
  itemPath: string
  articleId?: string
}

export interface SetRSSRuleRequest {
  name: string
  rule: RSSAutoDownloadRule
}

export interface RenameRSSRuleRequest {
  newName: string
}
