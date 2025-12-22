/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { cn, copyTextToClipboard, formatRelativeTime, parseTrackerDomains } from "@/lib/utils"
import type { Automation, AutomationActivity, AutomationPreviewResult } from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Clock, Copy, Folder, Info, Loader2, Pause, Pencil, Plus, RefreshCcw, Scale, Search, Tag, Trash2 } from "lucide-react"
import { useCallback, useMemo, useState } from "react"
import { toast } from "sonner"
import { WorkflowDialog } from "./WorkflowDialog"
import { WorkflowPreviewDialog } from "./WorkflowPreviewDialog"

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

function formatAction(action: AutomationActivity["action"]): string {
  switch (action) {
    case "deleted_ratio":
      return "Ratio limit"
    case "deleted_seeding":
      return "Seeding time"
    case "deleted_unregistered":
      return "Unregistered"
    case "deleted_condition":
      return "Condition"
    case "delete_failed":
      return "Delete"
    case "limit_failed":
      return "Set limits"
    case "tags_changed":
      return "Tags"
    case "category_changed":
      return "Category"
    default:
      return action
  }
}

function formatTagsChangedSummary(details: AutomationActivity["details"]): string {
  const added = details?.added ?? {}
  const removed = details?.removed ?? {}
  const addedTotal = Object.values(added).reduce((a, b) => a + b, 0)
  const removedTotal = Object.values(removed).reduce((a, b) => a + b, 0)
  const parts: string[] = []
  if (addedTotal > 0) parts.push(`+${addedTotal} tagged`)
  if (removedTotal > 0) parts.push(`-${removedTotal} untagged`)
  return parts.join(", ") || "Tag operation"
}

function formatCategoryChangedSummary(details: AutomationActivity["details"]): string {
  const categories = details?.categories ?? {}
  const total = Object.values(categories).reduce((a: number, b: unknown) => a + (b as number), 0)
  return `${total} torrent${total !== 1 ? "s" : ""} moved`
}

interface WorkflowsOverviewProps {
  expandedInstances?: string[]
  onExpandedInstancesChange?: (values: string[]) => void
}

