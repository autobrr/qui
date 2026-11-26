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
  SCORE_FIELDS,
  SORT_FIELDS,
  STATE_VALUE_REQUIREMENTS,
  getCapabilityReason,
  getDisabledFields,
  getRuleSkipReason,
  type Capabilities,
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
import { FieldHelp } from "@/components/ui/field-help"
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
  TooltipTrigger
} from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { useIndexerTrackerDomains } from "@/hooks/useIndexerTrackerDomains"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { useInstances } from "@/hooks/useInstances"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { buildTrackerCustomizationMaps, trackerGroupKey, useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { withBasePath } from "@/lib/base-url"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { type CsvColumn, downloadBlob, toCsv } from "@/lib/csv-export"
import { usableFreeSpaceSourceType } from "@/lib/free-space-source"
import { isMovePathServerError, isVisiblyRelativeMovePath } from "@/lib/move-path"
import { pickTrackerIconDomain } from "@/lib/tracker-icons"
import { ACTION_LABEL_KEYS, ACTION_RUN_ORDER, ALLOWANCE_DECIMAL_UNITS, allowanceInputOf, firstActionCondition, freeSpaceSourceOf, getSwitchedOffActions, getTrackerMatchMode, getTrackerTokens, hasMixedActionConditions, type ActionType, type SwitchedOffActions, type TrackerMatchMode } from "@/lib/workflow-utils"
import { cn, formatBytes, normalizeTrackerDomains } from "@/lib/utils"
import { BYTES_PER_UNIT, unitLabel } from "@/lib/unit-format"
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
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { AutomationActivityRunDialog } from "./AutomationActivityRunDialog"
import { RunWorkflowNowDialog } from "./RunWorkflowNowDialog"
import { DryRunToggle, WorkflowPreviewDialog } from "./WorkflowPreviewDialog"
import { EDITING, workflowSaveFlowReducer } from "@/lib/workflow-save-flow"
import { TransferForm } from "@/components/transfers/TransferForm"
import { emptyTransferForm, type TransferFormValue } from "@/components/transfers/transfer-form-value"

let ruleIdCounter = 0

interface WorkflowDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  rule: Automation | null
  onSuccess?: () => void
}

const SPEED_LIMIT_LADDER = ["KiB", "MiB"] as const

const getSpeedLimitUnits = () =>
  SPEED_LIMIT_LADDER.map((unit) => ({
    value: BYTES_PER_UNIT[unit] / BYTES_PER_UNIT.KiB,
    label: unitLabel(unit, true),
  }))

const CONTENT_LAYOUT_OPTIONS = [
  { value: "Original", labelKey: "preferences.workflowDialog.contentLayout.original" },
  { value: "Subfolder", labelKey: "preferences.workflowDialog.contentLayout.subfolder" },
  { value: "NoSubfolder", labelKey: "preferences.workflowDialog.contentLayout.noSubfolder" },
] as const

const CONTENT_LAYOUT_VALUES = CONTENT_LAYOUT_OPTIONS.map(o => o.value)

const COMBINABLE_ACTIONS: ActionType[] = ACTION_RUN_ORDER.filter(action => action !== "delete")

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
  transferred_to_instance: "preferences.workflowDialog.dryRun.actions.transferredToInstance",
  dry_run_no_match: "preferences.workflowDialog.dryRun.actions.noMatches",
  rule_skipped: "preferences.workflowDialog.dryRun.actions.ruleSkipped",
}

const DRY_RUN_ACTION_FALLBACK_KEY = "preferences.workflowDialog.dryRun.actionFallback"

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
    case "rule_skipped":
      return getRuleSkipReason(details, t)
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

function getDisabledStateValues(capabilities: Capabilities, t: ReturnType<typeof useTranslation<"instances">>["t"]): DisabledStateValue[] {
  return Object.entries(STATE_VALUE_REQUIREMENTS)
    .filter(([, capability]) => !capabilities[capability as keyof Capabilities])
    .map(([value, capability]) => ({ value, reason: getCapabilityReason(capability, t) }))
}

const SIMPLE_SORT_DISABLED_FIELDS = Object.keys(CONDITION_FIELDS)
  .filter(field => !SORT_FIELDS.has(field as ConditionField))

const SCORE_MULTIPLIER_DISABLED_FIELDS = Object.keys(CONDITION_FIELDS)
  .filter(field => !SCORE_FIELDS.has(field as ConditionField))

function isSupportedSimpleSortField(field: string): field is ConditionField {
  return SORT_FIELDS.has(field as ConditionField)
}

function isSupportedScoreMultiplierField(field: string): field is ConditionField {
  return SCORE_FIELDS.has(field as ConditionField)
}



function conditionUsesField(condition: RuleCondition | null | undefined, field: string): boolean {
  if (!condition) return false
  if (condition.field === field) return true
  if (condition.conditions) {
    return condition.conditions.some(c => conditionUsesField(c, field))
  }
  return false
}

function formFreeSpaceSource(state: Pick<FormState, "exprFreeSpaceSourceType" | "exprFreeSpaceSourcePath" | "exprFreeSpaceAllowance" | "exprFreeSpaceAllowanceUnit">) {
  return freeSpaceSourceOf({
    type: state.exprFreeSpaceSourceType,
    path: state.exprFreeSpaceSourcePath,
    allowance: state.exprFreeSpaceAllowance,
    allowanceUnit: state.exprFreeSpaceAllowanceUnit,
  })
}

function formUsesFreeSpace(state: Pick<FormState, "actionCondition" | "sortingType" | "simpleSortField" | "scoreRules">): boolean {
  if (conditionUsesField(state.actionCondition, "FREE_SPACE")) return true
  if (state.sortingType === "simple") return state.simpleSortField === "FREE_SPACE"
  return state.sortingType === "score" && state.scoreRules.some(rule =>
    rule.fieldMultiplier?.field === "FREE_SPACE" || conditionUsesField(rule.conditional?.condition, "FREE_SPACE"))
}

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

type SpeedLimitMode = "no_change" | "unlimited" | "custom"

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
  includeCrossSeeds: boolean
  deleteFromClient: boolean
  useTrackerAsTag: boolean
  useDisplayName: boolean
}

