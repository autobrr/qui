/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FieldCombobox } from "@/components/query-builder/FieldCombobox"
import { Badge } from "@/components/ui/badge"
import { QueryBuilder, type GroupOption } from "@/components/query-builder"
import {
  CONDITION_FIELDS,
  CATEGORY_UNCATEGORIZED_VALUE,
  CAPABILITY_REASONS,
  FIELD_REQUIREMENTS,
  STATE_VALUE_REQUIREMENTS,
  type Capabilities,
  type DisabledField,
  type DisabledStateValue
} from "@/components/query-builder/constants"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect, type Option } from "@/components/ui/multi-select"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { useIndexerTrackerDomains } from "@/hooks/useIndexerTrackerDomains"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { useInstances } from "@/hooks/useInstances"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { buildTrackerCustomizationMaps, useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { withBasePath } from "@/lib/base-url"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { type CsvColumn, downloadBlob, toCsv } from "@/lib/csv-export"
import { pickTrackerIconDomain } from "@/lib/tracker-icons"
import { getTrackerMatchMode, getTrackerTokens, type TrackerMatchMode } from "@/lib/workflow-utils"
import { cn, formatBytes, normalizeTrackerDomains } from "@/lib/utils"
import type {
  ActionConditions,
  Automation,
  AutomationActivity,
  AutomationInput,
  AutomationPreviewResult,
  AutomationPreviewTorrent,
  GroupDefinition,
  GroupingConfig,
  PreviewView,
  RegexValidationError,
  RuleCondition,
  ScoreRule,
  ConditionField,
  SortingConfig
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Folder, Info, Loader2, Plus, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { AutomationActivityRunDialog } from "./AutomationActivityRunDialog"
import { WorkflowPreviewDialog } from "./WorkflowPreviewDialog"

let ruleIdCounter = 0

interface WorkflowDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  /** Rule to edit, or null to create a new rule */
  rule: Automation | null
  onSuccess?: () => void
}

// Speed units for display - storage is always KiB/s
const SPEED_LIMIT_UNITS = [
  { value: 1, label: "KiB/s" },
  { value: 1024, label: "MiB/s" },
]

const CONTENT_LAYOUT_OPTIONS = [
  { value: "Original", labelKey: "preferences.workflowDialog.contentLayout.original" },
  { value: "Subfolder", labelKey: "preferences.workflowDialog.contentLayout.subfolder" },
  { value: "NoSubfolder", labelKey: "preferences.workflowDialog.contentLayout.noSubfolder" },
] as const

const CONTENT_LAYOUT_VALUES = CONTENT_LAYOUT_OPTIONS.map(o => o.value)

type ActionType = "speedLimits" | "shareLimits" | "pause" | "resume" | "recheck" | "reannounce" | "autoManagement" | "delete" | "tag" | "category" | "move" | "externalProgram" | "exportToInstance"

// Actions that can be combined (Delete must be standalone)
const COMBINABLE_ACTIONS: ActionType[] = ["speedLimits", "shareLimits", "pause", "resume", "recheck", "reannounce", "autoManagement", "tag", "category", "move", "externalProgram", "exportToInstance"]

const ACTION_LABEL_KEYS: Record<ActionType, string> = {
  speedLimits: "preferences.workflowDialog.actions.speedLimits",
  shareLimits: "preferences.workflowDialog.actions.shareLimits",
  pause: "preferences.workflowDialog.actions.pause",
  resume: "preferences.workflowDialog.actions.resume",
  recheck: "preferences.workflowDialog.actions.recheck",
  reannounce: "preferences.workflowDialog.actions.reannounce",
  autoManagement: "preferences.workflowDialog.actions.autoManagement",
  delete: "preferences.workflowDialog.actions.delete",
  tag: "preferences.workflowDialog.actions.tag",
  category: "preferences.workflowDialog.actions.category",
  move: "preferences.workflowDialog.actions.move",
  externalProgram: "preferences.workflowDialog.actions.externalProgram",
  exportToInstance: "preferences.workflowDialog.actions.exportToInstance",
}

const DRY_RUN_ACTION_LABEL_KEYS: Record<AutomationActivity["action"], string> = {
  deleted_ratio: "preferences.workflowDialog.dryRun.actions.deletedRatio",
  deleted_seeding: "preferences.workflowDialog.dryRun.actions.deletedSeeding",
  deleted_unregistered: "preferences.workflowDialog.dryRun.actions.deletedUnregistered",
  deleted_condition: "preferences.workflowDialog.dryRun.actions.deletedCondition",
  delete_failed: "preferences.workflowDialog.dryRun.actions.deleteFailed",
  limit_failed: "preferences.workflowDialog.dryRun.actions.limitFailed",
  tags_changed: "preferences.workflowDialog.dryRun.actions.tagsChanged",
  category_changed: "preferences.workflowDialog.dryRun.actions.categoryChanged",
  speed_limits_changed: "preferences.workflowDialog.dryRun.actions.speedLimitsChanged",
  share_limits_changed: "preferences.workflowDialog.dryRun.actions.shareLimitsChanged",
  paused: "preferences.workflowDialog.dryRun.actions.paused",
  resumed: "preferences.workflowDialog.dryRun.actions.resumed",
  rechecked: "preferences.workflowDialog.dryRun.actions.rechecked",
  reannounced: "preferences.workflowDialog.dryRun.actions.reannounced",
  auto_managed: "preferences.workflowDialog.dryRun.actions.autoManaged",
  moved: "preferences.workflowDialog.dryRun.actions.moved",
  external_program: "preferences.workflowDialog.dryRun.actions.externalProgram",
  exported_to_instance: "preferences.workflowDialog.dryRun.actions.exportedToInstance",
  dry_run_no_match: "preferences.workflowDialog.dryRun.actions.noMatches",
}

function sumDetailsRecord(values: Record<string, number> | undefined): number {
  return Object.values(values ?? {}).reduce((sum, value) => {
    const asNumber = typeof value === "number" ? value : Number(value)
    return sum + (Number.isFinite(asNumber) ? asNumber : 0)
  }, 0)
}

function getDryRunImpactCount(event: AutomationActivity): number {
  const details = event.details
  switch (event.action) {
    case "tags_changed":
      return sumDetailsRecord(details?.added) + sumDetailsRecord(details?.removed)
    case "category_changed":
      return sumDetailsRecord(details?.categories)
    case "speed_limits_changed":
    case "share_limits_changed":
      return sumDetailsRecord(details?.limits)
    case "moved":
      return sumDetailsRecord(details?.paths)
    case "dry_run_no_match":
      return 0
    default:
      return typeof details?.count === "number" ? details.count : 0
  }
}

function formatDryRunEventSummary(
  event: AutomationActivity,
  t: ReturnType<typeof useTranslation<"instances">>["t"]
): string {
  const details = event.details
  switch (event.action) {
    case "tags_changed": {
      const added = sumDetailsRecord(details?.added)
      const removed = sumDetailsRecord(details?.removed)
      if (added > 0 && removed > 0) return t("preferences.workflowDialog.dryRun.summary.tagsChangedBoth", { added, removed })
      if (added > 0) return t("preferences.workflowDialog.dryRun.summary.tagsAdded", { count: added })
      if (removed > 0) return t("preferences.workflowDialog.dryRun.summary.tagsRemoved", { count: removed })
      return t("preferences.workflowDialog.dryRun.summary.noTagChanges")
    }
    case "category_changed": {
      const moved = sumDetailsRecord(details?.categories)
      return t("preferences.workflowDialog.dryRun.summary.categoryChanged", { count: moved })
    }
    case "speed_limits_changed":
    case "share_limits_changed": {
      const limited = sumDetailsRecord(details?.limits)
      return t("preferences.workflowDialog.dryRun.summary.updated", { count: limited })
    }
    case "moved": {
      const moved = sumDetailsRecord(details?.paths)
      return t("preferences.workflowDialog.dryRun.summary.moved", { count: moved })
    }
    case "dry_run_no_match":
      return t("preferences.workflowDialog.dryRun.summary.noMatches")
    case "paused":
    case "resumed":
    case "rechecked":
    case "reannounced":
    case "auto_managed":
    case "external_program":
    case "exported_to_instance":
    case "deleted_ratio":
    case "deleted_seeding":
    case "deleted_unregistered":
    case "deleted_condition": {
      const count = typeof details?.count === "number" ? details.count : 0
      return t("preferences.workflowDialog.dryRun.summary.impacted", { count })
    }
    default:
      return t("preferences.workflowDialog.dryRun.summary.completed")
  }
}

function getDisabledFields(capabilities: Capabilities): DisabledField[] {
  return Object.entries(FIELD_REQUIREMENTS)
    .filter(([, capability]) => !capabilities[capability as keyof Capabilities])
    .map(([field, capability]) => ({ field, reason: CAPABILITY_REASONS[capability] }))
}

function getDisabledStateValues(capabilities: Capabilities): DisabledStateValue[] {
  return Object.entries(STATE_VALUE_REQUIREMENTS)
    .filter(([, capability]) => !capabilities[capability as keyof Capabilities])
    .map(([value, capability]) => ({ value, reason: CAPABILITY_REASONS[capability] }))
}

const SIMPLE_SORT_FIELD_SET = new Set<ConditionField>([
  "SIZE",
  "TOTAL_SIZE",
  "DOWNLOADED",
  "UPLOADED",
  "AMOUNT_LEFT",
  "FREE_SPACE",
  "ADDED_ON",
  "COMPLETION_ON",
  "LAST_ACTIVITY",
  "SEEDING_TIME",
  "TIME_ACTIVE",
  "ADDED_ON_AGE",
  "COMPLETION_ON_AGE",
  "LAST_ACTIVITY_AGE",
  "RATIO",
  "PROGRESS",
  "AVAILABILITY",
  "DL_SPEED",
  "UP_SPEED",
  "NUM_SEEDS",
  "NUM_LEECHS",
  "NUM_COMPLETE",
  "NUM_INCOMPLETE",
  "TRACKERS_COUNT",
  "NAME",
  "CATEGORY",
  "TAGS",
  "TRACKER",
  "STATE",
  "SAVE_PATH",
  "CONTENT_PATH",
  "COMMENT",
])

const SCORE_MULTIPLIER_FIELD_SET = new Set<ConditionField>([
  "SIZE",
  "TOTAL_SIZE",
  "DOWNLOADED",
  "UPLOADED",
  "AMOUNT_LEFT",
  "FREE_SPACE",
  "ADDED_ON",
  "COMPLETION_ON",
  "LAST_ACTIVITY",
  "SEEDING_TIME",
  "TIME_ACTIVE",
  "ADDED_ON_AGE",
  "COMPLETION_ON_AGE",
  "LAST_ACTIVITY_AGE",
  "RATIO",
  "PROGRESS",
  "AVAILABILITY",
  "DL_SPEED",
  "UP_SPEED",
  "NUM_SEEDS",
  "NUM_LEECHS",
  "NUM_COMPLETE",
  "NUM_INCOMPLETE",
  "TRACKERS_COUNT",
])

const SIMPLE_SORT_DISABLED_FIELDS = Object.keys(CONDITION_FIELDS)
  .filter(field => !SIMPLE_SORT_FIELD_SET.has(field as ConditionField))
  .map(field => ({ field, reason: "Not supported for simple sorting" }))

const SCORE_MULTIPLIER_DISABLED_FIELDS = Object.keys(CONDITION_FIELDS)
  .filter(field => !SCORE_MULTIPLIER_FIELD_SET.has(field as ConditionField))
  .map(field => ({ field, reason: "Not supported for score multipliers" }))

function isSupportedSimpleSortField(field: string): field is ConditionField {
  return SIMPLE_SORT_FIELD_SET.has(field as ConditionField)
}

function isSupportedScoreMultiplierField(field: string): field is ConditionField {
  return SCORE_MULTIPLIER_FIELD_SET.has(field as ConditionField)
}



/**
 * Recursively checks if a condition tree uses a specific field.
 * Used to validate that FREE_SPACE conditions aren't paired with keep-files mode.
 */
function conditionUsesField(condition: RuleCondition | null | undefined, field: string): boolean {
  if (!condition) return false
  if (condition.field === field) return true
  if (condition.conditions) {
    return condition.conditions.some(c => conditionUsesField(c, field))
  }
  return false
}

/**
 * Available keys for custom group definitions
 */
const AVAILABLE_GROUP_KEYS = [
  "contentPath",
  "savePath",
  "effectiveName",
  "contentType",
  "tracker",
  "rlsSource",
  "rlsResolution",
  "rlsCodec",
  "rlsHDR",
  "rlsAudio",
  "rlsChannels",
  "rlsGroup",
  "hardlinkSignature",
] as const

/**
 * Built-in group IDs with descriptions
 */
const BUILTIN_GROUPS = [
  {
    id: "cross_seed_content_path",
    labelKey: "preferences.workflowDialog.grouping.builtIn.crossSeedContentPath.label",
    descriptionKey: "preferences.workflowDialog.grouping.builtIn.crossSeedContentPath.description",
  },
  {
    id: "cross_seed_content_save_path",
    labelKey: "preferences.workflowDialog.grouping.builtIn.crossSeedContentSavePath.label",
    descriptionKey: "preferences.workflowDialog.grouping.builtIn.crossSeedContentSavePath.description",
  },
  {
    id: "release_item",
    labelKey: "preferences.workflowDialog.grouping.builtIn.releaseItem.label",
    descriptionKey: "preferences.workflowDialog.grouping.builtIn.releaseItem.description",
  },
  {
    id: "tracker_release_item",
    labelKey: "preferences.workflowDialog.grouping.builtIn.trackerReleaseItem.label",
    descriptionKey: "preferences.workflowDialog.grouping.builtIn.trackerReleaseItem.description",
  },
  {
    id: "hardlink_signature",
    labelKey: "preferences.workflowDialog.grouping.builtIn.hardlinkSignature.label",
    descriptionKey: "preferences.workflowDialog.grouping.builtIn.hardlinkSignature.description",
  },
] as const

const AMBIGUOUS_POLICY_NONE_VALUE = "__none__"

// Speed limit mode: no_change = omit, unlimited = 0, custom = user value (>0)
type SpeedLimitMode = "no_change" | "unlimited" | "custom"

// Local form types that allow strings for intermediate input states (e.g. during typing "-")
interface FormFieldMultiplierScoreRule {
  field: ConditionField
  multiplier: number | string
}

interface FormConditionalScoreRule {
  condition?: RuleCondition
  score: number | string
}

type ScoreRuleType = "field_multiplier" | "conditional"

interface FormScoreRule {
  id: number
  type: ScoreRuleType
  fieldMultiplier?: FormFieldMultiplierScoreRule
  conditional?: FormConditionalScoreRule
}

type TagActionForm = {
  tags: string[]
  mode: "full" | "add" | "remove"
  deleteFromClient: boolean
  useTrackerAsTag: boolean
  useDisplayName: boolean
}

function createDefaultTagAction(): TagActionForm {
  return {
    tags: [],
    mode: "full",
    deleteFromClient: false,
    useTrackerAsTag: false,
    useDisplayName: false,
  }
}

function stripTrackerNegation(token: string): string {
  return token.startsWith("!") ? token.slice(1) : token
}

type FormState = {
  name: string
  trackerPattern: string
  trackerDomains: string[]
  trackerMatchMode: TrackerMatchMode
  applyToAllTrackers: boolean
  enabled: boolean
  dryRun: boolean
  notify: boolean
  sortOrder?: number
  intervalSeconds: number | null // null = use global default (15m)
  // Shared condition for all actions
  actionCondition: RuleCondition | null
  // Grouping settings (advanced)
  exprGrouping?: GroupingConfig
  // Multi-action enabled flags
  speedLimitsEnabled: boolean
  shareLimitsEnabled: boolean
  pauseEnabled: boolean
  resumeEnabled: boolean
  recheckEnabled: boolean
  reannounceEnabled: boolean
  autoManagementEnabled: boolean
  autoManageMode: "enable" | "disable"
  deleteEnabled: boolean
  tagEnabled: boolean
  categoryEnabled: boolean
  moveEnabled: boolean
  externalProgramEnabled: boolean
  // Speed limits settings (mode-based)
  exprUploadMode: SpeedLimitMode
  exprUploadValue?: number // KiB/s, only used when mode is "custom"
  exprDownloadMode: SpeedLimitMode
  exprDownloadValue?: number // KiB/s, only used when mode is "custom"
  // Share limits settings
  exprRatioLimitMode: "no_change" | "global" | "unlimited" | "custom"
  exprRatioLimitValue?: number
  exprSeedingTimeMode: "no_change" | "global" | "unlimited" | "custom"
  exprSeedingTimeValue?: number
  exprShareLimitAction: string
  exprShareLimitsMode: string
  // Delete settings
  exprDeleteMode: "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds" | "deleteWithFilesIncludeCrossSeeds"
  exprIncludeHardlinks: boolean // Only for deleteWithFilesIncludeCrossSeeds mode
  exprDeleteGroupId: string
  exprDeleteAtomic: "all" | ""
  // Free space source settings (for FREE_SPACE conditions)
  exprFreeSpaceSourceType: "qbittorrent" | "path"
  exprFreeSpaceSourcePath: string
  // Tag action settings
  exprTagActions: TagActionForm[]
  // Category action settings
  exprCategory: string
  exprIncludeCrossSeeds: boolean
  exprCategoryGroupId: string
  exprBlockIfCrossSeedInCategories: string[]
  // Sorting/Scoring
  sortingType: "default" | "simple" | "score"
  simpleSortField: ConditionField
  sortDirection: "ASC" | "DESC"
  scoreRules: FormScoreRule[]
  // Move action settings
  exprMovePath: string
  exprMoveBlockIfCrossSeed: boolean
  exprMoveGroupId: string
  exprMoveAtomic: "all" | ""
  // External program action settings
  exprExternalProgramId: number | null
  // Export to instance action settings
  exportToInstanceEnabled: boolean
  exprExportTargetInstanceId: number | null
  exprExportSavePath: string
  exprExportCategory: string
  exprExportTags: string
  exprExportPaused: boolean
  exprExportSkipChecking: boolean
  exprExportContentLayout: "" | "Original" | "Subfolder" | "NoSubfolder"
}

const emptyFormState: FormState = {
  name: "",
  trackerPattern: "",
  trackerDomains: [],
  trackerMatchMode: "include",
  applyToAllTrackers: false,
  enabled: false,
  dryRun: false,
  notify: true,
  intervalSeconds: null,
  actionCondition: null,
  exprGrouping: undefined,
  speedLimitsEnabled: false,
  shareLimitsEnabled: false,
  pauseEnabled: false,
  resumeEnabled: false,
  recheckEnabled: false,
  reannounceEnabled: false,
  autoManagementEnabled: false,
  autoManageMode: "enable",
  deleteEnabled: false,
  tagEnabled: false,
  categoryEnabled: false,
  moveEnabled: false,
  externalProgramEnabled: false,
  exprUploadMode: "no_change",
  exprUploadValue: undefined,
  exprDownloadMode: "no_change",
  exprDownloadValue: undefined,
  exprRatioLimitMode: "no_change",
  exprRatioLimitValue: undefined,
  exprSeedingTimeMode: "no_change",
  exprSeedingTimeValue: undefined,
  exprShareLimitAction: "default",
  exprShareLimitsMode: "default",
  exprDeleteMode: "deleteWithFilesPreserveCrossSeeds",
  exprIncludeHardlinks: false,
  exprDeleteGroupId: "",
  exprDeleteAtomic: "",
  exprFreeSpaceSourceType: "qbittorrent",
  exprFreeSpaceSourcePath: "",
  exprTagActions: [createDefaultTagAction()],
  exprCategory: "",
  exprIncludeCrossSeeds: false,
  exprCategoryGroupId: "",
  exprBlockIfCrossSeedInCategories: [],
  sortingType: "default",
  simpleSortField: "ADDED_ON",
  sortDirection: "ASC",
  scoreRules: [],
  exprMovePath: "",
  exprMoveBlockIfCrossSeed: false,
  exprMoveGroupId: "",
  exprMoveAtomic: "",
  exprExternalProgramId: null,
  exportToInstanceEnabled: false,
  exprExportTargetInstanceId: null,
  exprExportSavePath: "",
  exprExportCategory: "",
  exprExportTags: "",
  exprExportPaused: false,
  exprExportSkipChecking: true,
  exprExportContentLayout: "",
}

// Helper to get enabled actions from form state
function getEnabledActions(state: FormState): ActionType[] {
  const actions: ActionType[] = []
  if (state.speedLimitsEnabled) actions.push("speedLimits")
  if (state.shareLimitsEnabled) actions.push("shareLimits")
  if (state.pauseEnabled) actions.push("pause")
  if (state.resumeEnabled) actions.push("resume")
  if (state.recheckEnabled) actions.push("recheck")
  if (state.reannounceEnabled) actions.push("reannounce")
  if (state.autoManagementEnabled) actions.push("autoManagement")
  if (state.deleteEnabled) actions.push("delete")
  if (state.tagEnabled) actions.push("tag")
  if (state.categoryEnabled) actions.push("category")
  if (state.moveEnabled) actions.push("move")
  if (state.externalProgramEnabled) actions.push("externalProgram")
  if (state.exportToInstanceEnabled) actions.push("exportToInstance")
  return actions
}

// Helper to set an action enabled/disabled
function setActionEnabled(action: ActionType, enabled: boolean): Partial<FormState> {
  const key = `${action}Enabled` as keyof FormState
  return { [key]: enabled }
}

function validateTagActions(
  actions: TagActionForm[],
  t: ReturnType<typeof useTranslation<"instances">>["t"]
): string | null {
  if (actions.length === 0) {
    return t("preferences.workflowDialog.toast.addTagAction")
  }
  for (const action of actions) {
    if (action.deleteFromClient && action.useTrackerAsTag) {
      return t("preferences.workflowDialog.toast.replaceModeRequiresExplicitTags")
    }
    if (!action.useTrackerAsTag && action.tags.length === 0) {
      return t("preferences.workflowDialog.toast.specifyTagOrTrackerName")
    }
  }
  return null
}

// Hydration helpers for converting stored values to form state
type SpeedLimitHydration = {
  mode: SpeedLimitMode
  value?: number
  inferredUnit: number
}

function hydrateSpeedLimit(storedValue: number | undefined): SpeedLimitHydration {
  if (storedValue === undefined) {
    return { mode: "no_change", inferredUnit: 1024 }
  }
  if (storedValue === 0) {
    return { mode: "unlimited", inferredUnit: 1024 }
  }
  return {
    mode: "custom",
    value: storedValue,
    inferredUnit: storedValue % 1024 === 0 ? 1024 : 1,
  }
}

type ShareLimitHydration = {
  mode: "no_change" | "global" | "unlimited" | "custom"
  value?: number
}

function hydrateShareLimit(storedValue: number | undefined): ShareLimitHydration {
  if (storedValue === undefined) return { mode: "no_change" }
  if (storedValue === -2) return { mode: "global" }
  if (storedValue === -1) return { mode: "unlimited" }
  return { mode: "custom", value: storedValue }
}

export function WorkflowDialog({ open, onOpenChange, instanceId, rule, onSuccess }: WorkflowDialogProps) {
  const { t } = useTranslation("instances")
  const queryClient = useQueryClient()
  const [formState, setFormState] = useState<FormState>(emptyFormState)
  const [previewResult, setPreviewResult] = useState<AutomationPreviewResult | null>(null)
  const [previewInput, setPreviewInput] = useState<FormState | null>(null)
  const [livePreviewResult, setLivePreviewResult] = useState<AutomationPreviewResult | null>(null)
  const [isLivePreviewLoading, setIsLivePreviewLoading] = useState(false)
  const [livePreviewError, setLivePreviewError] = useState<string | null>(null)
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [enabledBeforePreview, setEnabledBeforePreview] = useState<boolean | null>(null)
  const [showDryRunPrompt, setShowDryRunPrompt] = useState(false)
  const [dryRunPromptedForNew, setDryRunPromptedForNew] = useState(false)
  const [latestDryRunEvents, setLatestDryRunEvents] = useState<AutomationActivity[]>([])
  const [latestDryRunError, setLatestDryRunError] = useState<string | null>(null)
  const [latestDryRunStartedAt, setLatestDryRunStartedAt] = useState<string | null>(null)
  const [activityRunDialog, setActivityRunDialog] = useState<AutomationActivity | null>(null)
  const [previewView, setPreviewView] = useState<PreviewView>("needed")
  const [isLoadingPreviewView, setIsLoadingPreviewView] = useState(false)
  const [isExporting, setIsExporting] = useState(false)
  const [isInitialLoading, setIsInitialLoading] = useState(false)
  // Speed limit units - track separately so they persist when value is cleared
  const [uploadSpeedUnit, setUploadSpeedUnit] = useState(1024) // Default MiB/s
  const [downloadSpeedUnit, setDownloadSpeedUnit] = useState(1024) // Default MiB/s
  const [regexErrors, setRegexErrors] = useState<RegexValidationError[]>([])
  const [freeSpaceSourcePathError, setFreeSpaceSourcePathError] = useState<string | null>(null)
  const [showAddCustomGroup, setShowAddCustomGroup] = useState(false)
  const [newGroupId, setNewGroupId] = useState("")
  const [newGroupKeys, setNewGroupKeys] = useState<string[]>([])
  const [newGroupAmbiguousPolicy, setNewGroupAmbiguousPolicy] = useState<"verify_overlap" | "skip" | "">("")
  const [newGroupMinOverlap, setNewGroupMinOverlap] = useState("90")
  const previewPageSize = 25
  const livePreviewPageSize = 5
  const livePreviewRequestRef = useRef(0)
  // Track whether we're in initial hydration to avoid noisy toasts when loading existing rules
  const isHydrating = useRef(true)
  const dryRunPromptKey = rule?.id ? `workflow-dry-run-prompted:${rule.id}` : null

  const hasPromptedDryRun = useCallback(() => {
    if (!rule?.id) return dryRunPromptedForNew
    if (typeof window === "undefined" || !dryRunPromptKey) return true
    return window.localStorage.getItem(dryRunPromptKey) === "1"
  }, [dryRunPromptKey, dryRunPromptedForNew, rule?.id])

  const markDryRunPrompted = useCallback(() => {
    if (!rule?.id) {
      setDryRunPromptedForNew(true)
      return
    }
    if (typeof window !== "undefined" && dryRunPromptKey) {
      window.localStorage.setItem(dryRunPromptKey, "1")
    }
  }, [dryRunPromptKey, rule?.id])

  const trackersQuery = useInstanceTrackers(instanceId, { enabled: open })
  const indexerTrackerDomainsQuery = useIndexerTrackerDomains({ enabled: open })
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const { data: metadata } = useInstanceMetadata(instanceId)
  const { data: targetMetadata, isLoading: targetMetadataLoading } = useInstanceMetadata(formState.exprExportTargetInstanceId ?? 0)
  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: open })
  const { instances, isLoading: instancesLoading, error: instancesError } = useInstances()
  const {
    data: allExternalPrograms,
    isError: externalProgramsError,
    isLoading: externalProgramsLoading,
  } = useQuery({
    queryKey: ["externalPrograms"],
    queryFn: () => api.listExternalPrograms(),
    enabled: open,
  })
  // Show enabled programs + the currently selected program (even if disabled) so users can see what's configured
  const externalPrograms = useMemo(() => {
    if (!allExternalPrograms) return undefined
    const selectedId = formState.exprExternalProgramId
    return allExternalPrograms.filter(p => p.enabled || p.id === selectedId)
  }, [allExternalPrograms, formState.exprExternalProgramId])
  const { data: notificationTargets } = useQuery({
    queryKey: ["notificationTargets"],
    queryFn: () => api.listNotificationTargets(),
    enabled: open,
    staleTime: 30 * 1000,
  })
  const hasNotificationTargets = (notificationTargets ?? []).length > 0

  const supportsTrackerHealth = capabilities?.supportsTrackerHealth ?? false
  const supportsFreeSpacePathSource = capabilities?.supportsFreeSpacePathSource ?? false
  const supportsPathAutocomplete = capabilities?.supportsPathAutocomplete ?? false
  const hasLocalFilesystemAccess = useMemo(
    () => instances?.find(i => i.id === instanceId)?.hasLocalFilesystemAccess ?? false,
    [instances, instanceId]
  )

  const fieldCapabilities = useMemo<Capabilities>(
    () => ({
      trackerHealth: supportsTrackerHealth,
      localFilesystemAccess: hasLocalFilesystemAccess,
    }),
    [supportsTrackerHealth, hasLocalFilesystemAccess]
  )

  // Callback for path autocomplete suggestion selection
  const handleFreeSpacePathSelect = useCallback((path: string) => {
    setFormState(prev => ({ ...prev, exprFreeSpaceSourcePath: path }))
    setFreeSpaceSourcePathError(null)
  }, [])

  // Path autocomplete for free space source
  const {
    suggestions: freeSpaceSuggestions,
    handleInputChange: handleFreeSpacePathInputChange,
    handleSelect: handleFreeSpacePathSelectSuggestion,
    handleKeyDown: handleFreeSpacePathKeyDown,
    highlightedIndex: freeSpaceHighlightedIndex,
    showSuggestions: showFreeSpaceSuggestions,
    inputRef: freeSpacePathInputRef,
  } = usePathAutocomplete(handleFreeSpacePathSelect, instanceId)

  // Container and position for autocomplete dropdown portal (inside dialog, outside scroll)
  const dropdownContainerRef = useRef<HTMLDivElement>(null)
  const [dropdownRect, setDropdownRect] = useState<{ top: number; left: number; width: number } | null>(null)

  useEffect(() => {
    if (showFreeSpaceSuggestions && freeSpaceSuggestions.length > 0 && freeSpacePathInputRef.current && dropdownContainerRef.current) {
      const inputRect = freeSpacePathInputRef.current.getBoundingClientRect()
      const containerRect = dropdownContainerRef.current.getBoundingClientRect()
      setDropdownRect({
        top: inputRect.bottom - containerRect.top,
        left: inputRect.left - containerRect.left,
        width: inputRect.width,
      })
    } else {
      setDropdownRect(null)
    }
  }, [showFreeSpaceSuggestions, freeSpaceSuggestions.length, freeSpacePathInputRef])

  // Build category options for the category action dropdown
  const categoryOptions = useMemo(() => {
    if (!metadata?.categories) return []
    const selected = [formState.exprCategory, ...formState.exprBlockIfCrossSeedInCategories].filter(Boolean)
    return buildCategorySelectOptions(metadata.categories, selected)
  }, [metadata?.categories, formState.exprCategory, formState.exprBlockIfCrossSeedInCategories])

  const categoryActionOptions = useMemo(() => {
    const filtered = categoryOptions.filter((opt) => opt.value !== "")
    return [
      { label: t("preferences.workflowDialog.uncategorized"), value: CATEGORY_UNCATEGORIZED_VALUE },
      ...filtered,
    ]
  }, [categoryOptions, t])

  const tagOptions = useMemo(() => {
    const selected = formState.exprTagActions.flatMap(action => action.tags)
    return buildTagSelectOptions(metadata?.tags ?? [], selected)
  }, [formState.exprTagActions, metadata?.tags])

  const trackerCustomizationMaps = useMemo(
    () => buildTrackerCustomizationMaps(trackerCustomizations),
    [trackerCustomizations]
  )

  // Process trackers to apply customizations (nicknames and merged domains)
  // Also includes trackers from the current workflow being edited, so they remain
  // visible even if no torrents currently use them
  const trackerOptions: Option[] = useMemo(() => {
    type TrackerOption = Option & { isCustom: boolean }
    const { domainToCustomization } = trackerCustomizationMaps
    const trackers = trackersQuery.data ? Object.keys(trackersQuery.data) : []
    const processed: TrackerOption[] = []
    const seenDisplayNames = new Set<string>()
    const seenValues = new Set<string>()

    // Helper to add a tracker option
    const addTracker = (tracker: string) => {
      const lowerTracker = tracker.toLowerCase()

      const customization = domainToCustomization.get(lowerTracker)

      if (customization) {
        const displayKey = customization.displayName.toLowerCase()
        const mergedValue = customization.domains.join(",")
        if (seenDisplayNames.has(displayKey) || seenValues.has(mergedValue)) return
        seenDisplayNames.add(displayKey)
        seenValues.add(mergedValue)

        const iconDomain = pickTrackerIconDomain(trackerIcons, customization.domains)
        processed.push({
          label: t("preferences.workflowDialog.customTrackerLabel", { name: customization.displayName }),
          value: mergedValue,
          icon: <TrackerIconImage tracker={iconDomain} trackerIcons={trackerIcons} />,
          isCustom: true,
        })
      } else {
        if (seenDisplayNames.has(lowerTracker) || seenValues.has(tracker)) return
        seenDisplayNames.add(lowerTracker)
        seenValues.add(tracker)

        processed.push({
          label: tracker,
          value: tracker,
          icon: <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />,
          isCustom: false,
        })
      }
    }

    // Add trackers from current torrents
    for (const tracker of trackers) {
      addTracker(tracker)
    }

    // Add trackers configured in qui's indexers, so they remain selectable even
    // when no torrent on this instance currently uses them.
    for (const domain of indexerTrackerDomainsQuery.data ?? []) {
      addTracker(domain)
    }

    // Add trackers from the workflow being edited (so they persist even if no torrents use them)
    if (rule && rule.trackerPattern !== "*") {
      const savedDomains = getTrackerTokens(rule).map(stripTrackerNegation).filter(Boolean)
      for (const domain of savedDomains) {
        addTracker(domain)
      }
    }

    processed.sort((a, b) => {
      if (a.isCustom !== b.isCustom) {
        return a.isCustom ? -1 : 1
      }
      return a.label.localeCompare(b.label, undefined, { sensitivity: "base" })
    })

    return processed.map((option) => ({
      label: option.label,
      value: option.value,
      icon: option.icon,
    }))
  }, [trackersQuery.data, indexerTrackerDomainsQuery.data, trackerCustomizationMaps, trackerIcons, rule, t])

  // Map individual domains to merged option values
  const mapDomainsToOptionValues = useMemo(() => {
    const { domainToCustomization } = trackerCustomizationMaps
    return (domains: string[]): string[] => {
      const result: string[] = []
      const processed = new Set<string>()

      for (const domain of domains) {
        const lowerDomain = domain.toLowerCase()
        if (processed.has(lowerDomain)) continue

        const customization = domainToCustomization.get(lowerDomain)
        if (customization) {
          const mergedValue = customization.domains.join(",")
          if (!result.includes(mergedValue)) {
            result.push(mergedValue)
          }
          for (const d of customization.domains) {
            processed.add(d.toLowerCase())
          }
        } else {
          result.push(domain)
          processed.add(lowerDomain)
        }
      }

      return result
    }
  }, [trackerCustomizationMaps])

  const groupedConditionOptions = useMemo<GroupOption[]>(() => {
    const options: GroupOption[] = BUILTIN_GROUPS.map((group) => ({
      id: group.id,
      label: t(group.labelKey),
      description: t(group.descriptionKey),
    }))
    const seen = new Set(options.map((option) => option.id.toLowerCase()))
    for (const group of (formState.exprGrouping?.groups || [])) {
      const id = group.id?.trim()
      if (!id) continue
      if (seen.has(id.toLowerCase())) continue
      seen.add(id.toLowerCase())
      options.push({
        id,
        label: t("preferences.workflowDialog.grouping.customLabel", { id }),
        description: group.keys.length > 0? t("preferences.workflowDialog.grouping.customDescription", { keys: group.keys.join(", ") }): t("preferences.workflowDialog.grouping.customGroup"),
      })
    }
    return options
  }, [formState.exprGrouping?.groups, t])

  const nonSelfInstances = useMemo(
    () => instances ? instances.filter(i => i.id !== instanceId) : undefined,
    [instances, instanceId]
  )

  const targetCategories = useMemo(() => {
    const cats = targetMetadata?.categories ? Object.keys(targetMetadata.categories) : []
    // Include the current saved category so it remains selectable even if not on the target yet
    if (formState.exprExportCategory && !cats.includes(formState.exprExportCategory)) {
      cats.push(formState.exprExportCategory)
    }
    return cats.sort()
  }, [targetMetadata, formState.exprExportCategory])

  // Initialize form state when dialog opens or rule changes
  useEffect(() => {
    let cancelled = false

    if (open) {
      if (rule) {
        const isAllTrackers = rule.trackerPattern === "*"
        const trackerTokens = isAllTrackers ? [] : getTrackerTokens(rule)
        const rawDomains = trackerTokens.map(stripTrackerNegation).filter(Boolean)
        const mappedDomains = mapDomainsToOptionValues(rawDomains)
        const trackerMatchMode = isAllTrackers ? "include" : getTrackerMatchMode(trackerTokens)

        // Parse existing conditions into form state
        const conditions = rule.conditions
        let actionCondition: RuleCondition | null = null
        let speedLimitsEnabled = false
        let shareLimitsEnabled = false
        let pauseEnabled = false
        let resumeEnabled = false
        let recheckEnabled = false
        let reannounceEnabled = false
        let autoManagementEnabled = false
        let deleteEnabled = false
        let tagEnabled = false
        let categoryEnabled = false
        let moveEnabled = false
        let externalProgramEnabled = false
        let exprUploadMode: SpeedLimitMode = "no_change"
        let exprUploadValue: number | undefined
        let exprDownloadMode: SpeedLimitMode = "no_change"
        let exprDownloadValue: number | undefined
        let exprRatioLimitMode: FormState["exprRatioLimitMode"] = "no_change"
        let exprRatioLimitValue: number | undefined
        let exprSeedingTimeMode: FormState["exprSeedingTimeMode"] = "no_change"
        let exprSeedingTimeValue: number | undefined
        let exprShareLimitAction = "default"
        let exprShareLimitsMode = "default"
        let exprDeleteMode: FormState["exprDeleteMode"] = "deleteWithFilesPreserveCrossSeeds"
        let exprIncludeHardlinks = false
        let exprFreeSpaceSourceType: FormState["exprFreeSpaceSourceType"] = "qbittorrent"
        let exprFreeSpaceSourcePath = ""
        let exprTagActions: TagActionForm[] = [createDefaultTagAction()]
        let exprCategory = ""
        let exprIncludeCrossSeeds = false
        let exprBlockIfCrossSeedInCategories: string[] = []
        let sortingType: FormState["sortingType"] = "default"
        let simpleSortField: ConditionField = "ADDED_ON"
        let sortDirection: "ASC" | "DESC" = "ASC"
        let scoreRules: FormScoreRule[] = []
        let exprMovePath = ""
        let exprMoveBlockIfCrossSeed = false
        let exprExternalProgramId: number | null = null
        let exprGrouping: GroupingConfig | undefined
        let exprDeleteGroupId = ""
        let exprDeleteAtomic: FormState["exprDeleteAtomic"] = ""
        let exprCategoryGroupId = ""
        let exprMoveGroupId = ""
        let exprMoveAtomic: FormState["exprMoveAtomic"] = ""
        let exportToInstanceEnabled = false
        let exprExportTargetInstanceId: number | null = null
        let exprExportSavePath = ""
        let exprExportCategory = ""
        let exprExportTags = ""
        let exprExportPaused = false
        let exprExportSkipChecking = true
        let exprExportContentLayout: FormState["exprExportContentLayout"] = ""

        if (rule.sortingConfig) {
          if (rule.sortingConfig.type === "simple") {
            sortingType = "simple"
            if (rule.sortingConfig.field) simpleSortField = rule.sortingConfig.field
            if (rule.sortingConfig.direction) sortDirection = rule.sortingConfig.direction
          } else if (rule.sortingConfig.type === "score") {
            sortingType = "score"
            if (rule.sortingConfig.direction) sortDirection = rule.sortingConfig.direction
            scoreRules = (rule.sortingConfig.scoreRules || []).flatMap<FormScoreRule>(r => {
              if (r.type === "field_multiplier") {
                return [{ id: ++ruleIdCounter, type: r.type, fieldMultiplier: { ...r.fieldMultiplier } }]
              }
              if (r.type === "conditional") {
                return [{ id: ++ruleIdCounter, type: r.type, conditional: { ...r.conditional } }]
              }
              return []
            })
          }
        }

        // Hydrate freeSpaceSource from rule
        if (rule.freeSpaceSource) {
          exprFreeSpaceSourceType = rule.freeSpaceSource.type ?? "qbittorrent"
          if (rule.freeSpaceSource.type === "path") {
            exprFreeSpaceSourcePath = rule.freeSpaceSource.path ?? ""
          }
        }

        if (conditions) {
          exprGrouping = conditions.grouping
          // Get condition from any enabled action (they should all be the same)
          actionCondition = conditions.speedLimits?.condition
            ?? conditions.shareLimits?.condition
            ?? conditions.pause?.condition
            ?? conditions.resume?.condition
            ?? conditions.recheck?.condition
            ?? conditions.reannounce?.condition
            ?? conditions.autoManagement?.condition
            ?? conditions.delete?.condition
            ?? conditions.tags?.[0]?.condition
            ?? conditions.tag?.condition
            ?? conditions.category?.condition
            ?? conditions.move?.condition
            ?? conditions.externalProgram?.condition
            ?? conditions.exportToInstance?.condition
            ?? null

          if (conditions.speedLimits?.enabled) {
            speedLimitsEnabled = true
            const upload = hydrateSpeedLimit(conditions.speedLimits.uploadKiB)
            exprUploadMode = upload.mode
            exprUploadValue = upload.value
            if (upload.mode === "custom") setUploadSpeedUnit(upload.inferredUnit)

            const download = hydrateSpeedLimit(conditions.speedLimits.downloadKiB)
            exprDownloadMode = download.mode
            exprDownloadValue = download.value
            if (download.mode === "custom") setDownloadSpeedUnit(download.inferredUnit)
          }
          if (conditions.shareLimits?.enabled) {
            shareLimitsEnabled = true
            const ratio = hydrateShareLimit(conditions.shareLimits.ratioLimit)
            exprRatioLimitMode = ratio.mode
            exprRatioLimitValue = ratio.value

            const seedTime = hydrateShareLimit(conditions.shareLimits.seedingTimeMinutes)
            exprSeedingTimeMode = seedTime.mode
            exprSeedingTimeValue = seedTime.value

            const rawAction = conditions.shareLimits.shareLimitAction
            exprShareLimitAction = rawAction !== undefined && rawAction !== "" ? rawAction : "default"
            const rawMode = conditions.shareLimits.shareLimitsMode
            exprShareLimitsMode = rawMode !== undefined && rawMode !== "" ? rawMode : "default"
          }
          if (conditions.pause?.enabled) {
            pauseEnabled = true
          }
          if (conditions.resume?.enabled) {
            resumeEnabled = true
          }
          if (conditions.recheck?.enabled) {
            recheckEnabled = true
          }
          if (conditions.reannounce?.enabled) {
            reannounceEnabled = true
          }
          if (conditions.autoManagement != null) {
            autoManagementEnabled = true
          }
          if (conditions.delete?.enabled) {
            deleteEnabled = true
            exprDeleteMode = conditions.delete.mode ?? "deleteWithFilesPreserveCrossSeeds"
            exprIncludeHardlinks = conditions.delete.includeHardlinks ?? false
            exprDeleteGroupId = conditions.delete.groupId ?? ""
            exprDeleteAtomic = conditions.delete.atomic ?? ""
          }
          const resolvedTagActions = (conditions.tags && conditions.tags.length > 0? conditions.tags: conditions.tag ? [conditions.tag] : [])
            .filter((action) => action && action.enabled)
          if (resolvedTagActions.length > 0) {
            tagEnabled = true
            exprTagActions = resolvedTagActions.map((action) => ({
              tags: action.tags ?? [],
              mode: action.mode ?? "full",
              deleteFromClient: action.deleteFromClient ?? false,
              useTrackerAsTag: action.useTrackerAsTag ?? false,
              useDisplayName: action.useDisplayName ?? false,
            }))
          }
          if (conditions.category?.enabled) {
            categoryEnabled = true
            exprCategory = conditions.category.category ?? ""
            exprIncludeCrossSeeds = conditions.category.includeCrossSeeds ?? false
            exprCategoryGroupId = conditions.category.groupId ?? ""
            exprBlockIfCrossSeedInCategories = conditions.category.blockIfCrossSeedInCategories ?? []
          }
          if (conditions.move?.enabled) {
            moveEnabled = true
            exprMovePath = conditions.move.path ?? ""
            exprMoveBlockIfCrossSeed = conditions.move.blockIfCrossSeed ?? false
            exprMoveGroupId = conditions.move.groupId ?? ""
            exprMoveAtomic = conditions.move.atomic ?? ""
          }
          if (conditions.externalProgram?.enabled) {
            externalProgramEnabled = true
            exprExternalProgramId = conditions.externalProgram.programId ?? null
          }
          if (conditions.exportToInstance?.enabled) {
            exportToInstanceEnabled = true
            exprExportTargetInstanceId = conditions.exportToInstance.targetInstanceId ?? null
            exprExportSavePath = conditions.exportToInstance.savePath ?? ""
            exprExportCategory = conditions.exportToInstance.category ?? ""
            exprExportTags = (conditions.exportToInstance.tags ?? []).join(", ")
            exprExportPaused = conditions.exportToInstance.paused ?? false
            exprExportSkipChecking = conditions.exportToInstance.skipChecking ?? true
            const rawLayout = conditions.exportToInstance.contentLayout ?? ""
            exprExportContentLayout = CONTENT_LAYOUT_VALUES.includes(rawLayout as typeof CONTENT_LAYOUT_VALUES[number])? rawLayout as FormState["exprExportContentLayout"]: ""
          }
        }

        const newState: FormState = {
          name: rule.name,
          trackerPattern: rule.trackerPattern,
          trackerDomains: mappedDomains,
          trackerMatchMode,
          applyToAllTrackers: isAllTrackers,
          enabled: rule.enabled,
          dryRun: rule.dryRun ?? false,
          notify: rule.notify ?? true,
          sortOrder: rule.sortOrder,
          intervalSeconds: rule.intervalSeconds ?? null,
          actionCondition,
          exprGrouping,
          speedLimitsEnabled,
          shareLimitsEnabled,
          pauseEnabled,
          resumeEnabled,
          recheckEnabled,
          reannounceEnabled,
          autoManagementEnabled,
          autoManageMode: conditions?.autoManagement?.enabled !== false ? "enable" : "disable",
          deleteEnabled,
          tagEnabled,
          categoryEnabled,
          moveEnabled,
          externalProgramEnabled,
          exprUploadMode,
          exprUploadValue,
          exprDownloadMode,
          exprDownloadValue,
          exprRatioLimitMode,
          exprRatioLimitValue,
          exprSeedingTimeMode,
          exprSeedingTimeValue,
          exprShareLimitAction,
          exprShareLimitsMode,
          exprDeleteMode,
          exprIncludeHardlinks,
          exprDeleteGroupId,
          exprDeleteAtomic,
          exprFreeSpaceSourceType,
          exprFreeSpaceSourcePath,
          exprTagActions,
          exprCategory,
          exprMovePath,
          exprMoveBlockIfCrossSeed,
          exprIncludeCrossSeeds,
          exprCategoryGroupId,
          exprBlockIfCrossSeedInCategories,
          sortingType,
          simpleSortField,
          sortDirection,
          scoreRules,
          exprMoveGroupId,
          exprMoveAtomic,
          exprExternalProgramId,
          exportToInstanceEnabled,
          exprExportTargetInstanceId,
          exprExportSavePath,
          exprExportCategory,
          exprExportTags,
          exprExportPaused,
          exprExportSkipChecking,
          exprExportContentLayout,
        }
        setFormState(newState)
      } else {
        setFormState(emptyFormState)
      }
      // Mark hydration complete after a microtask to ensure state is settled
      // Use cancelled flag to avoid race condition if dialog closes before microtask runs
      queueMicrotask(() => {
        if (!cancelled) {
          isHydrating.current = false
        }
      })
    } else {
      // Reset flags when dialog closes so they're ready for next open
      isHydrating.current = true
    }

    return () => { cancelled = true }
  }, [open, rule, mapDomainsToOptionValues])

  useEffect(() => {
    if (!open) {
      setShowDryRunPrompt(false)
      setLatestDryRunEvents([])
      setLatestDryRunError(null)
      setLatestDryRunStartedAt(null)
      setActivityRunDialog(null)
      return
    }
    if (!rule) {
      setDryRunPromptedForNew(false)
    }
  }, [open, rule])

  useEffect(() => {
    if (!open || !rule?.id || !rule.enabled) {
      return
    }
    if (typeof window !== "undefined" && dryRunPromptKey) {
      window.localStorage.setItem(dryRunPromptKey, "1")
    }
  }, [dryRunPromptKey, open, rule?.enabled, rule?.id])

  // Auto-switch delete mode from keep-files to deleteWithFiles when FREE_SPACE is used
  // This prevents users from creating invalid combinations that the backend would reject
  // Only toast on user edits, not during initial form hydration
  useEffect(() => {
    if (formState.deleteEnabled && formState.exprDeleteMode === "delete") {
      if (conditionUsesField(formState.actionCondition, "FREE_SPACE")) {
        setFormState(prev => ({ ...prev, exprDeleteMode: "deleteWithFiles" }))
        if (!isHydrating.current) {
          toast.info(t("preferences.workflowDialog.toast.switchedDeleteModeForFreeSpace"))
        }
      }
    }
  }, [formState.actionCondition, formState.deleteEnabled, formState.exprDeleteMode, t])

  // Auto-switch interval from 1 minute when FREE_SPACE delete condition is added
  // The backend has a ~5 minute cooldown, so 1 minute intervals would be ineffective
  // Only switch on user edits, not during initial hydration (respect saved config)
  useEffect(() => {
    if (isHydrating.current) return
    if (formState.deleteEnabled && formState.intervalSeconds === 60) {
      if (conditionUsesField(formState.actionCondition, "FREE_SPACE")) {
        setFormState(prev => ({ ...prev, intervalSeconds: 300 })) // Switch to 5 minutes
        toast.info(t("preferences.workflowDialog.toast.switchedIntervalForFreeSpace"))
      }
    }
  }, [formState.actionCondition, formState.deleteEnabled, formState.intervalSeconds, t])

  // Auto-switch free space source from "path" to "qbittorrent" on Windows (not supported)
  // This must run during hydration to handle legacy workflows opened on Windows.
  // Only toast after hydration to avoid noise when opening dialogs.
  useEffect(() => {
    if (!supportsFreeSpacePathSource && formState.exprFreeSpaceSourceType === "path") {
      setFormState(prev => ({ ...prev, exprFreeSpaceSourceType: "qbittorrent" }))
      if (!isHydrating.current) {
        toast.warning(t("preferences.workflowDialog.toast.pathSourceUnsupportedWindows"))
      }
    }
  }, [supportsFreeSpacePathSource, formState.exprFreeSpaceSourceType, t])

  const validateFreeSpaceSource = useCallback((state: FormState): boolean => {
    const usesFreeSpace = conditionUsesField(state.actionCondition, "FREE_SPACE")
    if (!usesFreeSpace || state.exprFreeSpaceSourceType !== "path") {
      setFreeSpaceSourcePathError(null)
      return true
    }

    // Reject if path source is selected but not supported (safety net for edge cases)
    if (!supportsFreeSpacePathSource) {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.unsupportedWindows"))
      toast.error(t("preferences.workflowDialog.toast.switchFreeSpaceSourceDefault"))
      return false
    }
    if (!hasLocalFilesystemAccess) {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.localAccessRequired"))
      toast.error(t("preferences.workflowDialog.toast.enableLocalAccessOrDefault"))
      return false
    }

    const trimmedPath = state.exprFreeSpaceSourcePath.trim()
    if (trimmedPath === "") {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.pathRequired"))
      toast.error(t("preferences.workflowDialog.toast.enterPathOrDefault"))
      return false
    }

    setFreeSpaceSourcePathError(null)
    return true
  }, [hasLocalFilesystemAccess, supportsFreeSpacePathSource, t])

  const hasValidFreeSpaceSourceForLivePreview = useCallback((state: FormState): boolean => {
    const usesFreeSpace = conditionUsesField(state.actionCondition, "FREE_SPACE")
    if (!usesFreeSpace || state.exprFreeSpaceSourceType !== "path") {
      return true
    }
    if (!supportsFreeSpacePathSource || !hasLocalFilesystemAccess) {
      return false
    }
    return state.exprFreeSpaceSourcePath.trim() !== ""
  }, [hasLocalFilesystemAccess, supportsFreeSpacePathSource])

  // Build payload from form state (shared by preview and save)
  const buildPayload = useCallback((input: FormState): AutomationInput => {
    const conditions: ActionConditions = { schemaVersion: "1" }
    if (input.exprGrouping) {
      conditions.grouping = input.exprGrouping
    }

    // Add all enabled actions
    if (input.speedLimitsEnabled) {
      // Convert speed limit modes to API values:
      // - no_change → undefined (omit from API call)
      // - unlimited → 0 (qBittorrent treats per-torrent speed limit 0 as unlimited)
      // - custom → the user-specified value (must be > 0)
      let uploadKiB: number | undefined
      if (input.exprUploadMode === "unlimited") {
        uploadKiB = 0
      } else if (input.exprUploadMode === "custom" && input.exprUploadValue !== undefined) {
        uploadKiB = input.exprUploadValue
      }
      // "no_change" leaves uploadKiB as undefined

      let downloadKiB: number | undefined
      if (input.exprDownloadMode === "unlimited") {
        downloadKiB = 0
      } else if (input.exprDownloadMode === "custom" && input.exprDownloadValue !== undefined) {
        downloadKiB = input.exprDownloadValue
      }
      // "no_change" leaves downloadKiB as undefined

      conditions.speedLimits = {
        enabled: true,
        uploadKiB,
        downloadKiB,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.shareLimitsEnabled) {
      // Convert mode/value to API format
      // -2 = use global, -1 = unlimited, >= 0 = custom value
      let ratioLimit: number | undefined
      if (input.exprRatioLimitMode === "global") {
        ratioLimit = -2
      } else if (input.exprRatioLimitMode === "unlimited") {
        ratioLimit = -1
      } else if (input.exprRatioLimitMode === "custom" && input.exprRatioLimitValue !== undefined) {
        // Normalize ratio to 2 decimal places to match qBittorrent/go-qbittorrent precision
        ratioLimit = Math.round(input.exprRatioLimitValue * 100) / 100
      }
      // "no_change" leaves ratioLimit as undefined

      let seedingTimeMinutes: number | undefined
      if (input.exprSeedingTimeMode === "global") {
        seedingTimeMinutes = -2
      } else if (input.exprSeedingTimeMode === "unlimited") {
        seedingTimeMinutes = -1
      } else if (input.exprSeedingTimeMode === "custom" && input.exprSeedingTimeValue !== undefined) {
        seedingTimeMinutes = input.exprSeedingTimeValue
      }
      // "no_change" leaves seedingTimeMinutes as undefined

      conditions.shareLimits = {
        enabled: true,
        ratioLimit,
        seedingTimeMinutes,
        shareLimitAction: input.exprShareLimitAction !== "default" ? input.exprShareLimitAction : undefined,
        shareLimitsMode: input.exprShareLimitsMode !== "default" ? input.exprShareLimitsMode : undefined,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.pauseEnabled) {
      conditions.pause = {
        enabled: true,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.resumeEnabled) {
      conditions.resume = {
        enabled: true,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.recheckEnabled) {
      conditions.recheck = {
        enabled: true,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.reannounceEnabled) {
      conditions.reannounce = {
        enabled: true,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.autoManagementEnabled) {
      conditions.autoManagement = {
        enabled: input.autoManageMode === "enable",
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.deleteEnabled) {
      conditions.delete = {
        enabled: true,
        mode: input.exprDeleteMode,
        // Only include includeHardlinks when using include cross-seeds mode
        includeHardlinks: input.exprDeleteMode === "deleteWithFilesIncludeCrossSeeds" ? input.exprIncludeHardlinks : undefined,
        groupId: input.exprDeleteGroupId || undefined,
        atomic: input.exprDeleteAtomic || undefined,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.tagEnabled) {
      const tagActions = input.exprTagActions
        .filter((action) => action.useTrackerAsTag || action.tags.length > 0)
        .map((action) => ({
          enabled: true,
          tags: action.tags,
          mode: action.mode,
          deleteFromClient: action.deleteFromClient,
          useTrackerAsTag: action.useTrackerAsTag,
          useDisplayName: action.useDisplayName,
          condition: input.actionCondition ?? undefined,
        }))

      if (tagActions.length > 0) {
        conditions.tags = tagActions
        // Keep legacy single-tag payload for backward compatibility.
        conditions.tag = tagActions[0]
      }
    }
    if (input.categoryEnabled) {
      conditions.category = {
        enabled: true,
        category: input.exprCategory,
        includeCrossSeeds: input.exprIncludeCrossSeeds,
        groupId: input.exprCategoryGroupId || undefined,
        blockIfCrossSeedInCategories: input.exprBlockIfCrossSeedInCategories,
        condition: input.actionCondition ?? undefined,
      }
    }
    const trimmedMovePath = input.exprMovePath?.trim()
    if (input.moveEnabled && trimmedMovePath) {
      conditions.move = {
        enabled: true,
        path: trimmedMovePath,
        blockIfCrossSeed: input.exprMoveBlockIfCrossSeed,
        groupId: input.exprMoveGroupId || undefined,
        atomic: input.exprMoveAtomic || undefined,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.externalProgramEnabled && input.exprExternalProgramId) {
      conditions.externalProgram = {
        enabled: true,
        programId: input.exprExternalProgramId,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.exportToInstanceEnabled) {
      if (!input.exprExportTargetInstanceId) {
        throw new Error("Export to instance requires a target instance")
      }
      const tags = input.exprExportTags.split(",").map(t => t.trim()).filter(Boolean)
      conditions.exportToInstance = {
        enabled: true,
        targetInstanceId: input.exprExportTargetInstanceId,
        savePath: input.exprExportSavePath.trim(),
        category: input.exprExportCategory.trim() || undefined,
        tags: tags.length > 0 ? tags : undefined,
        paused: input.exprExportPaused || undefined,
        skipChecking: input.exprExportSkipChecking,
        contentLayout: input.exprExportContentLayout || undefined,
        condition: input.actionCondition ?? undefined,
      }
    }

    const usesFreeSpace = conditionUsesField(input.actionCondition, "FREE_SPACE")
    const trimmedFreeSpacePath = input.exprFreeSpaceSourcePath.trim()
    let freeSpaceSource: AutomationInput["freeSpaceSource"]
    if (usesFreeSpace && input.exprFreeSpaceSourceType === "path" && trimmedFreeSpacePath) {
      freeSpaceSource = { type: "path", path: trimmedFreeSpacePath }
    } else if (input.exprFreeSpaceSourceType === "path" && trimmedFreeSpacePath) {
      // Keep the path source even if FREE_SPACE isn't currently in the condition
      // (user might add it later, or just want to preserve the setting)
      freeSpaceSource = { type: "path", path: trimmedFreeSpacePath }
    }

    let sortingConfig: SortingConfig | undefined
    if (input.sortingType === "simple") {
      if (!isSupportedSimpleSortField(input.simpleSortField)) {
        throw new Error("Invalid sort field: not supported for simple sorting")
      }
      sortingConfig = {
        schemaVersion: "1",
        type: "simple",
        field: input.simpleSortField!,
        direction: input.sortDirection!,
      }
    } else if (input.sortingType === "score") {
      sortingConfig = {
        schemaVersion: "1",
        type: "score",
        direction: input.sortDirection!,
        scoreRules: input.scoreRules.flatMap((r): ScoreRule[] => {
          if (r.type === "field_multiplier" && r.fieldMultiplier) {
            if (!isSupportedScoreMultiplierField(r.fieldMultiplier.field)) {
              throw new Error("Invalid score rule: field is not supported for multipliers")
            }
            const val = r.fieldMultiplier.multiplier
            const multiplier = typeof val === "string" ? parseFloat(val) : val
            if (Number.isFinite(multiplier)) {
              return [{ type: "field_multiplier", fieldMultiplier: { ...r.fieldMultiplier, multiplier } }]
            } else {
              throw new Error("Invalid score rule: Field multiplier must be a valid number")
            }
          }
          if (r.type === "conditional" && r.conditional && r.conditional.condition) {
            const val = r.conditional.score
            const score = typeof val === "string" ? parseFloat(val) : val
            if (Number.isFinite(score)) {
              return [{ type: "conditional", conditional: { ...r.conditional, score, condition: r.conditional.condition } }]
            } else {
              throw new Error("Invalid score rule: Conditional score must be a valid number")
            }
          }
          return []
        }),
      }
    }

    const trackerDomains = input.applyToAllTrackers ? [] : normalizeTrackerDomains(input.trackerDomains)
    let normalizedTrackerDomains = trackerDomains
    if (input.trackerMatchMode === "exclude") {
      normalizedTrackerDomains = trackerDomains.map((domain) => `!${domain}`)
    }

    let trackerPattern = normalizedTrackerDomains.join(",")
    if (input.applyToAllTrackers) {
      trackerPattern = "*"
    } else if (input.trackerMatchMode === "mixed") {
      trackerPattern = input.trackerPattern
    }

    return {
      name: input.name,
      trackerDomains: input.trackerMatchMode === "mixed" ? [] : normalizedTrackerDomains,
      trackerPattern,
      enabled: input.enabled,
      dryRun: input.dryRun,
      notify: input.notify,
      sortOrder: input.sortOrder,
      intervalSeconds: input.intervalSeconds,
      conditions,
      freeSpaceSource,
      sortingConfig,
    }
  }, [])

  // Check if current form state represents a delete or category rule (both need previews)
  const isDeleteRule = formState.deleteEnabled
  const isCategoryRule = formState.categoryEnabled

  // Check if condition uses FREE_SPACE field (for free space source UI - shown regardless of action)
  const conditionUsesFreeSpace = useMemo(() => {
    return conditionUsesField(formState.actionCondition, "FREE_SPACE")
  }, [formState.actionCondition])

  // Check if delete rule uses FREE_SPACE field (for preview view toggle - only for delete rules)
  const deleteUsesFreeSpace = formState.deleteEnabled && conditionUsesFreeSpace
  const intervalOptions = useMemo(() => ([
    { value: "default", label: t("preferences.workflowDialog.interval.default") },
    { value: "60", label: t("preferences.workflowDialog.interval.oneMinute"), disabled: deleteUsesFreeSpace },
    { value: "300", label: t("preferences.workflowDialog.interval.fiveMinutes") },
    { value: "900", label: t("preferences.workflowDialog.interval.fifteenMinutes") },
    { value: "1800", label: t("preferences.workflowDialog.interval.thirtyMinutes") },
    { value: "3600", label: t("preferences.workflowDialog.interval.oneHour") },
    { value: "7200", label: t("preferences.workflowDialog.interval.twoHours") },
    { value: "14400", label: t("preferences.workflowDialog.interval.fourHours") },
    { value: "21600", label: t("preferences.workflowDialog.interval.sixHours") },
    { value: "43200", label: t("preferences.workflowDialog.interval.twelveHours") },
    { value: "86400", label: t("preferences.workflowDialog.interval.twentyFourHours") },
  ]), [deleteUsesFreeSpace, t])

  // Count enabled actions
  const enabledActionsCount = [
    formState.speedLimitsEnabled,
    formState.shareLimitsEnabled,
    formState.pauseEnabled,
    formState.resumeEnabled,
    formState.recheckEnabled,
    formState.reannounceEnabled,
    formState.autoManagementEnabled,
    formState.deleteEnabled,
    formState.tagEnabled,
    formState.categoryEnabled,
    formState.moveEnabled,
    formState.externalProgramEnabled,
    formState.exportToInstanceEnabled,
  ].filter(Boolean).length

  const latestDryRunOperationCount = useMemo(
    () => latestDryRunEvents.reduce((sum, event) => sum + getDryRunImpactCount(event), 0),
    [latestDryRunEvents]
  )

  const previewMutation = useMutation({
    mutationFn: async ({ input, view }: { input: FormState; view: PreviewView }) => {
      const payload = {
        ...buildPayload(input),
        previewLimit: previewPageSize,
        previewOffset: 0,
        previewView: view,
      }
      const minDelay = new Promise(resolve => setTimeout(resolve, 1000))
      try {
        const result = await api.previewAutomation(instanceId, payload)
        await minDelay
        return result
      } catch (error) {
        await minDelay
        throw error
      }
    },
    onSuccess: (result, { input }) => {
      // Last warning before enabling a delete rule (even if 0 matches right now).
      setPreviewInput(input)
      setPreviewResult(result)
      setIsInitialLoading(false)
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.previewFailed"))
      setIsInitialLoading(false)
      setShowConfirmDialog(false)
    },
  })

  const loadMorePreview = useMutation({
    mutationFn: async () => {
      if (!previewInput || !previewResult) {
        throw new Error("Preview data not available")
      }
      const payload = {
        ...buildPayload(previewInput),
        previewLimit: previewPageSize,
        previewOffset: previewResult.examples.length,
        previewView: previewView,
      }
      return api.previewAutomation(instanceId, payload)
    },
    onSuccess: (result) => {
      setPreviewResult(prev => prev ? { ...prev, examples: [...prev.examples, ...result.examples], totalMatches: result.totalMatches } : result)
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.loadMoreFailed"))
    },
  })

  const handleLoadMore = () => {
    if (!previewInput || !previewResult) {
      return
    }
    loadMorePreview.mutate()
  }

  const dryRunNowMutation = useMutation({
    mutationFn: async (input: FormState) => {
      const payload = buildPayload(input)
      return api.dryRunAutomation(instanceId, {
        ...payload,
        enabled: true,
        dryRun: true,
      })
    },
    onSuccess: async (result) => {
      toast.success(t("preferences.workflowDialog.toast.dryRunCompleted"))
      void queryClient.invalidateQueries({ queryKey: ["automation-activity", instanceId] })

      if (result.activities && result.activities.length > 0) {
        const events = [...result.activities].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
        setLatestDryRunEvents(events)
        setLatestDryRunError(null)
        return
      }

      if (result.activityIds && result.activityIds.length > 0) {
        try {
          const activities = await api.getAutomationActivity(instanceId, 1000)
          const activityIDSet = new Set(result.activityIds)
          const events = activities
            .filter((activity) => activityIDSet.has(activity.id))
            .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
          setLatestDryRunEvents(events)
          setLatestDryRunError(events.length === 0 ? t("preferences.workflowDialog.dryRun.detailsUnavailable") : null)
          return
        } catch (error) {
          setLatestDryRunEvents([])
          setLatestDryRunError(error instanceof Error ? error.message : t("preferences.workflowDialog.dryRun.loadSummaryFailed"))
          return
        }
      }

      setLatestDryRunEvents([])
      setLatestDryRunError(t("preferences.workflowDialog.dryRun.noActivityIds"))
    },
    onError: (error) => {
      setLatestDryRunEvents([])
      setLatestDryRunError(null)
      setLatestDryRunStartedAt(null)
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.dryRunFailed"))
    },
  })

  const showLatestDryRunPanel = dryRunNowMutation.isPending ||
    latestDryRunEvents.length > 0 ||
    latestDryRunError !== null ||
    latestDryRunStartedAt !== null

  const livePreviewPayload = useMemo(() => {
    if (!open) return null
    if (!(isDeleteRule || isCategoryRule)) return null
    if (isDeleteRule && !formState.actionCondition) return null
    if (!formState.applyToAllTrackers && normalizeTrackerDomains(formState.trackerDomains).length === 0) return null
    if (!hasValidFreeSpaceSourceForLivePreview(formState)) return null

    try {
      return {
        ...buildPayload(formState),
        previewLimit: livePreviewPageSize,
        previewOffset: 0,
        previewView: "needed" as PreviewView,
      }
    } catch {
      return null
    }
  }, [
    buildPayload,
    formState,
    hasValidFreeSpaceSourceForLivePreview,
    isCategoryRule,
    isDeleteRule,
    livePreviewPageSize,
    open,
  ])

  const livePreviewPayloadKey = useMemo(
    () => livePreviewPayload ? JSON.stringify(livePreviewPayload) : "",
    [livePreviewPayload]
  )

  useEffect(() => {
    if (!livePreviewPayload) {
      livePreviewRequestRef.current += 1
      setLivePreviewResult(null)
      setLivePreviewError(null)
      setIsLivePreviewLoading(false)
      return
    }

    const requestID = livePreviewRequestRef.current + 1
    livePreviewRequestRef.current = requestID
    setIsLivePreviewLoading(true)
    setLivePreviewError(null)

    const timeout = setTimeout(async () => {
      try {
        const result = await api.previewAutomation(instanceId, livePreviewPayload)
        if (livePreviewRequestRef.current !== requestID) return
        setLivePreviewResult(result)
      } catch (error) {
        if (livePreviewRequestRef.current !== requestID) return
        setLivePreviewResult(null)
        setLivePreviewError(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.loadLivePreviewFailed"))
      } finally {
        if (livePreviewRequestRef.current === requestID) {
          setIsLivePreviewLoading(false)
        }
      }
    }, 400)

    return () => clearTimeout(timeout)
  }, [instanceId, livePreviewPayload, livePreviewPayloadKey, t])

  const handleRunDryRunNow = () => {
    const dryRunInput: FormState = { ...formState }

    if (!validateFreeSpaceSource(dryRunInput)) return
    if (!dryRunInput.name.trim()) {
      toast.error(t("preferences.workflowDialog.toast.nameRequired"))
      return
    }
    if (!dryRunInput.applyToAllTrackers && normalizeTrackerDomains(dryRunInput.trackerDomains).length === 0) {
      toast.error(t("preferences.workflowDialog.toast.selectTracker"))
      return
    }
    if (enabledActionsCount === 0) {
      toast.error(t("preferences.workflowDialog.toast.enableAction"))
      return
    }
    if (dryRunInput.deleteEnabled && !dryRunInput.actionCondition) {
      toast.error(t("preferences.workflowDialog.toast.deleteRequiresCondition"))
      return
    }
    if (dryRunInput.moveEnabled && !dryRunInput.exprMovePath.trim()) {
      toast.error(t("preferences.workflowDialog.toast.moveRequiresPath"))
      return
    }
    if (dryRunInput.externalProgramEnabled && !dryRunInput.exprExternalProgramId) {
      toast.error(t("preferences.workflowDialog.toast.selectExternalProgram"))
      return
    }
    if (!validateExportTarget(dryRunInput)) {
      return
    }
    if (dryRunInput.tagEnabled) {
      const validationError = validateTagActions(dryRunInput.exprTagActions, t)
      if (validationError) {
        toast.error(validationError)
        return
      }
    }

    setLatestDryRunStartedAt(new Date().toISOString())
    setLatestDryRunEvents([])
    setLatestDryRunError(null)
    setActivityRunDialog(null)
    dryRunNowMutation.mutate(dryRunInput)
  }

  const validateExportTarget = useCallback((state: FormState): boolean => {
    if (!state.exportToInstanceEnabled) return true
    if (!state.exprExportTargetInstanceId) {
      toast.error(t("preferences.workflowDialog.toast.selectTargetInstance"))
      return false
    }
    // Only check existence when the instances list is available;
    // if still loading/errored, allow the save — backend validates via instanceStore.Get()
    if (nonSelfInstances && !nonSelfInstances.some(i => i.id === state.exprExportTargetInstanceId)) {
      toast.error(t("preferences.workflowDialog.toast.targetInstanceMissing"))
      setFormState(prev => ({ ...prev, exprExportTargetInstanceId: null }))
      return false
    }
    return true
  }, [nonSelfInstances, t])

  const applyEnabledChange = useCallback((checked: boolean, options?: { forceDryRun?: boolean }) => {
    if (checked && isDeleteRule && !formState.actionCondition) {
      toast.error(t("preferences.workflowDialog.toast.deleteRequiresCondition"))
      return
    }
    if (checked && !validateExportTarget(formState)) {
      return
    }

    if (checked && (isDeleteRule || isCategoryRule)) {
      const nextState = {
        ...formState,
        enabled: true,
        dryRun: options?.forceDryRun ? true : formState.dryRun,
      }
      if (!validateFreeSpaceSource(nextState)) {
        return
      }
      setEnabledBeforePreview(formState.enabled)
      setFormState(nextState)
      // Reset preview view to "needed" when starting a new preview
      setPreviewView("needed")
      // Open dialog immediately with loading state
      setPreviewResult(null)
      setIsInitialLoading(true)
      setShowConfirmDialog(true)
      previewMutation.mutate({ input: nextState, view: "needed" })
      return
    }

    setFormState(prev => ({
      ...prev,
      enabled: checked,
      dryRun: options?.forceDryRun ? true : prev.dryRun,
    }))
  }, [formState, isCategoryRule, isDeleteRule, previewMutation, t, validateExportTarget, validateFreeSpaceSource])

  const handleEnabledToggle = useCallback((checked: boolean) => {
    if (checked && !formState.dryRun && !hasPromptedDryRun()) {
      setShowDryRunPrompt(true)
      return
    }
    applyEnabledChange(checked)
  }, [applyEnabledChange, formState.dryRun, hasPromptedDryRun])

  // Handler for switching preview view - refetches with new view and resets pagination
  const handlePreviewViewChange = async (newView: PreviewView) => {
    if (!previewInput) return
    setPreviewView(newView)
    setIsLoadingPreviewView(true)
    try {
      const payload = {
        ...buildPayload(previewInput),
        previewLimit: previewPageSize,
        previewOffset: 0,
        previewView: newView,
      }
      const result = await api.previewAutomation(instanceId, payload)
      setPreviewResult(result)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.switchViewFailed"))
    } finally {
      setIsLoadingPreviewView(false)
    }
  }

  // CSV columns for automation preview export
  const csvColumns: CsvColumn<AutomationPreviewTorrent>[] = [
    { header: t("preferences.workflowDialog.csv.name"), accessor: t => t.name },
    { header: t("preferences.workflowDialog.csv.hash"), accessor: t => t.hash },
    { header: t("preferences.workflowDialog.csv.tracker"), accessor: t => t.tracker },
    { header: t("preferences.workflowDialog.csv.size"), accessor: t => formatBytes(t.size) },
    { header: t("preferences.workflowDialog.csv.ratio"), accessor: item => item.ratio === -1 ? t("preferences.workflowDialog.infinity") : item.ratio.toFixed(2) },
    { header: t("preferences.workflowDialog.csv.seedingTimeSeconds"), accessor: t => t.seedingTime },
    { header: t("preferences.workflowDialog.csv.category"), accessor: t => t.category },
    { header: t("preferences.workflowDialog.csv.tags"), accessor: t => t.tags },
    { header: t("preferences.workflowDialog.csv.state"), accessor: t => t.state },
    { header: t("preferences.workflowDialog.csv.addedOn"), accessor: t => t.addedOn },
    { header: t("preferences.workflowDialog.csv.path"), accessor: t => t.contentPath ?? "" },
    { header: t("preferences.workflowDialog.csv.score"), accessor: t => (t.score !== null && t.score !== undefined) ? t.score.toFixed(2) : "" },
  ]

  const handleExport = async () => {
    if (!previewInput || !previewResult) return

    setIsExporting(true)
    try {
      const pageSize = 500
      const allItems: AutomationPreviewTorrent[] = []
      let offset = 0
      const total = previewResult.totalMatches

      while (allItems.length < total) {
        const payload = {
          ...buildPayload(previewInput),
          previewLimit: pageSize,
          previewOffset: offset,
          previewView,
        }
        const result = await api.previewAutomation(instanceId, payload)
        allItems.push(...result.examples)
        offset += pageSize
        // Safety check in case total changes
        if (result.examples.length === 0) break
      }

      const csv = toCsv(allItems, csvColumns)
      const ruleName = (formState.name || t("preferences.workflowDialog.automationFallbackName")).replace(/[^a-zA-Z0-9-_]/g, "_")
      downloadBlob(csv, `${ruleName}_preview.csv`)
      toast.success(t("preferences.workflowDialog.toast.exportedTorrents", { count: allItems.length }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.exportFailed"))
    } finally {
      setIsExporting(false)
    }
  }

  const createOrUpdate = useMutation({
    mutationFn: async (input: FormState) => {
      const payload = buildPayload(input)
      if (rule) {
        return api.updateAutomation(instanceId, rule.id, payload)
      }
      return api.createAutomation(instanceId, payload)
    },
    onSuccess: () => {
      toast.success(t("preferences.workflowDialog.toast.workflowSaved", { action: rule ? "updated" : "created" }))
      setShowConfirmDialog(false)
      setPreviewResult(null)
      setPreviewInput(null)
      onOpenChange(false)
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
      onSuccess?.()
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.saveFailed"))
    },
  })

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    setRegexErrors([]) // Clear previous errors

    const submitState: FormState = { ...formState }

    if (!validateFreeSpaceSource(submitState)) {
      return
    }

    if (!submitState.name) {
      toast.error(t("preferences.workflowDialog.toast.nameRequired"))
      return
    }
    const selectedTrackers = submitState.trackerDomains.filter(Boolean)
    if (!submitState.applyToAllTrackers && selectedTrackers.length === 0) {
      toast.error(t("preferences.workflowDialog.toast.selectTracker"))
      return
    }

    // At least one action must be enabled
    if (enabledActionsCount === 0) {
      toast.error(t("preferences.workflowDialog.toast.enableAction"))
      return
    }

    // Validate score sorting configuration
    if (submitState.sortingType === "score") {
      if (submitState.scoreRules.length === 0) {
        toast.error(t("preferences.workflowDialog.toast.addScoreRule"))
        return
      }

      // Validate individual rules for valid numeric inputs
      for (const rule of submitState.scoreRules) {
        if (rule.type === "field_multiplier") {
          if (!rule.fieldMultiplier) continue
          const val = rule.fieldMultiplier.multiplier
          const multiplier = typeof val === "string" ? parseFloat(val) : val
          if (!Number.isFinite(multiplier)) {
            toast.error(t("preferences.workflowDialog.toast.fieldMultiplierInvalid"))
            return
          }
        } else if (rule.type === "conditional") {
          if (!rule.conditional) continue
          const val = rule.conditional.score
          const score = typeof val === "string" ? parseFloat(val) : val
          if (!Number.isFinite(score)) {
            toast.error(t("preferences.workflowDialog.toast.conditionalScoreInvalid"))
            return
          }
        }
      }
    }

    // Action-specific validation for enabled actions
    if (submitState.speedLimitsEnabled) {
      // At least one field must be set to something other than "no_change"
      const uploadIsSet = submitState.exprUploadMode !== "no_change" &&
        (submitState.exprUploadMode !== "custom" || (submitState.exprUploadValue !== undefined && submitState.exprUploadValue > 0))
      const downloadIsSet = submitState.exprDownloadMode !== "no_change" &&
        (submitState.exprDownloadMode !== "custom" || (submitState.exprDownloadValue !== undefined && submitState.exprDownloadValue > 0))
      if (!uploadIsSet && !downloadIsSet) {
        toast.error(t("preferences.workflowDialog.toast.setSpeedLimit"))
        return
      }
      // Validate custom values are > 0
      if (submitState.exprUploadMode === "custom" && (submitState.exprUploadValue === undefined || submitState.exprUploadValue <= 0)) {
        toast.error(t("preferences.workflowDialog.toast.uploadSpeedInvalid"))
        return
      }
      if (submitState.exprDownloadMode === "custom" && (submitState.exprDownloadValue === undefined || submitState.exprDownloadValue <= 0)) {
        toast.error(t("preferences.workflowDialog.toast.downloadSpeedInvalid"))
        return
      }
    }
    if (submitState.shareLimitsEnabled) {
      // At least one of the limits must be set to something other than "no_change"
      const ratioIsSet = submitState.exprRatioLimitMode !== "no_change" &&
        (submitState.exprRatioLimitMode !== "custom" || submitState.exprRatioLimitValue !== undefined)
      const seedingTimeIsSet = submitState.exprSeedingTimeMode !== "no_change" &&
        (submitState.exprSeedingTimeMode !== "custom" || submitState.exprSeedingTimeValue !== undefined)
      if (!ratioIsSet && !seedingTimeIsSet) {
        toast.error(t("preferences.workflowDialog.toast.setShareLimit"))
        return
      }
    }
    if (submitState.tagEnabled) {
      const validationError = validateTagActions(submitState.exprTagActions, t)
      if (validationError) {
        toast.error(validationError)
        return
      }
    }
    if (submitState.categoryEnabled) {
      if (!submitState.exprCategory) {
        toast.error(t("preferences.workflowDialog.toast.selectCategory"))
        return
      }
    }
    if (submitState.externalProgramEnabled) {
      if (!submitState.exprExternalProgramId) {
        toast.error(t("preferences.workflowDialog.toast.selectExternalProgram"))
        return
      }
    }
    if (!validateExportTarget(submitState)) {
      return
    }
    if (submitState.deleteEnabled && !submitState.actionCondition) {
      toast.error(t("preferences.workflowDialog.toast.deleteRequiresCondition"))
      return
    }
    const trimmedSubmitMovePath = submitState.exprMovePath?.trim()
    if (submitState.moveEnabled && !trimmedSubmitMovePath) {
      toast.error(t("preferences.workflowDialog.toast.moveRequiresPath"))
      return
    }

    // Validate regex patterns before saving (only if enabling the workflow)
    const payload = buildPayload(submitState)
    if (submitState.enabled) {
      try {
        const validation = await api.validateAutomationRegex(instanceId, payload)
        if (!validation.valid && validation.errors.length > 0) {
          setRegexErrors(validation.errors)
          toast.error(t("preferences.workflowDialog.toast.invalidRegexPatternUnsupported"))
          return
        }
      } catch {
        // If validation endpoint fails, let the save attempt proceed
        // The backend will still reject invalid regexes
      }
    }

    // For delete and category rules, show preview as a last warning before enabling.
    const needsPreview = (isDeleteRule || isCategoryRule) && submitState.enabled
    if (needsPreview) {
      // Reset preview view to "needed" when starting a new preview
      setPreviewView("needed")
      // Open dialog immediately with loading state
      setPreviewResult(null)
      setIsInitialLoading(true)
      setShowConfirmDialog(true)
      previewMutation.mutate({ input: submitState, view: "needed" })
    } else {
      createOrUpdate.mutate(submitState)
    }
  }

  const handleConfirmSave = () => {
    // Clear the stored value so onOpenChange won't restore it after successful save
    setEnabledBeforePreview(null)
    if (!validateFreeSpaceSource(formState)) {
      return
    }
    if (!validateExportTarget(formState)) {
      return
    }
    createOrUpdate.mutate(formState)
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-4xl lg:max-w-5xl max-h-[90dvh] flex flex-col p-2 sm:p-6">
          {/* Container for portaled dropdowns - outside scroll area but inside dialog */}
          <div ref={dropdownContainerRef} className="absolute inset-0 pointer-events-none overflow-visible" style={{ zIndex: 100 }}>
            {/* Dropdown portals render here */}
          </div>
          <DialogHeader>
            <DialogTitle>{rule ? t("preferences.workflowDialog.editWorkflow") : t("preferences.workflowDialog.addWorkflow")}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
            <div className="flex-1 overflow-y-auto space-y-3 sm:pr-1">
              {/* Header row: Name + All Trackers toggle */}
              <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
                <div className="space-y-1.5">
                  <Label htmlFor="rule-name">{t("preferences.workflowDialog.nameLabel")}</Label>
                  <Input
                    id="rule-name"
                    value={formState.name}
                    onChange={(e) => setFormState(prev => ({ ...prev, name: e.target.value }))}
                    required
                    placeholder={t("preferences.workflowDialog.namePlaceholder")}
                    autoComplete="off"
                    data-1p-ignore
                  />
                </div>
                <div className="flex items-center gap-2 rounded-md border px-3 py-2">
                  <Switch
                    id="all-trackers"
                    checked={formState.applyToAllTrackers}
                    onCheckedChange={(checked) => setFormState(prev => ({
                      ...prev,
                      applyToAllTrackers: checked,
                      trackerDomains: checked ? [] : prev.trackerDomains,
                    }))}
                  />
                  <Label htmlFor="all-trackers" className="text-sm cursor-pointer whitespace-nowrap">{t("preferences.workflowDialog.allTrackers")}</Label>
                </div>
              </div>

              {/* Trackers */}
              {!formState.applyToAllTrackers && (
                <div className="space-y-1.5">
                  <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                    <Label>{t("preferences.workflowDialog.trackersLabel")}</Label>
                    <div className="flex items-center border rounded-md">
                      <Button
                        type="button"
                        variant={formState.trackerMatchMode === "include" ? "secondary" : "ghost"}
                        size="sm"
                        className="px-2 h-7 rounded-r-none text-xs"
                        onClick={() => setFormState(prev => ({ ...prev, trackerMatchMode: "include" }))}
                      >
                        {t("preferences.workflowDialog.trackerMatchInclude")}
                      </Button>
                      <div className="w-[1px] bg-border h-4" />
                      <Button
                        type="button"
                        variant={formState.trackerMatchMode === "exclude" ? "secondary" : "ghost"}
                        size="sm"
                        className="px-2 h-7 rounded-l-none text-xs"
                        onClick={() => setFormState(prev => ({ ...prev, trackerMatchMode: "exclude" }))}
                      >
                        {t("preferences.workflowDialog.trackerMatchExclude")}
                      </Button>
                    </div>
                  </div>
                  <MultiSelect
                    options={trackerOptions}
                    selected={formState.trackerDomains}
                    onChange={(next) => setFormState(prev => ({
                      ...prev,
                      trackerDomains: next,
                      trackerMatchMode: prev.trackerMatchMode === "mixed" ? "include" : prev.trackerMatchMode,
                    }))}
                    placeholder={t("preferences.workflowDialog.trackersPlaceholder")}
                    creatable
                    onCreateOption={(value) => setFormState(prev => ({
                      ...prev,
                      trackerDomains: [...prev.trackerDomains, value],
                      trackerMatchMode: prev.trackerMatchMode === "mixed" ? "include" : prev.trackerMatchMode,
                    }))}
                    disabled={trackersQuery.isLoading}
                    hideCheckIcon
                  />
                </div>
              )}

              {/* Torrent Priority */}
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Label>{t("preferences.workflowDialog.priority.title")}</Label>
                    <TooltipProvider delayDuration={300}>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Info className="h-4 w-4 text-muted-foreground hover:text-foreground cursor-help transition-colors" />
                        </TooltipTrigger>
                        <TooltipContent side="right" className="max-w-[300px]">
                          <p>{t("preferences.workflowDialog.priority.description")}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  </div>
                  <Select
                    value={formState.sortingType}
                    onValueChange={(val: "default" | "simple" | "score") => setFormState(prev => ({ ...prev, sortingType: val }))}
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="default">{t("preferences.workflowDialog.priority.types.default")}</SelectItem>
                      <SelectItem value="simple">{t("preferences.workflowDialog.priority.types.simple")}</SelectItem>
                      <SelectItem value="score">{t("preferences.workflowDialog.priority.types.score")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {formState.sortingType === "simple" && (
                  <div className="flex gap-2 items-center rounded-lg border p-3">
                    <span className="text-sm text-muted-foreground">{t("preferences.workflowDialog.priority.sortBy")}</span>
                    <FieldCombobox
                      value={formState.simpleSortField}
                      onChange={(val) => setFormState(prev => ({ ...prev, simpleSortField: val as ConditionField }))}
                      disabledFields={SIMPLE_SORT_DISABLED_FIELDS}
                    />
                    <div className="flex items-center border rounded-md">
                      <Button
                        type="button"
                        variant={formState.sortDirection === "ASC" ? "secondary" : "ghost"}
                        size="sm"
                        className="px-2 h-8 rounded-r-none"
                        onClick={() => setFormState(prev => ({ ...prev, sortDirection: "ASC" }))}
                      >
                        <ArrowUp className="h-3.5 w-3.5 mr-1" />
                        {t("preferences.workflowDialog.priority.asc")}
                      </Button>
                      <div className="w-[1px] bg-border h-4" />
                      <Button
                        type="button"
                        variant={formState.sortDirection === "DESC" ? "secondary" : "ghost"}
                        size="sm"
                        className="px-2 h-8 rounded-l-none"
                        onClick={() => setFormState(prev => ({ ...prev, sortDirection: "DESC" }))}
                      >
                        <ArrowDown className="h-3.5 w-3.5 mr-1" />
                        {t("preferences.workflowDialog.priority.desc")}
                      </Button>
                    </div>
                  </div>
                )}

                {formState.sortingType === "score" && (
                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <Label className="text-xs text-muted-foreground uppercase tracking-wider">{t("preferences.workflowDialog.priority.scoreRules")}</Label>
                      <div className="flex items-center border rounded-md">
                        <Button
                          type="button"
                          variant={formState.sortDirection === "ASC" ? "secondary" : "ghost"}
                          size="sm"
                          className="px-2 h-7 rounded-r-none text-xs"
                          onClick={() => setFormState(prev => ({ ...prev, sortDirection: "ASC" }))}
                        >
                          <ArrowUp className="h-3 w-3 mr-1" />
                          {t("preferences.workflowDialog.priority.lowToHigh")}
                        </Button>
                        <div className="w-[1px] bg-border h-4" />
                        <Button
                          type="button"
                          variant={formState.sortDirection === "DESC" ? "secondary" : "ghost"}
                          size="sm"
                          className="px-2 h-7 rounded-l-none text-xs"
                          onClick={() => setFormState(prev => ({ ...prev, sortDirection: "DESC" }))}
                        >
                          <ArrowDown className="h-3 w-3 mr-1" />
                          {t("preferences.workflowDialog.priority.highToLow")}
                        </Button>
                      </div>
                    </div>
                    {formState.scoreRules.map((rule, idx) => (
                      <div key={rule.id} className="p-3 border rounded-md relative group">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="absolute top-2 right-2 h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity"
                          onClick={() => setFormState(prev => ({
                            ...prev,
                            scoreRules: prev.scoreRules.filter((_, i) => i !== idx),
                          }))}
                        >
                          <X className="h-4 w-4" />
                        </Button>

                        <div className="mb-2">
                          <Badge variant="secondary" className="text-xs uppercase tracking-wider font-mono">
                            {rule.type === "field_multiplier" ? t("preferences.workflowDialog.priority.fieldMultiplier") : t("preferences.workflowDialog.priority.conditional")}
                          </Badge>
                        </div>

                        {rule.type === "field_multiplier" && rule.fieldMultiplier ? (
                          <div className="flex items-center gap-2 flex-wrap">
                            <FieldCombobox
                              value={rule.fieldMultiplier.field}
                              onChange={(val) => {
                                const newRules = [...formState.scoreRules]
                                if (newRules[idx].type === "field_multiplier" && newRules[idx].fieldMultiplier) {
                                  newRules[idx] = {
                                    ...newRules[idx],
                                    fieldMultiplier: { ...newRules[idx].fieldMultiplier!, field: val as ConditionField },
                                  }
                                  setFormState(prev => ({ ...prev, scoreRules: newRules }))
                                }
                              }}
                              disabledFields={SCORE_MULTIPLIER_DISABLED_FIELDS}
                            />

                            <span className="text-sm text-muted-foreground">x</span>

                            <Input
                              type="number"
                              className="w-24"
                              value={rule.fieldMultiplier.multiplier}
                              onChange={(e) => {
                                const newRules = [...formState.scoreRules]
                                if (newRules[idx].type === "field_multiplier" && newRules[idx].fieldMultiplier) {
                                  newRules[idx] = {
                                    ...newRules[idx],
                                    fieldMultiplier: { ...newRules[idx].fieldMultiplier!, multiplier: e.target.value },
                                  }
                                  setFormState(prev => ({ ...prev, scoreRules: newRules }))
                                }
                              }}
                            />
                          </div>
                        ) : rule.type === "conditional" && rule.conditional ? (
                          <div className="space-y-2">
                            <QueryBuilder
                              condition={rule.conditional.condition ?? null}
                              onChange={(cond) => {
                                const newRules = [...formState.scoreRules]
                                if (newRules[idx].type === "conditional" && newRules[idx].conditional) {
                                  newRules[idx] = {
                                    ...newRules[idx],
                                    conditional: { ...newRules[idx].conditional!, condition: cond ?? undefined },
                                  }
                                  setFormState(prev => ({ ...prev, scoreRules: newRules }))
                                }
                              }}
                              categoryOptions={categoryOptions}
                              disabledFields={getDisabledFields(fieldCapabilities)}
                              disabledStateValues={getDisabledStateValues(fieldCapabilities)}
                            />
                            <div className="flex items-center gap-2">
                              <span className="text-sm text-muted-foreground">{t("preferences.workflowDialog.priority.addScore")}</span>
                              <Input
                                type="number"
                                className="w-24"
                                value={rule.conditional.score}
                                onChange={(e) => {
                                  const newRules = [...formState.scoreRules]
                                  if (newRules[idx].type === "conditional" && newRules[idx].conditional) {
                                    newRules[idx] = {
                                      ...newRules[idx],
                                      conditional: { ...newRules[idx].conditional!, score: e.target.value },
                                    }
                                    setFormState(prev => ({ ...prev, scoreRules: newRules }))
                                  }
                                }}
                              />
                            </div>
                          </div>
                        ) : null}
                      </div>
                    ))}

                    <div className="flex gap-2">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => setFormState(prev => ({
                          ...prev,
                          scoreRules: [...prev.scoreRules, {
                            id: ++ruleIdCounter,
                            type: "field_multiplier",
                            fieldMultiplier: { field: "SIZE", multiplier: 1 },
                          }],
                        }))}
                      >
                        <Plus className="h-3.5 w-3.5 mr-1" />
                        {t("preferences.workflowDialog.priority.addFieldMultiplier")}
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => setFormState(prev => ({
                          ...prev,
                          scoreRules: [...prev.scoreRules, {
                            id: ++ruleIdCounter,
                            type: "conditional",
                            conditional: {
                              condition: { field: "NAME", operator: "CONTAINS", value: "" },
                              score: 100,
                            },
                          }],
                        }))}
                      >
                        <Plus className="h-3.5 w-3.5 mr-1" />
                        {t("preferences.workflowDialog.priority.addBonus")}
                      </Button>
                    </div>
                  </div>
                )}


              </div>

              {/* Condition and Action */}
              <div className="space-y-3">
                {/* Query Builder */}
                <div className="space-y-1.5">
                  <Label>{t("preferences.workflowDialog.conditionsLabel")}</Label>
                  <QueryBuilder
                    condition={formState.actionCondition}
                    onChange={(condition) => {
                      setFormState(prev => ({ ...prev, actionCondition: condition }))
                      setRegexErrors([]) // Clear errors when condition changes
                    }}
                    allowEmpty
                    categoryOptions={categoryOptions}
                    disabledFields={getDisabledFields(fieldCapabilities)}
                    disabledStateValues={getDisabledStateValues(fieldCapabilities)}
                    groupOptions={groupedConditionOptions}
                  />
                  {formState.deleteEnabled && !formState.actionCondition && (
                    <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm">
                      <p className="font-medium text-destructive">{t("preferences.workflowDialog.toast.deleteRequiresCondition")}</p>
                    </div>
                  )}
                  {regexErrors.length > 0 && (
                    <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm">
                      <p className="font-medium text-destructive mb-1">{t("preferences.workflowDialog.invalidRegexPattern")}</p>
                      {regexErrors.map((err, i) => (
                        <p key={i} className="text-destructive/80 text-xs">
                          <span className="font-mono">{err.pattern}</span>: {err.message}
                        </p>
                      ))}
                      <p className="text-muted-foreground text-xs mt-2">{t("preferences.workflowDialog.regexHelp")}</p>
                    </div>
                  )}

                  {(isDeleteRule || isCategoryRule) && (
                    <div className="rounded-md border bg-muted/20 p-3 space-y-2">
                      <div className="flex items-center justify-between gap-2">
                        <p className="text-sm font-medium">{t("preferences.workflowDialog.liveImpactPreview")}</p>
                        {isLivePreviewLoading && (
                          <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            {t("preferences.workflowDialog.updating")}
                          </span>
                        )}
                      </div>
                      {livePreviewError ? (
                        <p className="text-xs text-destructive">{livePreviewError}</p>
                      ) : !livePreviewResult ? (
                        <p className="text-xs text-muted-foreground">
                          {t("preferences.workflowDialog.addConditionsToPreview")}
                        </p>
                      ) : (
                        <>
                          <p className="text-xs text-muted-foreground">
                            {isCategoryRule ? t("preferences.workflowDialog.torrentsImpactedWithCrossSeeds", { total: livePreviewResult.totalMatches, direct: (livePreviewResult.totalMatches) - (livePreviewResult.crossSeedCount ?? 0), crossSeeds: livePreviewResult.crossSeedCount ?? 0 }) : t("preferences.workflowDialog.torrentsImpacted", { total: livePreviewResult.totalMatches })}
                          </p>
                          {livePreviewResult.examples.length > 0 ? (
                            <div className="space-y-1">
                              {livePreviewResult.examples.map((example) => (
                                <div key={example.hash} className="text-xs text-foreground/90 truncate">
                                  {example.name}
                                </div>
                              ))}
                            </div>
                          ) : (
                            <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.noCurrentMatches")}</p>
                          )}
                        </>
                      )}
                    </div>
                  )}
                </div>

                {/* Grouping Configuration - shown when GROUP_SIZE or IS_GROUPED is used */}
                {(conditionUsesField(formState.actionCondition, "GROUP_SIZE") || conditionUsesField(formState.actionCondition, "IS_GROUPED")) && (
                  <div className="rounded-lg border p-3 space-y-3 bg-muted/30">
                    <div className="flex items-center gap-2">
                      <Label className="text-sm font-medium">{t("preferences.workflowDialog.grouping.title")}</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label={t("preferences.workflowDialog.grouping.aria")}
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="right" className="max-w-[340px]">
                            <p>{t("preferences.workflowDialog.grouping.tooltip")}</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>

                    <p className="text-xs text-muted-foreground">
                      {t("preferences.workflowDialog.grouping.description")}
                    </p>

                    <div className="rounded-sm border border-border/50 bg-background p-2 text-xs space-y-1.5">
                      <p className="font-medium text-foreground">{t("preferences.workflowDialog.grouping.builtInTitle")}</p>
                      <div className="space-y-1">
                        {BUILTIN_GROUPS.map((group) => (
                          <div key={group.id}>
                            <p className="font-medium">{t(group.labelKey)}</p>
                            <p className="text-muted-foreground">{t(group.descriptionKey)}</p>
                          </div>
                        ))}
                      </div>
                    </div>

                    {/* Custom groups editor */}
                    {(formState.exprGrouping?.groups || []).length > 0 && (
                      <div className="space-y-2 border-t pt-3">
                        <p className="text-xs font-medium text-muted-foreground">
                          {t("preferences.workflowDialog.grouping.customGroupsTitle")}
                        </p>
                        {(formState.exprGrouping?.groups || []).map((group, idx) => (
                          <div key={group.id} className="border rounded-sm p-2 space-y-1.5 text-xs bg-background">
                            <div className="flex items-center justify-between gap-1">
                              <div className="flex-1 min-w-0">
                                <p className="font-mono font-medium truncate">{group.id}</p>
                                <p className="text-muted-foreground">{t("preferences.workflowDialog.grouping.keys", { keys: group.keys.join(", ") })}</p>
                              </div>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon"
                                className="h-6 w-6 shrink-0"
                                onClick={() => {
                                  setFormState(prev => ({
                                    ...prev,
                                    exprGrouping: {
                                      ...prev.exprGrouping,
                                      groups: (prev.exprGrouping?.groups || []).filter((_, i) => i !== idx),
                                      defaultGroupId: prev.exprGrouping?.defaultGroupId === group.id? undefined: prev.exprGrouping?.defaultGroupId,
                                    },
                                  }))
                                }}
                              >
                                <X className="h-3 w-3" />
                              </Button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}

                    {/* Add custom group button */}
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="w-full text-xs h-7"
                      onClick={() => {
                        setShowAddCustomGroup(true)
                      }}
                    >
                      <Plus className="h-3 w-3 mr-1" />
                      {t("preferences.workflowDialog.grouping.addCustomGroup")}
                    </Button>
                  </div>
                )}

                {/* Actions section */}
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <Label>{t("preferences.workflowDialog.actions.title")}</Label>
                    {/* Add action dropdown - only show if Delete is not enabled, at least one action exists, and there are available actions to add */}
                    {!formState.deleteEnabled && enabledActionsCount > 0 && (() => {
                      const enabledActions = getEnabledActions(formState)
                      const availableActions = COMBINABLE_ACTIONS.filter(a => !enabledActions.includes(a))
                      if (availableActions.length === 0) return null
                      return (
                        <Select
                          value=""
                          onValueChange={(action: ActionType) => {
                            setFormState(prev => {
                              const next = { ...prev, ...setActionEnabled(action, true) }
                              if (action === "tag" && next.exprTagActions.length === 0) {
                                next.exprTagActions = [createDefaultTagAction()]
                              }
                              return next
                            })
                          }}
                        >
                          <SelectTrigger className="w-fit h-7 text-xs">
                            <Plus className="h-3 w-3 mr-1" />
                            {t("preferences.workflowDialog.actions.addAction")}
                          </SelectTrigger>
                          <SelectContent>
                            {availableActions.map(action => (
                              <SelectItem key={action} value={action}>{t(ACTION_LABEL_KEYS[action])}</SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      )
                    })()}
                  </div>

                  {/* No actions selected - show selector */}
                  {enabledActionsCount === 0 && (
                    <Select
                      value=""
                      onValueChange={(action: ActionType) => {
                        if (action === "delete") {
                          // Delete is standalone - clear all others and set delete
                          setFormState(prev => ({
                            ...prev,
                            speedLimitsEnabled: false,
                            shareLimitsEnabled: false,
                            pauseEnabled: false,
                            resumeEnabled: false,
                            recheckEnabled: false,
                            reannounceEnabled: false,
                            autoManagementEnabled: false,
                            deleteEnabled: true,
                            tagEnabled: false,
                            categoryEnabled: false,
                            moveEnabled: false,
                            externalProgramEnabled: false,
                            exportToInstanceEnabled: false,
                            // Safety: when selecting delete in "create new" mode, start disabled
                            enabled: !rule ? false : prev.enabled,
                          }))
                        } else {
                          setFormState(prev => {
                            const next = { ...prev, ...setActionEnabled(action, true) }
                            if (action === "tag" && next.exprTagActions.length === 0) {
                              next.exprTagActions = [createDefaultTagAction()]
                            }
                            return next
                          })
                        }
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder={t("preferences.workflowDialog.actions.selectAction")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="speedLimits">{t("preferences.workflowDialog.actions.speedLimits")}</SelectItem>
                        <SelectItem value="shareLimits">{t("preferences.workflowDialog.actions.shareLimits")}</SelectItem>
                        <SelectItem value="pause">{t("preferences.workflowDialog.actions.pause")}</SelectItem>
                        <SelectItem value="resume">{t("preferences.workflowDialog.actions.resume")}</SelectItem>
                        <SelectItem value="recheck">{t("preferences.workflowDialog.actions.recheck")}</SelectItem>
                        <SelectItem value="reannounce">{t("preferences.workflowDialog.actions.reannounce")}</SelectItem>
                        <SelectItem value="tag">{t("preferences.workflowDialog.actions.tag")}</SelectItem>
                        <SelectItem value="category">{t("preferences.workflowDialog.actions.category")}</SelectItem>
                        <SelectItem value="move">{t("preferences.workflowDialog.actions.move")}</SelectItem>
                        <SelectItem value="externalProgram">{t("preferences.workflowDialog.actions.externalProgram")}</SelectItem>
                        <SelectItem value="autoManagement">{t("preferences.workflowDialog.actions.autoManagement")}</SelectItem>
                        <SelectItem value="exportToInstance">{t("preferences.workflowDialog.actions.exportToInstance")}</SelectItem>
                        <SelectItem value="delete" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.actions.deleteStandalone")}</SelectItem>
                      </SelectContent>
                    </Select>
                  )}

                  {/* Render enabled actions */}
                  <div className="space-y-3">
                    {/* Speed limits */}
                    {formState.speedLimitsEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.speedLimits")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, speedLimitsEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="space-y-3">
                          {/* Upload limit */}
                          <div className="space-y-1.5">
                            <Label className="text-xs">{t("preferences.workflowDialog.speedLimits.uploadLimit")}</Label>
                            <div className="flex gap-2">
                              <Select
                                value={formState.exprUploadMode}
                                onValueChange={(value: SpeedLimitMode) => setFormState(prev => ({
                                  ...prev,
                                  exprUploadMode: value,
                                  exprUploadValue: value === "custom" ? prev.exprUploadValue : undefined,
                                }))}
                              >
                                <SelectTrigger className="w-[140px]">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="no_change">{t("preferences.workflowDialog.option.noChange")}</SelectItem>
                                  <SelectItem value="unlimited">{t("preferences.workflowDialog.option.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{t("preferences.workflowDialog.option.custom")}</SelectItem>
                                </SelectContent>
                              </Select>
                              {formState.exprUploadMode === "custom" && (
                                <div className="flex gap-1 flex-1">
                                  <Input
                                    type="number"
                                    min={1}
                                    className="w-24"
                                    value={formState.exprUploadValue !== undefined ? formState.exprUploadValue / uploadSpeedUnit : ""}
                                    onChange={(e) => {
                                      const rawValue = e.target.value
                                      if (rawValue === "") {
                                        setFormState(prev => ({ ...prev, exprUploadValue: undefined }))
                                        return
                                      }

                                      const parsed = Number(rawValue)
                                      if (Number.isNaN(parsed)) {
                                        return
                                      }

                                      setFormState(prev => ({
                                        ...prev,
                                        exprUploadValue: Math.round(parsed * uploadSpeedUnit),
                                      }))
                                    }}
                                    placeholder={t("preferences.workflowDialog.speedLimits.placeholder")}
                                  />
                                  <Select
                                    value={String(uploadSpeedUnit)}
                                    onValueChange={(v) => {
                                      const newUnit = Number(v)
                                      if (formState.exprUploadValue !== undefined) {
                                        const displayValue = formState.exprUploadValue / uploadSpeedUnit
                                        setFormState(prev => ({
                                          ...prev,
                                          exprUploadValue: Math.round(displayValue * newUnit),
                                        }))
                                      }
                                      setUploadSpeedUnit(newUnit)
                                    }}
                                  >
                                    <SelectTrigger className="w-fit">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      {SPEED_LIMIT_UNITS.map((u) => (
                                        <SelectItem key={u.value} value={String(u.value)}>{u.label}</SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>
                                </div>
                              )}
                            </div>
                          </div>
                          {/* Download limit */}
                          <div className="space-y-1.5">
                            <Label className="text-xs">{t("preferences.workflowDialog.speedLimits.downloadLimit")}</Label>
                            <div className="flex gap-2">
                              <Select
                                value={formState.exprDownloadMode}
                                onValueChange={(value: SpeedLimitMode) => setFormState(prev => ({
                                  ...prev,
                                  exprDownloadMode: value,
                                  exprDownloadValue: value === "custom" ? prev.exprDownloadValue : undefined,
                                }))}
                              >
                                <SelectTrigger className="w-[140px]">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="no_change">{t("preferences.workflowDialog.option.noChange")}</SelectItem>
                                  <SelectItem value="unlimited">{t("preferences.workflowDialog.option.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{t("preferences.workflowDialog.option.custom")}</SelectItem>
                                </SelectContent>
                              </Select>
                              {formState.exprDownloadMode === "custom" && (
                                <div className="flex gap-1 flex-1">
                                  <Input
                                    type="number"
                                    min={1}
                                    className="w-24"
                                    value={formState.exprDownloadValue !== undefined ? formState.exprDownloadValue / downloadSpeedUnit : ""}
                                    onChange={(e) => {
                                      const rawValue = e.target.value
                                      if (rawValue === "") {
                                        setFormState(prev => ({ ...prev, exprDownloadValue: undefined }))
                                        return
                                      }

                                      const parsed = Number(rawValue)
                                      if (Number.isNaN(parsed)) {
                                        return
                                      }

                                      setFormState(prev => ({
                                        ...prev,
                                        exprDownloadValue: Math.round(parsed * downloadSpeedUnit),
                                      }))
                                    }}
                                    placeholder={t("preferences.workflowDialog.speedLimits.placeholder")}
                                  />
                                  <Select
                                    value={String(downloadSpeedUnit)}
                                    onValueChange={(v) => {
                                      const newUnit = Number(v)
                                      if (formState.exprDownloadValue !== undefined) {
                                        const displayValue = formState.exprDownloadValue / downloadSpeedUnit
                                        setFormState(prev => ({
                                          ...prev,
                                          exprDownloadValue: Math.round(displayValue * newUnit),
                                        }))
                                      }
                                      setDownloadSpeedUnit(newUnit)
                                    }}
                                  >
                                    <SelectTrigger className="w-fit">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      {SPEED_LIMIT_UNITS.map((u) => (
                                        <SelectItem key={u.value} value={String(u.value)}>{u.label}</SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>
                                </div>
                              )}
                            </div>
                          </div>
                        </div>
                      </div>
                    )}

                    {/* Share limits */}
                    {formState.shareLimitsEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.shareLimits")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, shareLimitsEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="space-y-3">
                          {/* Ratio limit */}
                          <div className="space-y-1.5">
                            <Label className="text-xs">{t("preferences.workflowDialog.shareLimits.ratioLimit")}</Label>
                            <div className="flex gap-2">
                              <Select
                                value={formState.exprRatioLimitMode}
                                onValueChange={(value: FormState["exprRatioLimitMode"]) => setFormState(prev => ({
                                  ...prev,
                                  exprRatioLimitMode: value,
                                  exprRatioLimitValue: value === "custom" ? prev.exprRatioLimitValue : undefined,
                                }))}
                              >
                                <SelectTrigger className="w-[140px]">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="no_change">{t("preferences.workflowDialog.option.noChange")}</SelectItem>
                                  <SelectItem value="global">{t("preferences.workflowDialog.option.useGlobal")}</SelectItem>
                                  <SelectItem value="unlimited">{t("preferences.workflowDialog.option.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{t("preferences.workflowDialog.option.custom")}</SelectItem>
                                </SelectContent>
                              </Select>
                              {formState.exprRatioLimitMode === "custom" && (
                                <Input
                                  type="number"
                                  step="0.01"
                                  min={0}
                                  className="flex-1"
                                  value={formState.exprRatioLimitValue ?? ""}
                                  onChange={(e) => {
                                    const val = e.target.value
                                    const parsed = parseFloat(val)
                                    setFormState(prev => ({
                                      ...prev,
                                      exprRatioLimitValue: val === "" ? undefined : (Number.isFinite(parsed) ? parsed : prev.exprRatioLimitValue),
                                    }))
                                  }}
                                  placeholder={t("preferences.workflowDialog.shareLimits.ratioPlaceholder")}
                                />
                              )}
                            </div>
                          </div>
                          {/* Seed time */}
                          <div className="space-y-1.5">
                            <Label className="text-xs">{t("preferences.workflowDialog.shareLimits.seedTime")}</Label>
                            <div className="flex gap-2">
                              <Select
                                value={formState.exprSeedingTimeMode}
                                onValueChange={(value: FormState["exprSeedingTimeMode"]) => setFormState(prev => ({
                                  ...prev,
                                  exprSeedingTimeMode: value,
                                  exprSeedingTimeValue: value === "custom" ? prev.exprSeedingTimeValue : undefined,
                                }))}
                              >
                                <SelectTrigger className="w-[140px]">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="no_change">{t("preferences.workflowDialog.option.noChange")}</SelectItem>
                                  <SelectItem value="global">{t("preferences.workflowDialog.option.useGlobal")}</SelectItem>
                                  <SelectItem value="unlimited">{t("preferences.workflowDialog.option.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{t("preferences.workflowDialog.option.custom")}</SelectItem>
                                </SelectContent>
                              </Select>
                              {formState.exprSeedingTimeMode === "custom" && (
                                <Input
                                  type="number"
                                  min={0}
                                  className="flex-1"
                                  value={formState.exprSeedingTimeValue ?? ""}
                                  onChange={(e) => {
                                    const val = e.target.value
                                    const parsed = parseInt(val, 10)
                                    setFormState(prev => ({
                                      ...prev,
                                      exprSeedingTimeValue: val === "" ? undefined : (Number.isFinite(parsed) ? parsed : prev.exprSeedingTimeValue),
                                    }))
                                  }}
                                  placeholder={t("preferences.workflowDialog.shareLimits.seedTimePlaceholder")}
                                />
                              )}
                            </div>
                          </div>
                          {capabilities?.supportsShareLimitsAction && (
                            <div className="space-y-1.5">
                              <Label className="text-xs">{t("preferences.workflowDialog.shareLimitAction.label")}</Label>
                              <Select
                                value={formState.exprShareLimitAction}
                                onValueChange={(value: string) => setFormState(prev => ({
                                  ...prev,
                                  exprShareLimitAction: value,
                                }))}
                              >
                                <SelectTrigger>
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="default">{t("preferences.workflowDialog.shareLimitAction.default")}</SelectItem>
                                  <SelectItem value="Stop">{t("preferences.workflowDialog.shareLimitAction.stop")}</SelectItem>
                                  <SelectItem value="Remove">{t("preferences.workflowDialog.shareLimitAction.remove")}</SelectItem>
                                  <SelectItem value="RemoveWithContent">{t("preferences.workflowDialog.shareLimitAction.removeWithContent")}</SelectItem>
                                  <SelectItem value="EnableSuperSeeding">{t("preferences.workflowDialog.shareLimitAction.enableSuperSeeding")}</SelectItem>
                                </SelectContent>
                              </Select>
                            </div>
                          )}
                          {capabilities?.supportsShareLimitsMode && (
                            <div className="space-y-1.5">
                              <Label className="text-xs">{t("preferences.workflowDialog.shareLimitsMode.label")}</Label>
                              <Select
                                value={formState.exprShareLimitsMode}
                                onValueChange={(value: string) => setFormState(prev => ({
                                  ...prev,
                                  exprShareLimitsMode: value,
                                }))}
                              >
                                <SelectTrigger>
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="default">{t("preferences.workflowDialog.shareLimitsMode.default")}</SelectItem>
                                  <SelectItem value="MatchAny">{t("preferences.workflowDialog.shareLimitsMode.matchAny")}</SelectItem>
                                  <SelectItem value="MatchAll">{t("preferences.workflowDialog.shareLimitsMode.matchAll")}</SelectItem>
                                </SelectContent>
                              </Select>
                            </div>
                          )}
                        </div>
                      </div>
                    )}

                    {/* Pause */}
                    {formState.pauseEnabled && (
                      <div className="rounded-lg border p-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.pause")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, pauseEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </div>
                    )}
                    {/* Resume */}
                    {formState.resumeEnabled && (
                      <div className="rounded-lg border p-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.resume")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, resumeEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </div>
                    )}
                    {/* Recheck */}
                    {formState.recheckEnabled && (
                      <div className="rounded-lg border p-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.recheck")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, recheckEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </div>
                    )}
                    {/* Reannounce */}
                    {formState.reannounceEnabled && (
                      <div className="rounded-lg border p-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.reannounce")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, reannounceEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </div>
                    )}
                    {/* Auto management */}
                    {formState.autoManagementEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.autoManagement")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, autoManagementEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <Select
                          value={formState.autoManageMode}
                          onValueChange={(v) => setFormState(prev => ({ ...prev, autoManageMode: v as "enable" | "disable" }))}
                        >
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="enable">{t("preferences.workflowDialog.autoManagement.enable")}</SelectItem>
                            <SelectItem value="disable">{t("preferences.workflowDialog.autoManagement.disable")}</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                    )}
                    {/* Tag */}
                    {formState.tagEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.tag.title")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, tagEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="space-y-3">
                          {formState.exprTagActions.map((tagAction, index) => (
                            <div key={`tag-action-${index}`} className="rounded-md border bg-muted/20 p-3 space-y-3">
                              <div className="flex items-center justify-between">
                                <Label className="text-xs font-medium">{t("preferences.workflowDialog.tag.actionN", { count: index + 1 })}</Label>
                                {formState.exprTagActions.length > 1 && (
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon"
                                    className="h-6 w-6"
                                    onClick={() => setFormState(prev => ({
                                      ...prev,
                                      exprTagActions: prev.exprTagActions.filter((_, i) => i !== index),
                                    }))}
                                  >
                                    <X className="h-3.5 w-3.5" />
                                  </Button>
                                )}
                              </div>
                              <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto_auto] gap-3 items-start">
                                {tagAction.useTrackerAsTag ? (
                                  <div className="space-y-1">
                                    <Label className="text-xs text-muted-foreground">{t("preferences.workflowDialog.tag.tagsDerivedFromTracker")}</Label>
                                    <div className="flex items-center gap-2 h-9 px-3 rounded-md border bg-muted/50 text-sm text-muted-foreground">
                                      {t("preferences.workflowDialog.tag.tagsDerivedDescription")}
                                    </div>
                                  </div>
                                ) : (
                                  <div className="space-y-1">
                                    <Label className="text-xs">{t("preferences.workflowDialog.tag.tags")}</Label>
                                    <MultiSelect
                                      options={tagOptions}
                                      selected={tagAction.tags}
                                      onChange={(next) => setFormState(prev => ({
                                        ...prev,
                                        exprTagActions: prev.exprTagActions.map((item, i) => i === index ? { ...item, tags: next } : item),
                                      }))}
                                      placeholder={t("preferences.workflowDialog.tag.selectTags")}
                                      creatable
                                      onCreateOption={(value) => setFormState(prev => ({
                                        ...prev,
                                        exprTagActions: prev.exprTagActions.map((item, i) => i === index ? { ...item, tags: [...item.tags, value] } : item),
                                      }))}
                                    />
                                  </div>
                                )}
                                <div className="space-y-1">
                                  <Label className="text-xs">{t("preferences.workflowDialog.tag.actionMode")}</Label>
                                  <Select
                                    value={tagAction.mode}
                                    onValueChange={(value: TagActionForm["mode"]) => setFormState(prev => ({
                                      ...prev,
                                      exprTagActions: prev.exprTagActions.map((item, i) => i === index ? { ...item, mode: value } : item),
                                    }))}
                                  >
                                    <SelectTrigger className="w-[120px]">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      <SelectItem value="full">{t("preferences.workflowDialog.tag.fullSync")}</SelectItem>
                                      <SelectItem value="add">{t("preferences.workflowDialog.tag.addOnly")}</SelectItem>
                                      <SelectItem value="remove">{t("preferences.workflowDialog.tag.removeOnly")}</SelectItem>
                                    </SelectContent>
                                  </Select>
                                </div>
                                <div className="space-y-1">
                                  <Label className="text-xs">{t("preferences.workflowDialog.tag.strategy")}</Label>
                                  <Select
                                    value={tagAction.deleteFromClient ? "replace" : "managed"}
                                    onValueChange={(value: "managed" | "replace") => {
                                      const replace = value === "replace"
                                      setFormState(prev => ({
                                        ...prev,
                                        exprTagActions: prev.exprTagActions.map((item, i) => {
                                          if (i !== index) return item
                                          return {
                                            ...item,
                                            deleteFromClient: replace,
                                            useTrackerAsTag: replace ? false : item.useTrackerAsTag,
                                            useDisplayName: replace ? false : item.useDisplayName,
                                          }
                                        }),
                                      }))
                                    }}
                                  >
                                    <SelectTrigger className="w-[170px]">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      <SelectItem value="managed">{t("preferences.workflowDialog.tag.strategyManaged")}</SelectItem>
                                      <SelectItem value="replace">{t("preferences.workflowDialog.tag.strategyReplace")}</SelectItem>
                                    </SelectContent>
                                  </Select>
                                </div>
                              </div>
                              {tagAction.deleteFromClient ? (
                                <div className="text-xs text-muted-foreground">
                                  {t("preferences.workflowDialog.tag.replaceDescription")}
                                </div>
                              ) : (
                                <div className="text-xs text-muted-foreground">
                                  {t("preferences.workflowDialog.tag.managedDescription")}
                                </div>
                              )}
                              <div className="flex flex-col sm:flex-row sm:items-center gap-3 sm:gap-4">
                                <div className="flex items-center gap-2">
                                  <Switch
                                    id={`use-tracker-tag-${index}`}
                                    checked={tagAction.useTrackerAsTag}
                                    disabled={tagAction.deleteFromClient}
                                    onCheckedChange={(checked) => setFormState(prev => ({
                                      ...prev,
                                      exprTagActions: prev.exprTagActions.map((item, i) => {
                                        if (i !== index) return item
                                        return {
                                          ...item,
                                          useTrackerAsTag: checked,
                                          useDisplayName: checked ? item.useDisplayName : false,
                                          tags: checked ? [] : item.tags,
                                        }
                                      }),
                                    }))}
                                  />
                                  <Label
                                    htmlFor={`use-tracker-tag-${index}`}
                                    className={`text-sm cursor-pointer whitespace-nowrap ${tagAction.deleteFromClient ? "text-muted-foreground" : ""}`}
                                  >
                                    {t("preferences.workflowDialog.tag.useTrackerName")}
                                  </Label>
                                </div>
                                {tagAction.useTrackerAsTag && (
                                  <div className="flex items-center gap-2">
                                    <Switch
                                      id={`use-display-name-${index}`}
                                      checked={tagAction.useDisplayName}
                                      onCheckedChange={(checked) => setFormState(prev => ({
                                        ...prev,
                                        exprTagActions: prev.exprTagActions.map((item, i) => i === index ? { ...item, useDisplayName: checked } : item),
                                      }))}
                                    />
                                    <Label htmlFor={`use-display-name-${index}`} className="text-sm cursor-pointer whitespace-nowrap">
                                      {t("preferences.workflowDialog.tag.useDisplayName")}
                                    </Label>
                                    <TooltipProvider delayDuration={150}>
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            type="button"
                                            className="inline-flex items-center text-muted-foreground hover:text-foreground"
                                            aria-label={t("preferences.workflowDialog.tag.aboutDisplayNames")}
                                          >
                                            <Info className="h-3.5 w-3.5" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent className="max-w-[280px]">
                                          <p>{t("preferences.workflowDialog.tag.displayNameDescription")}</p>
                                        </TooltipContent>
                                      </Tooltip>
                                    </TooltipProvider>
                                  </div>
                                )}
                              </div>
                            </div>
                          ))}
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="w-fit"
                            onClick={() => setFormState(prev => ({
                              ...prev,
                              exprTagActions: [...prev.exprTagActions, createDefaultTagAction()],
                            }))}
                          >
                            <Plus className="h-3.5 w-3.5 mr-1" />
                            {t("preferences.workflowDialog.tag.addAction")}
                          </Button>
                        </div>
                        <p className="text-xs text-muted-foreground">
                          {t("preferences.workflowDialog.tag.multipleActionsHelp")}
                        </p>
                      </div>
                    )}

                    {/* Category */}
                    {formState.categoryEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.category")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, categoryEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="flex items-center gap-3">
                          <div className="space-y-1">
                            <Label className="text-xs">{t("preferences.workflowDialog.category.moveToCategory")}</Label>
                            <Select
                              value={formState.exprCategory === "" ? CATEGORY_UNCATEGORIZED_VALUE : formState.exprCategory}
                              onValueChange={(value) => setFormState(prev => ({
                                ...prev,
                                exprCategory: value === CATEGORY_UNCATEGORIZED_VALUE ? "" : value,
                              }))}
                            >
                              <SelectTrigger className="w-fit min-w-[160px]">
                                <Folder className="h-3.5 w-3.5 mr-2 text-muted-foreground" />
                                <SelectValue placeholder={t("preferences.workflowDialog.category.selectCategory")} />
                              </SelectTrigger>
                              <SelectContent>
                                {categoryActionOptions.map(opt => (
                                  <SelectItem key={`${opt.value}-${opt.label}`} value={opt.value}>
                                    {opt.value === CATEGORY_UNCATEGORIZED_VALUE ? (
                                      <span className="italic text-muted-foreground">{opt.label}</span>
                                    ) : (
                                      opt.label
                                    )}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </div>
                          {formState.exprCategory && (
                            <div className="flex items-center gap-2 mt-5">
                              <Switch
                                id="include-crossseeds"
                                checked={formState.exprIncludeCrossSeeds}
                                onCheckedChange={(checked) => setFormState(prev => ({ ...prev, exprIncludeCrossSeeds: checked }))}
                              />
                              <Label htmlFor="include-crossseeds" className="text-sm cursor-pointer whitespace-nowrap">
                                {t("preferences.workflowDialog.category.includeAffectedCrossSeeds")}
                              </Label>
                            </div>
                          )}
                        </div>
                      </div>
                    )}

                    {/* External Program */}
                    {formState.externalProgramEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.externalProgram")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, externalProgramEnabled: false, exprExternalProgramId: null }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.externalProgram.program")}</Label>
                          {externalProgramsLoading ? (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50 flex items-center gap-2">
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              {t("preferences.workflowDialog.externalProgram.loading")}
                            </div>
                          ) : externalProgramsError ? (
                            <div className="text-sm text-destructive p-2 border border-destructive/50 rounded-md bg-destructive/10">
                              {t("preferences.workflowDialog.externalProgram.loadFailed")}
                            </div>
                          ) : externalPrograms && externalPrograms.length > 0 ? (
                            <Select
                              value={formState.exprExternalProgramId?.toString() ?? ""}
                              onValueChange={(value) => setFormState(prev => ({
                                ...prev,
                                exprExternalProgramId: value ? parseInt(value, 10) : null,
                              }))}
                            >
                              <SelectTrigger>
                                <SelectValue placeholder={t("preferences.workflowDialog.externalProgram.selectProgram")} />
                              </SelectTrigger>
                              <SelectContent>
                                {externalPrograms.map(program => (
                                  <SelectItem
                                    key={program.id}
                                    value={program.id.toString()}
                                  >
                                    {program.name}
                                    {!program.enabled && (
                                      <span className="ml-2 text-xs text-muted-foreground">{t("preferences.workflowDialog.externalProgram.disabled")}</span>
                                    )}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          ) : (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50">
                              {t("preferences.workflowDialog.externalProgram.noneConfigured")}{" "}
                              <a
                                href={withBasePath("/settings?tab=external-programs")}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-primary hover:underline"
                              >
                                {t("preferences.workflowDialog.externalProgram.configureInSettings")}
                              </a>
                            </div>
                          )}
                        </div>
                      </div>
                    )}

                    {/* Export to Instance */}
                    {formState.exportToInstanceEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.export.title")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({
                              ...prev,
                              exportToInstanceEnabled: false,
                              exprExportTargetInstanceId: null,
                              exprExportSavePath: "",
                              exprExportCategory: "",
                              exprExportTags: "",
                              exprExportPaused: false,
                              exprExportSkipChecking: true,
                              exprExportContentLayout: "",
                            }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.export.targetInstance")}</Label>
                          {instancesLoading ? (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50 flex items-center gap-2">
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              {t("preferences.workflowDialog.export.loadingInstances")}
                            </div>
                          ) : instancesError ? (
                            <div className="text-sm text-destructive p-2 border border-destructive/50 rounded-md bg-destructive/5">
                              {t("preferences.workflowDialog.export.loadInstancesFailed")}
                            </div>
                          ) : nonSelfInstances && nonSelfInstances.length > 0 ? (
                            <Select
                              value={formState.exprExportTargetInstanceId?.toString() ?? ""}
                              onValueChange={(value) => setFormState(prev => ({
                                ...prev,
                                exprExportTargetInstanceId: value ? parseInt(value, 10) : null,
                                exprExportCategory: "",
                              }))}
                            >
                              <SelectTrigger>
                                <SelectValue placeholder={t("preferences.workflowDialog.export.selectTargetPlaceholder")} />
                              </SelectTrigger>
                              <SelectContent>
                                {nonSelfInstances.map(instance => (
                                  <SelectItem key={instance.id} value={instance.id.toString()}>
                                    {instance.name}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          ) : (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50">
                              {t("preferences.workflowDialog.export.noOtherInstances")}
                            </div>
                          )}
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.export.savePathLabel")}</Label>
                          <Input
                            value={formState.exprExportSavePath}
                            onChange={(e) => setFormState(prev => ({ ...prev, exprExportSavePath: e.target.value }))}
                            placeholder={t("preferences.workflowDialog.export.savePathPlaceholder")}
                            className="text-sm"
                          />
                          <p className="text-xs text-muted-foreground">
                            {t("preferences.workflowDialog.export.savePathHelp")} <code>{"{{ .Name }}"}</code>, <code>{"{{ .Category }}"}</code>, <code>{"{{ .Hash }}"}</code>, <code>{"{{ .Tracker }}"}</code>
                          </p>
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.export.categoryLabel")}</Label>
                          {!formState.exprExportTargetInstanceId ? (
                            <Input
                              value={formState.exprExportCategory}
                              onChange={(e) => setFormState(prev => ({ ...prev, exprExportCategory: e.target.value }))}
                              placeholder={t("preferences.workflowDialog.export.selectTargetFirst")}
                              className="text-sm"
                              disabled
                            />
                          ) : targetMetadataLoading ? (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50 flex items-center gap-2">
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              {t("preferences.workflowDialog.export.loadingCategories")}
                            </div>
                          ) : targetCategories.length > 0 ? (
                            <Select
                              value={formState.exprExportCategory || "__none__"}
                              onValueChange={(value) => setFormState(prev => ({
                                ...prev,
                                exprExportCategory: value === "__none__" ? "" : value,
                              }))}
                            >
                              <SelectTrigger>
                                <SelectValue placeholder={t("preferences.workflowDialog.export.noCategory")} />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="__none__">{t("preferences.workflowDialog.export.noCategory")}</SelectItem>
                                {targetCategories.map(cat => (
                                  <SelectItem key={cat} value={cat}>{cat}</SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          ) : (
                            <Input
                              value={formState.exprExportCategory}
                              onChange={(e) => setFormState(prev => ({ ...prev, exprExportCategory: e.target.value }))}
                              placeholder={t("preferences.workflowDialog.export.noCategoriesFound")}
                              className="text-sm"
                            />
                          )}
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.export.tagsLabel")}</Label>
                          <Input
                            value={formState.exprExportTags}
                            onChange={(e) => setFormState(prev => ({ ...prev, exprExportTags: e.target.value }))}
                            placeholder={t("preferences.workflowDialog.export.tagsPlaceholder")}
                            className="text-sm"
                          />
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.export.contentLayoutLabel")}</Label>
                          <Select
                            value={formState.exprExportContentLayout || "default"}
                            onValueChange={(value) => setFormState(prev => ({
                              ...prev,
                              exprExportContentLayout: (value === "default" ? "" : value) as FormState["exprExportContentLayout"],
                            }))}
                          >
                            <SelectTrigger>
                              <SelectValue placeholder={t("preferences.workflowDialog.export.contentLayoutDefaultPlaceholder")} />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="default">{t("preferences.workflowDialog.export.contentLayoutDefault")}</SelectItem>
                              {CONTENT_LAYOUT_OPTIONS.map(opt => (
                                <SelectItem key={opt.value} value={opt.value}>{t(opt.labelKey)}</SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                        <div className="flex items-center gap-4">
                          <div className="flex items-center gap-2">
                            <Switch
                              id="export-skip-checking"
                              checked={formState.exprExportSkipChecking}
                              onCheckedChange={(checked) => setFormState(prev => ({ ...prev, exprExportSkipChecking: checked }))}
                            />
                            <Label htmlFor="export-skip-checking" className="text-sm cursor-pointer whitespace-nowrap">
                              {t("preferences.workflowDialog.export.skipChecking")}
                            </Label>
                          </div>
                          <div className="flex items-center gap-2">
                            <Switch
                              id="export-paused"
                              checked={formState.exprExportPaused}
                              onCheckedChange={(checked) => setFormState(prev => ({ ...prev, exprExportPaused: checked }))}
                            />
                            <Label htmlFor="export-paused" className="text-sm cursor-pointer whitespace-nowrap">
                              {t("preferences.workflowDialog.export.addPaused")}
                            </Label>
                          </div>
                        </div>
                      </div>
                    )}

                    {/* Delete - standalone only */}
                    {formState.deleteEnabled && (
                      <div className="rounded-lg border border-destructive/50 p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium text-destructive">{t("preferences.workflowDialog.actions.delete")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, deleteEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.delete.mode")}</Label>
                          {(() => {
                            const usesFreeSpace = conditionUsesField(formState.actionCondition, "FREE_SPACE")
                            const keepFilesDisabled = usesFreeSpace
                            return (
                              <Select
                                value={formState.exprDeleteMode}
                                onValueChange={(value: FormState["exprDeleteMode"]) => setFormState(prev => ({ ...prev, exprDeleteMode: value }))}
                              >
                                <SelectTrigger className="w-fit text-destructive">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <TooltipProvider delayDuration={150}>
                                    <Tooltip>
                                      <TooltipTrigger asChild>
                                        <div>
                                          <SelectItem
                                            value="delete"
                                            className="text-destructive focus:text-destructive"
                                            disabled={keepFilesDisabled}
                                          >
                                            {t("preferences.workflowDialog.delete.keepFiles")}
                                          </SelectItem>
                                        </div>
                                      </TooltipTrigger>
                                      {keepFilesDisabled && (
                                        <TooltipContent side="left" className="max-w-[280px]">
                                          <p>{t("preferences.workflowDialog.delete.keepFilesDisabledReason")}</p>
                                        </TooltipContent>
                                      )}
                                    </Tooltip>
                                  </TooltipProvider>
                                  <SelectItem value="deleteWithFiles" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.delete.withFiles")}</SelectItem>
                                  <SelectItem value="deleteWithFilesPreserveCrossSeeds" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.delete.withFilesPreserveCrossSeeds")}</SelectItem>
                                  <SelectItem value="deleteWithFilesIncludeCrossSeeds" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.delete.withFilesIncludeCrossSeeds")}</SelectItem>
                                </SelectContent>
                              </Select>
                            )
                          })()}
                        </div>
                        {/* Include Hardlinks checkbox - only for deleteWithFilesIncludeCrossSeeds mode */}
                        {formState.exprDeleteMode === "deleteWithFilesIncludeCrossSeeds" && (
                          <div className="flex items-center gap-2">
                            <TooltipProvider delayDuration={150}>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                                    <input
                                      type="checkbox"
                                      checked={formState.exprIncludeHardlinks}
                                      onChange={(e) => setFormState(prev => ({ ...prev, exprIncludeHardlinks: e.target.checked }))}
                                      disabled={!hasLocalFilesystemAccess}
                                      className="h-3.5 w-3.5 rounded border-border disabled:opacity-50"
                                    />
                                    <span className={!hasLocalFilesystemAccess ? "opacity-50" : ""}>
                                      {t("preferences.workflowDialog.delete.includeHardlinkedCopies")}
                                    </span>
                                  </label>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="max-w-[320px]">
                                  {hasLocalFilesystemAccess ? (
                                    <p>{t("preferences.workflowDialog.delete.includeHardlinkedCopiesDescription")}</p>
                                  ) : (
                                    <p>{t("preferences.workflowDialog.delete.localAccessRequired")}</p>
                                  )}
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                        )}
                      </div>
                    )}

                    {formState.moveEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.actions.move")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={() => setFormState(prev => ({ ...prev, moveEnabled: false }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <div className="space-y-1">
                          <Label className="text-xs">{t("preferences.workflowDialog.move.newSavePath")}</Label>
                          <Input
                            type="text"
                            value={formState.exprMovePath}
                            onChange={(e) => setFormState(prev => ({ ...prev, exprMovePath: e.target.value }))}
                            placeholder={t("preferences.workflowDialog.move.placeholder")}
                          />
                        </div>
                        <div className="flex items-start gap-2">
                          <Switch
                            id="block-if-cross-seed"
                            className="mt-0.5 shrink-0"
                            checked={formState.exprMoveBlockIfCrossSeed}
                            onCheckedChange={(checked) => setFormState(prev => ({
                              ...prev,
                              exprMoveBlockIfCrossSeed: checked,
                            }))}
                          />
                          <div className="flex items-center gap-2">
                            <Label htmlFor="block-if-cross-seed" className="text-sm cursor-pointer">
                              {t("preferences.workflowDialog.move.skipIfCrossSeedsDontMatch")}
                            </Label>
                            <TooltipProvider delayDuration={150}>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <button
                                    type="button"
                                    className="shrink-0 inline-flex items-center text-muted-foreground hover:text-foreground"
                                    aria-label={t("preferences.workflowDialog.move.aboutSkipping")}
                                  >
                                    <Info className="h-3.5 w-3.5" />
                                  </button>
                                </TooltipTrigger>
                                <TooltipContent className="max-w-[320px]">
                                  <p>{t("preferences.workflowDialog.move.skipDescription")}</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                </div>

                {/* Free Space Source - shown whenever FREE_SPACE is used in conditions */}
                {conditionUsesFreeSpace && (
                  <div className="rounded-lg border p-3 space-y-2">
                    <div className="flex items-center gap-1.5">
                      <Label className="text-sm font-medium">{t("preferences.workflowDialog.freeSpace.title")}</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label={t("preferences.workflowDialog.freeSpace.aria")}
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="left" className="max-w-[320px]">
                            <p>{t("preferences.workflowDialog.freeSpace.description")}</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                    <Select
                      value={formState.exprFreeSpaceSourceType}
                      onValueChange={(value) => {
                        const nextType = value as FormState["exprFreeSpaceSourceType"]
                        setFormState(prev => ({
                          ...prev,
                          exprFreeSpaceSourceType: nextType,
                        }))
                        if (nextType !== "path") {
                          setFreeSpaceSourcePathError(null)
                          // Clear autocomplete state to prevent stale suggestions
                          handleFreeSpacePathInputChange("")
                        }
                      }}
                    >
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue placeholder={t("preferences.workflowDialog.freeSpace.selectSource")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="qbittorrent">{t("preferences.workflowDialog.freeSpace.defaultSource")}</SelectItem>
                        <SelectItem value="path" disabled={!hasLocalFilesystemAccess || !supportsFreeSpacePathSource}>
                          {!supportsFreeSpacePathSource? t("preferences.workflowDialog.freeSpace.pathSourceWindowsUnsupported"): !hasLocalFilesystemAccess? t("preferences.workflowDialog.freeSpace.pathSourceLocalAccessRequired"): t("preferences.workflowDialog.freeSpace.pathSource")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    {formState.exprFreeSpaceSourceType === "path" && supportsFreeSpacePathSource && (
                      <div className="flex flex-col gap-1">
                        <div className="relative">
                          <Folder className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground z-10" />
                          <Input
                            ref={supportsPathAutocomplete ? freeSpacePathInputRef : undefined}
                            value={formState.exprFreeSpaceSourcePath}
                            autoComplete="off"
                            spellCheck={false}
                            onKeyDown={supportsPathAutocomplete ? handleFreeSpacePathKeyDown : undefined}
                            onChange={(e) => {
                              const nextPath = e.target.value
                              setFormState(prev => ({
                                ...prev,
                                exprFreeSpaceSourcePath: nextPath,
                              }))
                              if (supportsPathAutocomplete) {
                                handleFreeSpacePathInputChange(nextPath)
                              }
                              if (freeSpaceSourcePathError && nextPath.trim() !== "") {
                                setFreeSpaceSourcePathError(null)
                              }
                            }}
                            placeholder={t("preferences.workflowDialog.freeSpace.pathPlaceholder")}
                            className={cn("h-8 text-xs pl-7", freeSpaceSourcePathError && "border-destructive/50")}
                          />
                        </div>
                        {dropdownRect && dropdownContainerRef.current && createPortal(
                          <div
                            className="absolute rounded-md border bg-popover text-popover-foreground shadow-md pointer-events-auto"
                            style={{
                              top: dropdownRect.top,
                              left: dropdownRect.left,
                              width: dropdownRect.width,
                            }}
                          >
                            <div className="max-h-40 overflow-y-auto py-1">
                              {freeSpaceSuggestions.map((entry, idx) => (
                                <button
                                  key={entry}
                                  type="button"
                                  title={entry}
                                  className={cn(
                                    "w-full px-3 py-1.5 text-xs text-left",
                                    freeSpaceHighlightedIndex === idx ? "bg-accent text-accent-foreground" : "hover:bg-accent hover:text-accent-foreground"
                                  )}
                                  onMouseDown={(e) => e.preventDefault()}
                                  onClick={() => handleFreeSpacePathSelectSuggestion(entry)}
                                >
                                  <span className="block truncate">{entry}</span>
                                </button>
                              ))}
                            </div>
                          </div>,
                          dropdownContainerRef.current
                        )}
                        {freeSpaceSourcePathError && (
                          <p className="text-xs text-destructive">{freeSpaceSourcePathError}</p>
                        )}
                        <p className="text-xs text-muted-foreground">
                          {t("preferences.workflowDialog.freeSpace.pathHelp")}
                        </p>
                      </div>
                    )}
                  </div>
                )}

                {formState.categoryEnabled && (formState.exprIncludeCrossSeeds || formState.exprBlockIfCrossSeedInCategories.length > 0) && (
                  <div className="space-y-1.5">
                    <div className="flex items-center gap-1.5">
                      <Label className="text-xs">{t("preferences.workflowDialog.category.skipIfCrossSeedExists")}</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label={t("preferences.workflowDialog.category.aboutSkipping")}
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent className="max-w-[320px]">
                            <p>{t("preferences.workflowDialog.category.skipHelp")}</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                    <MultiSelect
                      options={categoryOptions}
                      selected={formState.exprBlockIfCrossSeedInCategories}
                      onChange={(next) => setFormState(prev => ({ ...prev, exprBlockIfCrossSeedInCategories: next }))}
                      placeholder={t("preferences.workflowDialog.category.selectCategories")}
                      creatable
                      onCreateOption={(value) => setFormState(prev => ({
                        ...prev,
                        exprBlockIfCrossSeedInCategories: [...prev.exprBlockIfCrossSeedInCategories, value],
                      }))}
                    />
                    <p className="text-xs text-muted-foreground">
                      {t("preferences.workflowDialog.category.skipDescription")}
                    </p>
                  </div>
                )}
              </div>
            </div>

            {showLatestDryRunPanel && (
              <div className="rounded-lg border bg-muted/20 p-3 space-y-2 mt-3">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-sm font-medium">{t("preferences.workflowDialog.dryRun.title")}</p>
                    <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.dryRun.description")}</p>
                  </div>
                  {!dryRunNowMutation.isPending && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-xs"
                      onClick={() => {
                        setLatestDryRunEvents([])
                        setLatestDryRunError(null)
                        setLatestDryRunStartedAt(null)
                        setActivityRunDialog(null)
                      }}
                    >
                      {t("preferences.workflowDialog.dryRun.clear")}
                    </Button>
                  )}
                </div>

                {dryRunNowMutation.isPending ? (
                  <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    {t("preferences.workflowDialog.dryRun.running")}
                  </div>
                ) : latestDryRunError ? (
                  <p className="text-xs text-destructive">{latestDryRunError}</p>
                ) : latestDryRunEvents.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.dryRun.noRows")}</p>
                ) : (
                  <>
                    <p className="text-xs text-muted-foreground">
                      {t("preferences.workflowDialog.dryRun.summaryLine", {
                        summaries: latestDryRunEvents.length,
                        operations: latestDryRunOperationCount,
                      })}
                    </p>
                    <div className="max-h-48 overflow-y-auto space-y-1 pr-1">
                      {latestDryRunEvents.map((event) => (
                        <div key={event.id} className="flex items-center justify-between gap-2 rounded-md border bg-background px-2 py-1.5">
                          <div className="min-w-0">
                            <p className="text-xs font-medium truncate">{t(DRY_RUN_ACTION_LABEL_KEYS[event.action] ?? "", { defaultValue: event.action })}</p>
                            <p className="text-xs text-muted-foreground truncate">{formatDryRunEventSummary(event, t)}</p>
                          </div>
                          <div className="shrink-0 flex items-center gap-2">
                            <span className="text-[11px] text-muted-foreground">{getDryRunImpactCount(event)}</span>
                            {event.action !== "dry_run_no_match" && getDryRunImpactCount(event) > 0 && (
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 px-2 text-xs"
                                onClick={() => setActivityRunDialog(event)}
                              >
                                {t("preferences.workflowDialog.dryRun.viewItems")}
                              </Button>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
            )}

            <div className="flex flex-wrap items-center justify-between gap-3 pt-3 border-t mt-3">
              <div className="flex items-center gap-4 flex-wrap">
                <div className="flex items-center gap-2">
                  <Switch
                    id="rule-enabled"
                    checked={formState.enabled}
                    onCheckedChange={handleEnabledToggle}
                  />
                  <Label htmlFor="rule-enabled" className="text-sm font-normal cursor-pointer">{t("preferences.workflowDialog.footer.enabled")}</Label>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    id="rule-dry-run"
                    checked={formState.dryRun}
                    onCheckedChange={(checked) => setFormState(prev => ({ ...prev, dryRun: checked }))}
                  />
                  <Label htmlFor="rule-dry-run" className="text-sm font-normal cursor-pointer">{t("preferences.workflowDialog.footer.dryRun")}</Label>
                </div>
                {hasNotificationTargets && (
                  <div className="flex items-center gap-2">
                    <Switch
                      id="rule-notify"
                      checked={formState.notify}
                      onCheckedChange={(checked) => setFormState(prev => ({ ...prev, notify: checked }))}
                    />
                    <Label htmlFor="rule-notify" className="text-sm font-normal cursor-pointer">{t("preferences.workflowDialog.footer.notify")}</Label>
                  </div>
                )}
                <div className="flex items-center gap-2">
                  <Label htmlFor="rule-interval" className="text-sm font-normal text-muted-foreground whitespace-nowrap">{t("preferences.workflowDialog.footer.runEvery")}</Label>
                  <Select
                    value={formState.intervalSeconds === null ? "default" : String(formState.intervalSeconds)}
                    onValueChange={(value) => {
                      const intervalSeconds = value === "default" ? null : Number(value)
                      setFormState(prev => ({ ...prev, intervalSeconds }))
                    }}
                  >
                    <SelectTrigger id="rule-interval" className="w-fit h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {intervalOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value} disabled={option.disabled}>
                          {option.label}
                        </SelectItem>
                      ))}
                      {/* Show custom option if current value is non-preset */}
                      {formState.intervalSeconds !== null &&
                        ![60, 300, 900, 1800, 3600, 7200, 14400, 21600, 43200, 86400].includes(formState.intervalSeconds) && (
                        <SelectItem value={String(formState.intervalSeconds)}>
                          {t("preferences.workflowDialog.interval.custom", { seconds: formState.intervalSeconds })}
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                  {deleteUsesFreeSpace && (
                    <TooltipProvider delayDuration={150}>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            className="inline-flex items-center text-muted-foreground hover:text-foreground"
                            aria-label={t("preferences.workflowDialog.interval.cooldownAria")}
                          >
                            <Info className="h-3.5 w-3.5" />
                          </button>
                        </TooltipTrigger>
                        <TooltipContent className="max-w-[280px]">
                          <p>{t("preferences.workflowDialog.interval.cooldownDescription")}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  )}
                  {deleteUsesFreeSpace && formState.intervalSeconds === 60 && (
                    <span className="text-xs text-yellow-500">{t("preferences.workflowDialog.interval.cooldownWarning")}</span>
                  )}
                </div>
              </div>
              <div className="flex gap-2 w-full sm:w-auto">
                <Button type="button" variant="outline" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" onClick={() => onOpenChange(false)}>
                  {t("preferences.workflowDialog.cancel")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="flex-1 sm:flex-initial h-10 sm:h-8"
                  onClick={handleRunDryRunNow}
                  disabled={dryRunNowMutation.isPending || createOrUpdate.isPending || previewMutation.isPending}
                >
                  {dryRunNowMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  {t("preferences.workflowDialog.runDryRunNow")}
                </Button>
                <Button type="submit" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" disabled={createOrUpdate.isPending || previewMutation.isPending}>
                  {(createOrUpdate.isPending || previewMutation.isPending) && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  {rule ? t("preferences.workflowDialog.save") : t("preferences.workflowDialog.create")}
                </Button>
              </div>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      <WorkflowPreviewDialog
        open={showConfirmDialog}
        onOpenChange={(open) => {
          if (!open) {
            // Restore enabled state if user cancels the preview
            if (enabledBeforePreview !== null) {
              setFormState(prev => ({ ...prev, enabled: enabledBeforePreview }))
              setEnabledBeforePreview(null)
            }
            setPreviewResult(null)
            setPreviewInput(null)
            setIsInitialLoading(false)
          }
          setShowConfirmDialog(open)
        }}
        title={
          isDeleteRule? (formState.enabled? t("preferences.workflowDialog.preview.confirmDeleteRule"): t("preferences.workflowDialog.preview.previewDeleteRule")): t("preferences.workflowDialog.preview.confirmCategoryChange", {
            category: previewInput?.exprCategory ?? formState.exprCategory,
          })
        }
        description={
          previewResult && previewResult.totalMatches > 0 ? (
            isDeleteRule ? (
              formState.enabled ? (
                <>
                  <p className="text-destructive font-medium">
                    {t("preferences.workflowDialog.preview.deleteEnabledSummary", { count: previewResult.totalMatches })}
                  </p>
                  <p className="text-muted-foreground text-sm">{t("preferences.workflowDialog.confirmSaveAndEnable")}</p>
                </>
              ) : (
                <>
                  <p className="text-muted-foreground">
                    {t("preferences.workflowDialog.preview.deleteDisabledSummary", { count: previewResult.totalMatches })}
                  </p>
                  <p className="text-muted-foreground text-sm">{t("preferences.workflowDialog.confirmSave")}</p>
                </>
              )
            ) : (
              <>
                <p>
                  {t("preferences.workflowDialog.preview.categoryPrefix")}{" "}
                  <strong>{(previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0)}</strong> {t("preferences.workflowDialog.preview.torrents", { count: (previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0) })}
                  {previewResult.crossSeedCount ? (
                    <> {t("preferences.workflowDialog.preview.and")} <strong>{previewResult.crossSeedCount}</strong> {t("preferences.workflowDialog.preview.crossSeeds", { count: previewResult.crossSeedCount })}</>
                  ) : null}
                  {" "}{t("preferences.workflowDialog.preview.toCategory")} <strong>"{previewInput?.exprCategory ?? formState.exprCategory}"</strong>.
                </p>
                <p className="text-muted-foreground text-sm">{t("preferences.workflowDialog.confirmSaveAndEnable")}</p>
              </>
            )
          ) : (
            <>
              <p>{t("preferences.workflowDialog.noTorrentsCurrentlyMatch")}</p>
              <p className="text-muted-foreground text-sm">{t("preferences.workflowDialog.confirmSave")}</p>
            </>
          )
        }
        preview={previewResult}
        condition={previewInput?.actionCondition ?? formState.actionCondition}
        onConfirm={handleConfirmSave}
        onLoadMore={handleLoadMore}
        isLoadingMore={loadMorePreview.isPending}
        confirmLabel={t("preferences.workflowDialog.saveRule")}
        isConfirming={createOrUpdate.isPending}
        destructive={isDeleteRule && formState.enabled}
        warning={isCategoryRule}
        previewView={previewView}
        onPreviewViewChange={handlePreviewViewChange}
        showPreviewViewToggle={isDeleteRule && deleteUsesFreeSpace}
        isLoadingPreview={isLoadingPreviewView}
        onExport={handleExport}
        isExporting={isExporting}
        isInitialLoading={isInitialLoading}
        showScore={(previewInput?.sortingType ?? formState.sortingType) === "score"}
      />

      {activityRunDialog && (
        <AutomationActivityRunDialog
          open={Boolean(activityRunDialog)}
          onOpenChange={(isOpen) => {
            if (!isOpen) {
              setActivityRunDialog(null)
            }
          }}
          instanceId={instanceId}
          activity={activityRunDialog}
        />
      )}

      <AlertDialog open={showDryRunPrompt} onOpenChange={setShowDryRunPrompt}>
        <AlertDialogContent className="sm:max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>{t("preferences.workflowDialog.enableDryRunPrompt.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("preferences.workflowDialog.enableDryRunPrompt.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.workflowDialog.cancel")}</AlertDialogCancel>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                markDryRunPrompted()
                setShowDryRunPrompt(false)
                applyEnabledChange(true)
              }}
            >
              {t("preferences.workflowDialog.enableDryRunPrompt.enableWithout")}
            </Button>
            <AlertDialogAction
              onClick={() => {
                markDryRunPrompted()
                setShowDryRunPrompt(false)
                applyEnabledChange(true, { forceDryRun: true })
              }}
            >
              {t("preferences.workflowDialog.enableDryRunPrompt.enableWith")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={showAddCustomGroup} onOpenChange={setShowAddCustomGroup}>
        <AlertDialogContent className="sm:max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>{t("preferences.workflowDialog.customGroup.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("preferences.workflowDialog.customGroup.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1">
              <Label htmlFor="group-id" className="text-sm">{t("preferences.workflowDialog.customGroup.groupId")}</Label>
              <Input
                id="group-id"
                value={newGroupId}
                onChange={(e) => setNewGroupId(e.target.value)}
                placeholder={t("preferences.workflowDialog.customGroup.groupIdPlaceholder")}
                className="h-8 text-xs"
              />
              <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.customGroup.groupIdHelp")}</p>
            </div>

            <div className="space-y-1">
              <Label className="text-sm">{t("preferences.workflowDialog.customGroup.keys")}</Label>
              <div className="grid grid-cols-2 gap-1">
                {AVAILABLE_GROUP_KEYS.map(key => (
                  <label key={key} className="flex items-center gap-2 text-xs cursor-pointer">
                    <input
                      type="checkbox"
                      checked={newGroupKeys.includes(key)}
                      onChange={(e) => {
                        if (e.target.checked) {
                          setNewGroupKeys([...newGroupKeys, key])
                        } else {
                          setNewGroupKeys(newGroupKeys.filter(k => k !== key))
                        }
                      }}
                      className="h-3 w-3 rounded border-border"
                    />
                    <span className="font-mono">{key}</span>
                  </label>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                {t("preferences.workflowDialog.customGroup.keysHelp")}
              </p>
            </div>

            <div className="space-y-1">
              <Label className="text-sm">{t("preferences.workflowDialog.customGroup.ambiguousPolicy")}</Label>
              <Select
                value={newGroupAmbiguousPolicy || AMBIGUOUS_POLICY_NONE_VALUE}
                onValueChange={(value) => setNewGroupAmbiguousPolicy(
                  value === AMBIGUOUS_POLICY_NONE_VALUE ? "" : value as "verify_overlap" | "skip"
                )}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={AMBIGUOUS_POLICY_NONE_VALUE}>{t("preferences.workflowDialog.customGroup.none")}</SelectItem>
                  <SelectItem value="verify_overlap">{t("preferences.workflowDialog.customGroup.verifyOverlap")}</SelectItem>
                  <SelectItem value="skip">{t("preferences.workflowDialog.customGroup.skip")}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {t("preferences.workflowDialog.customGroup.ambiguousPolicyHelp")}
              </p>
            </div>

            {newGroupAmbiguousPolicy === "verify_overlap" && (
              <div className="space-y-1">
                <Label htmlFor="min-overlap" className="text-sm">{t("preferences.workflowDialog.customGroup.minFileOverlap")}</Label>
                <Input
                  id="min-overlap"
                  type="number"
                  value={newGroupMinOverlap}
                  onChange={(e) => setNewGroupMinOverlap(e.target.value)}
                  min="0"
                  max="100"
                  className="h-8 text-xs"
                />
                <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.customGroup.defaultOverlap")}</p>
              </div>
            )}
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.workflowDialog.cancel")}</AlertDialogCancel>
            <Button
              type="button"
              onClick={() => {
                // Validate
                if (!newGroupId.trim()) {
                  toast.error(t("preferences.workflowDialog.toast.groupIdEmpty"))
                  return
                }
                if (!/^[a-zA-Z0-9_]+$/.test(newGroupId)) {
                  toast.error(t("preferences.workflowDialog.toast.groupIdInvalid"))
                  return
                }
                if (newGroupKeys.length === 0) {
                  toast.error(t("preferences.workflowDialog.toast.selectGroupKey"))
                  return
                }
                // Check for duplicates
                if ((formState.exprGrouping?.groups || []).some(g => g.id === newGroupId)) {
                  toast.error(t("preferences.workflowDialog.toast.groupExists"))
                  return
                }

                // Add the group
                const groupDef: GroupDefinition = {
                  id: newGroupId,
                  keys: newGroupKeys as string[],
                  ambiguousPolicy: newGroupAmbiguousPolicy || undefined,
                  minFileOverlapPercent: newGroupAmbiguousPolicy === "verify_overlap" ? parseInt(newGroupMinOverlap, 10) : undefined,
                }

                setFormState(prev => ({
                  ...prev,
                  exprGrouping: {
                    ...prev.exprGrouping,
                    groups: [...(prev.exprGrouping?.groups || []), groupDef],
                  },
                }))

                // Reset form
                setShowAddCustomGroup(false)
                setNewGroupId("")
                setNewGroupKeys([])
                setNewGroupAmbiguousPolicy("")
                setNewGroupMinOverlap("90")
                toast.success(t("preferences.workflowDialog.toast.customGroupAdded"))
              }}
            >
              {t("preferences.workflowDialog.customGroup.addGroup")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
