/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryBuilder, type GroupOption } from "@/components/query-builder"
import {
  CATEGORY_UNCATEGORIZED_VALUE,
  CAPABILITY_REASONS,
  FIELD_REQUIREMENTS,
  STATE_VALUE_REQUIREMENTS,
  type Capabilities,
  type DisabledField,
  type DisabledStateValue,
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
import { cn, formatBytes, normalizeTrackerDomains, parseTrackerDomains } from "@/lib/utils"
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
  RuleCondition
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Folder, Info, Loader2, Plus, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { AutomationActivityRunDialog } from "./AutomationActivityRunDialog"
import { WorkflowPreviewDialog } from "./WorkflowPreviewDialog"

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

type ActionType = "speedLimits" | "shareLimits" | "pause" | "resume" | "recheck" | "reannounce" | "delete" | "tag" | "category" | "move" | "externalProgram"
type TranslateFn = (key: string, options?: Record<string, unknown>) => string

// Actions that can be combined (Delete must be standalone)
const COMBINABLE_ACTIONS: ActionType[] = ["speedLimits", "shareLimits", "pause", "resume", "recheck", "reannounce", "tag", "category", "move", "externalProgram"]

const ACTION_LABEL_KEYS: Record<ActionType, string> = {
  speedLimits: "workflowDialog.actions.labels.speedLimits",
  shareLimits: "workflowDialog.actions.labels.shareLimits",
  pause: "workflowDialog.actions.labels.pause",
  resume: "workflowDialog.actions.labels.resume",
  recheck: "workflowDialog.actions.labels.recheck",
  reannounce: "workflowDialog.actions.labels.reannounce",
  delete: "workflowDialog.actions.labels.delete",
  tag: "workflowDialog.actions.labels.tag",
  category: "workflowDialog.actions.labels.category",
  move: "workflowDialog.actions.labels.move",
  externalProgram: "workflowDialog.actions.labels.externalProgram",
}

const DRY_RUN_ACTION_LABEL_KEYS: Record<AutomationActivity["action"], string> = {
  deleted_ratio: "workflowDialog.dryRun.actions.deletedRatio",
  deleted_seeding: "workflowDialog.dryRun.actions.deletedSeeding",
  deleted_unregistered: "workflowDialog.dryRun.actions.deletedUnregistered",
  deleted_condition: "workflowDialog.dryRun.actions.deletedCondition",
  delete_failed: "workflowDialog.dryRun.actions.deleteFailed",
  limit_failed: "workflowDialog.dryRun.actions.limitFailed",
  tags_changed: "workflowDialog.dryRun.actions.tagsChanged",
  category_changed: "workflowDialog.dryRun.actions.categoryChanged",
  speed_limits_changed: "workflowDialog.dryRun.actions.speedLimitsChanged",
  share_limits_changed: "workflowDialog.dryRun.actions.shareLimitsChanged",
  paused: "workflowDialog.dryRun.actions.paused",
  resumed: "workflowDialog.dryRun.actions.resumed",
  rechecked: "workflowDialog.dryRun.actions.rechecked",
  reannounced: "workflowDialog.dryRun.actions.reannounced",
  moved: "workflowDialog.dryRun.actions.moved",
  external_program: "workflowDialog.dryRun.actions.externalProgram",
  dry_run_no_match: "workflowDialog.dryRun.actions.noMatches",
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

function formatDryRunEventSummary(event: AutomationActivity, tr: TranslateFn): string {
  const details = event.details
  switch (event.action) {
    case "tags_changed": {
      const added = sumDetailsRecord(details?.added)
      const removed = sumDetailsRecord(details?.removed)
      if (added > 0 && removed > 0) return tr("workflowDialog.dryRun.summaries.tagsAddedRemoved", { added, removed })
      if (added > 0) return tr("workflowDialog.dryRun.summaries.tagsAdded", { count: added })
      if (removed > 0) return tr("workflowDialog.dryRun.summaries.tagsRemoved", { count: removed })
      return tr("workflowDialog.dryRun.summaries.noTagChanges")
    }
    case "category_changed": {
      const moved = sumDetailsRecord(details?.categories)
      return tr("workflowDialog.dryRun.summaries.categoryChanged", { count: moved })
    }
    case "speed_limits_changed":
    case "share_limits_changed": {
      const limited = sumDetailsRecord(details?.limits)
      return tr("workflowDialog.dryRun.summaries.limitsUpdated", { count: limited })
    }
    case "moved": {
      const moved = sumDetailsRecord(details?.paths)
      return tr("workflowDialog.dryRun.summaries.moved", { count: moved })
    }
    case "dry_run_no_match":
      return tr("workflowDialog.dryRun.summaries.noMatches")
    case "paused":
    case "resumed":
    case "rechecked":
    case "reannounced":
    case "external_program":
    case "deleted_ratio":
    case "deleted_seeding":
    case "deleted_unregistered":
    case "deleted_condition": {
      const count = typeof details?.count === "number" ? details.count : 0
      return tr("workflowDialog.dryRun.summaries.impacted", { count })
    }
    default:
      return tr("workflowDialog.dryRun.summaries.completed")
  }
}

function getDisabledFields(capabilities: Capabilities): DisabledField[] {
  return Object.entries(FIELD_REQUIREMENTS)
    .filter(([_, capability]) => !capabilities[capability as keyof Capabilities])
    .map(([field, capability]) => ({ field, reason: CAPABILITY_REASONS[capability] }))
}

function getDisabledStateValues(capabilities: Capabilities): DisabledStateValue[] {
  return Object.entries(STATE_VALUE_REQUIREMENTS)
    .filter(([_, capability]) => !capabilities[capability as keyof Capabilities])
    .map(([value, capability]) => ({ value, reason: CAPABILITY_REASONS[capability] }))
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
    labelKey: "workflowDialog.builtinGroups.crossSeedContentPath.label",
    descriptionKey: "workflowDialog.builtinGroups.crossSeedContentPath.description",
  },
  {
    id: "cross_seed_content_save_path",
    labelKey: "workflowDialog.builtinGroups.crossSeedContentSavePath.label",
    descriptionKey: "workflowDialog.builtinGroups.crossSeedContentSavePath.description",
  },
  {
    id: "release_item",
    labelKey: "workflowDialog.builtinGroups.releaseItem.label",
    descriptionKey: "workflowDialog.builtinGroups.releaseItem.description",
  },
  {
    id: "tracker_release_item",
    labelKey: "workflowDialog.builtinGroups.trackerReleaseItem.label",
    descriptionKey: "workflowDialog.builtinGroups.trackerReleaseItem.description",
  },
  {
    id: "hardlink_signature",
    labelKey: "workflowDialog.builtinGroups.hardlinkSignature.label",
    descriptionKey: "workflowDialog.builtinGroups.hardlinkSignature.description",
  },
] as const

const AMBIGUOUS_POLICY_NONE_VALUE = "__none__"

// Speed limit mode: no_change = omit, unlimited = 0, custom = user value (>0)
type SpeedLimitMode = "no_change" | "unlimited" | "custom"

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

type FormState = {
  name: string
  trackerPattern: string
  trackerDomains: string[]
  applyToAllTrackers: boolean
  enabled: boolean
  dryRun: boolean
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
  // Move action settings
  exprMovePath: string
  exprMoveBlockIfCrossSeed: boolean
  exprMoveGroupId: string
  exprMoveAtomic: "all" | ""
  // External program action settings
  exprExternalProgramId: number | null
}

const emptyFormState: FormState = {
  name: "",
  trackerPattern: "",
  trackerDomains: [],
  applyToAllTrackers: false,
  enabled: false,
  dryRun: false,
  intervalSeconds: null,
  actionCondition: null,
  exprGrouping: undefined,
  speedLimitsEnabled: false,
  shareLimitsEnabled: false,
  pauseEnabled: false,
  resumeEnabled: false,
  recheckEnabled: false,
  reannounceEnabled: false,
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
  exprMovePath: "",
  exprMoveBlockIfCrossSeed: false,
  exprMoveGroupId: "",
  exprMoveAtomic: "",
  exprExternalProgramId: null,
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
  if (state.deleteEnabled) actions.push("delete")
  if (state.tagEnabled) actions.push("tag")
  if (state.categoryEnabled) actions.push("category")
  if (state.moveEnabled) actions.push("move")
  if (state.externalProgramEnabled) actions.push("externalProgram")
  return actions
}

// Helper to set an action enabled/disabled
function setActionEnabled(action: ActionType, enabled: boolean): Partial<FormState> {
  const key = `${action}Enabled` as keyof FormState
  return { [key]: enabled }
}

function validateTagActions(actions: TagActionForm[], tr: TranslateFn): string | null {
  if (actions.length === 0) {
    return tr("workflowDialog.validation.addTagAction")
  }
  for (const action of actions) {
    if (action.deleteFromClient && action.useTrackerAsTag) {
      return tr("workflowDialog.validation.replaceRequiresExplicitTags")
    }
    if (!action.useTrackerAsTag && action.tags.length === 0) {
      return tr("workflowDialog.validation.tagOrTrackerRequired")
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
  const { t } = useTranslation("common")
  const tr = useCallback((key: string, options?: Record<string, unknown>) => String(t(key as never, options as never)), [t])
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
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const { data: metadata } = useInstanceMetadata(instanceId)
  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: open })
  const { instances } = useInstances()
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
      { label: tr("workflowDialog.category.uncategorized"), value: CATEGORY_UNCATEGORIZED_VALUE },
      ...filtered,
    ]
  }, [categoryOptions, tr])

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
          label: tr("workflowDialog.trackers.customLabel", { name: customization.displayName }),
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

    // Add trackers from the workflow being edited (so they persist even if no torrents use them)
    if (rule && rule.trackerPattern !== "*") {
      const savedDomains = parseTrackerDomains(rule)
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
  }, [trackersQuery.data, trackerCustomizationMaps, trackerIcons, rule, tr])

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
      label: tr(group.labelKey),
      description: tr(group.descriptionKey),
    }))
    const seen = new Set(options.map((option) => option.id.toLowerCase()))
    for (const group of (formState.exprGrouping?.groups || [])) {
      const id = group.id?.trim()
      if (!id) continue
      if (seen.has(id.toLowerCase())) continue
      seen.add(id.toLowerCase())
      options.push({
        id,
        label: tr("workflowDialog.groups.customLabel", { id }),
        description: group.keys.length > 0
          ? tr("workflowDialog.groups.customKeys", { keys: group.keys.join(", ") })
          : tr("workflowDialog.groups.customGroup"),
      })
    }
    return options
  }, [formState.exprGrouping?.groups, tr])

  // Initialize form state when dialog opens or rule changes
  useEffect(() => {
    let cancelled = false

    if (open) {
      if (rule) {
        const isAllTrackers = rule.trackerPattern === "*"
        const rawDomains = isAllTrackers ? [] : parseTrackerDomains(rule)
        const mappedDomains = mapDomainsToOptionValues(rawDomains)

        // Parse existing conditions into form state
        const conditions = rule.conditions
        let actionCondition: RuleCondition | null = null
        let speedLimitsEnabled = false
        let shareLimitsEnabled = false
        let pauseEnabled = false
        let resumeEnabled = false
        let recheckEnabled = false
        let reannounceEnabled = false
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
        let exprDeleteMode: FormState["exprDeleteMode"] = "deleteWithFilesPreserveCrossSeeds"
        let exprIncludeHardlinks = false
        let exprFreeSpaceSourceType: FormState["exprFreeSpaceSourceType"] = "qbittorrent"
        let exprFreeSpaceSourcePath = ""
        let exprTagActions: TagActionForm[] = [createDefaultTagAction()]
        let exprCategory = ""
        let exprIncludeCrossSeeds = false
        let exprBlockIfCrossSeedInCategories: string[] = []
        let exprMovePath = ""
        let exprMoveBlockIfCrossSeed = false
        let exprExternalProgramId: number | null = null
        let exprGrouping: GroupingConfig | undefined
        let exprDeleteGroupId = ""
        let exprDeleteAtomic: FormState["exprDeleteAtomic"] = ""
        let exprCategoryGroupId = ""
        let exprMoveGroupId = ""
        let exprMoveAtomic: FormState["exprMoveAtomic"] = ""

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
            ?? conditions.delete?.condition
            ?? conditions.tags?.[0]?.condition
            ?? conditions.tag?.condition
            ?? conditions.category?.condition
            ?? conditions.move?.condition
            ?? conditions.externalProgram?.condition
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
          if (conditions.delete?.enabled) {
            deleteEnabled = true
            exprDeleteMode = conditions.delete.mode ?? "deleteWithFilesPreserveCrossSeeds"
            exprIncludeHardlinks = conditions.delete.includeHardlinks ?? false
            exprDeleteGroupId = conditions.delete.groupId ?? ""
            exprDeleteAtomic = conditions.delete.atomic ?? ""
          }
          const resolvedTagActions = (conditions.tags && conditions.tags.length > 0
            ? conditions.tags
            : conditions.tag ? [conditions.tag] : [])
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
        }

        const newState: FormState = {
          name: rule.name,
          trackerPattern: rule.trackerPattern,
          trackerDomains: mappedDomains,
          applyToAllTrackers: isAllTrackers,
          enabled: rule.enabled,
          dryRun: rule.dryRun ?? false,
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
          exprMoveGroupId,
          exprMoveAtomic,
          exprExternalProgramId,
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
          toast.info(tr("workflowDialog.toasts.switchedDeleteModeForFreeSpace"))
        }
      }
    }
  }, [formState.actionCondition, formState.deleteEnabled, formState.exprDeleteMode, tr])

  // Auto-switch interval from 1 minute when FREE_SPACE delete condition is added
  // The backend has a ~5 minute cooldown, so 1 minute intervals would be ineffective
  // Only switch on user edits, not during initial hydration (respect saved config)
  useEffect(() => {
    if (isHydrating.current) return
    if (formState.deleteEnabled && formState.intervalSeconds === 60) {
      if (conditionUsesField(formState.actionCondition, "FREE_SPACE")) {
        setFormState(prev => ({ ...prev, intervalSeconds: 300 })) // Switch to 5 minutes
        toast.info(tr("workflowDialog.toasts.switchedIntervalForFreeSpace"))
      }
    }
  }, [formState.actionCondition, formState.deleteEnabled, formState.intervalSeconds, tr])

  // Auto-switch free space source from "path" to "qbittorrent" on Windows (not supported)
  // This must run during hydration to handle legacy workflows opened on Windows.
  // Only toast after hydration to avoid noise when opening dialogs.
  useEffect(() => {
    if (!supportsFreeSpacePathSource && formState.exprFreeSpaceSourceType === "path") {
      setFormState(prev => ({ ...prev, exprFreeSpaceSourceType: "qbittorrent" }))
      if (!isHydrating.current) {
        toast.warning(tr("workflowDialog.toasts.pathSourceNotSupportedWindows"))
      }
    }
  }, [supportsFreeSpacePathSource, formState.exprFreeSpaceSourceType, tr])

  const validateFreeSpaceSource = useCallback((state: FormState): boolean => {
    const usesFreeSpace = conditionUsesField(state.actionCondition, "FREE_SPACE")
    if (!usesFreeSpace || state.exprFreeSpaceSourceType !== "path") {
      setFreeSpaceSourcePathError(null)
      return true
    }

    // Reject if path source is selected but not supported (safety net for edge cases)
    if (!supportsFreeSpacePathSource) {
      setFreeSpaceSourcePathError(tr("workflowDialog.freeSpace.errors.windowsUnsupported"))
      toast.error(tr("workflowDialog.freeSpace.errors.switchToDefault"))
      return false
    }
    if (!hasLocalFilesystemAccess) {
      setFreeSpaceSourcePathError(tr("workflowDialog.freeSpace.errors.localAccessRequired"))
      toast.error(tr("workflowDialog.freeSpace.errors.enableLocalAccess"))
      return false
    }

    const trimmedPath = state.exprFreeSpaceSourcePath.trim()
    if (trimmedPath === "") {
      setFreeSpaceSourcePathError(tr("workflowDialog.freeSpace.errors.pathRequired"))
      toast.error(tr("workflowDialog.freeSpace.errors.enterPath"))
      return false
    }

    setFreeSpaceSourcePathError(null)
    return true
  }, [hasLocalFilesystemAccess, supportsFreeSpacePathSource, tr])

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

    const trackerDomains = input.applyToAllTrackers ? [] : normalizeTrackerDomains(input.trackerDomains)

    return {
      name: input.name,
      trackerDomains,
      trackerPattern: input.applyToAllTrackers ? "*" : trackerDomains.join(","),
      enabled: input.enabled,
      dryRun: input.dryRun,
      sortOrder: input.sortOrder,
      intervalSeconds: input.intervalSeconds,
      conditions,
      freeSpaceSource,
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

  // Count enabled actions
  const enabledActionsCount = [
    formState.speedLimitsEnabled,
    formState.shareLimitsEnabled,
    formState.pauseEnabled,
    formState.resumeEnabled,
    formState.recheckEnabled,
    formState.reannounceEnabled,
    formState.deleteEnabled,
    formState.tagEnabled,
    formState.categoryEnabled,
    formState.moveEnabled,
    formState.externalProgramEnabled,
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
      toast.error(error instanceof Error ? error.message : tr("workflowDialog.toasts.failedPreviewRule"))
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
      toast.error(error instanceof Error ? error.message : tr("workflowDialog.toasts.failedLoadMorePreviews"))
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
      toast.success(tr("workflowDialog.toasts.dryRunCompleted"))
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
          setLatestDryRunError(events.length === 0 ? tr("workflowDialog.dryRun.messages.activityPending") : null)
          return
        } catch (error) {
          setLatestDryRunEvents([])
          setLatestDryRunError(error instanceof Error ? error.message : tr("workflowDialog.dryRun.messages.failedSummary"))
          return
        }
      }

      setLatestDryRunEvents([])
      setLatestDryRunError(tr("workflowDialog.dryRun.messages.noActivityIds"))
    },
    onError: (error) => {
      setLatestDryRunEvents([])
      setLatestDryRunError(null)
      setLatestDryRunStartedAt(null)
      toast.error(error instanceof Error ? error.message : tr("workflowDialog.toasts.failedRunDryRun"))
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

    return {
      ...buildPayload(formState),
      previewLimit: livePreviewPageSize,
      previewOffset: 0,
      previewView: "needed" as PreviewView,
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
        setLivePreviewError(error instanceof Error ? error.message : tr("workflowDialog.livePreview.failedLoad"))
      } finally {
        if (livePreviewRequestRef.current === requestID) {
          setIsLivePreviewLoading(false)
        }
      }
    }, 400)

    return () => clearTimeout(timeout)
  }, [instanceId, livePreviewPayload, livePreviewPayloadKey])

  const handleRunDryRunNow = () => {
    const dryRunInput: FormState = { ...formState }

    if (!validateFreeSpaceSource(dryRunInput)) return
    if (!dryRunInput.name.trim()) {
      toast.error(tr("workflowDialog.validation.nameRequired"))
      return
    }
    if (!dryRunInput.applyToAllTrackers && normalizeTrackerDomains(dryRunInput.trackerDomains).length === 0) {
      toast.error(tr("workflowDialog.validation.selectTracker"))
      return
    }
    if (enabledActionsCount === 0) {
      toast.error(tr("workflowDialog.validation.enableAction"))
      return
    }
    if (dryRunInput.deleteEnabled && !dryRunInput.actionCondition) {
      toast.error(tr("workflowDialog.validation.deleteConditionRequired"))
      return
    }
    if (dryRunInput.moveEnabled && !dryRunInput.exprMovePath.trim()) {
      toast.error(tr("workflowDialog.validation.movePathRequired"))
      return
    }
    if (dryRunInput.externalProgramEnabled && !dryRunInput.exprExternalProgramId) {
      toast.error(tr("workflowDialog.validation.selectExternalProgram"))
      return
    }
    if (dryRunInput.tagEnabled) {
      const validationError = validateTagActions(dryRunInput.exprTagActions, tr)
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

  const applyEnabledChange = useCallback((checked: boolean, options?: { forceDryRun?: boolean }) => {
    if (checked && isDeleteRule && !formState.actionCondition) {
      toast.error(tr("workflowDialog.validation.deleteConditionRequired"))
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
  }, [formState, isCategoryRule, isDeleteRule, previewMutation, tr, validateFreeSpaceSource])

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
      toast.error(error instanceof Error ? error.message : tr("workflowDialog.toasts.failedSwitchPreviewView"))
    } finally {
      setIsLoadingPreviewView(false)
    }
  }

  // CSV columns for automation preview export
  const csvColumns: CsvColumn<AutomationPreviewTorrent>[] = [
    { header: tr("workflowDialog.csvHeaders.name"), accessor: t => t.name },
    { header: tr("workflowDialog.csvHeaders.hash"), accessor: t => t.hash },
    { header: tr("workflowDialog.csvHeaders.tracker"), accessor: t => t.tracker },
    { header: tr("workflowDialog.csvHeaders.size"), accessor: t => formatBytes(t.size) },
    { header: tr("workflowDialog.csvHeaders.ratio"), accessor: t => t.ratio === -1 ? tr("workflowDialog.values.infinity") : t.ratio.toFixed(2) },
    { header: tr("workflowDialog.csvHeaders.seedingTimeSeconds"), accessor: t => t.seedingTime },
    { header: tr("workflowDialog.csvHeaders.category"), accessor: t => t.category },
    { header: tr("workflowDialog.csvHeaders.tags"), accessor: t => t.tags },
    { header: tr("workflowDialog.csvHeaders.state"), accessor: t => t.state },
    { header: tr("workflowDialog.csvHeaders.addedOn"), accessor: t => t.addedOn },
    { header: tr("workflowDialog.csvHeaders.path"), accessor: t => t.contentPath ?? "" },
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
      const ruleName = (formState.name || "automation").replace(/[^a-zA-Z0-9-_]/g, "_")
      downloadBlob(csv, `${ruleName}_preview.csv`)
      toast.success(tr("workflowDialog.toasts.exportedCsv", { count: allItems.length }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : tr("workflowDialog.toasts.failedExportPreview"))
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
      toast.success(tr(rule ? "workflowDialog.toasts.workflowUpdated" : "workflowDialog.toasts.workflowCreated"))
      setShowConfirmDialog(false)
      setPreviewResult(null)
      setPreviewInput(null)
      onOpenChange(false)
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
      onSuccess?.()
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : tr("workflowDialog.toasts.failedSaveAutomation"))
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
      toast.error(tr("workflowDialog.validation.nameRequired"))
      return
    }
    const selectedTrackers = submitState.trackerDomains.filter(Boolean)
    if (!submitState.applyToAllTrackers && selectedTrackers.length === 0) {
      toast.error(tr("workflowDialog.validation.selectTracker"))
      return
    }

    // At least one action must be enabled
    if (enabledActionsCount === 0) {
      toast.error(tr("workflowDialog.validation.enableAction"))
      return
    }

    // Action-specific validation for enabled actions
    if (submitState.speedLimitsEnabled) {
      // At least one field must be set to something other than "no_change"
      const uploadIsSet = submitState.exprUploadMode !== "no_change" &&
        (submitState.exprUploadMode !== "custom" || (submitState.exprUploadValue !== undefined && submitState.exprUploadValue > 0))
      const downloadIsSet = submitState.exprDownloadMode !== "no_change" &&
        (submitState.exprDownloadMode !== "custom" || (submitState.exprDownloadValue !== undefined && submitState.exprDownloadValue > 0))
      if (!uploadIsSet && !downloadIsSet) {
        toast.error(tr("workflowDialog.validation.setSpeedLimit"))
        return
      }
      // Validate custom values are > 0
      if (submitState.exprUploadMode === "custom" && (submitState.exprUploadValue === undefined || submitState.exprUploadValue <= 0)) {
        toast.error(tr("workflowDialog.validation.uploadSpeedPositive"))
        return
      }
      if (submitState.exprDownloadMode === "custom" && (submitState.exprDownloadValue === undefined || submitState.exprDownloadValue <= 0)) {
        toast.error(tr("workflowDialog.validation.downloadSpeedPositive"))
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
        toast.error(tr("workflowDialog.validation.setShareLimit"))
        return
      }
    }
    if (submitState.tagEnabled) {
      const validationError = validateTagActions(submitState.exprTagActions, tr)
      if (validationError) {
        toast.error(validationError)
        return
      }
    }
    if (submitState.categoryEnabled) {
      if (!submitState.exprCategory) {
        toast.error(tr("workflowDialog.validation.selectCategory"))
        return
      }
    }
    if (submitState.externalProgramEnabled) {
      if (!submitState.exprExternalProgramId) {
        toast.error(tr("workflowDialog.validation.selectExternalProgram"))
        return
      }
    }
    if (submitState.deleteEnabled && !submitState.actionCondition) {
      toast.error(tr("workflowDialog.validation.deleteConditionRequired"))
      return
    }
    const trimmedSubmitMovePath = submitState.exprMovePath?.trim()
    if (submitState.moveEnabled && !trimmedSubmitMovePath) {
      toast.error(tr("workflowDialog.validation.movePathRequired"))
      return
    }

    // Validate regex patterns before saving (only if enabling the workflow)
    const payload = buildPayload(submitState)
    if (submitState.enabled) {
      try {
        const validation = await api.validateAutomationRegex(instanceId, payload)
        if (!validation.valid && validation.errors.length > 0) {
          setRegexErrors(validation.errors)
          toast.error(tr("workflowDialog.validation.invalidRegex"))
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
            <DialogTitle>{rule ? tr("workflowDialog.title.edit") : tr("workflowDialog.title.add")}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
            <div className="flex-1 overflow-y-auto space-y-3 sm:pr-1">
              {/* Header row: Name + All Trackers toggle */}
              <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
                <div className="space-y-1.5">
                  <Label htmlFor="rule-name">{tr("workflowDialog.fields.name")}</Label>
                  <Input
                    id="rule-name"
                    value={formState.name}
                    onChange={(e) => setFormState(prev => ({ ...prev, name: e.target.value }))}
                    required
                    placeholder={tr("workflowDialog.placeholders.workflowName")}
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
                  <Label htmlFor="all-trackers" className="text-sm cursor-pointer whitespace-nowrap">{tr("workflowDialog.fields.allTrackers")}</Label>
                </div>
              </div>

              {/* Trackers */}
              {!formState.applyToAllTrackers && (
                <div className="space-y-1.5">
                  <Label>{tr("workflowDialog.fields.trackers")}</Label>
                  <MultiSelect
                    options={trackerOptions}
                    selected={formState.trackerDomains}
                    onChange={(next) => setFormState(prev => ({ ...prev, trackerDomains: next }))}
                    placeholder={tr("workflowDialog.placeholders.selectTrackers")}
                    creatable
                    onCreateOption={(value) => setFormState(prev => ({ ...prev, trackerDomains: [...prev.trackerDomains, value] }))}
                    disabled={trackersQuery.isLoading}
                    hideCheckIcon
                  />
                </div>
              )}

              {/* Condition and Action */}
              <div className="space-y-3">
                {/* Query Builder */}
                <div className="space-y-1.5">
                  <Label>{tr("workflowDialog.fields.conditionsOptional")}</Label>
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
                      <p className="font-medium text-destructive">{tr("workflowDialog.validation.deleteConditionRequired")}</p>
                    </div>
                  )}
                  {regexErrors.length > 0 && (
                    <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm">
                      <p className="font-medium text-destructive mb-1">{tr("workflowDialog.validation.invalidRegexTitle")}</p>
                      {regexErrors.map((err, i) => (
                        <p key={i} className="text-destructive/80 text-xs">
                          <span className="font-mono">{err.pattern}</span>: {err.message}
                        </p>
                      ))}
                      <p className="text-muted-foreground text-xs mt-2">
                        {tr("workflowDialog.validation.invalidRegexHint")}
                      </p>
                    </div>
                  )}

                  {(isDeleteRule || isCategoryRule) && (
                    <div className="rounded-md border bg-muted/20 p-3 space-y-2">
                      <div className="flex items-center justify-between gap-2">
                        <p className="text-sm font-medium">{tr("workflowDialog.livePreview.title")}</p>
                        {isLivePreviewLoading && (
                          <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            {tr("workflowDialog.livePreview.updating")}
                          </span>
                        )}
                      </div>
                      {livePreviewError ? (
                        <p className="text-xs text-destructive">{livePreviewError}</p>
                      ) : !livePreviewResult ? (
                        <p className="text-xs text-muted-foreground">
                          {tr("workflowDialog.livePreview.addConditions")}
                        </p>
                      ) : (
                        <>
                          <p className="text-xs text-muted-foreground">
                            {isCategoryRule
                              ? tr("workflowDialog.livePreview.categoryImpact", {
                                total: livePreviewResult.totalMatches,
                                direct: (livePreviewResult.totalMatches) - (livePreviewResult.crossSeedCount ?? 0),
                                crossSeeds: livePreviewResult.crossSeedCount ?? 0,
                              })
                              : tr("workflowDialog.livePreview.impactCount", {
                                count: livePreviewResult.totalMatches,
                              })}
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
                            <p className="text-xs text-muted-foreground">{tr("workflowDialog.livePreview.noCurrentMatches")}</p>
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
                      <Label className="text-sm font-medium">{tr("workflowDialog.groups.title")}</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label={tr("workflowDialog.groups.aboutAria")}
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="right" className="max-w-[340px]">
                            <p>
                              {tr("workflowDialog.groups.tooltip")}
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>

                    <p className="text-xs text-muted-foreground">
                      {tr("workflowDialog.groups.description")}
                    </p>

                    <div className="rounded-sm border border-border/50 bg-background p-2 text-xs space-y-1.5">
                      <p className="font-medium text-foreground">{tr("workflowDialog.groups.builtInTitle")}</p>
                      <div className="space-y-1">
                        {BUILTIN_GROUPS.map((group) => (
                          <div key={group.id}>
                            <p className="font-medium">{tr(group.labelKey)}</p>
                            <p className="text-muted-foreground">{tr(group.descriptionKey)}</p>
                          </div>
                        ))}
                      </div>
                    </div>

                    {/* Custom groups editor */}
                    {(formState.exprGrouping?.groups || []).length > 0 && (
                      <div className="space-y-2 border-t pt-3">
                        <p className="text-xs font-medium text-muted-foreground">
                          {tr("workflowDialog.groups.customTitle")}
                        </p>
                        {(formState.exprGrouping?.groups || []).map((group, idx) => (
                          <div key={group.id} className="border rounded-sm p-2 space-y-1.5 text-xs bg-background">
                            <div className="flex items-center justify-between gap-1">
                              <div className="flex-1 min-w-0">
                                <p className="font-mono font-medium truncate">{group.id}</p>
                                <p className="text-muted-foreground">{tr("workflowDialog.groups.keys", { keys: group.keys.join(", ") })}</p>
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
                                      defaultGroupId: prev.exprGrouping?.defaultGroupId === group.id
                                        ? undefined
                                        : prev.exprGrouping?.defaultGroupId,
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
                      {tr("workflowDialog.groups.addCustom")}
                    </Button>
                  </div>
                )}

                {/* Actions section */}
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <Label>{tr("workflowDialog.fields.action")}</Label>
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
                            {tr("workflowDialog.actions.addAction")}
                          </SelectTrigger>
                          <SelectContent>
                            {availableActions.map(action => (
                              <SelectItem key={action} value={action}>{tr(ACTION_LABEL_KEYS[action])}</SelectItem>
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
                            deleteEnabled: true,
                            tagEnabled: false,
                            categoryEnabled: false,
                            moveEnabled: false,
                            externalProgramEnabled: false,
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
                        <SelectValue placeholder={tr("workflowDialog.placeholders.selectAction")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="speedLimits">{tr("workflowDialog.actions.labels.speedLimits")}</SelectItem>
                        <SelectItem value="shareLimits">{tr("workflowDialog.actions.labels.shareLimits")}</SelectItem>
                        <SelectItem value="pause">{tr("workflowDialog.actions.labels.pause")}</SelectItem>
                        <SelectItem value="resume">{tr("workflowDialog.actions.labels.resume")}</SelectItem>
                        <SelectItem value="recheck">{tr("workflowDialog.actions.labels.recheck")}</SelectItem>
                        <SelectItem value="reannounce">{tr("workflowDialog.actions.labels.reannounce")}</SelectItem>
                        <SelectItem value="tag">{tr("workflowDialog.actions.labels.tag")}</SelectItem>
                        <SelectItem value="category">{tr("workflowDialog.actions.labels.category")}</SelectItem>
                        <SelectItem value="move">{tr("workflowDialog.actions.labels.move")}</SelectItem>
                        <SelectItem value="externalProgram">{tr("workflowDialog.actions.labels.externalProgram")}</SelectItem>
                        <SelectItem value="delete" className="text-destructive focus:text-destructive">{tr("workflowDialog.actions.labels.deleteStandalone")}</SelectItem>
                      </SelectContent>
                    </Select>
                  )}

                  {/* Render enabled actions */}
                  <div className="space-y-3">
                    {/* Speed limits */}
                    {formState.speedLimitsEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.speedLimits")}</Label>
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
                            <Label className="text-xs">{tr("workflowDialog.speedLimits.uploadLimit")}</Label>
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
                                  <SelectItem value="no_change">{tr("workflowDialog.speedLimits.mode.noChange")}</SelectItem>
                                  <SelectItem value="unlimited">{tr("workflowDialog.speedLimits.mode.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{tr("workflowDialog.speedLimits.mode.custom")}</SelectItem>
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
                                    placeholder={tr("workflowDialog.placeholders.exampleTen")}
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
                            <Label className="text-xs">{tr("workflowDialog.speedLimits.downloadLimit")}</Label>
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
                                  <SelectItem value="no_change">{tr("workflowDialog.speedLimits.mode.noChange")}</SelectItem>
                                  <SelectItem value="unlimited">{tr("workflowDialog.speedLimits.mode.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{tr("workflowDialog.speedLimits.mode.custom")}</SelectItem>
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
                                    placeholder={tr("workflowDialog.placeholders.exampleTen")}
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
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.shareLimits")}</Label>
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
                            <Label className="text-xs">{tr("workflowDialog.shareLimits.ratioLimit")}</Label>
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
                                  <SelectItem value="no_change">{tr("workflowDialog.shareLimits.mode.noChange")}</SelectItem>
                                  <SelectItem value="global">{tr("workflowDialog.shareLimits.mode.useGlobal")}</SelectItem>
                                  <SelectItem value="unlimited">{tr("workflowDialog.shareLimits.mode.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{tr("workflowDialog.shareLimits.mode.custom")}</SelectItem>
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
                                  placeholder={tr("workflowDialog.placeholders.exampleRatio")}
                                />
                              )}
                            </div>
                          </div>
                          {/* Seed time */}
                          <div className="space-y-1.5">
                            <Label className="text-xs">{tr("workflowDialog.shareLimits.seedTimeMinutes")}</Label>
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
                                  <SelectItem value="no_change">{tr("workflowDialog.shareLimits.mode.noChange")}</SelectItem>
                                  <SelectItem value="global">{tr("workflowDialog.shareLimits.mode.useGlobal")}</SelectItem>
                                  <SelectItem value="unlimited">{tr("workflowDialog.shareLimits.mode.unlimited")}</SelectItem>
                                  <SelectItem value="custom">{tr("workflowDialog.shareLimits.mode.custom")}</SelectItem>
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
                                  placeholder={tr("workflowDialog.placeholders.exampleSeedMinutes")}
                                />
                              )}
                            </div>
                          </div>
                        </div>
                      </div>
                    )}

                    {/* Pause */}
                    {formState.pauseEnabled && (
                      <div className="rounded-lg border p-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.pause")}</Label>
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
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.resume")}</Label>
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
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.recheck")}</Label>
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
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.reannounce")}</Label>
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
                    {/* Tag */}
                    {formState.tagEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{tr("workflowDialog.tagActions.title")}</Label>
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
                                <Label className="text-xs font-medium">{tr("workflowDialog.tagActions.itemTitle", { index: index + 1 })}</Label>
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
                                    <Label className="text-xs text-muted-foreground">{tr("workflowDialog.tagActions.derivedFromTracker")}</Label>
                                    <div className="flex items-center gap-2 h-9 px-3 rounded-md border bg-muted/50 text-sm text-muted-foreground">
                                      {tr("workflowDialog.tagActions.derivedFromTrackerDescription")}
                                    </div>
                                  </div>
                                ) : (
                                  <div className="space-y-1">
                                    <Label className="text-xs">{tr("workflowDialog.tagActions.tags")}</Label>
                                    <MultiSelect
                                      options={tagOptions}
                                      selected={tagAction.tags}
                                      onChange={(next) => setFormState(prev => ({
                                        ...prev,
                                        exprTagActions: prev.exprTagActions.map((item, i) => i === index ? { ...item, tags: next } : item),
                                      }))}
                                      placeholder={tr("workflowDialog.placeholders.selectTags")}
                                      creatable
                                      onCreateOption={(value) => setFormState(prev => ({
                                        ...prev,
                                        exprTagActions: prev.exprTagActions.map((item, i) => i === index ? { ...item, tags: [...item.tags, value] } : item),
                                      }))}
                                    />
                                  </div>
                                )}
                                <div className="space-y-1">
                                  <Label className="text-xs">{tr("workflowDialog.tagActions.actionMode")}</Label>
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
                                      <SelectItem value="full">{tr("workflowDialog.tagActions.modes.full")}</SelectItem>
                                      <SelectItem value="add">{tr("workflowDialog.tagActions.modes.addOnly")}</SelectItem>
                                      <SelectItem value="remove">{tr("workflowDialog.tagActions.modes.removeOnly")}</SelectItem>
                                    </SelectContent>
                                  </Select>
                                </div>
                                <div className="space-y-1">
                                  <Label className="text-xs">{tr("workflowDialog.tagActions.strategy")}</Label>
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
                                      <SelectItem value="managed">{tr("workflowDialog.tagActions.strategies.managedDefault")}</SelectItem>
                                      <SelectItem value="replace">{tr("workflowDialog.tagActions.strategies.replaceInClient")}</SelectItem>
                                    </SelectContent>
                                  </Select>
                                </div>
                              </div>
                              {tagAction.deleteFromClient ? (
                                <div className="text-xs text-muted-foreground">
                                  {tr("workflowDialog.tagActions.replaceModeInfo")}
                                </div>
                              ) : (
                                <div className="text-xs text-muted-foreground">
                                  {tr("workflowDialog.tagActions.managedModeInfo")}
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
                                    {tr("workflowDialog.tagActions.useTrackerName")}
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
                                      {tr("workflowDialog.tagActions.useDisplayName")}
                                    </Label>
                                    <TooltipProvider delayDuration={150}>
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            type="button"
                                            className="inline-flex items-center text-muted-foreground hover:text-foreground"
                                            aria-label={tr("workflowDialog.tagActions.aboutDisplayNamesAria")}
                                          >
                                            <Info className="h-3.5 w-3.5" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent className="max-w-[280px]">
                                          <p>{tr("workflowDialog.tagActions.displayNameTooltip")}</p>
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
                            {tr("workflowDialog.tagActions.add")}
                          </Button>
                        </div>
                        <p className="text-xs text-muted-foreground">
                          {tr("workflowDialog.tagActions.multipleHint")}
                        </p>
                      </div>
                    )}

                    {/* Category */}
                    {formState.categoryEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.category")}</Label>
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
                            <Label className="text-xs">{tr("workflowDialog.category.moveToCategory")}</Label>
                            <Select
                              value={formState.exprCategory === "" ? CATEGORY_UNCATEGORIZED_VALUE : formState.exprCategory}
                              onValueChange={(value) => setFormState(prev => ({
                                ...prev,
                                exprCategory: value === CATEGORY_UNCATEGORIZED_VALUE ? "" : value,
                              }))}
                            >
                              <SelectTrigger className="w-fit min-w-[160px]">
                                <Folder className="h-3.5 w-3.5 mr-2 text-muted-foreground" />
                                <SelectValue placeholder={tr("workflowDialog.placeholders.selectCategory")} />
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
                                {tr("workflowDialog.category.includeCrossSeeds")}
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
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.externalProgram")}</Label>
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
                          <Label className="text-xs">{tr("workflowDialog.externalProgram.program")}</Label>
                          {externalProgramsLoading ? (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50 flex items-center gap-2">
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              {tr("workflowDialog.externalProgram.loading")}
                            </div>
                          ) : externalProgramsError ? (
                            <div className="text-sm text-destructive p-2 border border-destructive/50 rounded-md bg-destructive/10">
                              {tr("workflowDialog.externalProgram.failedLoad")}
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
                                <SelectValue placeholder={tr("workflowDialog.placeholders.selectProgram")} />
                              </SelectTrigger>
                              <SelectContent>
                                {externalPrograms.map(program => (
                                  <SelectItem
                                    key={program.id}
                                    value={program.id.toString()}
                                  >
                                    {program.name}
                                    {!program.enabled && (
                                      <span className="ml-2 text-xs text-muted-foreground">{tr("workflowDialog.externalProgram.disabledSuffix")}</span>
                                    )}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          ) : (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50">
                              {tr("workflowDialog.externalProgram.noneConfigured")}{" "}
                              <a
                                href={withBasePath("/settings?tab=external-programs")}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-primary hover:underline"
                              >
                                {tr("workflowDialog.externalProgram.configure")}
                              </a>
                            </div>
                          )}
                        </div>
                      </div>
                    )}

                    {/* Delete - standalone only */}
                    {formState.deleteEnabled && (
                      <div className="rounded-lg border border-destructive/50 p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium text-destructive">{tr("workflowDialog.actions.labels.delete")}</Label>
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
                          <Label className="text-xs">{tr("workflowDialog.delete.mode")}</Label>
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
                                            {tr("workflowDialog.delete.modes.keepFiles")}
                                          </SelectItem>
                                        </div>
                                      </TooltipTrigger>
                                      {keepFilesDisabled && (
                                        <TooltipContent side="left" className="max-w-[280px]">
                                          <p>{tr("workflowDialog.delete.keepFilesDisabledTooltip")}</p>
                                        </TooltipContent>
                                      )}
                                    </Tooltip>
                                  </TooltipProvider>
                                  <SelectItem value="deleteWithFiles" className="text-destructive focus:text-destructive">{tr("workflowDialog.delete.modes.withFiles")}</SelectItem>
                                  <SelectItem value="deleteWithFilesPreserveCrossSeeds" className="text-destructive focus:text-destructive">{tr("workflowDialog.delete.modes.withFilesPreserveCrossSeeds")}</SelectItem>
                                  <SelectItem value="deleteWithFilesIncludeCrossSeeds" className="text-destructive focus:text-destructive">{tr("workflowDialog.delete.modes.withFilesIncludeCrossSeeds")}</SelectItem>
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
                                      {tr("workflowDialog.delete.includeHardlinkedCopies")}
                                    </span>
                                  </label>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="max-w-[320px]">
                                  {hasLocalFilesystemAccess ? (
                                    <p>
                                      {tr("workflowDialog.delete.includeHardlinkedTooltip")}
                                    </p>
                                  ) : (
                                    <p>
                                      {tr("workflowDialog.delete.localAccessRequiredTooltip")}
                                    </p>
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
                          <Label className="text-sm font-medium">{tr("workflowDialog.actions.labels.move")}</Label>
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
                          <Label className="text-xs">{tr("workflowDialog.move.newSavePath")}</Label>
                          <Input
                            type="text"
                            value={formState.exprMovePath}
                            onChange={(e) => setFormState(prev => ({ ...prev, exprMovePath: e.target.value }))}
                            placeholder={tr("workflowDialog.placeholders.exampleMovePath")}
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
                              {tr("workflowDialog.move.skipIfCrossSeedsDontMatch")}
                            </Label>
                            <TooltipProvider delayDuration={150}>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <button
                                    type="button"
                                    className="shrink-0 inline-flex items-center text-muted-foreground hover:text-foreground"
                                    aria-label={tr("workflowDialog.move.skipTooltipAria")}
                                  >
                                    <Info className="h-3.5 w-3.5" />
                                  </button>
                                </TooltipTrigger>
                                <TooltipContent className="max-w-[320px]">
                                  <p>
                                    {tr("workflowDialog.move.skipTooltip")}
                                  </p>
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
                      <Label className="text-sm font-medium">{tr("workflowDialog.freeSpace.title")}</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label={tr("workflowDialog.freeSpace.aboutAria")}
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="left" className="max-w-[320px]">
                            <p>
                              {tr("workflowDialog.freeSpace.tooltip")}
                            </p>
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
                        <SelectValue placeholder={tr("workflowDialog.placeholders.selectSource")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="qbittorrent">{tr("workflowDialog.freeSpace.source.defaultQbittorrent")}</SelectItem>
                        <SelectItem value="path" disabled={!hasLocalFilesystemAccess || !supportsFreeSpacePathSource}>
                          {!supportsFreeSpacePathSource
                            ? tr("workflowDialog.freeSpace.source.pathOnServerUnsupported")
                            : !hasLocalFilesystemAccess
                              ? tr("workflowDialog.freeSpace.source.pathOnServerNeedsAccess")
                              : tr("workflowDialog.freeSpace.source.pathOnServer")}
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
                            placeholder={tr("workflowDialog.placeholders.exampleMountPath")}
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
                          {tr("workflowDialog.freeSpace.pathHelp")}
                        </p>
                      </div>
                    )}
                  </div>
                )}

                {formState.categoryEnabled && (formState.exprIncludeCrossSeeds || formState.exprBlockIfCrossSeedInCategories.length > 0) && (
                  <div className="space-y-1.5">
                    <div className="flex items-center gap-1.5">
                      <Label className="text-xs">{tr("workflowDialog.category.skipIfCrossSeedInCategories")}</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label={tr("workflowDialog.category.skipTooltipAria")}
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent className="max-w-[320px]">
                            <p>
                              {tr("workflowDialog.category.skipTooltip")}
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                    <MultiSelect
                      options={categoryOptions}
                      selected={formState.exprBlockIfCrossSeedInCategories}
                      onChange={(next) => setFormState(prev => ({ ...prev, exprBlockIfCrossSeedInCategories: next }))}
                      placeholder={tr("workflowDialog.placeholders.selectCategories")}
                      creatable
                      onCreateOption={(value) => setFormState(prev => ({
                        ...prev,
                        exprBlockIfCrossSeedInCategories: [...prev.exprBlockIfCrossSeedInCategories, value],
                      }))}
                    />
                    <p className="text-xs text-muted-foreground">
                      {tr("workflowDialog.category.skipDescription")}
                    </p>
                  </div>
                )}
              </div>
            </div>

            {showLatestDryRunPanel && (
              <div className="rounded-lg border bg-muted/20 p-3 space-y-2 mt-3">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-sm font-medium">{tr("workflowDialog.dryRun.panelTitle")}</p>
                    <p className="text-xs text-muted-foreground">{tr("workflowDialog.dryRun.panelDescription")}</p>
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
                      {tr("workflowDialog.actions.clear")}
                    </Button>
                  )}
                </div>

                {dryRunNowMutation.isPending ? (
                  <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    {tr("workflowDialog.dryRun.running")}
                  </div>
                ) : latestDryRunError ? (
                  <p className="text-xs text-destructive">{latestDryRunError}</p>
                ) : latestDryRunEvents.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{tr("workflowDialog.dryRun.noRowsYet")}</p>
                ) : (
                  <>
                    <p className="text-xs text-muted-foreground">
                      {tr("workflowDialog.dryRun.summary", {
                        summaries: latestDryRunEvents.length,
                        operations: latestDryRunOperationCount,
                      })}
                    </p>
                    <div className="max-h-48 overflow-y-auto space-y-1 pr-1">
                      {latestDryRunEvents.map((event) => (
                        <div key={event.id} className="flex items-center justify-between gap-2 rounded-md border bg-background px-2 py-1.5">
                          <div className="min-w-0">
                            <p className="text-xs font-medium truncate">{tr(DRY_RUN_ACTION_LABEL_KEYS[event.action])}</p>
                            <p className="text-xs text-muted-foreground truncate">{formatDryRunEventSummary(event, tr)}</p>
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
                                {tr("workflowDialog.actions.viewItems")}
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
                  <Label htmlFor="rule-enabled" className="text-sm font-normal cursor-pointer">{tr("workflowDialog.fields.enabled")}</Label>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    id="rule-dry-run"
                    checked={formState.dryRun}
                    onCheckedChange={(checked) => setFormState(prev => ({ ...prev, dryRun: checked }))}
                  />
                  <Label htmlFor="rule-dry-run" className="text-sm font-normal cursor-pointer">{tr("workflowDialog.fields.dryRun")}</Label>
                </div>
                <div className="flex items-center gap-2">
                  <Label htmlFor="rule-interval" className="text-sm font-normal text-muted-foreground whitespace-nowrap">{tr("workflowDialog.fields.runEvery")}</Label>
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
                      <SelectItem value="default">{tr("workflowDialog.intervals.default15m")}</SelectItem>
                      <SelectItem value="60" disabled={deleteUsesFreeSpace}>{tr("workflowDialog.intervals.oneMinute")}</SelectItem>
                      <SelectItem value="300">{tr("workflowDialog.intervals.fiveMinutes")}</SelectItem>
                      <SelectItem value="900">{tr("workflowDialog.intervals.fifteenMinutes")}</SelectItem>
                      <SelectItem value="1800">{tr("workflowDialog.intervals.thirtyMinutes")}</SelectItem>
                      <SelectItem value="3600">{tr("workflowDialog.intervals.oneHour")}</SelectItem>
                      <SelectItem value="7200">{tr("workflowDialog.intervals.twoHours")}</SelectItem>
                      <SelectItem value="14400">{tr("workflowDialog.intervals.fourHours")}</SelectItem>
                      <SelectItem value="21600">{tr("workflowDialog.intervals.sixHours")}</SelectItem>
                      <SelectItem value="43200">{tr("workflowDialog.intervals.twelveHours")}</SelectItem>
                      <SelectItem value="86400">{tr("workflowDialog.intervals.twentyFourHours")}</SelectItem>
                      {/* Show custom option if current value is non-preset */}
                      {formState.intervalSeconds !== null &&
                        ![60, 300, 900, 1800, 3600, 7200, 14400, 21600, 43200, 86400].includes(formState.intervalSeconds) && (
                        <SelectItem value={String(formState.intervalSeconds)}>
                          {tr("workflowDialog.intervals.custom", { seconds: formState.intervalSeconds })}
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
                            aria-label={tr("workflowDialog.intervals.cooldownAria")}
                          >
                            <Info className="h-3.5 w-3.5" />
                          </button>
                        </TooltipTrigger>
                        <TooltipContent className="max-w-[280px]">
                          <p>{tr("workflowDialog.intervals.cooldownTooltip")}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  )}
                  {deleteUsesFreeSpace && formState.intervalSeconds === 60 && (
                    <span className="text-xs text-yellow-500">{tr("workflowDialog.intervals.cooldownWarning")}</span>
                  )}
                </div>
              </div>
              <div className="flex gap-2 w-full sm:w-auto">
                <Button type="button" variant="outline" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" onClick={() => onOpenChange(false)}>
                  {tr("workflowDialog.actions.cancel")}
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
                  {tr("workflowDialog.actions.dryRunNow")}
                </Button>
                <Button type="submit" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" disabled={createOrUpdate.isPending || previewMutation.isPending}>
                  {(createOrUpdate.isPending || previewMutation.isPending) && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  {rule ? tr("workflowDialog.actions.save") : tr("workflowDialog.actions.create")}
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
          isDeleteRule
            ? (formState.enabled ? tr("workflowDialog.previewDialog.confirmDeleteRule") : tr("workflowDialog.previewDialog.previewDeleteRule"))
            : tr("workflowDialog.previewDialog.confirmCategoryChange", { category: previewInput?.exprCategory ?? formState.exprCategory })
        }
        description={
          previewResult && previewResult.totalMatches > 0 ? (
            isDeleteRule ? (
              formState.enabled ? (
                <>
                  <p className="text-destructive font-medium">
                    {tr("workflowDialog.previewDialog.deleteEnabledImpact", { count: previewResult.totalMatches })}
                  </p>
                  <p className="text-muted-foreground text-sm">{tr("workflowDialog.previewDialog.confirmSaveEnable")}</p>
                </>
              ) : (
                <>
                  <p className="text-muted-foreground">
                    {tr("workflowDialog.previewDialog.deleteDisabledImpact", { count: previewResult.totalMatches })}
                  </p>
                  <p className="text-muted-foreground text-sm">{tr("workflowDialog.previewDialog.confirmSaveOnly")}</p>
                </>
              )
            ) : (
              <>
                <p>
                  {tr("workflowDialog.previewDialog.categoryImpact", {
                    direct: (previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0),
                    crossSeeds: previewResult.crossSeedCount ?? 0,
                    category: previewInput?.exprCategory ?? formState.exprCategory,
                  })}
                </p>
                <p className="text-muted-foreground text-sm">{tr("workflowDialog.previewDialog.confirmSaveEnable")}</p>
              </>
            )
          ) : (
            <>
              <p>{tr("workflowDialog.previewDialog.noMatches")}</p>
              <p className="text-muted-foreground text-sm">{tr("workflowDialog.previewDialog.confirmSaveOnly")}</p>
            </>
          )
        }
        preview={previewResult}
        condition={previewInput?.actionCondition ?? formState.actionCondition}
        onConfirm={handleConfirmSave}
        onLoadMore={handleLoadMore}
        isLoadingMore={loadMorePreview.isPending}
        confirmLabel={tr("workflowDialog.previewDialog.saveRule")}
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
            <AlertDialogTitle>{tr("workflowDialog.dryRunPrompt.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {tr("workflowDialog.dryRunPrompt.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tr("workflowDialog.actions.cancel")}</AlertDialogCancel>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                markDryRunPrompted()
                setShowDryRunPrompt(false)
                applyEnabledChange(true)
              }}
            >
              {tr("workflowDialog.dryRunPrompt.enableWithout")}
            </Button>
            <AlertDialogAction
              onClick={() => {
                markDryRunPrompted()
                setShowDryRunPrompt(false)
                applyEnabledChange(true, { forceDryRun: true })
              }}
            >
              {tr("workflowDialog.dryRunPrompt.enableWith")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={showAddCustomGroup} onOpenChange={setShowAddCustomGroup}>
        <AlertDialogContent className="sm:max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>{tr("workflowDialog.customGroup.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {tr("workflowDialog.customGroup.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1">
              <Label htmlFor="group-id" className="text-sm">{tr("workflowDialog.customGroup.groupId")}</Label>
              <Input
                id="group-id"
                value={newGroupId}
                onChange={(e) => setNewGroupId(e.target.value)}
                placeholder={tr("workflowDialog.placeholders.exampleCustomGroupId")}
                className="h-8 text-xs"
              />
              <p className="text-xs text-muted-foreground">{tr("workflowDialog.customGroup.groupIdDescription")}</p>
            </div>

            <div className="space-y-1">
              <Label className="text-sm">{tr("workflowDialog.customGroup.keysLabel")}</Label>
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
                {tr("workflowDialog.customGroup.keysDescription")}
              </p>
            </div>

            <div className="space-y-1">
              <Label className="text-sm">{tr("workflowDialog.customGroup.ambiguousPolicy")}</Label>
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
                  <SelectItem value={AMBIGUOUS_POLICY_NONE_VALUE}>{tr("workflowDialog.customGroup.ambiguous.noneDefault")}</SelectItem>
                  <SelectItem value="verify_overlap">{tr("workflowDialog.customGroup.ambiguous.verifyOverlap")}</SelectItem>
                  <SelectItem value="skip">{tr("workflowDialog.customGroup.ambiguous.skip")}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {tr("workflowDialog.customGroup.ambiguousDescription")}
              </p>
            </div>

            {newGroupAmbiguousPolicy === "verify_overlap" && (
              <div className="space-y-1">
                <Label htmlFor="min-overlap" className="text-sm">{tr("workflowDialog.customGroup.minOverlap")}</Label>
                <Input
                  id="min-overlap"
                  type="number"
                  value={newGroupMinOverlap}
                  onChange={(e) => setNewGroupMinOverlap(e.target.value)}
                  min="0"
                  max="100"
                  className="h-8 text-xs"
                />
                <p className="text-xs text-muted-foreground">{tr("workflowDialog.customGroup.defaultNinety")}</p>
              </div>
            )}
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel>{tr("workflowDialog.actions.cancel")}</AlertDialogCancel>
            <Button
              type="button"
              onClick={() => {
                // Validate
                if (!newGroupId.trim()) {
                  toast.error(tr("workflowDialog.customGroup.errors.groupIdEmpty"))
                  return
                }
                if (!/^[a-zA-Z0-9_]+$/.test(newGroupId)) {
                  toast.error(tr("workflowDialog.customGroup.errors.groupIdInvalid"))
                  return
                }
                if (newGroupKeys.length === 0) {
                  toast.error(tr("workflowDialog.customGroup.errors.selectKey"))
                  return
                }
                // Check for duplicates
                if ((formState.exprGrouping?.groups || []).some(g => g.id === newGroupId)) {
                  toast.error(tr("workflowDialog.customGroup.errors.groupIdExists"))
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
                toast.success(tr("workflowDialog.customGroup.toasts.added"))
              }}
            >
              {tr("workflowDialog.customGroup.add")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
