/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { downloadBlob, toCsv, type CsvColumn } from "@/lib/csv-export"
import { pickTrackerIconDomain } from "@/lib/tracker-icons"
import { cn, copyTextToClipboard, formatBytes, parseTrackerDomains } from "@/lib/utils"
import {
  fromImportFormat,
  parseImportJSON,
  toDuplicateInput,
  toExportFormat,
  toExportJSON
} from "@/lib/workflow-utils"
import type { Automation, AutomationActivity, AutomationPreviewResult, AutomationPreviewTorrent, InstanceResponse, PreviewView, RuleCondition } from "@/types"
import type { DragEndEvent } from "@dnd-kit/core"
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors
} from "@dnd-kit/core"
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Clock, Copy, CopyPlus, Download, Folder, GripVertical, Info, Loader2, MoreVertical, Move, Pause, Play, Pencil, Plus, RefreshCcw, Scale, Search, Send, Tag, Terminal, Trash2, Upload } from "lucide-react"
import { useCallback, useMemo, useState, type CSSProperties, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import i18n from "../../../i18n"
import { toast } from "sonner"
import { AutomationActivityRunDialog } from "./AutomationActivityRunDialog"
import { WorkflowDialog } from "./WorkflowDialog"
import { WorkflowPreviewDialog } from "./WorkflowPreviewDialog"

/**
 * Recursively checks if a condition tree uses a specific field.
 */
function conditionUsesField(condition: RuleCondition | null | undefined, field: string): boolean {
  if (!condition) return false
  if (condition.field === field) return true
  if (condition.conditions) {
    return condition.conditions.some(c => conditionUsesField(c, field))
  }
  return false
}

interface ActivityStats {
  deletionsToday: number
  failedToday: number
  lastActivity?: Date
}

function computeActivityStats(events: AutomationActivity[]): ActivityStats {
  const now = new Date()
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())

  let deletionsToday = 0
  let failedToday = 0
  let lastActivity: Date | undefined

  for (const event of events) {
    const eventDate = new Date(event.createdAt)
    if (!lastActivity || eventDate > lastActivity) {
      lastActivity = eventDate
    }
    if (eventDate >= startOfToday) {
      if (event.outcome === "success") {
        if (event.action.startsWith("deleted_")) {
          deletionsToday++
        }
      } else if (event.outcome === "failed") {
        failedToday++
      }
    }
  }

  return { deletionsToday, failedToday, lastActivity }
}

/** Format share limit value for display: -2 = "Global", -1 = "Unlimited", >= 0 = number with optional precision */
function formatShareLimit(value: number | undefined, isRatio: boolean): string | null {
  if (value === undefined) return null
  if (value === -2) return i18n.t("preferences.workflowsOverview.shareLimit.global", { ns: "instances" })
  if (value === -1) return i18n.t("preferences.workflowsOverview.shareLimit.unlimited", { ns: "instances" })
  return isRatio ? value.toFixed(2) : String(value)
}

/** Format speed limit for compact badge display: 0 = "∞", > 0 = number */
function formatSpeedLimitCompact(kiB: number): string {
  if (kiB === 0) return "∞"
  return String(kiB)
}

function getRuleTagActions(rule: Automation) {
  if (rule.conditions?.tags && rule.conditions.tags.length > 0) {
    return rule.conditions.tags
  }
  if (rule.conditions?.tag) {
    return [rule.conditions.tag]
  }
  return []
}

function formatAction(action: AutomationActivity["action"]): string {
  const actionKeys: Record<string, string> = {
    deleted_ratio: "ratioLimit",
    deleted_seeding: "seedingTime",
    deleted_unregistered: "unregistered",
    deleted_condition: "condition",
    delete_failed: "delete",
    limit_failed: "setLimits",
    tags_changed: "tags",
    category_changed: "category",
    speed_limits_changed: "speed",
    share_limits_changed: "share",
    paused: "pause",
    resumed: "resume",
    rechecked: "recheck",
    reannounced: "reannounce",
    auto_managed: "autoManagement",
    moved: "move",
    external_program: "externalProgram",
    exported_to_instance: "exportToInstance",
    dry_run_no_match: "dryRun",
  }
  const key = actionKeys[action]
  return key ? i18n.t(`preferences.workflowsOverview.actions.${key}`, { ns: "instances" }) : action
}

function getOutcomeBadgeText(event: AutomationActivity): string {
  const s = "preferences.workflowsOverview"
  const ns = "instances"
  if (event.outcome === "dry-run") return i18n.t(`${s}.dryRun`, { ns })
  if (event.action === "external_program") return i18n.t(`${s}.${event.outcome === "success" ? "executed" : "failed"}`, { ns })
  if (event.action === "exported_to_instance") return i18n.t(`${s}.${event.outcome === "success" ? "exported" : "failed"}`, { ns })
  return i18n.t(`${s}.${event.outcome === "success" ? "removed" : "failed"}`, { ns })
}

function sumRecordValues(values: Record<string, number> | undefined): number {
  return Object.values(values ?? {}).reduce((sum, value) => {
    const asNumber = typeof value === "number" ? value : Number(value)
    return sum + (Number.isFinite(asNumber) ? asNumber : 0)
  }, 0)
}

function formatTagsChangedSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const addedTotal = sumRecordValues(details?.added)
  const removedTotal = sumRecordValues(details?.removed)
  const parts: string[] = []
  const s = "preferences.workflowsOverview.summary"
  const ns = "instances"
  if (addedTotal > 0) parts.push(i18n.t(outcome === "dry-run" ? `${s}.taggedDryRun` : `${s}.tagged`, { ns, count: addedTotal }))
  if (removedTotal > 0) parts.push(i18n.t(outcome === "dry-run" ? `${s}.untaggedDryRun` : `${s}.untagged`, { ns, count: removedTotal }))
  return parts.join(", ") || i18n.t(`${s}.tagOperation`, { ns })
}

function formatCategoryChangedSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const total = sumRecordValues(details?.categories)
  const s = "preferences.workflowsOverview.summary"
  return i18n.t(outcome === "dry-run" ? `${s}.movedDryRun` : `${s}.moved`, { ns: "instances", count: total })
}

function formatSpeedLimitsSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const total = sumRecordValues(details?.limits)
  const s = "preferences.workflowsOverview.summary"
  return i18n.t(outcome === "dry-run" ? `${s}.limitedDryRun` : `${s}.limited`, { ns: "instances", count: total })
}

function formatShareLimitsSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const total = sumRecordValues(details?.limits)
  const s = "preferences.workflowsOverview.summary"
  return i18n.t(outcome === "dry-run" ? `${s}.limitedDryRun` : `${s}.limited`, { ns: "instances", count: total })
}

function formatPausedSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const count = details?.count ?? 0
  const s = "preferences.workflowsOverview.summary"
  return i18n.t(outcome === "dry-run" ? `${s}.pausedDryRun` : `${s}.paused`, { ns: "instances", count })
}

function formatResumedSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const count = details?.count ?? 0
  const s = "preferences.workflowsOverview.summary"
  return i18n.t(outcome === "dry-run" ? `${s}.resumedDryRun` : `${s}.resumed`, { ns: "instances", count })
}

function formatRecheckedSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const count = details?.count ?? 0
  const s = "preferences.workflowsOverview.summary"
  return i18n.t(outcome === "dry-run" ? `${s}.recheckedDryRun` : `${s}.rechecked`, { ns: "instances", count })
}

function formatReannouncedSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const count = details?.count ?? 0
  const s = "preferences.workflowsOverview.summary"
  return i18n.t(outcome === "dry-run" ? `${s}.reannouncedDryRun` : `${s}.reannounced`, { ns: "instances", count })
}

function formatMovedSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const count = sumRecordValues(details?.paths)
  const s = "preferences.workflowsOverview.summary"
  if (outcome === "failed") {
    return i18n.t(`${s}.moveFailed`, { ns: "instances", count })
  }
  return i18n.t(outcome === "dry-run" ? `${s}.movedDryRun` : `${s}.moved`, { ns: "instances", count })
}

function formatExternalProgramSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const s = "preferences.workflowsOverview.summary"
  const ns = "instances"
  const programName = details?.programName
    ?? (typeof details?.programId === "number" ? i18n.t(`${s}.programFallback`, { ns, id: details.programId }) : i18n.t("preferences.workflowsOverview.actions.externalProgram", { ns }))
  if (outcome === "dry-run") {
    return i18n.t(`${s}.programWouldRun`, { ns, name: programName })
  }
  return outcome === "failed" ? i18n.t(`${s}.programFailed`, { ns, name: programName }) : i18n.t(`${s}.programExecuted`, { ns, name: programName })
}