export function WorkflowsOverview({
  expandedInstances: controlledExpanded,
  onExpandedInstancesChange,
}: WorkflowsOverviewProps) {
  const { instances } = useInstances()
  const queryClient = useQueryClient()

  // Internal state for standalone usage
  const [internalExpanded, setInternalExpanded] = useState<string[]>([])

  // Use controlled props if provided, otherwise internal state
  const expandedInstances = controlledExpanded ?? internalExpanded
  const setExpandedInstances = onExpandedInstancesChange ?? setInternalExpanded
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<Automation | null>(null)
  const [editingInstanceId, setEditingInstanceId] = useState<number | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<{ instanceId: number; rule: Automation } | null>(null)
  const [enableConfirm, setEnableConfirm] = useState<{ instanceId: number; rule: Automation; preview: AutomationPreviewResult } | null>(null)
  const previewPageSize = 25

  // Activity-related state
  const { formatISOTimestamp } = useDateTimeFormatters()
  const [activityFilterMap, setActivityFilterMap] = useState<Record<number, "all" | "deletions" | "errors">>({})
  const [activitySearchMap, setActivitySearchMap] = useState<Record<number, string>>({})
  const [clearDaysMap, setClearDaysMap] = useState<Record<number, string>>({})
  const [displayLimitMap, setDisplayLimitMap] = useState<Record<number, number>>({})

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
        iconDomain: customization.domains[0],
        isCustomized: true,
      }
    }
    return {
      displayName: domain,
      iconDomain: domain,
      isCustomized: false,
    }
  }, [domainToCustomization])

  const deleteRule = useMutation({
    mutationFn: ({ instanceId, ruleId }: { instanceId: number; ruleId: number }) =>
      api.deleteAutomation(instanceId, ruleId),
    onSuccess: (_, { instanceId }) => {
      toast.success("Workflow deleted")
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to delete automation")
    },
  })

  const toggleEnabled = useMutation({
    mutationFn: ({ instanceId, rule }: { instanceId: number; rule: Automation }) =>
      api.updateAutomation(instanceId, rule.id, { ...rule, enabled: !rule.enabled }),
    onSuccess: (_, { instanceId }) => {
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to toggle rule")
    },
  })

  const previewRule = useMutation({
    mutationFn: ({ instanceId, rule }: { instanceId: number; rule: Automation }) =>
      api.previewAutomation(instanceId, { ...rule, enabled: true, previewLimit: previewPageSize, previewOffset: 0 }),
    onSuccess: (preview, { instanceId, rule }) => {
      // Last warning before enabling a delete rule (even if 0 matches right now).
      setEnableConfirm({ instanceId, rule, preview })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to preview rule")
    },
  })

  const loadMorePreview = useMutation({
    mutationFn: ({ instanceId, rule, offset }: { instanceId: number; rule: Automation; offset: number }) =>
      api.previewAutomation(instanceId, { ...rule, enabled: true, previewLimit: previewPageSize, previewOffset: offset }),
    onSuccess: (preview) => {
      setEnableConfirm(prev => prev
        ? { ...prev, preview: { ...prev.preview, examples: [...prev.preview.examples, ...preview.examples], totalMatches: preview.totalMatches } }
        : prev
      )
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to load more previews")
    },
  })

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
      previewRule.mutate({ instanceId, rule })
    } else {
      // Disabling or non-destructive rule - just toggle
      toggleEnabled.mutate({ instanceId, rule })
    }
  }

  const confirmEnableRule = () => {
    if (enableConfirm) {
      toggleEnabled.mutate({ instanceId: enableConfirm.instanceId, rule: enableConfirm.rule })
      setEnableConfirm(null)
    }
  }

  const handleLoadMorePreview = () => {
    if (!enableConfirm) {
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
      refetchInterval: expandedInstances.includes(String(instance.id)) ? 5000 : 30000,
      staleTime: 5000,
    })),
  })

  const handleDeleteOldActivity = async (instanceId: number, days: number) => {
    try {
      const result = await api.deleteAutomationActivity(instanceId, days)
      toast.success(`Deleted ${result.deleted} activity entries`)
      queryClient.invalidateQueries({ queryKey: ["automation-activity", instanceId] })
    } catch (error) {
      toast.error("Failed to delete activity", {
        description: error instanceof Error ? error.message : "Unknown error",
      })
    }
  }

  const outcomeClasses: Record<AutomationActivity["outcome"], string> = {
    success: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20",
    failed: "bg-destructive/10 text-destructive border-destructive/30",
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
          <CardTitle className="text-lg font-semibold">Workflows</CardTitle>
          <CardDescription>
            No instances configured. Add one in Settings to use this service.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-lg font-semibold">Workflows</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[340px]">
              <p>
                Condition-based automation rules. Actions: speed limits, share limits, pause, delete, tag, and category changes.
                Match torrents by tracker, category, tag, ratio, seed time, size, and more.
                Cross-seed and hardlink aware—safely delete or move without losing shared files.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          Automate torrent management with conditional rules.
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
              if (activityFilter === "deletions" && e.outcome !== "success") return false
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
                          {enabledRulesCount}/{rules.length} active
                        </Badge>
                      )}
                      {activityStats.deletionsToday > 0 && (
                        <Badge variant="outline" className="bg-emerald-500/10 text-emerald-500 border-emerald-500/20 text-xs">
                          {activityStats.deletionsToday} today
                        </Badge>
                      )}
                      {activityStats.failedToday > 0 && (
                        <Badge variant="outline" className="bg-destructive/10 text-destructive border-destructive/30 text-xs">
                          {activityStats.failedToday} failed
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
                        <p className="text-sm text-destructive">Failed to load rules</p>
                        <p className="text-xs text-destructive/70 mt-1">Check connection to the instance.</p>
                      </div>
                    ) : rulesQuery?.isLoading ? (
                      <div className="flex items-center gap-2 text-muted-foreground text-sm py-4">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        Loading rules...
                      </div>
                    ) : sortedRules.length === 0 ? (
                      <div className="flex flex-col items-center justify-center py-6 text-center space-y-2 border border-dashed rounded-lg">
                        <p className="text-sm text-muted-foreground">
                          No automations configured yet.
                        </p>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => openCreateDialog(instance.id)}
                        >
                          <Plus className="h-4 w-4 mr-2" />
                          Add your first rule
                        </Button>
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {sortedRules.map((rule) => (
                          <RulePreview
                            key={rule.id}
                            rule={rule}
                            onToggle={() => handleToggle(instance.id, rule)}
                            isToggling={toggleEnabled.isPending || previewRule.isPending}
                            onEdit={() => openEditDialog(instance.id, rule)}
                            onDelete={() => setDeleteConfirm({ instanceId: instance.id, rule })}
                          />
                        ))}
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => openCreateDialog(instance.id)}
                          className="w-full"
                        >
                          <Plus className="h-4 w-4 mr-2" />
                          Add rule
                        </Button>
                      </div>
                    )}

                    {/* Activity Section */}
                    <div className="space-y-3">
                        {/* Activity filters */}
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span className="text-xs text-muted-foreground">
                              {allFilteredEvents.length === events.length
                                ? `${events.length} events`
                                : `${allFilteredEvents.length} of ${events.length}`}
                            </span>
                          </div>
                          <div className="flex items-center gap-2">
                            <Select
                              value={activityFilter}
                              onValueChange={(value: "all" | "deletions" | "errors") =>
                                setActivityFilterMap((prev) => ({ ...prev, [instance.id]: value }))
                              }
                            >
                              <SelectTrigger className="h-7 w-28 text-xs">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="all">All</SelectItem>
                                <SelectItem value="deletions">Deletions</SelectItem>
                                <SelectItem value="errors">Errors</SelectItem>
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
                                  <AlertDialogTitle>Clear Activity History</AlertDialogTitle>
                                  <AlertDialogDescription>
                                    Delete activity history older than the selected period.
                                    This action cannot be undone.
                                  </AlertDialogDescription>
                                </AlertDialogHeader>
                                <div className="py-4">
                                  <label className="text-sm font-medium mb-2 block">
                                    Keep activity from the last:
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
                                      <SelectItem value="1">1 day</SelectItem>
                                      <SelectItem value="3">3 days</SelectItem>
                                      <SelectItem value="7">7 days</SelectItem>
                                      <SelectItem value="14">14 days</SelectItem>
                                      <SelectItem value="30">30 days</SelectItem>
                                      <SelectItem value="0">Delete all</SelectItem>
                                    </SelectContent>
                                  </Select>
                                </div>
                                <AlertDialogFooter>
                                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                                  <AlertDialogAction
                                    onClick={() => handleDeleteOldActivity(
                                      instance.id,
                                      parseInt(clearDaysMap[instance.id] ?? "7", 10)
                                    )}
                                  >
                                    Delete
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
                            placeholder="Filter by name, hash, or rule..."
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
                            <p className="text-sm text-destructive">Failed to load activity</p>
                            <p className="text-xs text-destructive/70 mt-1">
                              Check connection to the instance.
                            </p>
                          </div>
                        ) : activityQuery?.isLoading ? (
                          <div className="h-[150px] flex items-center justify-center border rounded-lg bg-muted/10">
                            <p className="text-sm text-muted-foreground">Loading activity...</p>
                          </div>
                        ) : filteredEvents.length === 0 ? (
                          <div className="h-[100px] flex flex-col items-center justify-center border border-dashed rounded-lg bg-muted/10 text-center p-4">
                            <p className="text-sm text-muted-foreground">
                              {activitySearchTerm ? "No matching events found." : "No activity recorded yet."}
                            </p>
                            <p className="text-xs text-muted-foreground/60 mt-1">
                              {activitySearchTerm
                                ? "Try a different search term or clear the filter."
                                : "Events will appear here when automations delete torrents."}
                            </p>
                          </div>
                        ) : (
                          <div className="max-h-[350px] overflow-auto rounded-md border">
                            <div className="divide-y divide-border/40">
                              {filteredEvents.map((event) => (
                                <div
                                  key={event.id}
                                  className="p-3 hover:bg-muted/20 transition-colors"
                                >
                                  <div className="flex flex-col gap-2">
                                    <div className="grid grid-cols-[1fr_auto] items-center gap-2">
                                      <div className="min-w-0">
                                        {event.action === "tags_changed" ? (
                                          <span className="font-medium text-sm block">
                                            {formatTagsChangedSummary(event.details)}
                                          </span>
                                        ) : event.action === "category_changed" ? (
                                          <span className="font-medium text-sm block">
                                            {formatCategoryChangedSummary(event.details)}
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
                                        {event.action !== "tags_changed" && event.action !== "category_changed" && (
                                          <Badge
                                            variant="outline"
                                            className={cn(
                                              "text-[10px] px-1.5 py-0 h-5 shrink-0",
                                              outcomeClasses[event.outcome]
                                            )}
                                          >
                                            {event.outcome === "success" ? "Removed" : "Failed"}
                                          </Badge>
                                        )}
                                      </div>
                                    </div>

                                    <div className="flex items-center gap-3 text-xs text-muted-foreground flex-wrap">
                                      {event.hash && (
                                        <div className="flex items-center gap-1 bg-muted/50 px-1.5 py-0.5 rounded">
                                          <span className="font-mono">{event.hash.substring(0, 7)}</span>
                                          <button
                                            type="button"
                                            className="hover:text-foreground transition-colors"
                                            onClick={() => {
                                              copyTextToClipboard(event.hash)
                                              toast.success("Hash copied")
                                            }}
                                            title="Copy hash"
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
                                                    <p className="text-xs">Original: {event.trackerDomain}</p>
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
                                      <div className="text-xs bg-muted/30 p-2 rounded">
                                        <span>{event.reason}</span>
                                      </div>
                                    )}

                                    {(event.details || event.ruleName) && (
                                      <div className="flex items-center gap-2 text-xs text-muted-foreground flex-wrap">
                                        {event.details?.ratio !== undefined && (
                                          <span>Ratio: {event.details.ratio.toFixed(2)}/{event.details.ratioLimit?.toFixed(2)}</span>
                                        )}
                                        {event.details?.seedingMinutes !== undefined && (
                                          <span>Seeding: {event.details.seedingMinutes}m/{event.details.seedingLimitMinutes}m</span>
                                        )}
                                        {event.details?.filesKept !== undefined && (() => {
                                          const { filesKept, deleteMode } = event.details
                                          let label: string
                                          const badgeClassName = "text-[10px] px-1.5 py-0 h-5"

                                          if (deleteMode === "delete") {
                                            label = "Torrent only"
                                          } else if (deleteMode === "deleteWithFilesPreserveCrossSeeds" && filesKept) {
                                            label = "Files kept due to cross-seeds"
                                          } else if (deleteMode === "deleteWithFiles" || deleteMode === "deleteWithFilesPreserveCrossSeeds") {
                                            label = "With files"
                                          } else {
                                            label = filesKept ? "Files kept" : "Files deleted"
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
                                        {event.ruleName && (
                                          <span className="text-muted-foreground">Rule: {event.ruleName}</span>
                                        )}
                                      </div>
                                    )}
                                  </div>
                                </div>
                              ))}
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
                                  Load more ({allFilteredEvents.length - displayLimit} remaining)
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

      <AlertDialog open={!!deleteConfirm} onOpenChange={(open) => !open && setDeleteConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Rule</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete "{deleteConfirm?.rule.name}"? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (deleteConfirm) {
                  deleteRule.mutate({ instanceId: deleteConfirm.instanceId, ruleId: deleteConfirm.rule.id })
                  setDeleteConfirm(null)
                }
              }}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <WorkflowPreviewDialog
        open={!!enableConfirm}
        onOpenChange={(open) => !open && setEnableConfirm(null)}
        title={
          enableConfirm && isCategoryRule(enableConfirm.rule)
            ? `Enable Category Rule → ${enableConfirm.rule.conditions?.category?.category}`
            : "Enable Delete Rule"
        }
        description={
          enableConfirm?.preview && enableConfirm.preview.totalMatches > 0 ? (
            enableConfirm && isCategoryRule(enableConfirm.rule) ? (
              <>
                <p>
                  Enabling "{enableConfirm.rule.name}" will move{" "}
                  <strong>{(enableConfirm.preview.totalMatches) - (enableConfirm.preview.crossSeedCount ?? 0)}</strong> torrent{((enableConfirm.preview.totalMatches) - (enableConfirm.preview.crossSeedCount ?? 0)) !== 1 ? "s" : ""}
                  {enableConfirm.preview.crossSeedCount ? (
                    <> and <strong>{enableConfirm.preview.crossSeedCount}</strong> cross-seed{enableConfirm.preview.crossSeedCount !== 1 ? "s" : ""}</>
                  ) : null}
                  {" "}to category <strong>"{enableConfirm.rule.conditions?.category?.category}"</strong>.
                </p>
                <p className="text-muted-foreground text-sm">Confirming will enable this rule immediately.</p>
              </>
            ) : (
              <>
                <p className="text-destructive font-medium">
                  Enabling "{enableConfirm.rule.name}" will affect {enableConfirm.preview.totalMatches} torrent{enableConfirm.preview.totalMatches !== 1 ? "s" : ""} that currently match.
                </p>
                <p className="text-muted-foreground text-sm">Confirming will enable this rule immediately.</p>
              </>
            )
          ) : (
            <>
              <p>No torrents currently match "{enableConfirm?.rule.name}".</p>
              <p className="text-muted-foreground text-sm">Confirming will enable this rule immediately.</p>
            </>
          )
        }
        preview={enableConfirm?.preview ?? null}
        onConfirm={confirmEnableRule}
        onLoadMore={handleLoadMorePreview}
        isLoadingMore={loadMorePreview.isPending}
        confirmLabel="Enable Rule"
        isConfirming={toggleEnabled.isPending}
        destructive={enableConfirm ? isDeleteRule(enableConfirm.rule) : false}
        warning={enableConfirm ? isCategoryRule(enableConfirm.rule) : false}
      />
    </Card>
  )
}

interface RulePreviewProps {
  rule: Automation
  onToggle: () => void
  isToggling: boolean
  onEdit: () => void
  onDelete: () => void
}

function RulePreview({ rule, onToggle, isToggling, onEdit, onDelete }: RulePreviewProps) {
  const trackers = parseTrackerDomains(rule)
  const isAllTrackers = rule.trackerPattern === "*"

  return (
    <div className={cn(
      "rounded-lg border bg-muted/20 p-3 grid grid-cols-[auto_1fr_auto] items-center gap-3",
      !rule.enabled && "opacity-60"
    )}>
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
            All trackers
          </Badge>
        ) : trackers.length > 0 && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge variant="outline" className="text-[10px] px-1.5 h-5 cursor-help">
                {trackers.length} tracker{trackers.length === 1 ? "" : "s"}
              </Badge>
            </TooltipTrigger>
            <TooltipContent className="max-w-[250px]">
              <p className="break-all">{trackers.join(", ")}</p>
            </TooltipContent>
          </Tooltip>
        )}
        {rule.conditions?.speedLimits?.enabled && rule.conditions.speedLimits.uploadKiB !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <ArrowUp className="h-3 w-3" />
            {rule.conditions.speedLimits.uploadKiB}
          </Badge>
        )}
        {rule.conditions?.speedLimits?.enabled && rule.conditions.speedLimits.downloadKiB !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <ArrowDown className="h-3 w-3" />
            {rule.conditions.speedLimits.downloadKiB}
          </Badge>
        )}
        {rule.conditions?.shareLimits?.enabled && rule.conditions.shareLimits.ratioLimit !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Scale className="h-3 w-3" />
            {rule.conditions.shareLimits.ratioLimit}
          </Badge>
        )}
        {rule.conditions?.shareLimits?.enabled && rule.conditions.shareLimits.seedingTimeMinutes !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Clock className="h-3 w-3" />
            {rule.conditions.shareLimits.seedingTimeMinutes}m
          </Badge>
        )}
        {rule.conditions?.pause?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Pause className="h-3 w-3" />
            Pause
          </Badge>
        )}
        {rule.conditions?.delete?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default text-destructive border-destructive/50">
            <Trash2 className="h-3 w-3" />
            {rule.conditions.delete.mode === "deleteWithFilesPreserveCrossSeeds"
              ? "XS safe"
              : rule.conditions.delete.mode === "deleteWithFiles"
                ? "+ files"
                : ""}
          </Badge>
        )}
        {rule.conditions?.tag?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default">
            <Tag className="h-3 w-3" />
            {rule.conditions.tag.tags?.join(", ")}
          </Badge>
        )}
        {rule.conditions?.category?.enabled && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5 cursor-default text-emerald-600 border-emerald-600/50">
            <Folder className="h-3 w-3" />
            {rule.conditions.category.category}
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
        <Button
          variant="ghost"
          size="icon"
          onClick={onDelete}
          className="h-7 w-7 text-destructive"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  )
}
