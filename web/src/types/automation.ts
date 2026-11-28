/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type {
  ConditionField,
  ConditionOperator,
  ContentLayout,
  DeleteMode,
  GroupAmbiguousPolicy,
  GroupKey,
  ShareLimitAction,
  ShareLimitsMode,
  SortDirection,
  TagMode,
  TransferAfter,
  TransferWayChoice
} from "./closed-sets.generated"

export interface RuleCondition {
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
  shareLimitAction?: ShareLimitAction
  shareLimitsMode?: ShareLimitsMode
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
  keys: GroupKey[]
  ambiguousPolicy?: GroupAmbiguousPolicy
  minFileOverlapPercent?: number
}

export interface GroupingConfig {
  defaultGroupId?: string
  groups?: GroupDefinition[]
}

export interface DeleteAction {
  enabled: boolean
  mode?: DeleteMode
  includeHardlinks?: boolean
  groupId?: string
  atomic?: "all"
  unregisteredWaitSeconds?: number
  condition?: RuleCondition
}

export interface TagAction {
  enabled: boolean
  tags: string[]
  mode: TagMode
  includeCrossSeeds?: boolean
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
  contentLayout?: ContentLayout
  condition?: RuleCondition
}

export interface TransferToInstanceAction {
  enabled: boolean
  targetInstanceId: number
  savePath: string
  category?: string
  tags?: string[]
  after?: TransferAfter
  leaveStopped?: boolean
  way?: TransferWayChoice
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
  tag?: TagAction
  tags?: TagAction[]
  category?: CategoryAction
  move?: MoveAction
  externalProgram?: ExternalProgramAction
  autoManagement?: AutoManagementAction
  exportToInstance?: ExportToInstanceAction
  transferToInstance?: TransferToInstanceAction
}

export type FreeSpaceSource =
  | { type: "qbittorrent" }
  | { type: "path"; path: string }
  | { type: "allowance"; path: string; allowanceBytes: number }
  | { type: "qbitPath"; path: string }

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
    direction: SortDirection
  }
  | {
    schemaVersion: string
    type: "score"
    direction: SortDirection
    scoreRules: ScoreRule[]
  }

export interface Automation {
  id: number
  instanceId: number
  name: string
  trackerPattern: string
  conditions: ActionConditions
  freeSpaceSource?: FreeSpaceSource
  sortingConfig?: SortingConfig
  enabled: boolean
  dryRun: boolean
  notify: boolean
  sortOrder: number
  intervalSeconds?: number | null
  createdAt?: string
  updatedAt?: string
}

export interface AutomationInput {
  name: string
  trackerPattern?: string
  conditions: ActionConditions
  freeSpaceSource?: FreeSpaceSource
  sortingConfig?: SortingConfig
  enabled?: boolean
  dryRun?: boolean
  notify?: boolean
  sortOrder?: number
  intervalSeconds?: number | null
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
  action: "deleted_ratio" | "deleted_seeding" | "deleted_unregistered" | "deleted_condition" | "delete_failed" | "limit_failed" | "tags_changed" | "category_changed" | "speed_limits_changed" | "share_limits_changed" | "paused" | "resumed" | "rechecked" | "reannounced" | "auto_managed" | "moved" | "external_program" | "exported_to_instance" | "transferred_to_instance" | "dry_run_no_match" | "delete_waiting" | "unregistered_wait_started" | "unregistered_wait_ended" | "rule_skipped"
  ruleId?: number
  ruleName?: string
  outcome: "success" | "failed" | "dry-run" | "skipped"
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
    field?: string
    requires?: string
    programId?: number
    programName?: string
    added?: Record<string, number>
    removed?: Record<string, number>
    categories?: Record<string, number>
    limits?: Record<string, number>
    paths?: Record<string, number>
    rules?: { id: number; name: string; count: number }[]
    causes?: { stopped: number; trackerWorks: number }
    unregistered?: AutomationUnregisteredFacts
  }
  createdAt: string
}

export interface AutomationUnregisteredFacts {
  since?: string
  host?: string
  status?: number
  message?: string
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
  isHardlinkCopy?: boolean
  hardlinkScope?: string
  hardlinkCrossScope?: string
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
  waitsUntil?: number
}

export interface AutomationRuleSkip {
  field: string
  requires: string
}

export interface AutomationPreviewResult {
  totalMatches: number
  waitingCount?: number
  crossSeedCount?: number
  examples: AutomationPreviewTorrent[]
  skipped?: AutomationRuleSkip
  warnings?: string[]
}

export interface PendingDeleteTorrent {
  hash: string
  name: string
  tracker: string
  message: string
  unregisteredSince: number
  deletesAt: number
  awaitsTracker: boolean
}

export interface PendingDeleteGroup {
  ruleId: number
  ruleName: string
  torrents: PendingDeleteTorrent[]
}

export interface PendingDeleteList {
  rules: PendingDeleteGroup[]
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