function formatExportedToInstanceSummary(details: AutomationActivity["details"], outcome?: AutomationActivity["outcome"]): string {
  const count = details?.count ?? 0
  const s = "preferences.workflowsOverview.summary"
  const ns = "instances"
  if (outcome === "failed") {
    return i18n.t(`${s}.exportFailed`, { ns, count })
  }
  return i18n.t(outcome === "dry-run" ? `${s}.exportedDryRun` : `${s}.exported`, { ns, count })
}

function formatDeleteDryRunSummary(details: AutomationActivity["details"], action: AutomationActivity["action"]): string {
  const count = details?.count ?? 0
  const s = "preferences.workflowsOverview.summary"
  const ns = "instances"
  const labelKeys: Record<string, string> = {
    deleted_ratio: "deleteLabelRatio",
    deleted_seeding: "deleteLabelSeeding",
    deleted_unregistered: "deleteLabelUnregistered",
  }
  const label = i18n.t(`${s}.${labelKeys[action] ?? "deleteLabelCondition"}`, { ns })
  return i18n.t(`${s}.deleteDryRun`, { ns, count, label })
}

const runSummaryActions = new Set<AutomationActivity["action"]>([
  "tags_changed",
  "category_changed",
  "speed_limits_changed",
  "share_limits_changed",
  "paused",
  "resumed",
  "rechecked",
  "reannounced",
  "auto_managed",
  "moved",
  "exported_to_instance",
])

function isRunSummary(event: AutomationActivity): boolean {
  if (event.action === "dry_run_no_match") return false
  if (event.hash !== "") return false
  return event.outcome === "dry-run"
    || (event.outcome === "success" && runSummaryActions.has(event.action))
}

interface WorkflowsOverviewProps {
  expandedInstances?: string[]
  onExpandedInstancesChange?: (values: string[]) => void
}

