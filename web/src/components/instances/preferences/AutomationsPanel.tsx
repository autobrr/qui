/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { cn, parseTrackerDomains } from "@/lib/utils"
import type { Automation } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Clock, Filter, Loader2, Pause, Pencil, Plus, RefreshCw, Scale, Tag, Trash2, XCircle } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"
import { AutomationDialog } from "./AutomationDialog"

interface AutomationsPanelProps {
  instanceId: number
  /** Render variant: "card" wraps in Card component, "embedded" renders without card wrapper */
  variant?: "card" | "embedded"
}

export function AutomationsPanel({ instanceId, variant = "card" }: AutomationsPanelProps) {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<Automation | null>(null)
  const [deleteConfirmRule, setDeleteConfirmRule] = useState<Automation | null>(null)

  const rulesQuery = useQuery({
    queryKey: ["automations", instanceId],
    queryFn: () => api.listAutomations(instanceId),
  })

  const deleteRule = useMutation({
    mutationFn: (ruleId: number) => api.deleteAutomation(instanceId, ruleId),
    onSuccess: () => {
      toast.success("Automation deleted")
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to delete automation")
    },
  })

  const reorderRules = useMutation({
    mutationFn: (orderedIds: number[]) => api.reorderAutomations(instanceId, orderedIds),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
  })

  const toggleEnabled = useMutation({
    mutationFn: (rule: Automation) => api.updateAutomation(instanceId, rule.id, { ...rule, enabled: !rule.enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["automations", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to toggle rule")
    },
  })

  const applyRules = useMutation({
    mutationFn: () => api.applyAutomations(instanceId),
    onSuccess: () => {
      toast.success("Automations applied")
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to apply automations")
    },
  })

  const sortedRules = useMemo(() => {
    const rules = rulesQuery.data ?? []
    return [...rules].sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id)
  }, [rulesQuery.data])

  const openForCreate = () => {
    setEditingRule(null)
    setDialogOpen(true)
  }

  const openForEdit = (rule: Automation) => {
    setEditingRule(rule)
    setDialogOpen(true)
  }

  const handleMove = (ruleId: number, direction: -1 | 1) => {
    if (!sortedRules) return
    const index = sortedRules.findIndex(r => r.id === ruleId)
    const target = index + direction
    if (index === -1 || target < 0 || target >= sortedRules.length) {
      return
    }
    const nextOrder = sortedRules.map(r => r.id)
    const [removed] = nextOrder.splice(index, 1)
    nextOrder.splice(target, 0, removed)
    reorderRules.mutate(nextOrder)
  }

  const headerContent = (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      {variant === "card" && (
        <div className="space-y-1">
          <h3 className="text-lg font-semibold">Automations</h3>
          <p className="text-sm text-muted-foreground">Automatic limits and deletion.</p>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => applyRules.mutate()} disabled={applyRules.isPending}>
          {applyRules.isPending ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <RefreshCw className="h-4 w-4 mr-2" />}
          Apply now
        </Button>
        <Button size="sm" onClick={openForCreate}>
          <Plus className="h-4 w-4 mr-2" />
          Add rule
        </Button>
      </div>
    </div>
  )

  const rulesContent = (
    <div className="space-y-3">
      {rulesQuery.isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading rules...
        </div>
      ) : (sortedRules?.length ?? 0) === 0 ? (
        <p className="text-muted-foreground text-sm">No automations yet. Add one to start enforcing per-tracker limits.</p>
      ) : (
        <div className="space-y-2">
          {sortedRules.map((rule) => {
            const actions = (
              <>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => handleMove(rule.id, -1)}
                  disabled={reorderRules.isPending}
                  className="h-8 w-8 sm:h-9 sm:w-9"
                >
                  <ArrowUp className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => handleMove(rule.id, 1)}
                  disabled={reorderRules.isPending}
                  className="h-8 w-8 sm:h-9 sm:w-9"
                >
                  <ArrowDown className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => openForEdit(rule)}
                  aria-label="Edit"
                  className="h-8 w-8 sm:h-9 sm:w-9"
                >
                  <Pencil className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setDeleteConfirmRule(rule)}
                  className="text-destructive h-8 w-8 sm:h-9 sm:w-9"
                  disabled={deleteRule.isPending}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </>
            )

            return (
              <div
                key={rule.id}
                className={cn(
                  "rounded-lg border-dashed border bg-muted/40 p-3 sm:p-4 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 sm:gap-4",
                  !rule.enabled && "opacity-60"
                )}
              >
                <div className="space-y-1.5 flex-1 min-w-0">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={() => toggleEnabled.mutate(rule)}
                        disabled={toggleEnabled.isPending}
                        className="shrink-0"
                      />
                      <span className={cn("font-medium truncate", !rule.enabled && "text-muted-foreground")}>{rule.name}</span>
                      {!rule.enabled && (
                        <Badge variant="outline" className="shrink-0 text-muted-foreground">
                          Disabled
                        </Badge>
                      )}
                    </div>
                    <div className="flex items-center gap-0.5 sm:hidden shrink-0 -mr-1">
                      {actions}
                    </div>
                  </div>
                  <RuleSummary rule={rule} />
                </div>

                <div className="hidden sm:flex items-center gap-1 shrink-0">
                  {actions}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )

  const deleteDialogContent = (
    <AlertDialog open={!!deleteConfirmRule} onOpenChange={(open) => !open && setDeleteConfirmRule(null)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete Rule</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to delete "{deleteConfirmRule?.name}"? This action cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              if (deleteConfirmRule) {
                deleteRule.mutate(deleteConfirmRule.id)
                setDeleteConfirmRule(null)
              }
            }}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )

  if (variant === "embedded") {
    return (
      <div className="space-y-4">
        {headerContent}
        {rulesContent}
        <AutomationDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          instanceId={instanceId}
          rule={editingRule}
        />
        {deleteDialogContent}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          {headerContent}
        </CardHeader>
        <CardContent>
          {rulesContent}
        </CardContent>
      </Card>
      <AutomationDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        instanceId={instanceId}
        rule={editingRule}
      />
      {deleteDialogContent}
    </div>
  )
}

function RuleSummary({ rule }: { rule: Automation }) {
  const trackers = parseTrackerDomains(rule)
  const isAllTrackers = rule.trackerPattern === "*"
  const categories = rule.categories ?? []
  const tags = rule.tags ?? []

  // Check if using expression-based conditions
  const isExpressionBased = rule.conditions?.schemaVersion != null
  const conditions = rule.conditions

  const hasLegacyActions =
    rule.downloadLimitKiB !== undefined ||
    rule.uploadLimitKiB !== undefined ||
    rule.ratioLimit !== undefined ||
    rule.seedingTimeLimitMinutes !== undefined ||
    (rule.deleteMode && rule.deleteMode !== "none") ||
    rule.deleteUnregistered

  const hasExpressionActions =
    conditions?.speedLimits?.enabled ||
    conditions?.pause?.enabled ||
    conditions?.delete?.enabled

  const hasActions = hasLegacyActions || hasExpressionActions

  if (!hasActions && !isAllTrackers && trackers.length === 0 && categories.length === 0 && tags.length === 0) {
    return <span className="text-xs text-muted-foreground">No actions set</span>
  }

  return (
    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      {isAllTrackers ? (
        <Badge variant="outline" className="text-[11px] cursor-default">All trackers</Badge>
      ) : trackers.length > 0 && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              variant="outline"
              className="text-[11px] max-w-[200px] sm:max-w-[220px] inline-flex items-center gap-0.5 cursor-help truncate"
            >
              <span className="truncate">{trackers[0]}</span>
              {trackers.length > 1 && (
                <span className="shrink-0 font-normal ml-0.5">
                  +{trackers.length - 1}
                </span>
              )}
            </Badge>
          </TooltipTrigger>
          <TooltipContent className="max-w-[300px] break-all">
            <p>{trackers.join(", ")}</p>
          </TooltipContent>
        </Tooltip>
      )}

      {categories.length > 0 && categories.length <= 2 ? (
        categories.map((cat) => (
          <Badge key={cat} variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-default">
            Cat: {cat}
          </Badge>
        ))
      ) : categories.length > 2 && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-help">
              Cat: {categories[0]} +{categories.length - 1}
            </Badge>
          </TooltipTrigger>
          <TooltipContent className="max-w-[300px] break-all">
            <p>{categories.join(", ")}</p>
          </TooltipContent>
        </Tooltip>
      )}

      {tags.length === 1 ? (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-default">
          Tag: {tags[0]}
        </Badge>
      ) : tags.length === 2 ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-help">
              Tags ({rule.tagMatchMode === "all" ? "all" : "any"}): {tags[0]} +1
            </Badge>
          </TooltipTrigger>
          <TooltipContent className="max-w-[300px] break-all">
            <p>{tags.join(", ")}</p>
          </TooltipContent>
        </Tooltip>
      ) : tags.length > 2 && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-help">
              Tags ({rule.tagMatchMode === "all" ? "all" : "any"}): {tags[0]} +{tags.length - 1}
            </Badge>
          </TooltipTrigger>
          <TooltipContent className="max-w-[300px] break-all">
            <p>{tags.join(", ")}</p>
          </TooltipContent>
        </Tooltip>
      )}

      {/* Expression-based mode indicator and actions */}
      {isExpressionBased && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-blue-600 border-blue-600/50 cursor-default">
          <Filter className="h-3 w-3" />
          Advanced
        </Badge>
      )}

      {/* Expression-based Speed Limits */}
      {conditions?.speedLimits?.enabled && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-help">
              <ArrowUp className="h-3 w-3 text-muted-foreground/70" />
              Speed limits
            </Badge>
          </TooltipTrigger>
          <TooltipContent>
            <div className="space-y-1">
              {conditions.speedLimits.uploadKiB !== undefined && (
                <p>Upload: {conditions.speedLimits.uploadKiB} KiB/s</p>
              )}
              {conditions.speedLimits.downloadKiB !== undefined && (
                <p>Download: {conditions.speedLimits.downloadKiB} KiB/s</p>
              )}
              <p className="text-muted-foreground">With condition filter</p>
            </div>
          </TooltipContent>
        </Tooltip>
      )}

      {/* Expression-based Pause */}
      {conditions?.pause?.enabled && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-yellow-600 border-yellow-600/50 cursor-default">
          <Pause className="h-3 w-3" />
          Pause
        </Badge>
      )}

      {/* Expression-based Delete */}
      {conditions?.delete?.enabled && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-destructive border-destructive/50 cursor-help">
              <Trash2 className="h-3 w-3" />
              Delete
            </Badge>
          </TooltipTrigger>
          <TooltipContent>
            <p>{conditions.delete.mode === "deleteWithFilesPreserveCrossSeeds"
              ? "Delete with files (preserve cross-seeds)"
              : conditions.delete.mode === "deleteWithFiles"
                ? "Delete with files"
                : "Delete (keep files)"}</p>
            <p className="text-muted-foreground">With condition filter</p>
          </TooltipContent>
        </Tooltip>
      )}

      {/* Expression-based Tag */}
      {conditions?.tag?.enabled && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-blue-600 border-blue-600/50 cursor-help">
              <Tag className="h-3 w-3" />
              {conditions.tag.tags.length} tag{conditions.tag.tags.length !== 1 ? "s" : ""}
            </Badge>
          </TooltipTrigger>
          <TooltipContent>
            <p>Tags: {conditions.tag.tags.join(", ")}</p>
            <p className="text-muted-foreground">
              Mode: {conditions.tag.mode === "full" ? "Full sync" : conditions.tag.mode === "add" ? "Add only" : "Remove only"}
            </p>
          </TooltipContent>
        </Tooltip>
      )}

      {/* Legacy mode actions (only show when not using expressions) */}
      {!isExpressionBased && (
        <>
          {rule.uploadLimitKiB !== undefined && (
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-default">
              <ArrowUp className="h-3 w-3 text-muted-foreground/70" />
              UL {rule.uploadLimitKiB} KiB/s
            </Badge>
          )}

          {rule.downloadLimitKiB !== undefined && (
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-default">
              <ArrowDown className="h-3 w-3 text-muted-foreground/70" />
              DL {rule.downloadLimitKiB} KiB/s
            </Badge>
          )}

          {rule.ratioLimit !== undefined && (
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-default">
              <Scale className="h-3 w-3 text-muted-foreground/70" />
              Ratio {rule.ratioLimit}
            </Badge>
          )}

          {rule.seedingTimeLimitMinutes !== undefined && (
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal cursor-default">
              <Clock className="h-3 w-3 text-muted-foreground/70" />
              {rule.seedingTimeLimitMinutes}m
            </Badge>
          )}

          {rule.deleteMode && rule.deleteMode !== "none" && (
            <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-destructive border-destructive/50 cursor-default">
              <Trash2 className="h-3 w-3" />
              {rule.deleteMode === "deleteWithFilesPreserveCrossSeeds"
                ? "Delete + files (XS safe)"
                : rule.deleteMode === "deleteWithFiles"
                  ? "Delete + files"
                  : "Delete"}
            </Badge>
          )}
        </>
      )}

      {rule.deleteUnregistered && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-orange-600 border-orange-600/50 cursor-default">
          <XCircle className="h-3 w-3" />
          Unregistered
        </Badge>
      )}
    </div>
  )
}

