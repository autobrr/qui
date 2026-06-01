/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
  // System Time fields
  | "SYSTEM_HOUR"
  | "SYSTEM_MINUTE"
  | "SYSTEM_DAY_OF_WEEK"
  | "SYSTEM_DAY"
  | "SYSTEM_MONTH"
  | "SYSTEM_YEAR"
  // Numeric fields (float64)
  | "RATIO"
  | "RATIO_LIMIT"
  | "MAX_RATIO"
  | "UPLOADED_OVER_SIZE"
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
  | "EXISTS_ON_OTHER_INSTANCE"
  | "SEEDING_ON_OTHER_INSTANCE"
  | "EXISTS_ON_SAME_INSTANCE"
  | "SEEDING_ON_SAME_INSTANCE"
  // Enum-like fields
  | "HARDLINK_SCOPE"
  | "HARDLINK_SCOPE_CROSS"

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
  shareLimitAction?: string
  shareLimitsMode?: string
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

export interface AutoManagementAction {
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

export interface ExportToInstanceAction {
  enabled: boolean
  targetInstanceId: number
  savePath: string
  category?: string
  tags?: string[]
  paused?: boolean
  skipChecking?: boolean
  contentLayout?: string
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
  autoManagement?: AutoManagementAction
  exportToInstance?: ExportToInstanceAction
}

export type FreeSpaceSource =
  | { type: "qbittorrent" }
  | { type: "path"; path: string }

export type FreeSpaceSourceType = FreeSpaceSource["type"]

export type ScoreRuleType = "field_multiplier" | "conditional"

export interface FieldMultiplierScoreRule {
  field: ConditionField
  multiplier: number
}

export interface ConditionalScoreRule {
  condition: RuleCondition
  score: number
}

export type ScoreRule =
  | { type: "field_multiplier"; fieldMultiplier: FieldMultiplierScoreRule }
  | { type: "conditional"; conditional: ConditionalScoreRule }

export type SortingConfig =
  | {
    schemaVersion: string
    type: "simple"
    field: ConditionField
    direction: "ASC" | "DESC"
  }
  | {
    schemaVersion: string
    type: "score"
    direction: "ASC" | "DESC"
    scoreRules: ScoreRule[]
  }

export interface Automation {
  id: number
  instanceId: number
  name: string
  trackerPattern: string
  trackerDomains?: string[]
  conditions: ActionConditions
  freeSpaceSource?: FreeSpaceSource
  sortingConfig?: SortingConfig
  enabled: boolean
  dryRun: boolean
  notify: boolean
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
  sortingConfig?: SortingConfig
  enabled?: boolean
  dryRun?: boolean
  notify?: boolean
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
  action: "deleted_ratio" | "deleted_seeding" | "deleted_unregistered" | "deleted_condition" | "delete_failed" | "limit_failed" | "tags_changed" | "category_changed" | "speed_limits_changed" | "share_limits_changed" | "paused" | "resumed" | "rechecked" | "reannounced" | "auto_managed" | "moved" | "external_program" | "exported_to_instance" | "dry_run_no_match"
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
  hardlinkCrossScope?: string // cross-instance: none, torrents_only, outside_qbittorrent
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
  score?: number
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