function createDefaultTagAction(): TagActionForm {
  return {
    tags: [],
    mode: "full",
    includeCrossSeeds: false,
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
  intervalSeconds: number | null
  actionCondition: RuleCondition | null
  exprGrouping?: GroupingConfig
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
  switchedOffActions: SwitchedOffActions
  exprUploadMode: SpeedLimitMode
  exprUploadValue?: number
  exprDownloadMode: SpeedLimitMode
  exprDownloadValue?: number
  exprRatioLimitMode: "no_change" | "global" | "unlimited" | "custom"
  exprRatioLimitValue?: number
  exprSeedingTimeMode: "no_change" | "global" | "unlimited" | "custom"
  exprSeedingTimeValue?: number
  exprShareLimitAction: string
  exprShareLimitsMode: string
  exprDeleteMode: "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds" | "deleteWithFilesIncludeCrossSeeds"
  exprIncludeHardlinks: boolean
  exprDeleteGroupId: string
  exprDeleteAtomic: "all" | ""
  exprFreeSpaceSourceType: "qbittorrent" | "path" | "qbitPath" | "allowance"
  exprFreeSpaceSourcePath: string
  exprFreeSpaceAllowance: string
  exprFreeSpaceAllowanceUnit: number
  exprTagActions: TagActionForm[]
  exprCategory: string | undefined
  exprIncludeCrossSeeds: boolean
  exprCategoryGroupId: string
  exprBlockIfCrossSeedInCategories: string[]
  sortingType: "default" | "simple" | "score"
  simpleSortField: ConditionField
  sortDirection: "ASC" | "DESC"
  scoreRules: FormScoreRule[]
  exprMovePath: string
  exprMoveBlockIfCrossSeed: boolean
  exprMoveGroupId: string
  exprMoveAtomic: "all" | ""
  exprExternalProgramId: number | null
  exportToInstanceEnabled: boolean
  exprExportTargetInstanceId: number | null
  exprExportSavePath: string
  exprExportCategory: string
  exprExportTags: string
  exprExportPaused: boolean
  exprExportSkipChecking: boolean
  exprExportContentLayout: "" | "Original" | "Subfolder" | "NoSubfolder"
  transferToInstanceEnabled: boolean
  exprTransfer: TransferFormValue
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
  switchedOffActions: {},
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
  exprFreeSpaceAllowance: "",
  exprFreeSpaceAllowanceUnit: BYTES_PER_UNIT.GiB,
  exprTagActions: [createDefaultTagAction()],
  exprCategory: undefined,
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
  transferToInstanceEnabled: false,
  exprTransfer: emptyTransferForm,
}

function getEnabledActions(state: FormState): ActionType[] {
  return ACTION_RUN_ORDER.filter(action => state[`${action}Enabled`])
}

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

const PATH_TEMPLATE_EXAMPLE = "/data/{{ .Category }}"

const MOVE_PATH_DOCS_URL = "https://getqui.com/docs/features/automations/#move-path-templates"
const EXPORT_PATH_DOCS_URL = "https://getqui.com/docs/features/automations/#save-path-templates"

export function WorkflowDialog({ open, onOpenChange, instanceId, rule, onSuccess }: WorkflowDialogProps) {
  const { t } = useTranslation("instances")
  const queryClient = useQueryClient()
  const scoreMultiplierDisabledFields = useMemo(() => {
    const reason = t("preferences.workflowDialog.priority.notSupportedForScoreMultiplier")
    return SCORE_MULTIPLIER_DISABLED_FIELDS.map(field => ({ field, reason }))
  }, [t])
  const [formState, setFormState] = useState<FormState>(emptyFormState)
  const mixedActionConditions = useMemo(() => rule != null && hasMixedActionConditions(rule), [rule])
  const loadedForm = useRef<FormState | null>(null)
  const currentForm = useRef(formState)
  const saveCheckRun = useRef(0)
  const [saveFlow, dispatchSaveFlow] = useReducer(workflowSaveFlowReducer, EDITING)
  const preview = saveFlow.step === "previewing" ? saveFlow.preview : null
  const previewResult = preview?.status === "loaded" ? preview.result : null
  const previewRequestRef = useRef(0)
  const [livePreviewResult, setLivePreviewResult] = useState<AutomationPreviewResult | null>(null)
  const [isLivePreviewLoading, setIsLivePreviewLoading] = useState(false)
  const [livePreviewError, setLivePreviewError] = useState<string | null>(null)
  const [showRunNowDialog, setShowRunNowDialog] = useState(false)
  const [latestDryRunEvents, setLatestDryRunEvents] = useState<AutomationActivity[]>([])
  const [latestDryRunError, setLatestDryRunError] = useState<string | null>(null)
  const [latestDryRunStartedAt, setLatestDryRunStartedAt] = useState<string | null>(null)
  const [activityRunDialog, setActivityRunDialog] = useState<AutomationActivity | null>(null)
  const [previewView, setPreviewView] = useState<PreviewView>("needed")
  const [isLoadingPreviewView, setIsLoadingPreviewView] = useState(false)
  const [isExporting, setIsExporting] = useState(false)
  const [uploadSpeedUnit, setUploadSpeedUnit] = useState(1024)
  const [downloadSpeedUnit, setDownloadSpeedUnit] = useState(1024)
  const [regexErrors, setRegexErrors] = useState<RegexValidationError[]>([])
  const [freeSpaceSourcePathError, setFreeSpaceSourcePathError] = useState<string | null>(null)
  const [movePathServerError, setMovePathServerError] = useState<string | null>(null)
  const [movePathDryRunAttempted, setMovePathDryRunAttempted] = useState(false)
  const movePathInputRef = useRef<HTMLInputElement>(null)
  const [showAddCustomGroup, setShowAddCustomGroup] = useState(false)
  const [newGroupId, setNewGroupId] = useState("")
  const [newGroupKeys, setNewGroupKeys] = useState<string[]>([])
  const [newGroupAmbiguousPolicy, setNewGroupAmbiguousPolicy] = useState<"verify_overlap" | "skip" | "">("")
  const [newGroupMinOverlap, setNewGroupMinOverlap] = useState("90")
  const previewPageSize = 25
  const livePreviewPageSize = 5
  const livePreviewRequestRef = useRef(0)
  const isHydrating = useRef(true)

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

  const supportsFreeSpaceAtPath = capabilities?.supportsFreeSpaceAtPath
  const supportsPathAutocomplete = capabilities?.supportsPathAutocomplete ?? false
  const ruleInstance = useMemo(() => instances?.find(i => i.id === instanceId), [instances, instanceId])
  const hasLocalFilesystemAccess = ruleInstance?.hasLocalFilesystemAccess ?? false
  const hasTrustedIdentity = ruleInstance?.capabilities.trustedIdentity ?? false
  const isRemoteInstance = ruleInstance?.filesystemMode === "remote"
  const pathSourceWindowsBlocked = hasLocalFilesystemAccess && capabilities?.supportsFreeSpacePathSource === false
  const canRead = ruleInstance?.capabilities.read ?? false
  const pathSourceAvailable = canRead && !pathSourceWindowsBlocked
  const allowanceSourceAvailable = (ruleInstance?.capabilities.identity ?? false) && !pathSourceWindowsBlocked

  const fieldCapabilities = useMemo<Capabilities>(
    () => ({
      supportsLiveReannounce: capabilities?.supportsLiveReannounce ?? false,
      supportsListFieldsV5: capabilities?.supportsListFieldsV5 ?? false,
      supportsTrackerHealth: capabilities?.supportsTrackerHealth ?? false,
      supportsListCreatedBy: capabilities?.supportsListCreatedBy ?? false,
      read: canRead,
      trustedIdentity: hasTrustedIdentity,
    }),
    [capabilities, canRead, hasTrustedIdentity]
  )
  const simpleSortDisabledFields = useMemo(() => {
    const reason = t("preferences.workflowDialog.priority.notSupportedForSimpleSort")
    return [
      ...SIMPLE_SORT_DISABLED_FIELDS.map(field => ({ field, reason })),
      ...getDisabledFields(fieldCapabilities, isRemoteInstance, t),
    ]
  }, [t, fieldCapabilities, isRemoteInstance])

  const handleFreeSpacePathSelect = useCallback((path: string) => {
    setFormState(prev => ({ ...prev, exprFreeSpaceSourcePath: path }))
    setFreeSpaceSourcePathError(null)
  }, [])

  const {
    suggestions: freeSpaceSuggestions,
    handleInputChange: handleFreeSpacePathInputChange,
    handleSelect: handleFreeSpacePathSelectSuggestion,
    handleKeyDown: handleFreeSpacePathKeyDown,
    highlightedIndex: freeSpaceHighlightedIndex,
    showSuggestions: showFreeSpaceSuggestions,
    inputRef: freeSpacePathInputRef,
    listRef: freeSpaceListRef,
  } = usePathAutocomplete(handleFreeSpacePathSelect, instanceId)

  const pathTemplateHelp = (docsUrl: string) => (
    <>
      {t("preferences.workflowDialog.templateHelp.absolutePath")}{" "}
      <code className="whitespace-nowrap">{PATH_TEMPLATE_EXAMPLE}</code>
      <br />
      {t("preferences.workflowDialog.templateHelp.intro")}{" "}
      <a href={docsUrl} target="_blank" rel="noopener noreferrer" className="underline underline-offset-2">
        {t("preferences.workflowDialog.templateHelp.learnMore")}
      </a>
    </>
  )

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

  const categoryOptions = useMemo(() => {
    if (!metadata?.categories) return []
    const selected = [formState.exprCategory, ...formState.exprBlockIfCrossSeedInCategories].filter((category): category is string => !!category)
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

  const trackerOptions: Option[] = useMemo(() => {
    type TrackerOption = Option & { isCustom: boolean }
    const { domainToCustomization } = trackerCustomizationMaps
    const trackers = trackersQuery.data ? Object.keys(trackersQuery.data) : []
    const processed: TrackerOption[] = []
    const seenGroups = new Set<string>()
    const seenValues = new Set<string>()

    const addTracker = (tracker: string) => {
      const groupKey = trackerGroupKey(tracker, domainToCustomization)
      const customization = domainToCustomization.get(tracker.toLowerCase())

      if (customization) {
        const mergedValue = customization.domains.join(",")
        if (seenGroups.has(groupKey) || seenValues.has(mergedValue)) return
        seenGroups.add(groupKey)
        seenValues.add(mergedValue)

        const iconDomain = pickTrackerIconDomain(trackerIcons, customization.domains)
        processed.push({
          label: t("preferences.workflowDialog.customTrackerLabel", { name: customization.displayName }),
          value: mergedValue,
          icon: <TrackerIconImage tracker={iconDomain} trackerIcons={trackerIcons} />,
          isCustom: true,
        })
      } else {
        if (seenGroups.has(groupKey) || seenValues.has(tracker)) return
        seenGroups.add(groupKey)
        seenValues.add(tracker)

        processed.push({
          label: tracker,
          value: tracker,
          icon: <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />,
          isCustom: false,
        })
      }
    }

    for (const tracker of trackers) {
      addTracker(tracker)
    }

    for (const domain of indexerTrackerDomainsQuery.data ?? []) {
      addTracker(domain)
    }

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
    if (formState.exprExportCategory && !cats.includes(formState.exprExportCategory)) {
      cats.push(formState.exprExportCategory)
    }
    return cats.sort()
  }, [targetMetadata, formState.exprExportCategory])

  useEffect(() => {
    let cancelled = false

    if (open) {
      if (rule) {
        const isAllTrackers = rule.trackerPattern === "*"
        const trackerTokens = isAllTrackers ? [] : getTrackerTokens(rule)
        const rawDomains = trackerTokens.map(stripTrackerNegation).filter(Boolean)
        const mappedDomains = mapDomainsToOptionValues(rawDomains)
        const trackerMatchMode = isAllTrackers ? "include" : getTrackerMatchMode(trackerTokens)

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
        let exprFreeSpaceAllowance = ""
        let exprFreeSpaceAllowanceUnit: number = BYTES_PER_UNIT.GiB
        let exprTagActions: TagActionForm[] = [createDefaultTagAction()]
        let exprCategory: string | undefined
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
        let transferToInstanceEnabled = false
        let exprTransfer = emptyTransferForm
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

        if (rule.freeSpaceSource) {
          exprFreeSpaceSourceType = rule.freeSpaceSource.type ?? "qbittorrent"
          if (rule.freeSpaceSource.type !== "qbittorrent") {
            exprFreeSpaceSourcePath = rule.freeSpaceSource.path ?? ""
          }
          if (rule.freeSpaceSource.type === "allowance") {
            const input = allowanceInputOf(rule.freeSpaceSource.allowanceBytes)
            exprFreeSpaceAllowance = input.allowance
            exprFreeSpaceAllowanceUnit = input.allowanceUnit
          }
        }

        if (conditions) {
          exprGrouping = conditions.grouping
          actionCondition = firstActionCondition(rule)

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
              includeCrossSeeds: action.includeCrossSeeds ?? false,
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
          const transfer = conditions.transferToInstance
          if (transfer?.enabled) {
            transferToInstanceEnabled = true
            exprTransfer = {
              targetInstanceId: transfer.targetInstanceId ?? null,
              savePath: transfer.savePath ?? "",
              category: transfer.category ?? "",
              tags: (transfer.tags ?? []).join(", "),
              after: transfer.after ?? "removeWithFiles",
              leaveStopped: transfer.leaveStopped ?? false,
              way: transfer.way ?? "auto",
            }
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
          switchedOffActions: getSwitchedOffActions(rule),
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
          exprFreeSpaceAllowance,
          exprFreeSpaceAllowanceUnit,
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
          transferToInstanceEnabled,
          exprTransfer,
        }
        setFormState(newState)
        loadedForm.current = newState
      } else {
        setFormState(emptyFormState)
      }
      queueMicrotask(() => {
        if (!cancelled) {
          isHydrating.current = false
        }
      })
    } else {
      isHydrating.current = true
    }

    return () => { cancelled = true }
  }, [open, rule, mapDomainsToOptionValues])

  useEffect(() => {
    currentForm.current = formState
  }, [formState])

  useEffect(() => {
    if (!open) {
      saveCheckRun.current += 1
      dispatchSaveFlow({ type: "closed" })
      setLatestDryRunEvents([])
      setLatestDryRunError(null)
      setLatestDryRunStartedAt(null)
      setActivityRunDialog(null)
      setMovePathServerError(null)
      setMovePathDryRunAttempted(false)
    }
  }, [open])

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

  useEffect(() => {
    if (isHydrating.current) return
    if (formState.deleteEnabled && formState.intervalSeconds === 60) {
      if (conditionUsesField(formState.actionCondition, "FREE_SPACE")) {
        setFormState(prev => ({ ...prev, intervalSeconds: 300 }))
        toast.info(t("preferences.workflowDialog.toast.switchedIntervalForFreeSpace"))
      }
    }
  }, [formState.actionCondition, formState.deleteEnabled, formState.intervalSeconds, t])

  useEffect(() => {
    if (pathSourceWindowsBlocked && (formState.exprFreeSpaceSourceType === "path" || formState.exprFreeSpaceSourceType === "allowance")) {
      setFormState(prev => ({ ...prev, exprFreeSpaceSourceType: "qbittorrent" }))
      if (!isHydrating.current) {
        toast.warning(t("preferences.workflowDialog.toast.pathSourceUnsupportedWindows"))
      }
    }
  }, [pathSourceWindowsBlocked, formState.exprFreeSpaceSourceType, t])

  useEffect(() => {
    const usable = usableFreeSpaceSourceType(formState.exprFreeSpaceSourceType, supportsFreeSpaceAtPath)
    if (usable !== formState.exprFreeSpaceSourceType) {
      setFormState(prev => ({ ...prev, exprFreeSpaceSourceType: usable }))
      if (!isHydrating.current) {
        toast.warning(t("preferences.workflowDialog.toast.qbitPathSourceUnsupported"))
      }
    }
  }, [supportsFreeSpaceAtPath, formState.exprFreeSpaceSourceType, t])

  const validateFreeSpaceSource = useCallback((state: FormState): boolean => {
    const usesFreeSpace = formUsesFreeSpace(state)
    if (!usesFreeSpace || state.exprFreeSpaceSourceType === "qbittorrent") {
      setFreeSpaceSourcePathError(null)
      return true
    }

    if (state.exprFreeSpaceSourceType === "qbitPath") {
      if (supportsFreeSpaceAtPath === false) {
        setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.qbitPathUnsupported"))
        toast.error(t("preferences.workflowDialog.toast.switchFreeSpaceSourceDefault"))
        return false
      }
      if (state.exprFreeSpaceSourcePath.trim() === "") {
        setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.pathRequired"))
        toast.error(t("preferences.workflowDialog.toast.enterPathOrDefault"))
        return false
      }
      setFreeSpaceSourcePathError(null)
      return true
    }
    const allowance = state.exprFreeSpaceSourceType === "allowance"

    if (pathSourceWindowsBlocked) {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.unsupportedWindows"))
      toast.error(t("preferences.workflowDialog.toast.switchFreeSpaceSourceDefault"))
      return false
    }
    if (allowance && !allowanceSourceAvailable) {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.allowanceAccessRequired"))
      toast.error(t("preferences.workflowDialog.toast.switchFreeSpaceSourceDefault"))
      return false
    }
    if (!allowance && !pathSourceAvailable) {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.filesystemAccessRequired"))
      toast.error(t("preferences.workflowDialog.toast.setUpFilesystemAccessOrDefault"))
      return false
    }

    const trimmedPath = state.exprFreeSpaceSourcePath.trim()
    if (trimmedPath === "") {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.pathRequired"))
      toast.error(t("preferences.workflowDialog.toast.enterPathOrDefault"))
      return false
    }
    if (allowance && !formFreeSpaceSource(state)) {
      setFreeSpaceSourcePathError(t("preferences.workflowDialog.freeSpace.errors.allowanceRequired"))
      toast.error(t("preferences.workflowDialog.freeSpace.errors.allowanceRequired"))
      return false
    }

    setFreeSpaceSourcePathError(null)
    return true
  }, [allowanceSourceAvailable, pathSourceAvailable, pathSourceWindowsBlocked, supportsFreeSpaceAtPath, t])

  const validateCategory = useCallback((state: FormState): boolean => {
    if (state.categoryEnabled && state.exprCategory === undefined) {
      toast.error(t("preferences.workflowDialog.toast.selectCategory"))
      return false
    }
    return true
  }, [t])

  const validateTags = useCallback((state: FormState): boolean => {
    if (!state.tagEnabled) return true
    const validationError = validateTagActions(state.exprTagActions, t)
    if (validationError) {
      toast.error(validationError)
      return false
    }
    return true
  }, [t])

  const hasValidFreeSpaceSourceForLivePreview = useCallback((state: FormState): boolean => {
    const usesFreeSpace = formUsesFreeSpace(state)
    if (!usesFreeSpace || state.exprFreeSpaceSourceType === "qbittorrent") {
      return true
    }
    if (state.exprFreeSpaceSourceType === "allowance") {
      return allowanceSourceAvailable && formFreeSpaceSource(state) !== undefined
    }
    if (state.exprFreeSpaceSourceType === "qbitPath") {
      return supportsFreeSpaceAtPath !== false && state.exprFreeSpaceSourcePath.trim() !== ""
    }
    if (!pathSourceAvailable) {
      return false
    }
    return state.exprFreeSpaceSourcePath.trim() !== ""
  }, [allowanceSourceAvailable, pathSourceAvailable, supportsFreeSpaceAtPath])

  const buildPayload = useCallback((input: FormState): AutomationInput => {
    const { tags: switchedOffTags = [], ...switchedOffActions } = input.switchedOffActions
    const conditions: ActionConditions = { schemaVersion: "1", ...switchedOffActions }
    if (input.exprGrouping) {
      conditions.grouping = input.exprGrouping
    }

    if (input.speedLimitsEnabled) {
      let uploadKiB: number | undefined
      if (input.exprUploadMode === "unlimited") {
        uploadKiB = 0
      } else if (input.exprUploadMode === "custom" && input.exprUploadValue !== undefined) {
        uploadKiB = input.exprUploadValue
      }

      let downloadKiB: number | undefined
      if (input.exprDownloadMode === "unlimited") {
        downloadKiB = 0
      } else if (input.exprDownloadMode === "custom" && input.exprDownloadValue !== undefined) {
        downloadKiB = input.exprDownloadValue
      }

      conditions.speedLimits = {
        enabled: true,
        uploadKiB,
        downloadKiB,
        condition: input.actionCondition ?? undefined,
      }
    }
    if (input.shareLimitsEnabled) {
      let ratioLimit: number | undefined
      if (input.exprRatioLimitMode === "global") {
        ratioLimit = -2
      } else if (input.exprRatioLimitMode === "unlimited") {
        ratioLimit = -1
      } else if (input.exprRatioLimitMode === "custom" && input.exprRatioLimitValue !== undefined) {
        ratioLimit = Math.round(input.exprRatioLimitValue * 100) / 100
      }

      let seedingTimeMinutes: number | undefined
      if (input.exprSeedingTimeMode === "global") {
        seedingTimeMinutes = -2
      } else if (input.exprSeedingTimeMode === "unlimited") {
        seedingTimeMinutes = -1
      } else if (input.exprSeedingTimeMode === "custom" && input.exprSeedingTimeValue !== undefined) {
        seedingTimeMinutes = input.exprSeedingTimeValue
      }

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
        includeHardlinks: input.exprDeleteMode === "deleteWithFilesIncludeCrossSeeds" ? input.exprIncludeHardlinks : undefined,
        groupId: input.exprDeleteGroupId || undefined,
        atomic: input.exprDeleteAtomic || undefined,
        condition: input.actionCondition ?? undefined,
      }
    }
    const formTagActions = !input.tagEnabled ? [] : input.exprTagActions
      .filter((action) => action.useTrackerAsTag || action.tags.length > 0)
      .map((action) => ({
        enabled: true,
        tags: action.tags,
        mode: action.mode,
        includeCrossSeeds: action.includeCrossSeeds,
        deleteFromClient: action.deleteFromClient,
        useTrackerAsTag: action.useTrackerAsTag,
        useDisplayName: action.useDisplayName,
        condition: input.actionCondition ?? undefined,
      }))
    const tagActions = input.deleteEnabled ? formTagActions : [...formTagActions, ...switchedOffTags]
    if (tagActions.length > 0) {
      conditions.tags = tagActions
      conditions.tag = tagActions[0]
    }
    if (input.categoryEnabled && input.exprCategory !== undefined) {
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
    if (input.transferToInstanceEnabled) {
      if (!input.exprTransfer.targetInstanceId) {
        throw new Error("Transfer to instance requires a target instance")
      }
      const transfer = input.exprTransfer
      const tags = transfer.tags.split(",").map(t => t.trim()).filter(Boolean)
      conditions.transferToInstance = {
        enabled: true,
        targetInstanceId: input.exprTransfer.targetInstanceId,
        savePath: transfer.savePath.trim(),
        category: transfer.category || undefined,
        tags: tags.length > 0 ? tags : undefined,
        after: transfer.after,
        leaveStopped: transfer.leaveStopped || undefined,
        way: transfer.way,
        condition: input.actionCondition ?? undefined,
      }
    }

    const freeSpaceSource = formFreeSpaceSource(input)

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

  const isDeleteRule = formState.deleteEnabled
  const isCategoryRule = formState.categoryEnabled
  const isTagRule = formState.tagEnabled && !formState.deleteEnabled && !formState.categoryEnabled
  const previewCategory = formState.exprCategory || t("preferences.workflowDialog.uncategorized")

  const conditionUsesFreeSpace = useMemo(() => {
    return conditionUsesField(formState.actionCondition, "FREE_SPACE")
  }, [formState.actionCondition])
  const deleteUsesFreeSpace = formState.deleteEnabled && conditionUsesFreeSpace
  const ruleUsesFreeSpace = formUsesFreeSpace(formState)
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
    formState.transferToInstanceEnabled,
  ].filter(Boolean).length

  const latestDryRunOperationCount = useMemo(
    () => latestDryRunEvents.reduce((sum, event) => sum + getDryRunImpactCount(event), 0),
    [latestDryRunEvents]
  )

  const previewMutation = useMutation({
    mutationFn: async ({ input, view }: { input: FormState; view: PreviewView; requestId: number }) => {
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
    onSuccess: (result, { requestId }) => {
      if (requestId !== previewRequestRef.current) return
      dispatchSaveFlow({ type: "previewLoaded", result })
    },
    onError: (error, { requestId }) => {
      if (requestId !== previewRequestRef.current) return
      dispatchSaveFlow({ type: "previewFailed", error: error instanceof Error ? error.message : t("preferences.workflowDialog.toast.previewFailed") })
    },
  })

  const startPreview = useCallback((input: FormState) => {
    setPreviewView("needed")
    dispatchSaveFlow({ type: "previewStarted" })
    previewRequestRef.current += 1
    previewMutation.mutate({ input, view: "needed", requestId: previewRequestRef.current })
  }, [previewMutation])

  const loadMorePreview = useMutation({
    mutationFn: async () => {
      if (!previewResult) {
        throw new Error("Preview data not available")
      }
      const payload = {
        ...buildPayload(formState),
        previewLimit: previewPageSize,
        previewOffset: previewResult.examples.length,
        previewView: previewView,
      }
      return api.previewAutomation(instanceId, payload)
    },
    onSuccess: (result) => {
      dispatchSaveFlow({ type: "morePreviewLoaded", result })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.loadMoreFailed"))
    },
  })

  const handleLoadMore = () => {
    if (!previewResult) {
      return
    }
    loadMorePreview.mutate()
  }

  const movePathClientError = isVisiblyRelativeMovePath(formState.exprMovePath)? t("preferences.workflowDialog.move.errors.mustBeAbsolute"): null
  const movePathCheckError = formState.moveEnabled ? movePathClientError ?? movePathServerError : null
  const movePathError = formState.enabled || movePathDryRunAttempted ? movePathCheckError : null
  const movePathBlocksSave = formState.enabled && Boolean(movePathCheckError)

  const showMovePathServerError = (error: unknown): boolean => {
    if (!(error instanceof Error) || !isMovePathServerError(error.message)) {
      return false
    }
    setMovePathServerError(error.message)
    movePathInputRef.current?.focus()
    return true
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
      if (showMovePathServerError(error)) {
        setMovePathDryRunAttempted(true)
        return
      }
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.dryRunFailed"))
    },
  })

  const showLatestDryRunPanel = dryRunNowMutation.isPending ||
    latestDryRunEvents.length > 0 ||
    latestDryRunError !== null ||
    latestDryRunStartedAt !== null

  const livePreviewPayloadKey = useMemo(() => {
    if (!open) return ""
    if (!(isDeleteRule || isCategoryRule || isTagRule)) return ""
    if (isDeleteRule && !formState.actionCondition) return ""
    if (isCategoryRule && formState.exprCategory === undefined) return ""
    if (!formState.applyToAllTrackers && normalizeTrackerDomains(formState.trackerDomains).length === 0) return ""
    if (!hasValidFreeSpaceSourceForLivePreview(formState)) return ""

    try {
      return JSON.stringify({
        ...buildPayload(formState),
        enabled: false,
        dryRun: false,
        previewLimit: livePreviewPageSize,
        previewOffset: 0,
        previewView: "needed" as PreviewView,
      })
    } catch {
      return ""
    }
  }, [
    buildPayload,
    formState,
    hasValidFreeSpaceSourceForLivePreview,
    isCategoryRule,
    isDeleteRule,
    isTagRule,
    livePreviewPageSize,
    open,
  ])

  useEffect(() => {
    if (!livePreviewPayloadKey) {
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
        const result = await api.previewAutomation(instanceId, JSON.parse(livePreviewPayloadKey))
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
  }, [instanceId, livePreviewPayloadKey, t])

  const handleRunDryRunNow = () => {
    const dryRunInput: FormState = { ...formState }

    if (!validateCategory(dryRunInput)) return
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
    if (dryRunInput.moveEnabled && isVisiblyRelativeMovePath(dryRunInput.exprMovePath)) {
      setMovePathDryRunAttempted(true)
      movePathInputRef.current?.focus()
      return
    }
    if (dryRunInput.externalProgramEnabled && !dryRunInput.exprExternalProgramId) {
      toast.error(t("preferences.workflowDialog.toast.selectExternalProgram"))
      return
    }
    if (!validateExportTarget(dryRunInput)) {
      return
    }
    if (!validateTags(dryRunInput)) return

    setLatestDryRunStartedAt(new Date().toISOString())
    setLatestDryRunEvents([])
    setLatestDryRunError(null)
    setActivityRunDialog(null)
    dryRunNowMutation.mutate(dryRunInput)
  }

  const validateExportTarget = useCallback((state: FormState): boolean => {
    if (state.transferToInstanceEnabled) {
      const target = state.exprTransfer.targetInstanceId
      if (!target) {
        toast.error(t("preferences.workflowDialog.toast.selectTargetInstance"))
        return false
      }
      if (nonSelfInstances && !nonSelfInstances.some(i => i.id === target)) {
        toast.error(t("preferences.workflowDialog.toast.targetInstanceMissing"))
        setFormState(prev => ({ ...prev, exprTransfer: { ...prev.exprTransfer, targetInstanceId: null } }))
        return false
      }
    }
    if (!state.exportToInstanceEnabled) return true
    if (!state.exprExportTargetInstanceId) {
      toast.error(t("preferences.workflowDialog.toast.selectTargetInstance"))
      return false
    }
    if (nonSelfInstances && !nonSelfInstances.some(i => i.id === state.exprExportTargetInstanceId)) {
      toast.error(t("preferences.workflowDialog.toast.targetInstanceMissing"))
      setFormState(prev => ({ ...prev, exprExportTargetInstanceId: null }))
      return false
    }
    return true
  }, [nonSelfInstances, t])

  const handlePreviewViewChange = async (newView: PreviewView) => {
    if (!previewResult) return
    setPreviewView(newView)
    setIsLoadingPreviewView(true)
    try {
      const payload = {
        ...buildPayload(formState),
        previewLimit: previewPageSize,
        previewOffset: 0,
        previewView: newView,
      }
      const result = await api.previewAutomation(instanceId, payload)
      dispatchSaveFlow({ type: "previewLoaded", result })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.switchViewFailed"))
    } finally {
      setIsLoadingPreviewView(false)
    }
  }

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
    if (!previewResult) return

    setIsExporting(true)
    try {
      const pageSize = 500
      const allItems: AutomationPreviewTorrent[] = []
      let offset = 0
      const total = previewResult.totalMatches

      while (allItems.length < total) {
        const payload = {
          ...buildPayload(formState),
          previewLimit: pageSize,
          previewOffset: offset,
          previewView,
        }
        const result = await api.previewAutomation(instanceId, payload)
        allItems.push(...result.examples)
        offset += pageSize
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
      onOpenChange(false)
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
      onSuccess?.()
    },
    onError: (error) => {
      if (showMovePathServerError(error)) {
        return
      }
      toast.error(error instanceof Error ? error.message : t("preferences.workflowDialog.toast.saveFailed"))
    },
  })

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    setRegexErrors([])

    const submitState: FormState = { ...formState }

    if (rule && JSON.stringify(submitState) === JSON.stringify(loadedForm.current)) {
      onOpenChange(false)
      return
    }

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

    if (enabledActionsCount === 0) {
      toast.error(t("preferences.workflowDialog.toast.enableAction"))
      return
    }

    if (submitState.sortingType === "score") {
      if (submitState.scoreRules.length === 0) {
        toast.error(t("preferences.workflowDialog.toast.addScoreRule"))
        return
      }

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

    if (submitState.speedLimitsEnabled) {
      const uploadIsSet = submitState.exprUploadMode !== "no_change" &&
        (submitState.exprUploadMode !== "custom" || (submitState.exprUploadValue !== undefined && submitState.exprUploadValue > 0))
      const downloadIsSet = submitState.exprDownloadMode !== "no_change" &&
        (submitState.exprDownloadMode !== "custom" || (submitState.exprDownloadValue !== undefined && submitState.exprDownloadValue > 0))
      if (!uploadIsSet && !downloadIsSet) {
        toast.error(t("preferences.workflowDialog.toast.setSpeedLimit"))
        return
      }
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
      const ratioIsSet = submitState.exprRatioLimitMode !== "no_change" &&
        (submitState.exprRatioLimitMode !== "custom" || submitState.exprRatioLimitValue !== undefined)
      const seedingTimeIsSet = submitState.exprSeedingTimeMode !== "no_change" &&
        (submitState.exprSeedingTimeMode !== "custom" || submitState.exprSeedingTimeValue !== undefined)
      if (!ratioIsSet && !seedingTimeIsSet) {
        toast.error(t("preferences.workflowDialog.toast.setShareLimit"))
        return
      }
    }
    if (!validateTags(submitState)) return
    if (!validateCategory(submitState)) return
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
    if (submitState.enabled && submitState.moveEnabled && isVisiblyRelativeMovePath(submitState.exprMovePath)) {
      movePathInputRef.current?.focus()
      return
    }

    const payload = buildPayload(submitState)
    if (submitState.enabled) {
      saveCheckRun.current += 1
      const checkRun = saveCheckRun.current
      const validation = await api.validateAutomationRegex(instanceId, payload).catch(() => null)
      if (validation && !validation.valid && validation.errors.length > 0) {
        setRegexErrors(validation.errors)
        toast.error(t("preferences.workflowDialog.toast.invalidRegexPatternUnsupported"))
        return
      }
      if (checkRun !== saveCheckRun.current || JSON.stringify(currentForm.current) !== JSON.stringify(submitState)) return
    }

    if (!submitState.enabled) {
      createOrUpdate.mutate(submitState)
    } else if (isDeleteRule || isCategoryRule || isTagRule) {
      startPreview(submitState)
    } else {
      dispatchSaveFlow({ type: "dryRunAsked" })
    }
  }

  const setDryRun = (dryRun: boolean) => setFormState(prev => ({ ...prev, dryRun }))

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-4xl lg:max-w-5xl max-h-[90dvh] flex flex-col p-2 sm:p-6">
          <div ref={dropdownContainerRef} className="absolute inset-0 pointer-events-none overflow-visible" style={{ zIndex: 100 }}>
          </div>
          <DialogHeader>
            <DialogTitle>{rule ? t("preferences.workflowDialog.editWorkflow") : t("preferences.workflowDialog.addWorkflow")}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
            <div className="flex-1 overflow-y-auto space-y-3 sm:pr-1">
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

              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Label>{t("preferences.workflowDialog.priority.title")}</Label>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-4 w-4 text-muted-foreground hover:text-foreground cursor-help transition-colors" />
                      </TooltipTrigger>
                      <TooltipContent side="right" className="max-w-[300px]">
                        <p>{t("preferences.workflowDialog.priority.description")}</p>
                      </TooltipContent>
                    </Tooltip>
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
                      disabledFields={simpleSortDisabledFields}
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
                          <Badge variant="secondary" className="uppercase tracking-wider font-mono">
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
                              disabledFields={scoreMultiplierDisabledFields}
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
                              disabledFields={getDisabledFields(fieldCapabilities, isRemoteInstance, t)}
                              disabledStateValues={getDisabledStateValues(fieldCapabilities, t)}
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

              <div className="space-y-3">
                <div className="space-y-1.5">
                  <Label>{t("preferences.workflowDialog.conditionsLabel")}</Label>
                  <QueryBuilder
                    condition={formState.actionCondition}
                    onChange={(condition) => {
                      setFormState(prev => ({ ...prev, actionCondition: condition }))
                      setRegexErrors([])
                    }}
                    allowEmpty
                    categoryOptions={categoryOptions}
                    disabledFields={getDisabledFields(fieldCapabilities, isRemoteInstance, t)}
                    disabledStateValues={getDisabledStateValues(fieldCapabilities, t)}
                    groupOptions={groupedConditionOptions}
                  />
                  {mixedActionConditions && (
                    <div className="rounded-md border border-warning/50 bg-warning/10 p-3 text-sm">
                      <p>{t("preferences.workflowDialog.mixedActionConditions")}</p>
                    </div>
                  )}
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

                  {(isDeleteRule || isCategoryRule || isTagRule) && (
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
                      ) : livePreviewResult.skipped ? (
                        <p className="text-xs text-warning">{getRuleSkipReason(livePreviewResult.skipped, t)}</p>
                      ) : (
                        <>
                          <p className="text-xs text-muted-foreground">
                            {(isCategoryRule || isTagRule) ? t("preferences.workflowDialog.torrentsImpactedWithCrossSeeds", { total: livePreviewResult.totalMatches, direct: (livePreviewResult.totalMatches) - (livePreviewResult.crossSeedCount ?? 0), crossSeeds: livePreviewResult.crossSeedCount ?? 0 }) : t("preferences.workflowDialog.torrentsImpacted", { total: livePreviewResult.totalMatches })}
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

                {(conditionUsesField(formState.actionCondition, "GROUP_SIZE") || conditionUsesField(formState.actionCondition, "IS_GROUPED")) && (
                  <div className="rounded-lg border p-3 space-y-3 bg-muted/30">
                    <div className="flex items-center gap-2">
                      <Label className="text-sm font-medium">{t("preferences.workflowDialog.grouping.title")}</Label>
                      <Tooltip>
                        <TooltipTrigger asChild openOnTap>
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

                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <Label>
                      {t("preferences.workflowDialog.actions.title")}
                      <FieldHelp>{t("preferences.workflowDialog.actions.runOrderHelp")}</FieldHelp>
                    </Label>
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

                  {enabledActionsCount === 0 && (
                    <Select
                      value=""
                      onValueChange={(action: ActionType) => {
                        if (action === "delete") {
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
                            transferToInstanceEnabled: false,
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
                        {COMBINABLE_ACTIONS.map(action => (
                          <SelectItem key={action} value={action}>{t(ACTION_LABEL_KEYS[action])}</SelectItem>
                        ))}
                        <SelectItem value="delete" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.actions.deleteStandalone")}</SelectItem>
                      </SelectContent>
                    </Select>
                  )}

                  <div className="space-y-3">
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
                          <div className="space-y-1.5">
                            <Label size="sm">{t("preferences.workflowDialog.speedLimits.uploadLimit")}</Label>
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
                                      {getSpeedLimitUnits().map((u) => (
                                        <SelectItem key={u.value} value={String(u.value)}>{u.label}</SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>
                                </div>
                              )}
                            </div>
                          </div>
                          <div className="space-y-1.5">
                            <Label size="sm">{t("preferences.workflowDialog.speedLimits.downloadLimit")}</Label>
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
                                      {getSpeedLimitUnits().map((u) => (
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
                          <div className="space-y-1.5">
                            <Label size="sm">{t("preferences.workflowDialog.shareLimits.ratioLimit")}</Label>
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
                          <div className="space-y-1.5">
                            <Label size="sm">{t("preferences.workflowDialog.shareLimits.seedTime")}</Label>
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
                              <Label size="sm">{t("preferences.workflowDialog.shareLimitAction.label")}</Label>
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
                              <Label size="sm">{t("preferences.workflowDialog.shareLimitsMode.label")}</Label>
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
                    {formState.tagEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">
                            {t("preferences.workflowDialog.tag.title")}
                            <FieldHelp>{t("preferences.workflowDialog.tag.multipleActionsHelp")}</FieldHelp>
                          </Label>
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
                                <Label size="sm">{t("preferences.workflowDialog.tag.actionN", { count: index + 1 })}</Label>
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
                                    <Label size="sm" className="text-muted-foreground">{t("preferences.workflowDialog.tag.tagsDerivedFromTracker")}</Label>
                                    <div className="flex items-center gap-2 h-9 px-3 rounded-md border bg-muted/50 text-sm text-muted-foreground">
                                      {t("preferences.workflowDialog.tag.tagsDerivedDescription")}
                                    </div>
                                  </div>
                                ) : (
                                  <div className="space-y-1">
                                    <Label size="sm">{t("preferences.workflowDialog.tag.tags")}</Label>
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
                                  <Label size="sm">{t("preferences.workflowDialog.tag.actionMode")}</Label>
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
                                  <Label size="sm">{t("preferences.workflowDialog.tag.strategy")}</Label>
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
                                          includeCrossSeeds: checked ? false : item.includeCrossSeeds,
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
                                    <Tooltip>
                                      <TooltipTrigger asChild openOnTap>
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
                                  </div>
                                )}
                                {!tagAction.useTrackerAsTag && (
                                  <div className="flex items-center gap-2">
                                    <Switch
                                      id={`include-crossseeds-tag-${index}`}
                                      checked={tagAction.includeCrossSeeds}
                                      onCheckedChange={(checked) => setFormState(prev => ({
                                        ...prev,
                                        exprTagActions: prev.exprTagActions.map((item, i) => i === index ? { ...item, includeCrossSeeds: checked } : item),
                                      }))}
                                    />
                                    <Label htmlFor={`include-crossseeds-tag-${index}`} className="text-sm cursor-pointer whitespace-nowrap">
                                      {t("preferences.workflowDialog.tag.includeAffectedCrossSeeds")}
                                    </Label>
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
                      </div>
                    )}

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
                            <Label size="sm">{t("preferences.workflowDialog.category.moveToCategory")}</Label>
                            <Select
                              value={formState.exprCategory === "" ? CATEGORY_UNCATEGORIZED_VALUE : formState.exprCategory ?? ""}
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
                          {formState.exprCategory !== undefined && (
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
                          <Label size="sm">
                            {t("preferences.workflowDialog.move.newSavePath")}
                            <FieldHelp>{pathTemplateHelp(MOVE_PATH_DOCS_URL)}</FieldHelp>
                          </Label>
                          <Input
                            ref={movePathInputRef}
                            type="text"
                            value={formState.exprMovePath}
                            onChange={(e) => {
                              setFormState(prev => ({ ...prev, exprMovePath: e.target.value }))
                              setMovePathServerError(null)
                            }}
                            placeholder={t("preferences.workflowDialog.move.placeholder")}
                            aria-invalid={movePathError ? true : undefined}
                            className={cn(movePathError && "border-destructive/50")}
                          />
                          {movePathError && (
                            <p className="text-xs text-destructive">{movePathError}</p>
                          )}
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
                            <Tooltip>
                              <TooltipTrigger asChild openOnTap>
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
                          </div>
                        </div>
                      </div>
                    )}

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
                          <Label size="sm">{t("preferences.workflowDialog.externalProgram.program")}</Label>
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
                          <Label size="sm">{t("preferences.workflowDialog.export.targetInstance")}</Label>
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
                          <Label size="sm">
                            {t("preferences.workflowDialog.export.savePathLabel")}
                            <FieldHelp>
                              {t("preferences.workflowDialog.export.savePathHelp")} {pathTemplateHelp(EXPORT_PATH_DOCS_URL)}
                            </FieldHelp>
                          </Label>
                          <Input
                            value={formState.exprExportSavePath}
                            onChange={(e) => setFormState(prev => ({ ...prev, exprExportSavePath: e.target.value }))}
                            placeholder={t("preferences.workflowDialog.export.savePathPlaceholder")}
                            className="text-sm"
                          />
                        </div>
                        <div className="space-y-1">
                          <Label size="sm">{t("preferences.workflowDialog.export.categoryLabel")}</Label>
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
                          <Label size="sm">{t("preferences.workflowDialog.export.tagsLabel")}</Label>
                          <Input
                            value={formState.exprExportTags}
                            onChange={(e) => setFormState(prev => ({ ...prev, exprExportTags: e.target.value }))}
                            placeholder={t("preferences.workflowDialog.export.tagsPlaceholder")}
                            className="text-sm"
                          />
                        </div>
                        <div className="space-y-1">
                          <Label size="sm">{t("preferences.workflowDialog.export.contentLayoutLabel")}</Label>
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

                    {formState.transferToInstanceEnabled && (
                      <div className="rounded-lg border p-3 space-y-3">
                        <div className="flex items-center justify-between">
                          <Label className="text-sm font-medium">{t("preferences.workflowDialog.transfer.title")}</Label>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            aria-label={t("preferences.workflowDialog.transfer.remove")}
                            onClick={() => setFormState(prev => ({ ...prev, transferToInstanceEnabled: false, exprTransfer: emptyTransferForm }))}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                        <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.transfer.description")}</p>
                        <TransferForm
                          value={formState.exprTransfer}
                          onChange={exprTransfer => setFormState(prev => ({ ...prev, exprTransfer }))}
                          targets={nonSelfInstances}
                        />
                      </div>
                    )}

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
                          <Label size="sm">{t("preferences.workflowDialog.delete.mode")}</Label>
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
                                  <SelectItem value="deleteWithFiles" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.delete.withFiles")}</SelectItem>
                                  <SelectItem value="deleteWithFilesPreserveCrossSeeds" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.delete.withFilesPreserveCrossSeeds")}</SelectItem>
                                  <SelectItem value="deleteWithFilesIncludeCrossSeeds" className="text-destructive focus:text-destructive">{t("preferences.workflowDialog.delete.withFilesIncludeCrossSeeds")}</SelectItem>
                                </SelectContent>
                              </Select>
                            )
                          })()}
                        </div>
                        {formState.exprDeleteMode === "deleteWithFilesIncludeCrossSeeds" && (
                          <div className="flex items-center gap-2">
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                                  <input
                                    type="checkbox"
                                    checked={formState.exprIncludeHardlinks}
                                    onChange={(e) => setFormState(prev => ({ ...prev, exprIncludeHardlinks: e.target.checked }))}
                                    disabled={!hasTrustedIdentity && !formState.exprIncludeHardlinks}
                                    className="h-3.5 w-3.5 rounded border-border disabled:pointer-events-none disabled:opacity-50"
                                  />
                                  <span className={!hasTrustedIdentity ? "opacity-50" : ""}>
                                    {t("preferences.workflowDialog.delete.includeHardlinkedCopies")}
                                  </span>
                                </label>
                              </TooltipTrigger>
                              <TooltipContent side="left" className="max-w-[320px]">
                                {hasTrustedIdentity ? (
                                  <p>{t("preferences.workflowDialog.delete.includeHardlinkedCopiesDescription")}</p>
                                ) : isRemoteInstance ? (
                                  <p>{t("preferences.workflowDialog.delete.remoteNotAvailable")}</p>
                                ) : (
                                  <p>{t("preferences.workflowDialog.delete.localAccessRequired")}</p>
                                )}
                              </TooltipContent>
                            </Tooltip>
                          </div>
                        )}
                      </div>
                    )}

                  </div>
                </div>

                {ruleUsesFreeSpace && (
                  <div className="rounded-lg border p-3 space-y-2">
                    <div className="flex items-center gap-1.5">
                      <Label className="text-sm font-medium">{t("preferences.workflowDialog.freeSpace.title")}</Label>
                      <Tooltip>
                        <TooltipTrigger asChild openOnTap>
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
                    </div>
                    <Select
                      value={formState.exprFreeSpaceSourceType}
                      onValueChange={(value) => {
                        const nextType = value as FormState["exprFreeSpaceSourceType"]
                        setFormState(prev => prev.exprFreeSpaceSourceType === nextType? prev: {
                          ...prev,
                          exprFreeSpaceSourceType: nextType,
                          exprFreeSpaceSourcePath: "",
                        })
                        setFreeSpaceSourcePathError(null)
                        handleFreeSpacePathInputChange("")
                      }}
                    >
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue placeholder={t("preferences.workflowDialog.freeSpace.selectSource")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="qbittorrent">{t("preferences.workflowDialog.freeSpace.defaultSource")}</SelectItem>
                        <SelectItem value="path" disabled={!pathSourceAvailable}>
                          {pathSourceWindowsBlocked? t("preferences.workflowDialog.freeSpace.pathSourceWindowsUnsupported"): !pathSourceAvailable? t("preferences.workflowDialog.freeSpace.pathSourceFilesystemAccessRequired"): t("preferences.workflowDialog.freeSpace.pathSource")}
                        </SelectItem>
                        <SelectItem value="qbitPath" disabled={supportsFreeSpaceAtPath === false}>
                          {supportsFreeSpaceAtPath === false? t("preferences.workflowDialog.freeSpace.qbitPathSourceUnsupported"): t("preferences.workflowDialog.freeSpace.qbitPathSource")}
                        </SelectItem>
                        <SelectItem value="allowance" disabled={!allowanceSourceAvailable}>
                          {pathSourceWindowsBlocked? t("preferences.workflowDialog.freeSpace.allowanceSourceWindowsUnsupported"): !allowanceSourceAvailable? t("preferences.workflowDialog.freeSpace.allowanceSourceAccessRequired"): t("preferences.workflowDialog.freeSpace.allowanceSource")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    {formState.exprFreeSpaceSourceType === "allowance" && !pathSourceWindowsBlocked && (
                      <div className="flex flex-col gap-1">
                        <div className="flex items-center gap-1.5">
                          <Label htmlFor="free-space-allowance" size="sm">{t("preferences.workflowDialog.freeSpace.allowanceLabel")}</Label>
                          <FieldHelp>{t("preferences.workflowDialog.freeSpace.allowanceHelp", { decimalUnit: t("preferences.workflowDialog.freeSpace.allowanceUnits.TB"), binaryUnit: unitLabel("TiB") })}</FieldHelp>
                        </div>
                        <div className="flex gap-2">
                          <Input
                            id="free-space-allowance"
                            type="number"
                            min={0}
                            step="any"
                            inputMode="decimal"
                            value={formState.exprFreeSpaceAllowance}
                            onChange={(e) => {
                              const nextAllowance = e.target.value
                              setFormState(prev => ({ ...prev, exprFreeSpaceAllowance: nextAllowance }))
                            }}
                            className="h-8 text-xs"
                          />
                          <Select
                            value={String(formState.exprFreeSpaceAllowanceUnit)}
                            onValueChange={(value) => setFormState(prev => ({ ...prev, exprFreeSpaceAllowanceUnit: Number(value) }))}
                          >
                            <SelectTrigger className="h-8 w-24 text-xs" aria-label={t("preferences.workflowDialog.freeSpace.allowanceUnitAria")}>
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value={String(ALLOWANCE_DECIMAL_UNITS.MB)}>{t("preferences.workflowDialog.freeSpace.allowanceUnits.MB")}</SelectItem>
                              <SelectItem value={String(ALLOWANCE_DECIMAL_UNITS.GB)}>{t("preferences.workflowDialog.freeSpace.allowanceUnits.GB")}</SelectItem>
                              <SelectItem value={String(ALLOWANCE_DECIMAL_UNITS.TB)}>{t("preferences.workflowDialog.freeSpace.allowanceUnits.TB")}</SelectItem>
                              {(["MiB", "GiB", "TiB"] as const).map(unit => (
                                <SelectItem key={unit} value={String(BYTES_PER_UNIT[unit])}>{unitLabel(unit)}</SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                      </div>
                    )}
                    {(((formState.exprFreeSpaceSourceType === "path" || formState.exprFreeSpaceSourceType === "allowance") && !pathSourceWindowsBlocked) || (formState.exprFreeSpaceSourceType === "qbitPath" && supportsFreeSpaceAtPath !== false)) && (
                      <div className="flex flex-col gap-1">
                        {formState.exprFreeSpaceSourceType === "allowance" && (
                          <div className="flex items-center gap-1.5">
                            <Label htmlFor="free-space-source-path" size="sm">{t("preferences.workflowDialog.freeSpace.allowancePathLabel")}</Label>
                            <FieldHelp>{t("preferences.workflowDialog.freeSpace.allowancePathHelp")}</FieldHelp>
                          </div>
                        )}
                        <div className="relative">
                          <Folder className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground z-10" />
                          <Input
                            id="free-space-source-path"
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
                            ref={freeSpaceListRef}
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
                                  className={cn(
                                    "w-full px-3 py-1.5 text-xs text-left",
                                    freeSpaceHighlightedIndex === idx ? "bg-accent text-accent-foreground" : "hover:bg-accent hover:text-accent-foreground"
                                  )}
                                  onMouseDown={(e) => e.preventDefault()}
                                  onClick={() => handleFreeSpacePathSelectSuggestion(entry)}
                                >
                                  <TruncatedText className="block" openOnTap={false}>{entry}</TruncatedText>
                                </button>
                              ))}
                            </div>
                          </div>,
                          dropdownContainerRef.current
                        )}
                        {freeSpaceSourcePathError && (
                          <p className="text-xs text-destructive">{freeSpaceSourcePathError}</p>
                        )}
                        {formState.exprFreeSpaceSourceType !== "allowance" && (
                          <p className="text-xs text-muted-foreground">
                            {formState.exprFreeSpaceSourceType === "qbitPath"? t("preferences.workflowDialog.freeSpace.qbitPathHelp"): t("preferences.workflowDialog.freeSpace.pathHelp")}
                          </p>
                        )}
                      </div>
                    )}
                  </div>
                )}

                {formState.categoryEnabled && (formState.exprIncludeCrossSeeds || formState.exprBlockIfCrossSeedInCategories.length > 0) && (
                  <div className="space-y-1.5">
                    <div className="flex items-center gap-1.5">
                      <Label size="sm">{t("preferences.workflowDialog.category.skipIfCrossSeedExists")}</Label>
                      <Tooltip>
                        <TooltipTrigger asChild openOnTap>
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
                            <p className="text-xs font-medium truncate">{t(Object.hasOwn(DRY_RUN_ACTION_LABEL_KEYS, event.action) ? DRY_RUN_ACTION_LABEL_KEYS[event.action] : DRY_RUN_ACTION_FALLBACK_KEY, { action: event.action })}</p>
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
                    onCheckedChange={(checked) => setFormState(prev => ({ ...prev, enabled: checked }))}
                  />
                  <Label htmlFor="rule-enabled" className="text-sm font-normal cursor-pointer">{t("preferences.workflowDialog.footer.enabled")}</Label>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    id="rule-dry-run"
                    checked={formState.dryRun}
                    onCheckedChange={setDryRun}
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
                      {formState.intervalSeconds !== null &&
                        ![60, 300, 900, 1800, 3600, 7200, 14400, 21600, 43200, 86400].includes(formState.intervalSeconds) && (
                        <SelectItem value={String(formState.intervalSeconds)}>
                          {t("preferences.workflowDialog.interval.custom", { seconds: formState.intervalSeconds })}
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                  {deleteUsesFreeSpace && (
                    <Tooltip>
                      <TooltipTrigger asChild openOnTap>
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
                  )}
                  {deleteUsesFreeSpace && formState.intervalSeconds === 60 && (
                    <span className="text-xs text-warning">{t("preferences.workflowDialog.interval.cooldownWarning")}</span>
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
                  disabled={dryRunNowMutation.isPending || createOrUpdate.isPending || previewMutation.isPending || Boolean(movePathError)}
                >
                  {dryRunNowMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  {t("preferences.workflowDialog.runDryRunNow")}
                </Button>
                {rule && (
                  <Button type="button" variant="outline" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" onClick={() => setShowRunNowDialog(true)}>
                    {t("preferences.runWorkflowNow.action")}
                  </Button>
                )}
                <Button type="submit" size="sm" className="flex-1 sm:flex-initial h-10 sm:h-8" disabled={createOrUpdate.isPending || previewMutation.isPending || movePathBlocksSave}>
                  {(createOrUpdate.isPending || previewMutation.isPending) && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  {rule ? t("preferences.workflowDialog.save") : t("preferences.workflowDialog.create")}
                </Button>
              </div>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      <RunWorkflowNowDialog
        target={showRunNowDialog && rule ? { instanceId, rule } : null}
        onClose={() => setShowRunNowDialog(false)}
      />

      <WorkflowPreviewDialog
        open={preview !== null}
        onOpenChange={(open) => {
          if (!open) {
            previewRequestRef.current += 1
            previewMutation.reset()
            dispatchSaveFlow({ type: "closed" })
          }
        }}
        title={
          isDeleteRule? (formState.enabled? t("preferences.workflowDialog.preview.confirmDeleteRule"): t("preferences.workflowDialog.preview.previewDeleteRule")): isCategoryRule? t("preferences.workflowDialog.preview.confirmCategoryChange", {
            category: previewCategory,
          }): t("preferences.workflowDialog.preview.confirmTagChange")
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
            ) : isCategoryRule ? (
              <>
                <p>
                  {t("preferences.workflowDialog.preview.categoryPrefix")}{" "}
                  <strong>{(previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0)}</strong> {t("preferences.workflowDialog.preview.torrents", { count: (previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0) })}
                  {previewResult.crossSeedCount ? (
                    <> {t("preferences.workflowDialog.preview.and")} <strong>{previewResult.crossSeedCount}</strong> {t("preferences.workflowDialog.preview.crossSeeds", { count: previewResult.crossSeedCount })}</>
                  ) : null}
                  {" "}{t("preferences.workflowDialog.preview.toCategory")} <strong>"{previewCategory}"</strong>.
                </p>
                <p className="text-muted-foreground text-sm">{t("preferences.workflowDialog.confirmSaveAndEnable")}</p>
              </>
            ) : (
              <>
                <p>
                  {t("preferences.workflowDialog.preview.tagPrefix")}{" "}
                  <strong>{(previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0)}</strong> {t("preferences.workflowDialog.preview.torrents", { count: (previewResult.totalMatches) - (previewResult.crossSeedCount ?? 0) })}
                  {previewResult.crossSeedCount ? (
                    <> {t("preferences.workflowDialog.preview.and")} <strong>{previewResult.crossSeedCount}</strong> {t("preferences.workflowDialog.preview.crossSeeds", { count: previewResult.crossSeedCount })}</>
                  ) : null}.
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
        condition={formState.actionCondition}
        onConfirm={() => createOrUpdate.mutate(formState)}
        previewError={preview?.status === "failed" ? preview.error : null}
        dryRun={formState.dryRun}
        onDryRunChange={setDryRun}
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
        isInitialLoading={preview?.status === "loading"}
        showScore={formState.sortingType === "score"}
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

      <AlertDialog
        open={saveFlow.step === "askingDryRun"}
        onOpenChange={(open) => {
          if (!open) dispatchSaveFlow({ type: "closed" })
        }}
      >
        <AlertDialogContent className="sm:max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>{t("preferences.workflowDialog.enableDryRunPrompt.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("preferences.workflowDialog.enableDryRunPrompt.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <DryRunToggle checked={formState.dryRun} onCheckedChange={setDryRun} />
          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.workflowDialog.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => createOrUpdate.mutate(formState)}>
              {rule?.enabled ? t("preferences.workflowDialog.save") : t("preferences.workflowDialog.enableDryRunPrompt.saveAndEnable")}
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
              <Label htmlFor="group-id" className="text-sm">
                {t("preferences.workflowDialog.customGroup.groupId")}
                <FieldHelp>{t("preferences.workflowDialog.customGroup.groupIdHelp")}</FieldHelp>
              </Label>
              <Input
                id="group-id"
                value={newGroupId}
                onChange={(e) => setNewGroupId(e.target.value)}
                placeholder={t("preferences.workflowDialog.customGroup.groupIdPlaceholder")}
                className="h-8 text-xs"
              />
            </div>

            <div className="space-y-1">
              <Label className="text-sm">
                {t("preferences.workflowDialog.customGroup.keys")}
                <FieldHelp>{t("preferences.workflowDialog.customGroup.keysHelp")}</FieldHelp>
              </Label>
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
            </div>

            <div className="space-y-1">
              <Label className="text-sm">
                {t("preferences.workflowDialog.customGroup.ambiguousPolicy")}
                <FieldHelp>{t("preferences.workflowDialog.customGroup.ambiguousPolicyHelp")}</FieldHelp>
              </Label>
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
            </div>

            {newGroupAmbiguousPolicy === "verify_overlap" && (
              <div className="space-y-1">
                <Label htmlFor="min-overlap" className="text-sm">
                  {t("preferences.workflowDialog.customGroup.minFileOverlap")}
                  <FieldHelp>{t("preferences.workflowDialog.customGroup.defaultOverlap")}</FieldHelp>
                </Label>
                <Input
                  id="min-overlap"
                  type="number"
                  value={newGroupMinOverlap}
                  onChange={(e) => setNewGroupMinOverlap(e.target.value)}
                  min="0"
                  max="100"
                  className="h-8 text-xs"
                />
              </div>
            )}
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.workflowDialog.cancel")}</AlertDialogCancel>
            <Button
              type="button"
              onClick={() => {
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
                if ((formState.exprGrouping?.groups || []).some(g => g.id === newGroupId)) {
                  toast.error(t("preferences.workflowDialog.toast.groupExists"))
                  return
                }

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
