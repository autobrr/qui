/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryBuilder } from "@/components/query-builder"
import {
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
import { buildCategorySelectOptions } from "@/lib/category-utils"
import { type CsvColumn, downloadBlob, toCsv } from "@/lib/csv-export"
import { pickTrackerIconDomain } from "@/lib/tracker-icons"
import { cn, formatBytes, normalizeTrackerDomains, parseTrackerDomains } from "@/lib/utils"
import type {
  ActionConditions,
  Automation,
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
import { toast } from "sonner"
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

type ActionType = "speedLimits" | "shareLimits" | "pause" | "resume" | "delete" | "tag" | "category" | "move" | "externalProgram"

// Actions that can be combined (Delete must be standalone)
const COMBINABLE_ACTIONS: ActionType[] = ["speedLimits", "shareLimits", "pause", "resume", "tag", "category", "move", "externalProgram"]

const ACTION_LABELS: Record<ActionType, string> = {
  speedLimits: "Speed limits",
  shareLimits: "Share limits",
  pause: "Pause",
  delete: "Delete",
  resume: "Resume",
  tag: "Tag",
  category: "Category",
  move: "Move",
  externalProgram: "Run external program",
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
    label: "Cross-seed (content path)",
    description: "Group torrents with the same content path",
  },
  {
    id: "cross_seed_content_save_path",
    label: "Cross-seed (content + save path)",
    description: "Group torrents with the same content path and save path",
  },
  {
    id: "release_item",
    label: "Release item",
    description: "Group torrents with the same content type and effective name",
  },
  {
    id: "tracker_release_item",
    label: "Tracker release item",
    description: "Group torrents from the same tracker with the same content type and effective name",
  },
  {
    id: "hardlink_signature",
    label: "Hardlink signature",
    description: "Group torrents that share the same physical files via hardlinks (requires local filesystem access)",
  },
] as const

// Speed limit mode: no_change = omit, unlimited = 0, custom = user value (>0)
type SpeedLimitMode = "no_change" | "unlimited" | "custom"

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
  exprTags: string[]
  exprTagMode: "full" | "add" | "remove"
  exprUseTrackerAsTag: boolean
  exprUseDisplayName: boolean
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
  deleteEnabled: false,
  resumeEnabled: false,
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
  exprTags: [],
  exprTagMode: "full",
  exprUseTrackerAsTag: false,
  exprUseDisplayName: false,
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
  if (state.deleteEnabled) actions.push("delete")
  if (state.resumeEnabled) actions.push("resume")
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
  const queryClient = useQueryClient()
  const [formState, setFormState] = useState<FormState>(emptyFormState)
  const [previewResult, setPreviewResult] = useState<AutomationPreviewResult | null>(null)
  const [previewInput, setPreviewInput] = useState<FormState | null>(null)
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [enabledBeforePreview, setEnabledBeforePreview] = useState<boolean | null>(null)
  const [showDryRunPrompt, setShowDryRunPrompt] = useState(false)
  const [dryRunPromptedForNew, setDryRunPromptedForNew] = useState(false)
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
  const tagsInputRef = useRef<HTMLInputElement>(null)
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

  const commitPendingTags = () => {
    if (tagsInputRef.current) {
      const tags = tagsInputRef.current.value.split(",").map(t => t.trim()).filter(Boolean)
      setFormState(prev => ({ ...prev, exprTags: tags }))
      tagsInputRef.current.value = tags.join(", ")
    }
  }

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

  const trackerCustomizationMaps = useMemo(
    () => buildTrackerCustomizationMaps(trackerCustomizations),
    [trackerCustomizations]
  )

  // Process trackers to apply customizations (nicknames and merged domains)
  // Also includes trackers from the current workflow being edited, so they remain
  // visible even if no torrents currently use them
  const trackerOptions: Option[] = useMemo(() => {
    const { domainToCustomization } = trackerCustomizationMaps
    const trackers = trackersQuery.data ? Object.keys(trackersQuery.data) : []
    const processed: Option[] = []
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
          label: customization.displayName,
          value: mergedValue,
          icon: <TrackerIconImage tracker={iconDomain} trackerIcons={trackerIcons} />,
        })
      } else {
        if (seenDisplayNames.has(lowerTracker) || seenValues.has(tracker)) return
        seenDisplayNames.add(lowerTracker)
        seenValues.add(tracker)

        processed.push({
          label: tracker,
          value: tracker,
          icon: <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />,
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

    processed.sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }))

    return processed
  }, [trackersQuery.data, trackerCustomizationMaps, trackerIcons, rule])

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
        let exprTags: string[] = []
        let exprTagMode: FormState["exprTagMode"] = "full"
        let exprUseTrackerAsTag = false
        let exprUseDisplayName = false
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
            ?? conditions.delete?.condition
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
          if (conditions.delete?.enabled) {
            deleteEnabled = true
            exprDeleteMode = conditions.delete.mode ?? "deleteWithFilesPreserveCrossSeeds"
            exprIncludeHardlinks = conditions.delete.includeHardlinks ?? false
            exprDeleteGroupId = conditions.delete.groupId ?? ""
            exprDeleteAtomic = conditions.delete.atomic ?? ""
          }
          if (conditions.tag?.enabled) {
            tagEnabled = true
            exprTags = conditions.tag.tags ?? []
            exprTagMode = conditions.tag.mode ?? "full"
            exprUseTrackerAsTag = conditions.tag.useTrackerAsTag ?? false
            exprUseDisplayName = conditions.tag.useDisplayName ?? false
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
          exprTags,
          exprTagMode,
          exprUseTrackerAsTag,
          exprUseDisplayName,
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
          toast.info("Switched to 'Remove with files' because Free Space condition requires actual disk space to be freed")
        }
      }
    }
  }, [formState.actionCondition, formState.deleteEnabled, formState.exprDeleteMode])

  // Auto-switch interval from 1 minute when FREE_SPACE delete condition is added
  // The backend has a ~5 minute cooldown, so 1 minute intervals would be ineffective
  // Only switch on user edits, not during initial hydration (respect saved config)
  useEffect(() => {
    if (isHydrating.current) return
    if (formState.deleteEnabled && formState.intervalSeconds === 60) {
      if (conditionUsesField(formState.actionCondition, "FREE_SPACE")) {
        setFormState(prev => ({ ...prev, intervalSeconds: 300 })) // Switch to 5 minutes
        toast.info("Switched to 5 minute interval because Free Space deletes have a ~5 minute cooldown")
      }
    }
  }, [formState.actionCondition, formState.deleteEnabled, formState.intervalSeconds])

  // Auto-switch free space source from "path" to "qbittorrent" on Windows (not supported)
  // This must run during hydration to handle legacy workflows opened on Windows.
  // Only toast after hydration to avoid noise when opening dialogs.
  useEffect(() => {
    if (!supportsFreeSpacePathSource && formState.exprFreeSpaceSourceType === "path") {
      setFormState(prev => ({ ...prev, exprFreeSpaceSourceType: "qbittorrent" }))
      if (!isHydrating.current) {
        toast.warning("Path-based free space source is not supported on Windows. Switched to qBittorrent default.")
      }
    }
  }, [supportsFreeSpacePathSource, formState.exprFreeSpaceSourceType])

  const validateFreeSpaceSource = (state: FormState): boolean => {
    const usesFreeSpace = conditionUsesField(state.actionCondition, "FREE_SPACE")
    if (!usesFreeSpace || state.exprFreeSpaceSourceType !== "path") {
      setFreeSpaceSourcePathError(null)
      return true
    }

    // Reject if path source is selected but not supported (safety net for edge cases)
    if (!supportsFreeSpacePathSource) {
      setFreeSpaceSourcePathError("Path-based free space source is not supported on Windows.")
      toast.error("Switch Free space source to Default (qBittorrent)")
      return false
    }
    if (!hasLocalFilesystemAccess) {
      setFreeSpaceSourcePathError("Path-based free space source requires Local Filesystem Access.")
      toast.error("Enable Local Filesystem Access in instance settings, or use Default (qBittorrent)")
      return false
    }

    const trimmedPath = state.exprFreeSpaceSourcePath.trim()
    if (trimmedPath === "") {
      setFreeSpaceSourcePathError("Path is required when using 'Path on server'.")
      toast.error("Enter a path or switch Free space source back to Default (qBittorrent)")
      return false
    }

    setFreeSpaceSourcePathError(null)
    return true
  }

  // Build payload from form state (shared by preview and save)
  const buildPayload = (input: FormState): AutomationInput => {
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
      conditions.tag = {
        enabled: true,
        tags: input.exprTags,
        mode: input.exprTagMode,
        useTrackerAsTag: input.exprUseTrackerAsTag,
        useDisplayName: input.exprUseDisplayName,
        condition: input.actionCondition ?? undefined,
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
  }

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
    formState.deleteEnabled,
    formState.tagEnabled,
    formState.categoryEnabled,
    formState.moveEnabled,
    formState.externalProgramEnabled,
  ].filter(Boolean).length

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
      toast.error(error instanceof Error ? error.message : "Failed to preview rule")
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
      toast.error(error instanceof Error ? error.message : "Failed to load more previews")
    },
  })

  const handleLoadMore = () => {
    if (!previewInput || !previewResult) {
      return
    }
    loadMorePreview.mutate()
  }

  const applyEnabledChange = useCallback((checked: boolean, options?: { forceDryRun?: boolean }) => {
    if (checked && isDeleteRule && !formState.actionCondition) {
      toast.error("Delete requires at least one condition")
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
  }, [formState, isCategoryRule, isDeleteRule, previewMutation, validateFreeSpaceSource])

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
      toast.error(error instanceof Error ? error.message : "Failed to switch preview view")
    } finally {
      setIsLoadingPreviewView(false)
    }
  }

  // CSV columns for automation preview export
  const csvColumns: CsvColumn<AutomationPreviewTorrent>[] = [
    { header: "Name", accessor: t => t.name },
    { header: "Hash", accessor: t => t.hash },
    { header: "Tracker", accessor: t => t.tracker },
    { header: "Size", accessor: t => formatBytes(t.size) },
    { header: "Ratio", accessor: t => t.ratio === -1 ? "Inf" : t.ratio.toFixed(2) },
    { header: "Seeding Time (s)", accessor: t => t.seedingTime },
    { header: "Category", accessor: t => t.category },
    { header: "Tags", accessor: t => t.tags },
    { header: "State", accessor: t => t.state },
    { header: "Added On", accessor: t => t.addedOn },
    { header: "Path", accessor: t => t.contentPath ?? "" },
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
      toast.success(`Exported ${allItems.length} torrents to CSV`)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to export preview")
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
      toast.success(`Workflow ${rule ? "updated" : "created"}`)
      setShowConfirmDialog(false)
      setPreviewResult(null)
      setPreviewInput(null)
      onOpenChange(false)
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
      onSuccess?.()
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to save automation")
    },
  })

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    setRegexErrors([]) // Clear previous errors

    // Parse tags from input ref synchronously to avoid relying on async setFormState
    let parsedTags: string[] = []
    if (!formState.exprUseTrackerAsTag && tagsInputRef.current) {
      parsedTags = tagsInputRef.current.value.split(",").map(t => t.trim()).filter(Boolean)
    }

    // Build submitState with parsed tags for validation and mutation
    const submitState: FormState = {
      ...formState,
      exprTags: formState.exprUseTrackerAsTag ? [] : parsedTags,
    }

    if (!validateFreeSpaceSource(submitState)) {
      return
    }

    // Sync the input display and formState (for UI consistency after save)
    if (tagsInputRef.current) {
      tagsInputRef.current.value = parsedTags.join(", ")
    }
    setFormState(submitState)

    if (!submitState.name) {
      toast.error("Name is required")
      return
    }
    const selectedTrackers = submitState.trackerDomains.filter(Boolean)
    if (!submitState.applyToAllTrackers && selectedTrackers.length === 0) {
      toast.error("Select at least one tracker")
      return
    }

    // At least one action must be enabled
    if (enabledActionsCount === 0) {
      toast.error("Enable at least one action")
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
        toast.error("Set at least one speed limit")
        return
      }
      // Validate custom values are > 0
      if (submitState.exprUploadMode === "custom" && (submitState.exprUploadValue === undefined || submitState.exprUploadValue <= 0)) {
        toast.error("Upload speed must be greater than 0 (use Unlimited for no limit)")
        return
      }
      if (submitState.exprDownloadMode === "custom" && (submitState.exprDownloadValue === undefined || submitState.exprDownloadValue <= 0)) {
        toast.error("Download speed must be greater than 0 (use Unlimited for no limit)")
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
        toast.error("Set ratio limit or seeding time")
        return
      }
    }
    if (submitState.tagEnabled) {
      if (!submitState.exprUseTrackerAsTag && submitState.exprTags.length === 0) {
        toast.error("Specify at least one tag or enable 'Use tracker name'")
        return
      }
    }
    if (submitState.categoryEnabled) {
      if (!submitState.exprCategory) {
        toast.error("Select a category")
        return
      }
    }
    if (submitState.externalProgramEnabled) {
      if (!submitState.exprExternalProgramId) {
        toast.error("Select an external program")
        return
      }
    }
    if (submitState.deleteEnabled && !submitState.actionCondition) {
      toast.error("Delete requires at least one condition")
      return
    }
    const trimmedSubmitMovePath = submitState.exprMovePath?.trim()
    if (submitState.moveEnabled && !trimmedSubmitMovePath) {
      toast.error("Move requires a path")
      return
    }

    // Validate regex patterns before saving (only if enabling the workflow)
    const payload = buildPayload(submitState)
    if (submitState.enabled) {
      try {
        const validation = await api.validateAutomationRegex(instanceId, payload)
        if (!validation.valid && validation.errors.length > 0) {
          setRegexErrors(validation.errors)
          toast.error("Invalid regex pattern - Go/RE2 does not support Perl features like lookahead/lookbehind")
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
            <DialogTitle>{rule ? "Edit Workflow" : "Add Workflow"}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
            <div className="flex-1 overflow-y-auto space-y-3 sm:pr-1">
              {/* Header row: Name + All Trackers toggle */}
              <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
                <div className="space-y-1.5">
                  <Label htmlFor="rule-name">Name</Label>
                  <Input
                    id="rule-name"
                    value={formState.name}
                    onChange={(e) => setFormState(prev => ({ ...prev, name: e.target.value }))}
                    required
                    placeholder="Workflow name"
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
                  <Label htmlFor="all-trackers" className="text-sm cursor-pointer whitespace-nowrap">All trackers</Label>
                </div>
              </div>

              {/* Trackers */}
              {!formState.applyToAllTrackers && (
                <div className="space-y-1.5">
                  <Label>Trackers</Label>
                  <MultiSelect
                    options={trackerOptions}
                    selected={formState.trackerDomains}
                    onChange={(next) => setFormState(prev => ({ ...prev, trackerDomains: next }))}
                    placeholder="Select trackers..."
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
                  <Label>Conditions (optional)</Label>
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
                  />
                  {formState.deleteEnabled && !formState.actionCondition && (
                    <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm">
                      <p className="font-medium text-destructive">Delete requires at least one condition.</p>
                    </div>
                  )}
                  {regexErrors.length > 0 && (
                    <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm">
                      <p className="font-medium text-destructive mb-1">Invalid regex pattern</p>
                      {regexErrors.map((err, i) => (
                        <p key={i} className="text-destructive/80 text-xs">
                          <span className="font-mono">{err.pattern}</span>: {err.message}
                        </p>
                      ))}
                      <p className="text-muted-foreground text-xs mt-2">
                        Go/RE2 does not support Perl features like lookahead (?=), lookbehind (?&lt;=), or negative variants (?!), (?&lt;!).
                      </p>
                    </div>
                  )}
                </div>

                {/* Grouping Configuration - shown when GROUP_SIZE or IS_GROUPED is used */}
                {(conditionUsesField(formState.actionCondition, "GROUP_SIZE") || conditionUsesField(formState.actionCondition, "IS_GROUPED")) && (
                  <div className="rounded-lg border p-3 space-y-3 bg-muted/30">
                    <div className="flex items-center gap-2">
                      <Label className="text-sm font-medium">Group-aware condition configuration</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label="About grouping configuration"
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="right" className="max-w-[340px]">
                            <p>
                              Configure how torrents are grouped for GROUP_SIZE and IS_GROUPED conditions.
                              GROUP_SIZE returns the number of torrents in the group; IS_GROUPED is true when group size &gt; 1.
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>

                    {/* Default Group ID selector */}
                    <div className="space-y-1">
                      <Label className="text-xs">Default group</Label>
                      <Select
                        value={formState.exprGrouping?.defaultGroupId ?? "_none"}
                        onValueChange={(value) => {
                          setFormState(prev => ({
                            ...prev,
                            exprGrouping: {
                              ...prev.exprGrouping,
                              defaultGroupId: value === "_none" ? undefined : value,
                            },
                          }))
                        }}
                      >
                        <SelectTrigger className="h-8 text-xs">
                          <SelectValue placeholder="Select a group..." />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="_none">(None - GROUP_SIZE/IS_GROUPED disabled)</SelectItem>
                          {BUILTIN_GROUPS.map(group => (
                            <SelectItem key={group.id} value={group.id}>
                              {group.label}
                            </SelectItem>
                          ))}
                          {(formState.exprGrouping?.groups || []).map(group => (
                            <SelectItem key={group.id} value={group.id}>
                              {group.id} (custom)
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-muted-foreground">
                        Which grouping strategy to use for GROUP_SIZE and IS_GROUPED conditions
                      </p>
                    </div>

                    {/* Show description of selected group */}
                    {formState.exprGrouping?.defaultGroupId && (
                      <div className="rounded-sm border border-border/50 bg-background p-2 text-xs text-muted-foreground">
                        {(() => {
                          const builtin = BUILTIN_GROUPS.find(g => g.id === formState.exprGrouping?.defaultGroupId)
                          if (builtin) {
                            return (
                              <>
                                <p className="font-medium text-foreground">{builtin.label}</p>
                                <p>{builtin.description}</p>
                              </>
                            )
                          }
                          const custom = (formState.exprGrouping?.groups || []).find(g => g.id === formState.exprGrouping?.defaultGroupId)
                          if (custom) {
                            return (
                              <>
                                <p className="font-medium text-foreground">{custom.id}</p>
                                <p>Custom group with keys: <span className="font-mono">{custom.keys.join(", ")}</span></p>
                                {custom.ambiguousPolicy && (
                                  <p>Ambiguous policy: {custom.ambiguousPolicy} (min overlap: {custom.minFileOverlapPercent ?? 90}%)</p>
                                )}
                              </>
                            )
                          }
                          return null
                        })()}
                      </div>
                    )}

                    {/* Custom groups editor */}
                    {(formState.exprGrouping?.groups || []).length > 0 && (
                      <div className="space-y-2 border-t pt-3">
                        <p className="text-xs font-medium text-muted-foreground">Custom groups</p>
                        {(formState.exprGrouping?.groups || []).map((group, idx) => (
                          <div key={group.id} className="border rounded-sm p-2 space-y-1.5 text-xs bg-background">
                            <div className="flex items-center justify-between gap-1">
                              <div className="flex-1 min-w-0">
                                <p className="font-mono font-medium truncate">{group.id}</p>
                                <p className="text-muted-foreground">Keys: {group.keys.join(", ")}</p>
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
                      Add custom group
                    </Button>
                  </div>
                )}

                {/* Actions section */}
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <Label>Action</Label>
                    {/* Add action dropdown - only show if Delete is not enabled, at least one action exists, and there are available actions to add */}
                    {!formState.deleteEnabled && enabledActionsCount > 0 && (() => {
                      const enabledActions = getEnabledActions(formState)
                      const availableActions = COMBINABLE_ACTIONS.filter(a => !enabledActions.includes(a))
                      if (availableActions.length === 0) return null
                      return (
                        <Select
                          value=""
                          onValueChange={(action: ActionType) => {
                            setFormState(prev => ({ ...prev, ...setActionEnabled(action, true) }))
                          }}
                        >
                          <SelectTrigger className="w-fit h-7 text-xs">
                            <Plus className="h-3 w-3 mr-1" />
                            Add action
                          </SelectTrigger>
                          <SelectContent>
                            {availableActions.map(action => (
                              <SelectItem key={action} value={action}>{ACTION_LABELS[action]}</SelectItem>
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
                            deleteEnabled: true,
                            tagEnabled: false,
                            categoryEnabled: false,
                            moveEnabled: false,
                            externalProgramEnabled: false,
                            // Safety: when selecting delete in "create new" mode, start disabled
                            enabled: !rule ? false : prev.enabled,
                          }))
                        } else {
                          setFormState(prev => ({ ...prev, ...setActionEnabled(action, true) }))
                        }
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select an action..." />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="speedLimits">Speed limits</SelectItem>
                        <SelectItem value="shareLimits">Share limits</SelectItem>
                        <SelectItem value="pause">Pause</SelectItem>
                        <SelectItem value="resume">Resume</SelectItem>
                        <SelectItem value="tag">Tag</SelectItem>
                        <SelectItem value="category">Category</SelectItem>
                        <SelectItem value="move">Move</SelectItem>
                        <SelectItem value="externalProgram">Run external program</SelectItem>
                        <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete (standalone only)</SelectItem>
                      </SelectContent>
                    </Select>
                  )}

                  {/* Render enabled actions */}
                  <div className="space-y-3">
                    {/* Speed limits */}
                    {formState.speedLimitsEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">Speed limits</Label>
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
                            <Label className="text-xs">Upload limit</Label>
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
                                  <SelectItem value="no_change">No change</SelectItem>
                                  <SelectItem value="unlimited">Unlimited</SelectItem>
                                  <SelectItem value="custom">Custom</SelectItem>
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
                                    placeholder="e.g. 10"
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
                            <Label className="text-xs">Download limit</Label>
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
                                  <SelectItem value="no_change">No change</SelectItem>
                                  <SelectItem value="unlimited">Unlimited</SelectItem>
                                  <SelectItem value="custom">Custom</SelectItem>
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
                                    placeholder="e.g. 10"
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
                          <Label className="text-sm font-medium">Share limits</Label>
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
                            <Label className="text-xs">Ratio limit</Label>
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
                                  <SelectItem value="no_change">No change</SelectItem>
                                  <SelectItem value="global">Use global</SelectItem>
                                  <SelectItem value="unlimited">Unlimited</SelectItem>
                                  <SelectItem value="custom">Custom</SelectItem>
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
                                  placeholder="e.g. 2.0"
                                />
                              )}
                            </div>
                          </div>
                          {/* Seed time */}
                          <div className="space-y-1.5">
                            <Label className="text-xs">Seed time (minutes)</Label>
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
                                  <SelectItem value="no_change">No change</SelectItem>
                                  <SelectItem value="global">Use global</SelectItem>
                                  <SelectItem value="unlimited">Unlimited</SelectItem>
                                  <SelectItem value="custom">Custom</SelectItem>
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
                                  placeholder="e.g. 1440"
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
                          <Label className="text-sm font-medium">Pause</Label>
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
                          <Label className="text-sm font-medium">Resume</Label>
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
                    {/* Tag */}
                    {formState.tagEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">Tag</Label>
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
                        <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-3 items-start">
                          {formState.exprUseTrackerAsTag ? (
                            <div className="space-y-1">
                              <Label className="text-xs text-muted-foreground">Tags derived from tracker</Label>
                              <div className="flex items-center gap-2 h-9 px-3 rounded-md border bg-muted/50 text-sm text-muted-foreground">
                                Torrents will be tagged with their tracker name
                              </div>
                            </div>
                          ) : (
                            <div className="space-y-1">
                              <Label className="text-xs">Tags</Label>
                              <Input
                                ref={tagsInputRef}
                                type="text"
                                defaultValue={formState.exprTags.join(", ")}
                                onBlur={commitPendingTags}
                                placeholder="tag1, tag2, ..."
                              />
                            </div>
                          )}
                          <div className="space-y-1">
                            <Label className="text-xs">Mode</Label>
                            <Select
                              value={formState.exprTagMode}
                              onValueChange={(value: FormState["exprTagMode"]) => setFormState(prev => ({ ...prev, exprTagMode: value }))}
                            >
                              <SelectTrigger className="w-[120px]">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="full">Full sync</SelectItem>
                                <SelectItem value="add">Add only</SelectItem>
                                <SelectItem value="remove">Remove only</SelectItem>
                              </SelectContent>
                            </Select>
                          </div>
                        </div>
                        <div className="flex flex-col sm:flex-row sm:items-center gap-3 sm:gap-4">
                          <div className="flex items-center gap-2">
                            <Switch
                              id="use-tracker-tag"
                              checked={formState.exprUseTrackerAsTag}
                              onCheckedChange={(checked) => setFormState(prev => ({
                                ...prev,
                                exprUseTrackerAsTag: checked,
                                exprUseDisplayName: checked ? prev.exprUseDisplayName : false,
                                exprTags: checked ? [] : prev.exprTags,
                              }))}
                            />
                            <Label htmlFor="use-tracker-tag" className="text-sm cursor-pointer whitespace-nowrap">
                              Use tracker name as tag
                            </Label>
                          </div>
                          {formState.exprUseTrackerAsTag && (
                            <div className="flex items-center gap-2">
                              <Switch
                                id="use-display-name"
                                checked={formState.exprUseDisplayName}
                                onCheckedChange={(checked) => setFormState(prev => ({ ...prev, exprUseDisplayName: checked }))}
                              />
                              <Label htmlFor="use-display-name" className="text-sm cursor-pointer whitespace-nowrap">
                                Use display name
                              </Label>
                              <TooltipProvider delayDuration={150}>
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      type="button"
                                      className="inline-flex items-center text-muted-foreground hover:text-foreground"
                                      aria-label="About display names"
                                    >
                                      <Info className="h-3.5 w-3.5" />
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent className="max-w-[280px]">
                                    <p>Uses friendly names from Tracker Customizations instead of raw domains (e.g., "MyTracker" instead of "tracker.example.com").</p>
                                  </TooltipContent>
                                </Tooltip>
                              </TooltipProvider>
                            </div>
                          )}
                        </div>
                      </div>
                    )}

                    {/* Category */}
                    {formState.categoryEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">Category</Label>
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
                            <Label className="text-xs">Move to category</Label>
                            <Select
                              value={formState.exprCategory}
                              onValueChange={(value) => setFormState(prev => ({ ...prev, exprCategory: value }))}
                            >
                              <SelectTrigger className="w-fit min-w-[160px]">
                                <Folder className="h-3.5 w-3.5 mr-2 text-muted-foreground" />
                                <SelectValue placeholder="Select category" />
                              </SelectTrigger>
                              <SelectContent>
                                {categoryOptions.map(opt => (
                                  <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
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
                                Include affected cross-seeds
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
                          <Label className="text-sm font-medium">Run external program</Label>
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
                          <Label className="text-xs">Program</Label>
                          {externalProgramsLoading ? (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50 flex items-center gap-2">
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              Loading external programs...
                            </div>
                          ) : externalProgramsError ? (
                            <div className="text-sm text-destructive p-2 border border-destructive/50 rounded-md bg-destructive/10">
                              Failed to load external programs. Please try again.
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
                                <SelectValue placeholder="Select a program..." />
                              </SelectTrigger>
                              <SelectContent>
                                {externalPrograms.map(program => (
                                  <SelectItem
                                    key={program.id}
                                    value={program.id.toString()}
                                  >
                                    {program.name}
                                    {!program.enabled && (
                                      <span className="ml-2 text-xs text-muted-foreground">(disabled)</span>
                                    )}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          ) : (
                            <div className="text-sm text-muted-foreground p-2 border rounded-md bg-muted/50">
                              No external programs configured.{" "}
                              <a
                                href={withBasePath("/settings?tab=external-programs")}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-primary hover:underline"
                              >
                                Configure in Settings
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
                          <Label className="text-sm font-medium text-destructive">Delete</Label>
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
                          <Label className="text-xs">Mode</Label>
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
                                            Remove (keep files)
                                          </SelectItem>
                                        </div>
                                      </TooltipTrigger>
                                      {keepFilesDisabled && (
                                        <TooltipContent side="left" className="max-w-[280px]">
                                          <p>Disabled when using Free Space condition. Keep-files mode cannot satisfy a free space target because no disk space is freed.</p>
                                        </TooltipContent>
                                      )}
                                    </Tooltip>
                                  </TooltipProvider>
                                  <SelectItem value="deleteWithFiles" className="text-destructive focus:text-destructive">Remove with files</SelectItem>
                                  <SelectItem value="deleteWithFilesPreserveCrossSeeds" className="text-destructive focus:text-destructive">Remove with files (preserve cross-seeds)</SelectItem>
                                  <SelectItem value="deleteWithFilesIncludeCrossSeeds" className="text-destructive focus:text-destructive">Remove with files (include cross-seeds)</SelectItem>
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
                                      Include hardlinked copies
                                    </span>
                                  </label>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="max-w-[320px]">
                                  {hasLocalFilesystemAccess ? (
                                    <p>
                                      Also delete torrents that share the same underlying files via hardlinks.
                                      Only includes hardlinks fully inside qBittorrent; never follows hardlinks outside.
                                    </p>
                                  ) : (
                                    <p>
                                      Requires &quot;Local Filesystem Access&quot; to be enabled in instance settings.
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
                          <Label className="text-sm font-medium">Move</Label>
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
                          <Label className="text-xs">New save path</Label>
                          <Input
                            type="text"
                            value={formState.exprMovePath}
                            onChange={(e) => setFormState(prev => ({ ...prev, exprMovePath: e.target.value }))}
                            placeholder="e.g., /data/torrents"
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
                              Skip if cross-seeds don't match the rule's conditions
                            </Label>
                            <TooltipProvider delayDuration={150}>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <button
                                    type="button"
                                    className="shrink-0 inline-flex items-center text-muted-foreground hover:text-foreground"
                                    aria-label="About skipping move if cross-seeds exist"
                                  >
                                    <Info className="h-3.5 w-3.5" />
                                  </button>
                                </TooltipTrigger>
                                <TooltipContent className="max-w-[320px]">
                                  <p>
                                    Skips the move if there are any other torrents in the same cross-seed group that do not match the rule's conditions. Otherwise, all cross-seeds will be moved, even if not matched by the rule's conditions.
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
                      <Label className="text-sm font-medium">Free space source</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label="About free space source"
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="left" className="max-w-[320px]">
                            <p>
                              Choose where to read free space from. Default uses qBittorrent&apos;s
                              reported free space. Use &quot;Path on server&quot; to check free space on
                              a specific disk or mount point.
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
                        <SelectValue placeholder="Select source" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="qbittorrent">Default (qBittorrent)</SelectItem>
                        <SelectItem value="path" disabled={!hasLocalFilesystemAccess || !supportsFreeSpacePathSource}>
                          Path on server{!supportsFreeSpacePathSource ? " (not supported on Windows)" : !hasLocalFilesystemAccess ? " (requires Local Access)" : ""}
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
                            placeholder="/mnt/downloads"
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
                          Enter the path to check free space on (e.g., a mount point)
                        </p>
                      </div>
                    )}
                  </div>
                )}

                {formState.categoryEnabled && (formState.exprIncludeCrossSeeds || formState.exprBlockIfCrossSeedInCategories.length > 0) && (
                  <div className="space-y-1.5">
                    <div className="flex items-center gap-1.5">
                      <Label className="text-xs">Skip if cross-seed exists in categories</Label>
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex items-center text-muted-foreground hover:text-foreground"
                              aria-label="About skipping when cross-seeds exist"
                            >
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent className="max-w-[320px]">
                            <p>
                              Useful with *arr import queues: prevents automation from moving the torrents if at least one of them are in the *arr import queue.
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                    <MultiSelect
                      options={categoryOptions}
                      selected={formState.exprBlockIfCrossSeedInCategories}
                      onChange={(next) => setFormState(prev => ({ ...prev, exprBlockIfCrossSeedInCategories: next }))}
                      placeholder="Select categories..."
                      creatable
                      onCreateOption={(value) => setFormState(prev => ({
                        ...prev,
                        exprBlockIfCrossSeedInCategories: [...prev.exprBlockIfCrossSeedInCategories, value],
                      }))}
                    />
                    <p className="text-xs text-muted-foreground">
                      Skips the category change if another torrent pointing at the same on-disk content is already in one of these categories.
                    </p>
                  </div>
                )}
              </div>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-3 pt-3 border-t mt-3">
              <div className="flex items-center gap-4 flex-wrap">
                <div className="flex items-center gap-2">
                  <Switch
                    id="rule-enabled"
                    checked={formState.enabled}
                    onCheckedChange={handleEnabledToggle}
                  />
                  <Label htmlFor="rule-enabled" className="text-sm font-normal cursor-pointer">Enabled</Label>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    id="rule-dry-run"
                    checked={formState.dryRun}
                    onCheckedChange={(checked) => setFormState(prev => ({ ...prev, dryRun: checked }))}
                  />
                  <Label htmlFor="rule-dry-run" className="text-sm font-normal cursor-pointer">Dry run</Label>
                </div>
                <div className="flex items-center gap-2">
                  <Label htmlFor="rule-interval" className="text-sm font-normal text-muted-foreground whitespace-nowrap">Run every</Label>
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
                      <SelectItem value="default">Default (15m)</SelectItem>
                      <SelectItem value="60" disabled={deleteUsesFreeSpace}>1 minute</SelectItem>
                      <SelectItem value="300">5 minutes</SelectItem>
                      <SelectItem value="900">15 minutes</SelectItem>
                      <SelectItem value="1800">30 minutes</SelectItem>
                      <SelectItem value="3600">1 hour</SelectItem>
                      <SelectItem value="7200">2 hours</SelectItem>
                      <SelectItem value="14400">4 hours</SelectItem>
                      <SelectItem value="21600">6 hours</SelectItem>
                      <SelectItem value="43200">12 hours</SelectItem>
                      <SelectItem value="86400">24 hours</SelectItem>
                      {/* Show custom option if current value is non-preset */}
                      {formState.intervalSeconds !== null &&
                        ![60, 300, 900, 1800, 3600, 7200, 14400, 21600, 43200, 86400].includes(formState.intervalSeconds) && (
                        <SelectItem value={String(formState.intervalSeconds)}>
                          Custom ({formState.intervalSeconds}s)
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
                            aria-label="About Free Space cooldown"
                          >
                            <Info className="h-3.5 w-3.5" />
                          </button>
                        </TooltipTrigger>
                        <TooltipContent className="max-w-[280px]">
                          <p>After removing files, qui waits ~5 minutes before running Free Space deletes again to allow qBittorrent to refresh disk free space.</p>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  )}
                  {deleteUsesFreeSpace && formState.intervalSeconds === 60 && (
                    <span className="text-xs text-yellow-500">Effective minimum ~5m due to cooldown</span>
                  )}
                </div>
              </div>
              <div className="flex gap-2 w-full sm:w-auto">
                <Button type="button" variant="outline" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" onClick={() => onOpenChange(false)}>
                  Cancel
                </Button>
                <Button type="submit" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" disabled={createOrUpdate.isPending || previewMutation.isPending}>
                  {(createOrUpdate.isPending || previewMutation.isPending) && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  {rule ? "Save" : "Create"}
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
          isDeleteRule ? (formState.enabled ? "Confirm Delete Rule" : "Preview Delete Rule") : `Confirm Category Change → ${previewInput?.exprCategory ?? formState.exprCategory}`
        }
        description={
          previewResult && previewResult.totalMatches > 0 ? (
            isDeleteRule ? (
              formState.enabled ? (
                <>
                  <p className="text-destructive font-medium">
                    This rule will affect {previewResult.totalMatches} torrent{previewResult.totalMatches !== 1 ? "s" : ""} that currently match.
                  </p>
                  <p className="text-muted-foreground text-sm">Confirming will save and enable this rule.</p>
                </>
              ) : (
                <>
                  <p className="text-muted-foreground">
                    {previewResult.totalMatches} torrent{previewResult.totalMatches !== 1 ? "s" : ""} would match this rule if enabled.
                  </p>
                  <p className="text-muted-foreground text-sm">Confirming will save this rule.</p>
                </>
              )
            ) : (
              <>
                <p>
                  This rule will move{" "}
                  <strong>{(previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0)}</strong> torrent{((previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0)) !== 1 ? "s" : ""}
                  {previewResult.crossSeedCount ? (
                    <> and <strong>{previewResult.crossSeedCount}</strong> cross-seed{previewResult.crossSeedCount !== 1 ? "s" : ""}</>
                  ) : null}
                  {" "}to category <strong>"{previewInput?.exprCategory ?? formState.exprCategory}"</strong>.
                </p>
                <p className="text-muted-foreground text-sm">Confirming will save and enable this rule.</p>
              </>
            )
          ) : (
            <>
              <p>No torrents currently match this rule.</p>
              <p className="text-muted-foreground text-sm">Confirming will save this rule.</p>
            </>
          )
        }
        preview={previewResult}
        condition={previewInput?.actionCondition ?? formState.actionCondition}
        onConfirm={handleConfirmSave}
        onLoadMore={handleLoadMore}
        isLoadingMore={loadMorePreview.isPending}
        confirmLabel="Save Rule"
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

      <AlertDialog open={showDryRunPrompt} onOpenChange={setShowDryRunPrompt}>
        <AlertDialogContent className="sm:max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>Enable dry run?</AlertDialogTitle>
            <AlertDialogDescription>
              Dry run simulates all actions without changing anything. You can review affected torrents in the activity log.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                markDryRunPrompted()
                setShowDryRunPrompt(false)
                applyEnabledChange(true)
              }}
            >
              Enable without dry run
            </Button>
            <AlertDialogAction
              onClick={() => {
                markDryRunPrompted()
                setShowDryRunPrompt(false)
                applyEnabledChange(true, { forceDryRun: true })
              }}
            >
              Enable with dry run
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={showAddCustomGroup} onOpenChange={setShowAddCustomGroup}>
        <AlertDialogContent className="sm:max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>Add custom group</AlertDialogTitle>
            <AlertDialogDescription>
              Combine multiple fields to create a custom grouping strategy
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1">
              <Label htmlFor="group-id" className="text-sm">Group ID</Label>
              <Input
                id="group-id"
                value={newGroupId}
                onChange={(e) => setNewGroupId(e.target.value)}
                placeholder="e.g., my_custom_group"
                className="h-8 text-xs"
              />
              <p className="text-xs text-muted-foreground">Unique identifier for this group (alphanumeric, underscores)</p>
            </div>

            <div className="space-y-1">
              <Label className="text-sm">Keys (select at least one)</Label>
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
                Torrents with the same combination of these fields will be grouped together
              </p>
            </div>

            <div className="space-y-1">
              <Label className="text-sm">Ambiguous policy (advanced)</Label>
              <Select
                value={newGroupAmbiguousPolicy}
                onValueChange={(value) => setNewGroupAmbiguousPolicy(value as "verify_overlap" | "skip" | "")}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue placeholder="None" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">(None)</SelectItem>
                  <SelectItem value="verify_overlap">Verify overlap</SelectItem>
                  <SelectItem value="skip">Skip</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                When content path equals save path, how to handle ambiguous cases
              </p>
            </div>

            {newGroupAmbiguousPolicy === "verify_overlap" && (
              <div className="space-y-1">
                <Label htmlFor="min-overlap" className="text-sm">Min file overlap %</Label>
                <Input
                  id="min-overlap"
                  type="number"
                  value={newGroupMinOverlap}
                  onChange={(e) => setNewGroupMinOverlap(e.target.value)}
                  min="0"
                  max="100"
                  className="h-8 text-xs"
                />
                <p className="text-xs text-muted-foreground">Default 90%</p>
              </div>
            )}
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button
              type="button"
              onClick={() => {
                // Validate
                if (!newGroupId.trim()) {
                  toast.error("Group ID cannot be empty")
                  return
                }
                if (!/^[a-zA-Z0-9_]+$/.test(newGroupId)) {
                  toast.error("Group ID must contain only alphanumeric characters and underscores")
                  return
                }
                if (newGroupKeys.length === 0) {
                  toast.error("Select at least one key")
                  return
                }
                // Check for duplicates
                if ((formState.exprGrouping?.groups || []).some(g => g.id === newGroupId)) {
                  toast.error("A group with this ID already exists")
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
                toast.success("Custom group added")
              }}
            >
              Add group
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
