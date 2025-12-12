/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect, type Option } from "@/components/ui/multi-select"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { api } from "@/lib/api"
import { cn, parseTrackerDomains } from "@/lib/utils"
import type { TrackerRule, TrackerRuleInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Clock, Loader2, Pencil, Plus, RefreshCw, Scale, Trash2 } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

interface TrackerRulesPanelProps {
  instanceId: number
  /** Render variant: "card" wraps in Card component, "embedded" renders without card wrapper */
  variant?: "card" | "embedded"
}

type FormState = TrackerRuleInput & { trackerDomains: string[] }

const emptyFormState: FormState = {
  name: "",
  trackerPattern: "",
  trackerDomains: [],
  category: "",
  tag: "",
  uploadLimitKiB: undefined,
  downloadLimitKiB: undefined,
  ratioLimit: undefined,
  seedingTimeLimitMinutes: undefined,
  enabled: true,
}

export function TrackerRulesPanel({ instanceId, variant = "card" }: TrackerRulesPanelProps) {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<TrackerRule | null>(null)
  const [formState, setFormState] = useState<FormState>(emptyFormState)

  const trackersQuery = useInstanceTrackers(instanceId, { enabled: true })
  const trackerOptions: Option[] = useMemo(() => {
    if (!trackersQuery.data) return []
    return Object.keys(trackersQuery.data)
      .map((domain) => ({ label: domain, value: domain }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }, [trackersQuery.data])

  const rulesQuery = useQuery({
    queryKey: ["tracker-rules", instanceId],
    queryFn: () => api.listTrackerRules(instanceId),
  })

  const createOrUpdate = useMutation({
    mutationFn: async (input: FormState) => {
      if (editingRule) {
        return api.updateTrackerRule(instanceId, editingRule.id, input)
      }
      return api.createTrackerRule(instanceId, input)
    },
    onSuccess: () => {
      toast.success(`Tracker rule ${editingRule ? "updated" : "created"}`)
      setDialogOpen(false)
      setEditingRule(null)
      setFormState(emptyFormState)
      void queryClient.invalidateQueries({ queryKey: ["tracker-rules", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to save tracker rule")
    },
  })

  const deleteRule = useMutation({
    mutationFn: (ruleId: number) => api.deleteTrackerRule(instanceId, ruleId),
    onSuccess: () => {
      toast.success("Tracker rule deleted")
      void queryClient.invalidateQueries({ queryKey: ["tracker-rules", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to delete tracker rule")
    },
  })

  const reorderRules = useMutation({
    mutationFn: (orderedIds: number[]) => api.reorderTrackerRules(instanceId, orderedIds),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["tracker-rules", instanceId] })
    },
  })

  const toggleEnabled = useMutation({
    mutationFn: (rule: TrackerRule) => api.updateTrackerRule(instanceId, rule.id, { ...rule, enabled: !rule.enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["tracker-rules", instanceId] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to toggle rule")
    },
  })

  const applyRules = useMutation({
    mutationFn: () => api.applyTrackerRules(instanceId),
    onSuccess: () => {
      toast.success("Tracker rules applied")
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to apply tracker rules")
    },
  })

  const sortedRules = useMemo(() => {
    const rules = rulesQuery.data ?? []
    return [...rules].sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id)
  }, [rulesQuery.data])

  const openForCreate = () => {
    setEditingRule(null)
    setFormState(emptyFormState)
    setDialogOpen(true)
  }

  const openForEdit = (rule: TrackerRule) => {
    const domains = parseTrackerDomains(rule)
    setEditingRule(rule)
    setFormState({
      name: rule.name,
      trackerPattern: rule.trackerPattern,
      trackerDomains: domains,
      category: rule.category,
      tag: rule.tag,
      uploadLimitKiB: rule.uploadLimitKiB,
      downloadLimitKiB: rule.downloadLimitKiB,
      ratioLimit: rule.ratioLimit,
      seedingTimeLimitMinutes: rule.seedingTimeLimitMinutes,
      enabled: rule.enabled,
      sortOrder: rule.sortOrder,
    })
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

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault()
    if (!formState.name) {
      toast.error("Name is required")
      return
    }
    const selectedTrackers = formState.trackerDomains.filter(Boolean)
    if (selectedTrackers.length === 0) {
      toast.error("Select at least one tracker")
      return
    }
    const payload: FormState = {
      ...formState,
      trackerDomains: selectedTrackers,
      trackerPattern: selectedTrackers.join(","),
      category: formState.category || undefined,
      tag: formState.tag || undefined,
    }
    createOrUpdate.mutate(payload)
  }

  const headerContent = (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      {variant === "card" && (
        <div className="space-y-1">
          <h3 className="text-lg font-semibold">Tracker Rules</h3>
          <p className="text-sm text-muted-foreground">Apply speed and ratio caps per tracker domain.</p>
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
            <p className="text-muted-foreground text-sm">No tracker rules yet. Add one to start enforcing per-tracker limits.</p>
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
                      onClick={() => deleteRule.mutate(rule.id)}
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

  const dialogContent = (
    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{editingRule ? "Edit Tracker Rule" : "Add Tracker Rule"}</DialogTitle>
            <DialogDescription>Match on tracker domain and optionally category/tag, then apply limits.</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-1">
              <div className="space-y-2">
                <Label htmlFor="rule-name">Name</Label>
                <Input
                  id="rule-name"
                  value={formState.name}
                  onChange={(e) => setFormState(prev => ({ ...prev, name: e.target.value }))}
                  required
                  placeholder="Tracker-specific rule"
                  autoComplete="off"
                  data-1p-ignore
                />
              </div>
              <div className="space-y-2">
                <Label>Trackers</Label>
                <MultiSelect
                  options={trackerOptions}
                  selected={formState.trackerDomains}
                  onChange={(next) => setFormState(prev => ({ ...prev, trackerDomains: next }))}
                  placeholder="Select trackers..."
                  creatable
                  onCreateOption={(value) => setFormState(prev => ({ ...prev, trackerDomains: [...prev.trackerDomains, value] }))}
                  disabled={trackersQuery.isLoading}
                />
                <p className="text-xs text-muted-foreground">
                  Choose from detected trackers or type a custom domain/glob (creates an entry).
                </p>
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="rule-category">Category (optional)</Label>
                <Input
                  id="rule-category"
                  value={formState.category ?? ""}
                  onChange={(e) => setFormState(prev => ({ ...prev, category: e.target.value || undefined }))}
                  placeholder="e.g. tv"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="rule-tag">Tag (optional)</Label>
                <Input
                  id="rule-tag"
                  value={formState.tag ?? ""}
                  onChange={(e) => setFormState(prev => ({ ...prev, tag: e.target.value || undefined }))}
                  placeholder="e.g. autobrr"
                />
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="rule-upload">Upload limit (KiB/s)</Label>
                <Input
                  id="rule-upload"
                  type="number"
                  min={0}
                  value={formState.uploadLimitKiB ?? ""}
                  onChange={(e) => setFormState(prev => ({ ...prev, uploadLimitKiB: e.target.value ? Number(e.target.value) : undefined }))}
                  placeholder="Leave blank to skip"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="rule-download">Download limit (KiB/s)</Label>
                <Input
                  id="rule-download"
                  type="number"
                  min={0}
                  value={formState.downloadLimitKiB ?? ""}
                  onChange={(e) => setFormState(prev => ({ ...prev, downloadLimitKiB: e.target.value ? Number(e.target.value) : undefined }))}
                  placeholder="Leave blank to skip"
                />
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="rule-ratio">Ratio limit</Label>
                <Input
                  id="rule-ratio"
                  type="number"
                  step="0.01"
                  min={-1}
                  value={formState.ratioLimit ?? ""}
                  onChange={(e) => setFormState(prev => ({ ...prev, ratioLimit: e.target.value ? Number(e.target.value) : undefined }))}
                  placeholder="e.g. 2.0"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="rule-seedtime">Seeding time limit (minutes)</Label>
                <Input
                  id="rule-seedtime"
                  type="number"
                  min={-1}
                  value={formState.seedingTimeLimitMinutes ?? ""}
                  onChange={(e) => setFormState(prev => ({ ...prev, seedingTimeLimitMinutes: e.target.value ? Number(e.target.value) : undefined }))}
                  placeholder="e.g. 1440"
                />
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-1">
              <div className="flex items-center justify-between rounded-lg border p-3">
                <div>
                  <Label htmlFor="rule-enabled">Enabled</Label>
                  <p className="text-sm text-muted-foreground">Rule is active and will be applied.</p>
                </div>
                <Switch
                  id="rule-enabled"
                  checked={formState.enabled ?? true}
                  onCheckedChange={(checked) => setFormState(prev => ({ ...prev, enabled: checked }))}
                />
              </div>
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createOrUpdate.isPending}>
                {createOrUpdate.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {editingRule ? "Save changes" : "Create rule"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
  )

  if (variant === "embedded") {
    return (
      <div className="space-y-4">
        {headerContent}
        {rulesContent}
        {dialogContent}
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
      {dialogContent}
    </div>
  )
}

function RuleSummary({ rule }: { rule: TrackerRule }) {
  const trackers = parseTrackerDomains(rule)

  const hasActions =
    rule.downloadLimitKiB !== undefined ||
    rule.uploadLimitKiB !== undefined ||
    rule.ratioLimit !== undefined ||
    rule.seedingTimeLimitMinutes !== undefined

  if (!hasActions && trackers.length === 0 && !rule.category && !rule.tag) {
    return <span className="text-xs text-muted-foreground">No actions set</span>
  }

  return (
    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      {trackers.length > 0 && (
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

      {rule.category && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
          Cat: {rule.category}
        </Badge>
      )}

      {rule.tag && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
          Tag: {rule.tag}
        </Badge>
      )}

      {rule.uploadLimitKiB !== undefined && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
          <ArrowUp className="h-3 w-3 text-muted-foreground/70" />
          UL {rule.uploadLimitKiB} KiB/s
        </Badge>
      )}

      {rule.downloadLimitKiB !== undefined && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
          <ArrowDown className="h-3 w-3 text-muted-foreground/70" />
          DL {rule.downloadLimitKiB} KiB/s
        </Badge>
      )}

      {rule.ratioLimit !== undefined && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
          <Scale className="h-3 w-3 text-muted-foreground/70" />
          Ratio {rule.ratioLimit}
        </Badge>
      )}

      {rule.seedingTimeLimitMinutes !== undefined && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
          <Clock className="h-3 w-3 text-muted-foreground/70" />
          {rule.seedingTimeLimitMinutes}m
        </Badge>
      )}
    </div>
  )
}

