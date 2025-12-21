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
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { cn, parseTrackerDomains } from "@/lib/utils"
import type { Automation, AutomationPreviewResult } from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Clock, Folder, Info, Loader2, Pause, Pencil, Plus, Scale, Tag, Trash2 } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"
import { AutomationDialog } from "./AutomationDialog"
import { AutomationPreviewDialog } from "./AutomationPreviewDialog"

export function AutomationsOverview() {
  const { instances } = useInstances()
  const queryClient = useQueryClient()
  const [expandedInstances, setExpandedInstances] = useState<string[]>([])
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<Automation | null>(null)
  const [editingInstanceId, setEditingInstanceId] = useState<number | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<{ instanceId: number; rule: Automation } | null>(null)
  const [enableConfirm, setEnableConfirm] = useState<{ instanceId: number; rule: Automation; preview: AutomationPreviewResult } | null>(null)
  const previewPageSize = 25

  const deleteRule = useMutation({
    mutationFn: ({ instanceId, ruleId }: { instanceId: number; ruleId: number }) =>
      api.deleteAutomation(instanceId, ruleId),
    onSuccess: (_, { instanceId }) => {
      toast.success("Automation deleted")
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
          <CardTitle className="text-lg font-semibold">Automations</CardTitle>
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
          <CardTitle className="text-lg font-semibold">Automations</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px]">
              <p>
                Apply per-tracker speed limits, ratio limits, and seeding time limits automatically.
                Rules are matched by tracker domain and optionally filtered by category or tag.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          Automatic limits and deletion.
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
                  </div>
                </AccordionContent>
              </AccordionItem>
            )
          })}
        </Accordion>
      </CardContent>

      {editingInstanceId !== null && (
        <AutomationDialog
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

      <AutomationPreviewDialog
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
