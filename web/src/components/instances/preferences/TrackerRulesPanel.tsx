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
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { cn, parseTrackerDomains } from "@/lib/utils"
import type { TrackerRule, TrackerRuleInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Clock, Loader2, Pencil, Plus, RefreshCw, Scale, Trash2, XCircle } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

interface TrackerRulesPanelProps {
  instanceId: number
  /** Render variant: "card" wraps in Card component, "embedded" renders without card wrapper */
  variant?: "card" | "embedded"
}

type FormState = Omit<TrackerRuleInput, "categories" | "tags" | "tagMatchMode"> & {
  trackerDomains: string[]
  applyToAllTrackers: boolean
  categories: string[]
  tags: string[]
  tagMatchMode: "any" | "all"
}

const emptyFormState: FormState = {
  name: "",
  trackerPattern: "",
  trackerDomains: [],
  applyToAllTrackers: false,
  categories: [],
  tags: [],
  tagMatchMode: "any",
  uploadLimitKiB: undefined,
  downloadLimitKiB: undefined,
  ratioLimit: undefined,
  seedingTimeLimitMinutes: undefined,
  deleteMode: undefined,
  deleteUnregistered: false,
  enabled: true,
}

export function TrackerRulesPanel({ instanceId, variant = "card" }: TrackerRulesPanelProps) {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<TrackerRule | null>(null)
  const [formState, setFormState] = useState<FormState>(emptyFormState)
  const [deleteConfirmRule, setDeleteConfirmRule] = useState<TrackerRule | null>(null)

  const trackersQuery = useInstanceTrackers(instanceId, { enabled: true })
  const categoriesQuery = useQuery({
    queryKey: ["instance-categories", instanceId],
    queryFn: () => api.getCategories(instanceId),
  })
  const tagsQuery = useQuery({
    queryKey: ["instance-tags", instanceId],
    queryFn: () => api.getTags(instanceId),
  })

  const trackerOptions: Option[] = useMemo(() => {
    if (!trackersQuery.data) return []
    return Object.keys(trackersQuery.data)
      .map((domain) => ({ label: domain, value: domain }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }, [trackersQuery.data])

  const categoryOptions = useMemo(() => {
    if (!categoriesQuery.data) return []
    return buildCategorySelectOptions(categoriesQuery.data)
  }, [categoriesQuery.data])

  const tagOptions = useMemo(() => {
    if (!tagsQuery.data) return []
    return buildTagSelectOptions(tagsQuery.data)
  }, [tagsQuery.data])

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
    const isAllTrackers = rule.trackerPattern === "*"
    const domains = isAllTrackers ? [] : parseTrackerDomains(rule)
    setEditingRule(rule)
    setFormState({
      name: rule.name,
      trackerPattern: rule.trackerPattern,
      trackerDomains: domains,
      applyToAllTrackers: isAllTrackers,
      categories: rule.categories ?? [],
      tags: rule.tags ?? [],
      tagMatchMode: rule.tagMatchMode ?? "any",
      uploadLimitKiB: rule.uploadLimitKiB,
      downloadLimitKiB: rule.downloadLimitKiB,
      ratioLimit: rule.ratioLimit,
      seedingTimeLimitMinutes: rule.seedingTimeLimitMinutes,
      deleteMode: rule.deleteMode,
      deleteUnregistered: rule.deleteUnregistered ?? false,
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
    if (!formState.applyToAllTrackers && selectedTrackers.length === 0) {
      toast.error("Select at least one tracker")
      return
    }
    const payload = {
      ...formState,
      trackerDomains: formState.applyToAllTrackers ? [] : selectedTrackers,
      trackerPattern: formState.applyToAllTrackers ? "*" : selectedTrackers.join(","),
      categories: formState.categories.filter(Boolean),
      tags: formState.tags.filter(Boolean),
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

  const dialogContent = (
    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
      <DialogContent className="sm:max-w-3xl max-h-[90dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{editingRule ? "Edit Tracker Rule" : "Add Tracker Rule"}</DialogTitle>
          <DialogDescription>Match on tracker domain and optionally category/tag, then apply limits.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
          <div className="flex-1 overflow-y-auto space-y-4 pr-1">
            <div className="grid gap-4">
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
              <div className="space-y-3">
                <div className="flex items-center justify-between rounded-lg border p-3">
                  <div>
                    <Label htmlFor="all-trackers">Apply to all trackers</Label>
                    <p className="text-sm text-muted-foreground">Rule will match torrents from any tracker</p>
                  </div>
                  <Switch
                    id="all-trackers"
                    checked={formState.applyToAllTrackers}
                    onCheckedChange={(checked) => setFormState(prev => ({
                      ...prev,
                      applyToAllTrackers: checked,
                      trackerDomains: checked ? [] : prev.trackerDomains,
                    }))}
                  />
                </div>

                {!formState.applyToAllTrackers && (
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
                )}
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label>Categories (optional)</Label>
                <MultiSelect
                  options={categoryOptions}
                  selected={formState.categories}
                  onChange={(values) => setFormState(prev => ({ ...prev, categories: values }))}
                  placeholder="Select categories..."
                  creatable
                  onCreateOption={(value) => setFormState(prev => ({ ...prev, categories: [...prev.categories, value] }))}
                />
              </div>
              <div className="space-y-2">
                <Label>Tags (optional)</Label>
                <MultiSelect
                  options={tagOptions}
                  selected={formState.tags}
                  onChange={(values) => setFormState(prev => ({
                    ...prev,
                    tags: values,
                    tagMatchMode: values.length < 2 ? "any" : prev.tagMatchMode,
                  }))}
                  placeholder="Select tags..."
                  creatable
                  onCreateOption={(value) => setFormState(prev => ({ ...prev, tags: [...prev.tags, value] }))}
                />
                {formState.tags.length > 1 && (
                  <div className="flex items-center gap-2">
                    <Label className="text-xs text-muted-foreground">Match</Label>
                    <Select
                      value={formState.tagMatchMode}
                      onValueChange={(value: "any" | "all") => setFormState(prev => ({ ...prev, tagMatchMode: value }))}
                    >
                      <SelectTrigger className="h-7 w-24 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="any">Any tag</SelectItem>
                        <SelectItem value="all">All tags</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                )}
              </div>
            </div>

            {/* Speed Limits Group */}
            <div className="rounded-lg border p-4 space-y-3">
              <div>
                <h4 className="text-sm font-medium">Speed limits</h4>
                <p className="text-xs text-muted-foreground">Limit transfer speeds only</p>
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="rule-upload">Upload (KiB/s)</Label>
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
                  <Label htmlFor="rule-download">Download (KiB/s)</Label>
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
            </div>

            {/* Seeding Limits & Auto-Delete Group */}
            <div className="rounded-lg border p-4 space-y-4">
              <div>
                <h4 className="text-sm font-medium">Seeding limits & auto-delete</h4>
                <p className="text-xs text-muted-foreground">Torrents are deleted when ratio or seeding time is reached</p>
              </div>
              <div className="grid gap-4 sm:grid-cols-[1fr_auto_1fr] items-end">
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
                <span className="text-xs text-muted-foreground pb-2.5">OR</span>
                <div className="space-y-2">
                  <Label htmlFor="rule-seedtime">Seeding time (minutes)</Label>
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

              <div className="space-y-2">
                <Label htmlFor="rule-delete-mode">Delete mode</Label>
                <Select
                  value={formState.deleteMode ?? "none"}
                  onValueChange={(value) => setFormState(prev => ({
                    ...prev,
                    deleteMode: value === "none" ? undefined : value as "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds",
                    deleteUnregistered: value === "none" ? false : prev.deleteUnregistered
                  }))}
                >
                  <SelectTrigger id="rule-delete-mode">
                    <SelectValue placeholder="Select delete mode" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">Don't delete (torrent pauses when limit reached)</SelectItem>
                    <SelectItem value="delete">Delete torrent (keep files)</SelectItem>
                    <SelectItem value="deleteWithFiles">Delete torrent and files</SelectItem>
                    <SelectItem value="deleteWithFilesPreserveCrossSeeds">Delete torrent and files (preserve cross-seeds)</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <Tooltip>
                <TooltipTrigger asChild>
                  <div className={cn("flex items-center space-x-2", !formState.deleteMode && "opacity-50")}>
                    <Checkbox
                      id="delete-unregistered"
                      checked={formState.deleteUnregistered ?? false}
                      disabled={!formState.deleteMode}
                      onCheckedChange={(checked: boolean) => setFormState(prev => ({
                        ...prev,
                        deleteUnregistered: checked,
                      }))}
                    />
                    <Label
                      htmlFor="delete-unregistered"
                      className={cn("text-sm font-normal", formState.deleteMode ? "cursor-pointer" : "cursor-not-allowed")}
                    >
                      Also delete unregistered torrents
                    </Label>
                  </div>
                </TooltipTrigger>
                {!formState.deleteMode && (
                  <TooltipContent>
                    <p>Select a delete mode above to enable this option</p>
                  </TooltipContent>
                )}
              </Tooltip>
            </div>
          </div>

          <div className="flex items-center justify-between pt-4 border-t mt-4">
            <div className="flex items-center gap-2">
              <Switch
                id="rule-enabled"
                checked={formState.enabled ?? true}
                onCheckedChange={(checked) => setFormState(prev => ({ ...prev, enabled: checked }))}
              />
              <Label htmlFor="rule-enabled" className="text-sm font-normal cursor-pointer">Enabled</Label>
            </div>
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createOrUpdate.isPending}>
                {createOrUpdate.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {editingRule ? "Save changes" : "Create rule"}
              </Button>
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
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
        {dialogContent}
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
      {dialogContent}
      {deleteDialogContent}
    </div>
  )
}

function RuleSummary({ rule }: { rule: TrackerRule }) {
  const trackers = parseTrackerDomains(rule)
  const isAllTrackers = rule.trackerPattern === "*"
  const categories = rule.categories ?? []
  const tags = rule.tags ?? []

  const hasActions =
    rule.downloadLimitKiB !== undefined ||
    rule.uploadLimitKiB !== undefined ||
    rule.ratioLimit !== undefined ||
    rule.seedingTimeLimitMinutes !== undefined ||
    (rule.deleteMode && rule.deleteMode !== "none") ||
    rule.deleteUnregistered

  if (!hasActions && !isAllTrackers && trackers.length === 0 && categories.length === 0 && tags.length === 0) {
    return <span className="text-xs text-muted-foreground">No actions set</span>
  }

  return (
    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      {isAllTrackers ? (
        <Badge variant="outline" className="text-[11px]">All trackers</Badge>
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
          <Badge key={cat} variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
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
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal">
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

      {rule.deleteMode && rule.deleteMode !== "none" && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-destructive border-destructive/50">
          <Trash2 className="h-3 w-3" />
          {rule.deleteMode === "deleteWithFilesPreserveCrossSeeds"
            ? "Delete + files (XS safe)"
            : rule.deleteMode === "deleteWithFiles"
              ? "Delete + files"
              : "Delete"}
        </Badge>
      )}

      {rule.deleteUnregistered && (
        <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-1 font-normal text-orange-600 border-orange-600/50">
          <XCircle className="h-3 w-3" />
          Unregistered
        </Badge>
      )}
    </div>
  )
}