export function WorkflowsOverview({
  expandedInstances: controlledExpanded,
  onExpandedInstancesChange,
}: WorkflowsOverviewProps) {
  const { t } = useTranslation("instances")
  const { instances } = useInstances()
  const queryClient = useQueryClient()

  // Keep the shared SSE stream open so automation-activity events invalidate
  // the matching react-query keys; this replaces the idle activity polling.
  useActivityStream()

  const reorderSensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 8 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  )

  // Internal state for standalone usage
  const [internalExpanded, setInternalExpanded] = useState<string[]>([])

  // Use controlled props if provided, otherwise internal state
  const expandedInstances = controlledExpanded ?? internalExpanded
  const setExpandedInstances = onExpandedInstancesChange ?? setInternalExpanded
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<Automation | null>(null)
  const [editingInstanceId, setEditingInstanceId] = useState<number | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<{ instanceId: number; rule: Automation } | null>(null)
  const [enableConfirm, setEnableConfirm] = useState<{
    instanceId: number
    rule: Automation
    preview: AutomationPreviewResult | null
    isInitialLoading: boolean
  } | null>(null)
  const [previewView, setPreviewView] = useState<PreviewView>("needed")
  const [isLoadingPreviewView, setIsLoadingPreviewView] = useState(false)
  const [isExporting, setIsExporting] = useState(false)
  const previewPageSize = 25

  const reorderRules = useMutation<
    void,
    Error,
    { instanceId: number; orderedIds: number[] },
    { previousRules?: Automation[] }
  >({
    mutationFn: ({ instanceId, orderedIds }) => api.reorderAutomations(instanceId, orderedIds),
    onMutate: async ({ instanceId, orderedIds }) => {
      await queryClient.cancelQueries({ queryKey: ["automations", instanceId] })

      const previousRules = queryClient.getQueryData<Automation[]>(["automations", instanceId])
      if (!previousRules) {
        return {}
      }

      const ruleByID = new Map(previousRules.map(r => [r.id, r]))
      const nextRules: Automation[] = []
      for (let i = 0; i < orderedIds.length; i++) {
        const id = orderedIds[i]
        const rule = ruleByID.get(id)
        if (!rule) continue
        nextRules.push({ ...rule, sortOrder: i + 1 })
      }

      queryClient.setQueryData<Automation[]>(["automations", instanceId], nextRules)

      return { previousRules }
    },
    onError: (error, { instanceId }, context) => {
      if (context?.previousRules) {
        queryClient.setQueryData<Automation[]>(["automations", instanceId], context.previousRules)
      }
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.reorderFailed"))
    },
    onSettled: (_, __, { instanceId }) => {
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
  })

  // Import dialog state
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [importInstanceId, setImportInstanceId] = useState<number | null>(null)
  const [importJSON, setImportJSON] = useState("")
  const [importError, setImportError] = useState<string | null>(null)

  // Activity-related state
  const { formatISOTimestamp } = useDateTimeFormatters()
  const [activityFilterMap, setActivityFilterMap] = useState<Record<number, "all" | "success" | "errors">>({})
  const [activitySearchMap, setActivitySearchMap] = useState<Record<number, string>>({})
  const [clearDaysMap, setClearDaysMap] = useState<Record<number, string>>({})
  const [displayLimitMap, setDisplayLimitMap] = useState<Record<number, number>>({})
  const [activityRunDialog, setActivityRunDialog] = useState<{ instanceId: number; activity: AutomationActivity } | null>(null)

  // Tracker customizations for display names and icons
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()

  const domainToCustomization = useMemo(() => {
    const map = new Map<string, { displayName: string; domains: string[] }>()
    for (const custom of trackerCustomizations ?? []) {
      for (const domain of custom.domains) {
        map.set(domain.toLowerCase(), {
          displayName: custom.displayName,
          domains: custom.domains,
        })
      }
    }
    return map
  }, [trackerCustomizations])

  const getTrackerDisplay = useCallback((domain: string): { displayName: string; iconDomain: string; isCustomized: boolean } => {
    const customization = domainToCustomization.get(domain.toLowerCase())
    if (customization) {
      return {
        displayName: customization.displayName,
        iconDomain: pickTrackerIconDomain(trackerIcons, customization.domains, domain),
        isCustomized: true,
      }
    }
    return {
      displayName: domain,
      iconDomain: domain,
      isCustomized: false,
    }
  }, [domainToCustomization, trackerIcons])

  const deleteRule = useMutation({
    mutationFn: ({ instanceId, ruleId }: { instanceId: number; ruleId: number }) =>
      api.deleteAutomation(instanceId, ruleId),
    onSuccess: (_, { instanceId }) => {
      toast.success(t("preferences.workflowsOverview.toast.workflowDeleted"))
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.deleteAutomationFailed"))
    },
  })

  const toggleEnabled = useMutation({
    mutationFn: ({ instanceId, rule }: { instanceId: number; rule: Automation }) =>
      api.updateAutomation(instanceId, rule.id, { ...rule, enabled: !rule.enabled }),
    onSuccess: (_, { instanceId }) => {
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.toggleRuleFailed"))
    },
  })

  const dryRunRule = useMutation({
    mutationFn: ({ instanceId, rule }: { instanceId: number; rule: Automation }) => {
      const payload = {
        name: rule.name,
        trackerPattern: rule.trackerPattern,
        trackerDomains: rule.trackerDomains ?? parseTrackerDomains(rule),
        conditions: rule.conditions,
        freeSpaceSource: rule.freeSpaceSource,
        enabled: true,
        dryRun: true,
        sortOrder: rule.sortOrder,
        intervalSeconds: rule.intervalSeconds ?? null,
      }
      return api.dryRunAutomation(instanceId, payload)
    },
    onSuccess: (_, { instanceId, rule }) => {
      toast.success(t("preferences.workflowsOverview.toast.dryRunCompleted", { name: rule.name }))
      void queryClient.invalidateQueries({ queryKey: ["automation-activity", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.dryRunFailed"))
    },
  })

  const previewRule = useMutation({
    mutationFn: async ({ instanceId, rule, view }: { instanceId: number; rule: Automation; view: PreviewView }) => {
      const minDelay = new Promise(resolve => setTimeout(resolve, 1000))
      try {
        const preview = await api.previewAutomation(instanceId, {
          ...rule,
          enabled: true,
          previewLimit: previewPageSize,
          previewOffset: 0,
          previewView: view,
        })
        await minDelay
        return preview
      } catch (error) {
        await minDelay
        throw error
      }
    },
    onSuccess: (preview) => {
      setEnableConfirm(prev => prev ? { ...prev, preview, isInitialLoading: false } : prev)
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to preview rule")
      setEnableConfirm(null)
    },
  })

  const loadMorePreview = useMutation({
    mutationFn: ({ instanceId, rule, offset }: { instanceId: number; rule: Automation; offset: number }) =>
      api.previewAutomation(instanceId, { ...rule, enabled: true, previewLimit: previewPageSize, previewOffset: offset, previewView }),
    onSuccess: (preview) => {
      setEnableConfirm(prev => {
        if (!prev?.preview) return prev
        return {
          ...prev,
          preview: {
            ...prev.preview,
            examples: [...prev.preview.examples, ...preview.examples],
            totalMatches: preview.totalMatches,
          },
        }
      })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.loadMoreFailed"))
    },
  })

  const createWorkflow = useMutation({
    mutationFn: ({ instanceId, payload }: { instanceId: number; payload: Parameters<typeof api.createAutomation>[1] }) =>
      api.createAutomation(instanceId, payload),
    onSuccess: (_, { instanceId }) => {
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.createFailed"))
    },
  })

  // Get existing workflow names for an instance
  const getExistingNames = useCallback((instanceId: number): string[] => {
    const queryData = queryClient.getQueryData<Automation[]>(["automations", instanceId])
    return queryData?.map(r => r.name) ?? []
  }, [queryClient])

  // Export workflow to clipboard
  const handleExport = useCallback(async (rule: Automation) => {
    const exportData = toExportFormat(rule)
    const json = toExportJSON(exportData)
    try {
      await copyTextToClipboard(json)
      toast.success(t("preferences.workflowsOverview.toast.workflowCopied"))
    } catch {
      toast.error(t("preferences.workflowsOverview.toast.copyFailed"))
    }
  }, [t])

  // Duplicate workflow in the same instance
  const handleDuplicate = useCallback((instanceId: number, rule: Automation) => {
    const existingNames = getExistingNames(instanceId)
    const input = toDuplicateInput(rule, existingNames)
    createWorkflow.mutate(
      { instanceId, payload: input },
      {
        onSuccess: () => {
          toast.success(t("preferences.workflowsOverview.toast.createdWorkflow", { name: input.name }))
        },
      }
    )
  }, [getExistingNames, createWorkflow, t])

  // Copy workflow to another instance
  const handleCopyToInstance = useCallback((sourceRule: Automation, targetInstanceId: number) => {
    const existingNames = getExistingNames(targetInstanceId)
    const input = toDuplicateInput(sourceRule, existingNames)
    createWorkflow.mutate(
      { instanceId: targetInstanceId, payload: input },
      {
        onSuccess: () => {
          const targetInstance = instances?.find(i => i.id === targetInstanceId)
          toast.success(t("preferences.workflowsOverview.toast.copiedWorkflow", {
            name: input.name,
            instance: targetInstance?.name ?? "instance",
          }))
        },
      }
    )
  }, [getExistingNames, createWorkflow, instances, t])

  // Open import dialog
  const openImportDialog = (instanceId: number) => {
    setImportInstanceId(instanceId)
    setImportJSON("")
    setImportError(null)
    setImportDialogOpen(true)
  }

  // Handle import
  const handleImport = useCallback(() => {
    if (!importInstanceId) return

    const result = parseImportJSON(importJSON)
    if (result.error || !result.data) {
      setImportError(result.error ?? t("preferences.workflowsOverview.importDialog.invalidImportData"))
      return
    }

    const existingNames = getExistingNames(importInstanceId)
    const input = fromImportFormat(result.data, existingNames)

    createWorkflow.mutate(
      { instanceId: importInstanceId, payload: input },
      {
        onSuccess: () => {
          toast.success(t("preferences.workflowsOverview.toast.importedWorkflow", { name: input.name }))
          setImportDialogOpen(false)
          setImportJSON("")
          setImportError(null)
        },
        onError: (err) => {
          setImportError(err instanceof Error ? err.message : t("preferences.workflowsOverview.importDialog.importFailed"))
        },
      }
    )
  }, [importInstanceId, importJSON, getExistingNames, createWorkflow, t])

  // Check if a rule is a delete or category rule (both need previews)
  const isDeleteRule = (rule: Automation): boolean => {
    return rule.conditions?.delete?.enabled === true
  }

  const isCategoryRule = (rule: Automation): boolean => {
    return rule.conditions?.category?.enabled === true
  }

  // Handle toggle - show preview when enabling delete or category rules
  const handleToggle = (instanceId: number, rule: Automation) => {
    if (!rule.enabled && (isDeleteRule(rule) || isCategoryRule(rule))) {
      // Enabling a delete or category rule - show preview first
      // Reset preview view to "needed" when starting a new preview
      setPreviewView("needed")
      setIsLoadingPreviewView(false)
      setEnableConfirm({ instanceId, rule, preview: null, isInitialLoading: true })
      previewRule.mutate({ instanceId, rule, view: "needed" })
    } else {
      // Disabling or non-destructive rule - just toggle
      toggleEnabled.mutate({ instanceId, rule })
    }
  }

  // Check if a delete rule uses FREE_SPACE field
  const ruleUsesFreeSpace = (rule: Automation): boolean => {
    if (!isDeleteRule(rule)) return false
    return conditionUsesField(rule.conditions?.delete?.condition, "FREE_SPACE")
  }

  // Handler for switching preview view - refetches with new view and resets pagination
  const handlePreviewViewChange = async (newView: PreviewView) => {
    if (!enableConfirm) return
    setPreviewView(newView)
    setIsLoadingPreviewView(true)
    try {
      const preview = await api.previewAutomation(enableConfirm.instanceId, {
        ...enableConfirm.rule,
        enabled: true,
        previewLimit: previewPageSize,
        previewOffset: 0,
        previewView: newView,
      })
      setEnableConfirm(prev => prev ? { ...prev, preview, isInitialLoading: false } : prev)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.switchViewFailed"))
    } finally {
      setIsLoadingPreviewView(false)
    }
  }

  // CSV columns for automation preview export
  const csvColumns: CsvColumn<AutomationPreviewTorrent>[] = [
    { header: t("preferences.workflowDialog.csv.name"), accessor: item => item.name },
    { header: t("preferences.workflowDialog.csv.hash"), accessor: item => item.hash },
    { header: t("preferences.workflowDialog.csv.tracker"), accessor: item => item.tracker },
    { header: t("preferences.workflowDialog.csv.size"), accessor: item => formatBytes(item.size) },
    { header: t("preferences.workflowDialog.csv.ratio"), accessor: item => item.ratio === -1 ? t("preferences.workflowDialog.infinity") : item.ratio.toFixed(2) },
    { header: t("preferences.workflowDialog.csv.seedingTimeSeconds"), accessor: item => item.seedingTime },
    { header: t("preferences.workflowDialog.csv.category"), accessor: item => item.category },
    { header: t("preferences.workflowDialog.csv.tags"), accessor: item => item.tags },
    { header: t("preferences.workflowDialog.csv.state"), accessor: item => item.state },
    { header: t("preferences.workflowDialog.csv.addedOn"), accessor: item => item.addedOn },
    { header: t("preferences.workflowDialog.csv.path"), accessor: item => item.contentPath ?? "" },
  ]

  const handleExportPreviewCsv = async () => {
    if (!enableConfirm?.preview) return

    setIsExporting(true)
    try {
      const pageSize = 500
      const allItems: AutomationPreviewTorrent[] = []
      let offset = 0
      const total = enableConfirm.preview.totalMatches

      while (allItems.length < total) {
        const result = await api.previewAutomation(enableConfirm.instanceId, {
          ...enableConfirm.rule,
          enabled: true,
          previewLimit: pageSize,
          previewOffset: offset,
          previewView,
        })
        allItems.push(...result.examples)
        offset += pageSize
        if (result.examples.length === 0) break
      }

      const csv = toCsv(allItems, csvColumns)
      const ruleName = (enableConfirm.rule.name || t("preferences.workflowDialog.automationFallbackName")).replace(/[^a-zA-Z0-9-_]/g, "_")
      downloadBlob(csv, `${ruleName}_preview.csv`)
      toast.success(t("preferences.workflowsOverview.toast.exportedTorrents", { count: allItems.length }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.toast.exportFailed"))
    } finally {
      setIsExporting(false)
    }
  }

  const confirmEnableRule = () => {
    if (enableConfirm) {
      toggleEnabled.mutate({ instanceId: enableConfirm.instanceId, rule: enableConfirm.rule })
      setEnableConfirm(null)
    }
  }

  const handleLoadMorePreview = () => {
    if (!enableConfirm?.preview) {
      return
    }
    loadMorePreview.mutate({
      instanceId: enableConfirm.instanceId,
      rule: enableConfirm.rule,
      offset: enableConfirm.preview.examples.length,
    })
  }

  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )

  // Fetch rules for all active instances
  const rulesQueries = useQueries({
    queries: activeInstances.map((instance) => ({
      queryKey: ["automations", instance.id],
      queryFn: () => api.listAutomations(instance.id),
      staleTime: 30000,
    })),
  })

  // Fetch activity for all active instances
  const activityQueries = useQueries({
    queries: activeInstances.map((instance) => ({
      queryKey: ["automation-activity", instance.id],
      queryFn: () => api.getAutomationActivity(instance.id, 100),
      refetchInterval: false as const,
      staleTime: 5000,
    })),
  })

  const handleDeleteOldActivity = async (instanceId: number, days: number) => {
    try {
      const result = await api.deleteAutomationActivity(instanceId, days)
      toast.success(t("preferences.workflowsOverview.toast.deletedActivityEntries", { count: result.deleted }))
      queryClient.invalidateQueries({ queryKey: ["automation-activity", instanceId] })
    } catch (error) {
      toast.error(t("preferences.workflowsOverview.toast.deleteActivityFailed"), {
        description: error instanceof Error ? error.message : t("preferences.workflowsOverview.unknownError"),
      })
    }
  }

  const outcomeClasses: Record<AutomationActivity["outcome"], string> = {
    success: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20",
    failed: "bg-destructive/10 text-destructive border-destructive/30",
    "dry-run": "bg-sky-500/10 text-sky-500 border-sky-500/20",
  }

  const actionClasses: Record<AutomationActivity["action"], string> = {
    deleted_ratio: "bg-blue-500/10 text-blue-500 border-blue-500/20",
    deleted_seeding: "bg-purple-500/10 text-purple-500 border-purple-500/20",
    deleted_unregistered: "bg-orange-500/10 text-orange-500 border-orange-500/20",
    deleted_condition: "bg-cyan-500/10 text-cyan-500 border-cyan-500/20",
    delete_failed: "bg-destructive/10 text-destructive border-destructive/30",
    limit_failed: "bg-yellow-500/10 text-yellow-500 border-yellow-500/20",
    tags_changed: "bg-indigo-500/10 text-indigo-500 border-indigo-500/20",
    category_changed: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20",
    speed_limits_changed: "bg-sky-500/10 text-sky-500 border-sky-500/20",
    share_limits_changed: "bg-violet-500/10 text-violet-500 border-violet-500/20",
    paused: "bg-amber-500/10 text-amber-500 border-amber-500/20",
    resumed: "bg-lime-500/10 text-lime-500 border-lime-500/20",
    rechecked: "bg-orange-500/10 text-orange-500 border-orange-500/20",
    reannounced: "bg-fuchsia-500/10 text-fuchsia-500 border-fuchsia-500/20",
    moved: "bg-green-500/10 text-green-500 border-green-500/20",
    external_program: "bg-teal-500/10 text-teal-500 border-teal-500/20",
    auto_managed: "bg-rose-500/10 text-rose-500 border-rose-500/20",
    exported_to_instance: "bg-blue-500/10 text-blue-500 border-blue-500/20",
    dry_run_no_match: "bg-slate-500/10 text-slate-500 border-slate-500/20",
  }

  const openCreateDialog = (instanceId: number) => {
    setEditingInstanceId(instanceId)
    setEditingRule(null)
    setDialogOpen(true)
  }

  const openEditDialog = (instanceId: number, rule: Automation) => {
    setEditingInstanceId(instanceId)
    setEditingRule(rule)
    setDialogOpen(true)
  }

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">{t("preferences.workflowsOverview.title")}</CardTitle>
          <CardDescription>
            {t("preferences.workflowsOverview.noInstancesDescription")}
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-lg font-semibold">{t("preferences.workflowsOverview.title")}</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[340px]">
              <p>{t("preferences.workflowsOverview.tooltip")}</p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          {t("preferences.workflowsOverview.description")}
        </CardDescription>
      </CardHeader>

      <CardContent className="p-0">
        <Accordion
          type="multiple"
          value={expandedInstances}
          onValueChange={setExpandedInstances}
          className="border-t"
        >
          {activeInstances.map((instance, index) => {
            const rulesQuery = rulesQueries[index]
            const rules = rulesQuery?.data ?? []
            const sortedRules = [...rules].sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id)
            const enabledRulesCount = rules.filter(r => r.enabled).length

            // Activity data for this instance
            const activityQuery = activityQueries[index]
            const events = activityQuery?.data ?? []
            const activityStats = computeActivityStats(events)
            const activityFilter = activityFilterMap[instance.id] ?? "all"
            const activitySearchTerm = (activitySearchMap[instance.id] ?? "").toLowerCase().trim()
            const displayLimit = displayLimitMap[instance.id] ?? 50

            // Filter events
            const allFilteredEvents = events.filter((e) => {
              if (activityFilter === "success" && e.outcome !== "success" && e.outcome !== "dry-run") return false
              if (activityFilter === "errors" && e.outcome !== "failed") return false
              if (activitySearchTerm) {
                const nameMatch = e.torrentName?.toLowerCase().includes(activitySearchTerm)
                const hashMatch = e.hash.toLowerCase().includes(activitySearchTerm)
                const ruleMatch = e.ruleName?.toLowerCase().includes(activitySearchTerm)
                if (!nameMatch && !hashMatch && !ruleMatch) return false
              }
              return true
            })
            const filteredEvents = allFilteredEvents.slice(0, displayLimit)
            const hasMoreEvents = allFilteredEvents.length > displayLimit

            return (
              <AccordionItem key={instance.id} value={String(instance.id)}>
                <AccordionTrigger className="px-6 py-4 hover:no-underline group">
                  <div className="flex items-center justify-between w-full pr-4">
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="font-medium truncate">{instance.name}</span>
                      {rules.length > 0 && (
                        <Badge variant="outline" className={cn(
                          "text-xs",
                          enabledRulesCount > 0 && "bg-emerald-500/10 text-emerald-500 border-emerald-500/20"
                        )}>
                          {t("preferences.workflowsOverview.activeCount", { enabled: enabledRulesCount, total: rules.length })}
                        </Badge>
                      )}
                      {activityStats.deletionsToday > 0 && (
                        <Badge variant="outline" className="bg-emerald-500/10 text-emerald-500 border-emerald-500/20 text-xs">
                          {t("preferences.workflowsOverview.today", { count: activityStats.deletionsToday })}
                        </Badge>
                      )}
                      {activityStats.failedToday > 0 && (
                        <Badge variant="outline" className="bg-destructive/10 text-destructive border-destructive/30 text-xs">
                          {t("preferences.workflowsOverview.failed", { count: activityStats.failedToday })}
                        </Badge>
                      )}
                    </div>

                    <div className="flex items-center gap-4">
                      {activityStats.lastActivity && (
                        <span className="text-xs text-muted-foreground hidden sm:block">
                          {formatRelativeTime(activityStats.lastActivity)}
                        </span>
                      )}
                    </div>
                  </div>
                </AccordionTrigger>

                <AccordionContent className="px-6 pb-4">
                  <div className="space-y-4">
                    {/* Rules list */}
                    {rulesQuery?.isError ? (
                      <div className="h-[100px] flex flex-col items-center justify-center border border-destructive/30 rounded-lg bg-destructive/10 text-center p-4">
                        <p className="text-sm text-destructive">{t("preferences.workflowsOverview.failedToLoadRules")}</p>
                        <p className="text-xs text-destructive/70 mt-1">{t("preferences.workflowsOverview.checkConnection")}</p>
                      </div>
                    ) : rulesQuery?.isLoading ? (
                      <div className="flex items-center gap-2 text-muted-foreground text-sm py-4">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        {t("preferences.workflowsOverview.loadingRules")}
                      </div>
                    ) : sortedRules.length === 0 ? (
                      <div className="flex flex-col items-center justify-center py-6 text-center space-y-2 border border-dashed rounded-lg">
                        <p className="text-sm text-muted-foreground">
                          {t("preferences.workflowsOverview.noAutomationsConfigured")}
                        </p>
                        <div className="flex gap-2">
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => openCreateDialog(instance.id)}
                          >
                            <Plus className="h-4 w-4 mr-2" />
                            {t("preferences.workflowsOverview.addFirstRule")}
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => openImportDialog(instance.id)}
                          >
                            <Upload className="h-4 w-4 mr-2" />
                            {t("preferences.workflowsOverview.import")}
                          </Button>
                        </div>
                      </div>
                    ) : (
                      <div className="space-y-2">
                        <DndContext
                          sensors={reorderSensors}
                          collisionDetection={closestCenter}
                          onDragEnd={(event: DragEndEvent) => {
                            const { active, over } = event
                            if (!over || active.id === over.id) return
                            if (reorderRules.isPending) return

                            const ids = sortedRules.map(r => r.id)
                            const fromIndex = ids.indexOf(active.id as number)
                            const toIndex = ids.indexOf(over.id as number)
                            if (fromIndex === -1 || toIndex === -1) return

                            const orderedIds = arrayMove(ids, fromIndex, toIndex)
                            reorderRules.mutate({ instanceId: instance.id, orderedIds })
                          }}
                        >
                          <SortableContext items={sortedRules.map(r => r.id)} strategy={verticalListSortingStrategy}>
                            <div className="space-y-2">
                              {sortedRules.map((rule) => {
                                const otherInstances = activeInstances.filter(i => i.id !== instance.id)
                                return (
                                  <SortableRulePreview
                                    key={rule.id}
                                    rule={rule}
                                    otherInstances={otherInstances}
                                    onToggle={() => handleToggle(instance.id, rule)}
                                    isToggling={toggleEnabled.isPending || previewRule.isPending}
                                    onEdit={() => openEditDialog(instance.id, rule)}
                                    onDelete={() => setDeleteConfirm({ instanceId: instance.id, rule })}
                                    onRunDryRun={() => dryRunRule.mutate({ instanceId: instance.id, rule })}
                                    onDuplicate={() => handleDuplicate(instance.id, rule)}
                                    onCopyToInstance={(targetId) => handleCopyToInstance(rule, targetId)}
                                    onExport={() => handleExport(rule)}
                                    disableDrag={sortedRules.length < 2 || reorderRules.isPending}
                                  />
                                )
                              })}
                            </div>
                          </SortableContext>
                        </DndContext>
                        <div className="flex gap-2">
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => openCreateDialog(instance.id)}
                            className="flex-1"
                          >
                            <Plus className="h-4 w-4 mr-2" />
                            {t("preferences.workflowsOverview.addRule")}
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => openImportDialog(instance.id)}
                          >
                            <Upload className="h-4 w-4 mr-2" />
                            {t("preferences.workflowsOverview.import")}
                          </Button>
                        </div>
                      </div>
                    )}

                    {/* Activity Section */}
                    <div className="space-y-3">
                      {/* Activity filters */}
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          <span className="text-xs text-muted-foreground">
                            {allFilteredEvents.length === events.length? t("preferences.workflowsOverview.events", { count: events.length }): t("preferences.workflowsOverview.eventsFiltered", { filtered: allFilteredEvents.length, total: events.length })}
                          </span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Select
                            value={activityFilter}
                            onValueChange={(value: "all" | "success" | "errors") =>
                              setActivityFilterMap((prev) => ({ ...prev, [instance.id]: value }))
                            }
                          >
                            <SelectTrigger className="h-7 w-28 text-xs">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="all">{t("preferences.workflowsOverview.all")}</SelectItem>
                              <SelectItem value="success">{t("preferences.workflowsOverview.success")}</SelectItem>
                              <SelectItem value="errors">{t("preferences.workflowsOverview.errors")}</SelectItem>
                            </SelectContent>
                          </Select>
                          <Button
                            type="button"
                            size="sm"
                            variant="ghost"
                            disabled={activityQuery?.isFetching}
                            onClick={() => queryClient.invalidateQueries({
                              queryKey: ["automation-activity", instance.id],
                            })}
                            className="h-7 px-2"
                          >
                            <RefreshCcw className={cn(
                              "h-3.5 w-3.5",
                              activityQuery?.isFetching && "animate-spin"
                            )} />
                          </Button>
                          <AlertDialog>
                            <AlertDialogTrigger asChild>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-7 px-2"
                                disabled={events.length === 0}
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                            </AlertDialogTrigger>
                            <AlertDialogContent>
                              <AlertDialogHeader>
                                <AlertDialogTitle>{t("preferences.workflowsOverview.clearActivityTitle")}</AlertDialogTitle>
                                <AlertDialogDescription>
                                  {t("preferences.workflowsOverview.clearActivityDescription")}
                                </AlertDialogDescription>
                              </AlertDialogHeader>
                              <div className="py-4">
                                <label className="text-sm font-medium mb-2 block">
                                  {t("preferences.workflowsOverview.keepActivityLabel")}
                                </label>
                                <Select
                                  value={clearDaysMap[instance.id] ?? "7"}
                                  onValueChange={(value) =>
                                    setClearDaysMap((prev) => ({ ...prev, [instance.id]: value }))
                                  }
                                >
                                  <SelectTrigger className="w-full">
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectItem value="1">{t("preferences.workflowsOverview.oneDay")}</SelectItem>
                                    <SelectItem value="3">{t("preferences.workflowsOverview.threeDays")}</SelectItem>
                                    <SelectItem value="7">{t("preferences.workflowsOverview.sevenDays")}</SelectItem>
                                    <SelectItem value="14">{t("preferences.workflowsOverview.fourteenDays")}</SelectItem>
                                    <SelectItem value="30">{t("preferences.workflowsOverview.thirtyDays")}</SelectItem>
                                    <SelectItem value="0">{t("preferences.workflowsOverview.deleteAll")}</SelectItem>
                                  </SelectContent>
                                </Select>
                              </div>
                              <AlertDialogFooter>
                                <AlertDialogCancel>{t("preferences.workflowsOverview.cancel")}</AlertDialogCancel>
                                <AlertDialogAction
                                  onClick={() => handleDeleteOldActivity(
                                    instance.id,
                                    parseInt(clearDaysMap[instance.id] ?? "7", 10)
                                  )}
                                >
                                  {t("preferences.workflowsOverview.deleteDialog.delete")}
                                </AlertDialogAction>
                              </AlertDialogFooter>
                            </AlertDialogContent>
                          </AlertDialog>
                        </div>
                      </div>

                      {/* Search filter */}
                      <div className="relative">
                        <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                        <Input
                          type="text"
                          placeholder={t("preferences.workflowsOverview.filterPlaceholder")}
                          value={activitySearchMap[instance.id] ?? ""}
                          onChange={(e) => setActivitySearchMap((prev) => ({
                            ...prev,
                            [instance.id]: e.target.value,
                          }))}
                          className="pl-9 h-8 text-sm"
                        />
                      </div>

                      {/* Activity list */}
                      {activityQuery?.isError ? (
                        <div className="h-[100px] flex flex-col items-center justify-center border border-destructive/30 rounded-lg bg-destructive/10 text-center p-4">
                          <p className="text-sm text-destructive">{t("preferences.workflowsOverview.failedToLoadActivity")}</p>
                          <p className="text-xs text-destructive/70 mt-1">
                            {t("preferences.workflowsOverview.checkConnection")}
                          </p>
                        </div>
                      ) : activityQuery?.isLoading ? (
                        <div className="h-[150px] flex items-center justify-center border rounded-lg bg-muted/40">
                          <p className="text-sm text-muted-foreground">{t("preferences.workflowsOverview.loadingActivity")}</p>
                        </div>
                      ) : filteredEvents.length === 0 ? (
                        <div className="h-[100px] flex flex-col items-center justify-center border border-dashed rounded-lg bg-muted/40 text-center p-4">
                          <p className="text-sm text-muted-foreground">
                            {activitySearchTerm ? t("preferences.workflowsOverview.noMatchingEvents") : t("preferences.workflowsOverview.noActivityYet")}
                          </p>
                          <p className="text-xs text-muted-foreground/60 mt-1">
                            {activitySearchTerm ? t("preferences.workflowsOverview.tryDifferentSearch") : t("preferences.workflowsOverview.eventsWillAppear")}
                          </p>
                        </div>
                      ) : (
                        <div className="max-h-[350px] overflow-auto rounded-md border bg-muted/20">
                          <div className="divide-y divide-border">
                            {filteredEvents.map((event) => {
                              const canOpenRun = isRunSummary(event)
                              return (
                                <div
                                  key={event.id}
                                  role={canOpenRun ? "button" : undefined}
                                  tabIndex={canOpenRun ? 0 : undefined}
                                  onClick={() => {
                                    if (canOpenRun) {
                                      setActivityRunDialog({ instanceId: instance.id, activity: event })
                                    }
                                  }}
                                  onKeyDown={(e) => {
                                    if (!canOpenRun) return
                                    if (e.key === "Enter" || e.key === " ") {
                                      e.preventDefault()
                                      setActivityRunDialog({ instanceId: instance.id, activity: event })
                                    }
                                  }}
                                  className={cn(
                                    "p-3 transition-colors",
                                    canOpenRun ? "cursor-pointer hover:bg-muted/40 focus:outline-none focus-visible:ring-2 focus-visible:ring-ring" : "hover:bg-muted/30"
                                  )}
                                >
                                  <div className="flex flex-col gap-2">
                                    <div className="grid grid-cols-[1fr_auto] items-center gap-2">
                                      <div className="min-w-0">
                                        {event.outcome === "dry-run" && event.action.startsWith("deleted_") ? (
                                          <span className="font-medium text-sm block">
                                            {formatDeleteDryRunSummary(event.details, event.action)}
                                          </span>
                                        ) : event.action === "tags_changed" ? (
                                          <span className="font-medium text-sm block">
                                            {formatTagsChangedSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "category_changed" ? (
                                          <span className="font-medium text-sm block">
                                            {formatCategoryChangedSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "speed_limits_changed" ? (
                                          <span className="font-medium text-sm block">
                                            {formatSpeedLimitsSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "share_limits_changed" ? (
                                          <span className="font-medium text-sm block">
                                            {formatShareLimitsSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "paused" ? (
                                          <span className="font-medium text-sm block">
                                            {formatPausedSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "resumed" ? (
                                          <span className="font-medium text-sm block">
                                            {formatResumedSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "rechecked" ? (
                                          <span className="font-medium text-sm block">
                                            {formatRecheckedSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "reannounced" ? (
                                          <span className="font-medium text-sm block">
                                            {formatReannouncedSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "moved" ? (
                                          <span className="font-medium text-sm block">
                                            {formatMovedSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "external_program" ? (
                                          <span className="font-medium text-sm block">
                                            {formatExternalProgramSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "exported_to_instance" ? (
                                          <span className="font-medium text-sm block">
                                            {formatExportedToInstanceSummary(event.details, event.outcome)}
                                          </span>
                                        ) : event.action === "dry_run_no_match" ? (
                                          <span className="font-medium text-sm block">
                                            {t("preferences.workflowsOverview.noDryRunMatches")}
                                          </span>
                                        ) : (
                                          <TruncatedText className="font-medium text-sm block cursor-default">
                                            {event.torrentName || event.hash}
                                          </TruncatedText>
                                        )}
                                      </div>
                                      <div className="flex items-center gap-1.5">
                                        <Badge
                                          variant="outline"
                                          className={cn(
                                            "text-[10px] px-1.5 py-0 h-5 shrink-0",
                                            actionClasses[event.action]
                                          )}
                                        >
                                          {formatAction(event.action)}
                                        </Badge>
                                        {(!runSummaryActions.has(event.action) || event.outcome === "dry-run") && (
                                          <Badge
                                            variant="outline"
                                            className={cn(
                                              "text-[10px] px-1.5 py-0 h-5 shrink-0",
                                              outcomeClasses[event.outcome]
                                            )}
                                          >
                                            {getOutcomeBadgeText(event)}
                                          </Badge>
                                        )}
                                      </div>
                                    </div>

                                    <div className="flex items-center gap-3 text-xs text-muted-foreground flex-wrap">
                                      {event.hash && (
                                        <div className="flex items-center gap-1 bg-muted/60 px-1.5 py-0.5 rounded">
                                          <span className="font-mono">{event.hash.substring(0, 7)}</span>
                                          <button
                                            type="button"
                                            className="hover:text-foreground transition-colors"
                                            onClick={(clickEvent) => {
                                              clickEvent.stopPropagation()
                                              copyTextToClipboard(event.hash)
                                              toast.success(t("preferences.workflowsOverview.toast.hashCopied"))
                                            }}
                                            title={t("preferences.workflowsOverview.copyHash")}
                                          >
                                            <Copy className="h-3 w-3" />
                                          </button>
                                        </div>
                                      )}
                                      {event.trackerDomain && (() => {
                                        const tracker = getTrackerDisplay(event.trackerDomain)
                                        return (
                                          <>
                                            <span className="text-muted-foreground/40">·</span>
                                            <div className="flex items-center gap-1">
                                              <TrackerIconImage tracker={tracker.iconDomain} trackerIcons={trackerIcons} />
                                              {tracker.isCustomized ? (
                                                <Tooltip>
                                                  <TooltipTrigger asChild>
                                                    <span className="text-xs font-medium cursor-default">{tracker.displayName}</span>
                                                  </TooltipTrigger>
                                                  <TooltipContent>
                                                    <p className="text-xs">{t("preferences.workflowsOverview.original", { domain: event.trackerDomain })}</p>
                                                  </TooltipContent>
                                                </Tooltip>
                                              ) : (
                                                <span className="text-xs font-medium">{tracker.displayName}</span>
                                              )}
                                            </div>
                                          </>
                                        )
                                      })()}
                                      {(event.hash || event.trackerDomain) && (
                                        <span className="text-muted-foreground/40">·</span>
                                      )}
                                      <span>{formatISOTimestamp(event.createdAt)}</span>
                                    </div>

                                    {event.reason && event.outcome === "failed" && (
                                      <div className="text-xs bg-muted/40 p-2 rounded">
                                        <span>{event.reason}</span>
                                      </div>
                                    )}

                                    {(event.details || event.ruleName) && (
                                      <div className="flex items-center gap-2 text-xs text-muted-foreground flex-wrap">
                                        {(() => {
                                          const ratio = event.details?.ratio
                                          const ratioLimit = event.details?.ratioLimit
                                          const hasRatio = typeof ratio === "number" && Number.isFinite(ratio)
                                          const hasRatioLimit = typeof ratioLimit === "number" && Number.isFinite(ratioLimit)

                                          if (!hasRatio) return null

                                          return (
                                            <span>
                                              {t("preferences.workflowsOverview.ratioLabel", { ratio: ratio.toFixed(2) })}
                                              {hasRatioLimit ? `/${ratioLimit.toFixed(2)}` : ""}
                                            </span>
                                          )
                                        })()}
                                        {(() => {
                                          const seedingMinutes = event.details?.seedingMinutes
                                          const seedingLimitMinutes = event.details?.seedingLimitMinutes
                                          const hasSeedingMinutes = typeof seedingMinutes === "number" && Number.isFinite(seedingMinutes)
                                          const hasSeedingLimitMinutes = typeof seedingLimitMinutes === "number" && Number.isFinite(seedingLimitMinutes)

                                          if (!hasSeedingMinutes) return null

                                          return (
                                            <span>
                                              {t("preferences.workflowsOverview.seedingLabel", { minutes: seedingMinutes })}
                                              {hasSeedingLimitMinutes ? `/${seedingLimitMinutes}m` : ""}
                                            </span>
                                          )
                                        })()}
                                        {event.details?.filesKept !== undefined && (() => {
                                          const { filesKept, deleteMode } = event.details
                                          let label: string
                                          const badgeClassName = "text-[10px] px-1.5 py-0 h-5"

                                          if (deleteMode === "delete") {
                                            label = t("preferences.workflowsOverview.torrentOnly")
                                          } else if (deleteMode === "deleteWithFilesPreserveCrossSeeds" && filesKept) {
                                            label = t("preferences.workflowsOverview.filesKeptDueToCrossSeeds")
                                          } else if (deleteMode === "deleteWithFilesIncludeCrossSeeds") {
                                            label = t("preferences.workflowsOverview.withFilesPlusCrossSeeds")
                                          } else if (deleteMode === "deleteWithFiles" || deleteMode === "deleteWithFilesPreserveCrossSeeds") {
                                            label = t("preferences.workflowsOverview.withFiles")
                                          } else {
                                            label = filesKept ? t("preferences.workflowsOverview.filesKept") : t("preferences.workflowsOverview.filesDeleted")
                                          }

                                          return (
                                            <Badge variant="outline" className={badgeClassName}>
                                              {label}
                                            </Badge>
                                          )
                                        })()}
                                        {event.action === "tags_changed" && event.details && (() => {
                                          const added = event.details.added ?? {}
                                          const removed = event.details.removed ?? {}
                                          const addedTags = Object.entries(added)
                                          const removedTags = Object.entries(removed)

                                          return (
                                            <div className="flex flex-wrap gap-1.5">
                                              {addedTags.map(([tag, count]) => (
                                                <Badge key={`add-${tag}`} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-emerald-500/10 text-emerald-500 border-emerald-500/20">
                                                  +{tag} ({count})
                                                </Badge>
                                              ))}
                                              {removedTags.map(([tag, count]) => (
                                                <Badge key={`rm-${tag}`} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-red-500/10 text-red-500 border-red-500/20">
                                                  -{tag} ({count})
                                                </Badge>
                                              ))}
                                            </div>
                                          )
                                        })()}
                                        {event.action === "category_changed" && event.details?.categories && (() => {
                                          const categories = Object.entries(event.details.categories as Record<string, number>)

                                          return (
                                            <div className="flex flex-wrap gap-1.5">
                                              {categories.map(([category, count]) => (
                                                <Badge key={category} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-emerald-500/10 text-emerald-500 border-emerald-500/20">
                                                  {category} ({count})
                                                </Badge>
                                              ))}
                                            </div>
                                          )
                                        })()}
                                        {event.action === "speed_limits_changed" && event.details?.limits && (() => {
                                          const limits = Object.entries(event.details.limits as Record<string, number>)

                                          return (
                                            <div className="flex flex-wrap gap-1.5">
                                              {limits.map(([key, count]) => {
                                                const [type, limitKiB] = key.split(":")
                                                const numKiB = Number(limitKiB)
                                                // 0 = Unlimited in qBittorrent per-torrent speed limits
                                                let label: string
                                                if (numKiB === 0) {
                                                  label = t("preferences.workflowsOverview.unlimited")
                                                } else {
                                                  const limitMiB = numKiB / 1024
                                                  label = limitMiB >= 1 ? `${limitMiB} MiB/s` : `${limitKiB} KiB/s`
                                                }
                                                return (
                                                  <Badge key={key} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-sky-500/10 text-sky-500 border-sky-500/20">
                                                    {type === "upload" ? "↑" : "↓"} {label} ({count})
                                                  </Badge>
                                                )
                                              })}
                                            </div>
                                          )
                                        })()}
                                        {event.action === "share_limits_changed" && event.details?.limits && (() => {
                                          const limits = Object.entries(event.details.limits as Record<string, number>)

                                          return (
                                            <div className="flex flex-wrap gap-1.5">
                                              {limits.map(([key, count]) => {
                                                const [ratio, seedMinutes] = key.split(":")
                                                const parts = []
                                                if (ratio !== "-1.00") parts.push(`${ratio}x`)
                                                if (seedMinutes !== "-1") {
                                                  const hours = Math.floor(Number(seedMinutes) / 60)
                                                  parts.push(`${hours}h`)
                                                }
                                                return (
                                                  <Badge key={key} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-violet-500/10 text-violet-500 border-violet-500/20">
                                                    {parts.join(" / ") || t("preferences.workflowsOverview.limit")} ({count})
                                                  </Badge>
                                                )
                                              })}
                                            </div>
                                          )
                                        })()}
                                        {event.action === "external_program" && event.details?.programName && (
                                          <span className="text-muted-foreground">{t("preferences.workflowsOverview.programLabel", { name: event.details.programName })}</span>
                                        )}
                                        {event.ruleName && (
                                          <span className="text-muted-foreground">{t("preferences.workflowsOverview.ruleLabel", { name: event.ruleName })}</span>
                                        )}
                                        {event.details?.paths && (() => {
                                          const paths = Object.entries(event.details.paths as Record<string, number>)
                                          return (
                                            <div className="flex flex-wrap gap-1.5">
                                              {paths.map(([path, count]) => (
                                                <Badge key={path} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-green-500/10 text-green-500 border-green-500/20">
                                                  {path} ({count})
                                                </Badge>
                                              ))}
                                            </div>
                                          )
                                        })()}
                                      </div>
                                    )}
                                  </div>
                                </div>
                              )
                            })}
                          </div>
                          {hasMoreEvents && (
                            <div className="p-2 border-t">
                              <Button
                                variant="ghost"
                                size="sm"
                                className="w-full text-xs"
                                onClick={() => setDisplayLimitMap((prev) => ({
                                  ...prev,
                                  [instance.id]: displayLimit + 50,
                                }))}
                              >
                                {t("preferences.workflowsOverview.loadMore", { remaining: allFilteredEvents.length - displayLimit })}
                              </Button>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                </AccordionContent>
              </AccordionItem>
            )
          })}
        </Accordion>
      </CardContent>

      {editingInstanceId !== null && (
        <WorkflowDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          instanceId={editingInstanceId}
          rule={editingRule}
        />
      )}

      {activityRunDialog && (
        <AutomationActivityRunDialog
          open={!!activityRunDialog}
          onOpenChange={(open) => {
            if (!open) {
              setActivityRunDialog(null)
            }
          }}
          instanceId={activityRunDialog.instanceId}
          activity={activityRunDialog.activity}
        />
      )}

      <AlertDialog open={!!deleteConfirm} onOpenChange={(open) => !open && setDeleteConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("preferences.workflowsOverview.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("preferences.workflowsOverview.deleteDialog.description", { name: deleteConfirm?.rule.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.workflowsOverview.deleteDialog.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (deleteConfirm) {
                  deleteRule.mutate({ instanceId: deleteConfirm.instanceId, ruleId: deleteConfirm.rule.id })
                  setDeleteConfirm(null)
                }
              }}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("preferences.workflowsOverview.deleteDialog.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <WorkflowPreviewDialog
        open={!!enableConfirm}
        onOpenChange={(open) => !open && setEnableConfirm(null)}
        title={
          enableConfirm && isCategoryRule(enableConfirm.rule)? t("preferences.workflowsOverview.enableCategoryRule", { category: enableConfirm.rule.conditions?.category?.category }): t("preferences.workflowsOverview.enableDeleteRule")
        }
        description={
          enableConfirm?.preview && enableConfirm.preview.totalMatches > 0 ? (
            enableConfirm && isCategoryRule(enableConfirm.rule) ? (
              <>
                <p>
                  {t("preferences.workflowDialog.preview.categoryPrefix", { name: enableConfirm.rule.name })}{" "}
                  <strong>{(enableConfirm.preview.totalMatches) - (enableConfirm.preview.crossSeedCount ?? 0)}</strong> {t("preferences.workflowDialog.preview.torrents", { count: (enableConfirm.preview.totalMatches) - (enableConfirm.preview.crossSeedCount ?? 0) })}
                  {enableConfirm.preview.crossSeedCount ? (
                    <> {t("preferences.workflowDialog.preview.and")} <strong>{enableConfirm.preview.crossSeedCount}</strong> {t("preferences.workflowDialog.preview.crossSeeds", { count: enableConfirm.preview.crossSeedCount })}</>
                  ) : null}
                  {" "}{t("preferences.workflowDialog.preview.toCategory")} <strong>"{enableConfirm.rule.conditions?.category?.category}"</strong>.
                </p>
                <p className="text-muted-foreground text-sm">{t("preferences.workflowsOverview.enableRuleImmediately")}</p>
              </>
            ) : (
              <>
                <p className="text-destructive font-medium">
                  {t("preferences.workflowDialog.preview.deleteEnabledSummary", { name: enableConfirm.rule.name, count: enableConfirm.preview.totalMatches })}
                </p>
                <p className="text-muted-foreground text-sm">{t("preferences.workflowsOverview.enableRuleImmediately")}</p>
              </>
            )
          ) : (
            <>
              <p>{t("preferences.workflowsOverview.noTorrentsMatch", { name: enableConfirm?.rule.name })}</p>
              <p className="text-muted-foreground text-sm">{t("preferences.workflowsOverview.enableRuleImmediately")}</p>
            </>
          )
        }
        preview={enableConfirm?.preview ?? null}
        condition={enableConfirm ? (enableConfirm.rule.conditions?.delete?.condition ?? enableConfirm.rule.conditions?.category?.condition) : null}
        onConfirm={confirmEnableRule}
        onLoadMore={handleLoadMorePreview}
        isLoadingMore={loadMorePreview.isPending}
        confirmLabel={t("preferences.workflowsOverview.enableRule")}
        isConfirming={toggleEnabled.isPending}
        destructive={enableConfirm ? isDeleteRule(enableConfirm.rule) : false}
        warning={enableConfirm ? isCategoryRule(enableConfirm.rule) : false}
        previewView={previewView}
        onPreviewViewChange={handlePreviewViewChange}
        showPreviewViewToggle={enableConfirm ? ruleUsesFreeSpace(enableConfirm.rule) : false}
        isLoadingPreview={isLoadingPreviewView}
        onExport={handleExportPreviewCsv}
        isExporting={isExporting}
        isInitialLoading={enableConfirm?.isInitialLoading ?? false}
      />

      <Dialog open={importDialogOpen} onOpenChange={setImportDialogOpen}>
        <DialogContent className="max-w-lg max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>{t("preferences.workflowsOverview.importDialog.title")}</DialogTitle>
            <DialogDescription>
              {t("preferences.workflowsOverview.importDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 overflow-y-auto flex-1 min-h-0">
            <Textarea
              placeholder={t("preferences.workflowsOverview.importDialog.placeholder")}
              value={importJSON}
              onChange={(e) => {
                setImportJSON(e.target.value)
                setImportError(null)
              }}
              className="min-h-[200px] max-h-[50vh] font-mono text-sm"
            />
            {importError && (
              <p className="text-sm text-destructive">{importError}</p>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setImportDialogOpen(false)}>
              {t("preferences.workflowsOverview.importDialog.cancel")}
            </Button>
            <Button
              onClick={handleImport}
              disabled={!importJSON.trim() || createWorkflow.isPending}
            >
              {createWorkflow.isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  {t("preferences.workflowsOverview.importDialog.importing")}
                </>
              ) : (
                <>
                  <Upload className="h-4 w-4 mr-2" />
                  {t("preferences.workflowsOverview.importDialog.import")}
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

interface RulePreviewProps {
  rule: Automation
  otherInstances: InstanceResponse[]
  onToggle: () => void
  isToggling: boolean
  dragHandle?: ReactNode
  onEdit: () => void
  onDelete: () => void
  onRunDryRun: () => void
  onDuplicate: () => void
  onCopyToInstance: (targetInstanceId: number) => void
  onExport: () => void
}

interface SortableRulePreviewProps extends Omit<RulePreviewProps, "dragHandle"> {
  disableDrag: boolean
}

function SortableRulePreview({
  rule,
  otherInstances,
  onToggle,
  isToggling,
  onEdit,
  onDelete,
  onRunDryRun,
  onDuplicate,
  onCopyToInstance,
  onExport,
  disableDrag,
}: SortableRulePreviewProps) {
  const { t } = useTranslation("instances")
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: rule.id,
    disabled: disableDrag,
  })

  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  }

  return (
    <div ref={setNodeRef} style={style} className={cn(isDragging && "opacity-70")}>
      <RulePreview
        rule={rule}
        otherInstances={otherInstances}
        onToggle={onToggle}
        isToggling={isToggling}
        onEdit={onEdit}
        onDelete={onDelete}
        onRunDryRun={onRunDryRun}
        onDuplicate={onDuplicate}
        onCopyToInstance={onCopyToInstance}
        onExport={onExport}
        dragHandle={(
          <Button
            type="button"
            variant="ghost"
            size="icon"
            ref={setActivatorNodeRef}
            disabled={disableDrag}
            className={cn(
              "h-7 w-7 cursor-grab active:cursor-grabbing text-muted-foreground hover:text-foreground",
              disableDrag && "cursor-default"
            )}
            aria-label={t("preferences.workflowsOverview.dragToReorder")}
            {...attributes}
            {...listeners}
          >
            <GripVertical className="h-4 w-4" />
          </Button>
        )}
      />
    </div>
  )
}

function RulePreview({
  rule,
  otherInstances,
  onToggle,
  isToggling,
  dragHandle,
  onEdit,
  onDelete,
  onRunDryRun,
  onDuplicate,
  onCopyToInstance,
  onExport,
}: RulePreviewProps) {
  const { t } = useTranslation("instances")
  const trackers = parseTrackerDomains(rule)
  const isAllTrackers = rule.trackerPattern === "*"
  const tagActions = getRuleTagActions(rule)
  const hasAnyCondition = Boolean(
    (rule.conditions?.speedLimits?.enabled && rule.conditions.speedLimits.condition) ||
    (rule.conditions?.shareLimits?.enabled && rule.conditions.shareLimits.condition) ||
    (rule.conditions?.pause?.enabled && rule.conditions.pause.condition) ||
    (rule.conditions?.resume?.enabled && rule.conditions.resume.condition) ||
    (rule.conditions?.recheck?.enabled && rule.conditions.recheck.condition) ||
    (rule.conditions?.reannounce?.enabled && rule.conditions.reannounce.condition) ||
    (rule.conditions?.delete?.enabled && rule.conditions.delete.condition) ||
    tagActions.some((action) => action.enabled && action.condition) ||
    (rule.conditions?.category?.enabled && rule.conditions.category.condition) ||
    (rule.conditions?.move?.enabled && rule.conditions.move.condition) ||
    (rule.conditions?.externalProgram?.enabled && rule.conditions.externalProgram.condition)
  )

  return (
    <div className={cn(
      "rounded-lg border bg-muted/40 p-3 grid grid-cols-[auto_auto_1fr_auto] items-center gap-3",
      !rule.enabled && "opacity-60"
    )}>
      {dragHandle ?? <div className="h-7 w-7" />}
      <Switch
        checked={rule.enabled}
        onCheckedChange={onToggle}
        disabled={isToggling}
        className="shrink-0"
      />
      <div className="min-w-0">
        <TruncatedText className={cn(
          "text-sm font-medium block cursor-default",
          !rule.enabled && "text-muted-foreground"
        )}>
          {rule.name}
        </TruncatedText>
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        {isAllTrackers ? (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 cursor-default">
            {t("preferences.workflows.allTrackers")}
          </Badge>
        ) : trackers.length > 0 && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge variant="outline" className="text-[10px] px-1.5 h-5 cursor-help">
                {t("preferences.workflowsOverview.trackers", { count: trackers.length })}
              </Badge>
            </TooltipTrigger>
            <TooltipContent className="max-w-[250px]">
              <p className="break-all">{trackers.join(", ")}</p>
            </TooltipContent>
          </Tooltip>
        )}
        {!hasAnyCondition && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 cursor-default">
            {t("preferences.workflowsOverview.allTorrents")}
          </Badge>
        )}
        {rule.conditions?.speedLimits?.enabled && rule.conditions.speedLimits.uploadKiB !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <ArrowUp className="h-3 w-3" />
            {formatSpeedLimitCompact(rule.conditions.speedLimits.uploadKiB)}
          </Badge>
        )}
        {rule.conditions?.speedLimits?.enabled && rule.conditions.speedLimits.downloadKiB !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <ArrowDown className="h-3 w-3" />
            {formatSpeedLimitCompact(rule.conditions.speedLimits.downloadKiB)}
          </Badge>
        )}
        {rule.conditions?.shareLimits?.enabled && rule.conditions.shareLimits.ratioLimit !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Scale className="h-3 w-3" />
            {formatShareLimit(rule.conditions.shareLimits.ratioLimit, true)}
          </Badge>
        )}
        {rule.conditions?.shareLimits?.enabled && rule.conditions.shareLimits.seedingTimeMinutes !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Clock className="h-3 w-3" />
            {formatShareLimit(rule.conditions.shareLimits.seedingTimeMinutes, false)}{rule.conditions.shareLimits.seedingTimeMinutes >= 0 ? t("preferences.workflows.minuteSuffix") : ""}
          </Badge>
        )}
        {rule.conditions?.pause?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Pause className="h-3 w-3" />
            {t("preferences.workflows.pause")}
          </Badge>
        )}
        {rule.conditions?.resume?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Play className="h-3 w-3" />
            {t("preferences.workflowsOverview.resume")}
          </Badge>
        )}
        {rule.conditions?.recheck?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <RefreshCcw className="h-3 w-3" />
            {t("preferences.workflows.recheck")}
          </Badge>
        )}
        {rule.conditions?.reannounce?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <RefreshCcw className="h-3 w-3" />
            {t("preferences.workflows.reannounce")}
          </Badge>
        )}
        {rule.conditions?.delete?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default text-destructive border-destructive/50">
            <Trash2 className="h-3 w-3" />
            {rule.conditions.delete.mode === "deleteWithFilesPreserveCrossSeeds"? t("preferences.workflowsOverview.xsSafe"): rule.conditions.delete.mode === "deleteWithFilesIncludeCrossSeeds"? t("preferences.workflowsOverview.plusXs"): rule.conditions.delete.mode === "deleteWithFiles"? t("preferences.workflowsOverview.plusFiles"): ""}
          </Badge>
        )}
        {tagActions.some((action) => action.enabled) && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Tag className="h-3 w-3" />
            {t("preferences.workflowsOverview.tagActions", { count: tagActions.length })}
          </Badge>
        )}
        {rule.conditions?.category?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default text-emerald-600 border-emerald-600/50">
            <Folder className="h-3 w-3" />
            {rule.conditions.category.category}
          </Badge>
        )}
        {rule.conditions?.move?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Move className="h-3 w-3" />
            {rule.conditions.move.path}
          </Badge>
        )}
        {rule.conditions?.externalProgram?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Terminal className="h-3 w-3" />
            {t("preferences.workflowsOverview.program")}
          </Badge>
        )}
        <Button
          variant="ghost"
          size="icon"
          onClick={onEdit}
          className="h-7 w-7 ml-1"
        >
          <Pencil className="h-3.5 w-3.5" />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-7 w-7">
              <MoreVertical className="h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onRunDryRun}>
              <RefreshCcw className="h-4 w-4 mr-2" />
              {t("preferences.workflowsOverview.runDryRunNow")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onDuplicate}>
              <CopyPlus className="h-4 w-4 mr-2" />
              {t("preferences.workflowsOverview.duplicate")}
            </DropdownMenuItem>
            {otherInstances.length > 0 && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <Send className="h-4 w-4 mr-2" />
                  {t("preferences.workflowsOverview.copyTo")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent>
                  {otherInstances.map(inst => (
                    <DropdownMenuItem key={inst.id} onClick={() => onCopyToInstance(inst.id)}>
                      {inst.name}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            <DropdownMenuItem onClick={onExport}>
              <Download className="h-4 w-4 mr-2" />
              {t("preferences.workflowsOverview.exportJSON")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onDelete} className="text-destructive focus:text-destructive">
              <Trash2 className="h-4 w-4 mr-2" />
              {t("preferences.workflowsOverview.deleteDialog.delete")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  )
}
