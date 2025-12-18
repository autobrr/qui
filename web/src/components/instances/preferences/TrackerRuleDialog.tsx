/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
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
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { QueryBuilder } from "@/components/query-builder"
import { TrackerRulePreviewDialog } from "./TrackerRulePreviewDialog"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { cn, parseTrackerDomains } from "@/lib/utils"
import type {
  TrackerRule,
  TrackerRuleInput,
  TrackerRulePreviewResult,
  ActionConditions,
  RuleCondition,
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

interface TrackerRuleDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  /** Rule to edit, or null to create a new rule */
  rule: TrackerRule | null
  onSuccess?: () => void
}

type ActionType = "speedLimits" | "pause" | "delete"

type FormState = Omit<TrackerRuleInput, "categories" | "tags" | "tagMatchMode" | "conditions"> & {
  trackerDomains: string[]
  applyToAllTrackers: boolean
  categories: string[]
  tags: string[]
  tagMatchMode: "any" | "all"
  // Expression-based conditions mode
  useExpressions: boolean
  // Single action with condition
  actionType: ActionType
  actionCondition: RuleCondition | null
  // Action-specific settings
  exprUploadKiB?: number
  exprDownloadKiB?: number
  exprDeleteMode: "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds"
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
  useExpressions: false,
  actionType: "delete",
  actionCondition: null,
  exprUploadKiB: undefined,
  exprDownloadKiB: undefined,
  exprDeleteMode: "deleteWithFilesPreserveCrossSeeds",
}

export function TrackerRuleDialog({ open, onOpenChange, instanceId, rule, onSuccess }: TrackerRuleDialogProps) {
  const queryClient = useQueryClient()
  const [formState, setFormState] = useState<FormState>(emptyFormState)
  const [previewResult, setPreviewResult] = useState<TrackerRulePreviewResult | null>(null)
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)

  const trackersQuery = useInstanceTrackers(instanceId, { enabled: open })
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const categoriesQuery = useQuery({
    queryKey: ["instance-categories", instanceId],
    queryFn: () => api.getCategories(instanceId),
    enabled: open,
  })
  const tagsQuery = useQuery({
    queryKey: ["instance-tags", instanceId],
    queryFn: () => api.getTags(instanceId),
    enabled: open,
  })

  // Build lookup maps from tracker customizations for merging and nicknames
  const trackerCustomizationMaps = useMemo(() => {
    const domainToCustomization = new Map<string, { displayName: string; domains: string[]; id: number }>()
    const secondaryDomains = new Set<string>()

    for (const custom of trackerCustomizations ?? []) {
      const domains = custom.domains
      if (domains.length === 0) continue

      for (let i = 0; i < domains.length; i++) {
        const domain = domains[i].toLowerCase()
        domainToCustomization.set(domain, {
          displayName: custom.displayName,
          domains: custom.domains,
          id: custom.id,
        })
        if (i > 0) {
          secondaryDomains.add(domain)
        }
      }
    }

    return { domainToCustomization, secondaryDomains }
  }, [trackerCustomizations])

  // Process trackers to apply customizations (nicknames and merged domains)
  const trackerOptions: Option[] = useMemo(() => {
    if (!trackersQuery.data) return []

    const { domainToCustomization, secondaryDomains } = trackerCustomizationMaps
    const trackers = Object.keys(trackersQuery.data)
    const processed: Option[] = []
    const seenDisplayNames = new Set<string>()

    for (const tracker of trackers) {
      const lowerTracker = tracker.toLowerCase()

      if (secondaryDomains.has(lowerTracker)) {
        continue
      }

      const customization = domainToCustomization.get(lowerTracker)

      if (customization) {
        const displayKey = customization.displayName.toLowerCase()
        if (seenDisplayNames.has(displayKey)) continue
        seenDisplayNames.add(displayKey)

        const primaryDomain = customization.domains[0]
        processed.push({
          label: customization.displayName,
          value: customization.domains.join(","),
          icon: <TrackerIconImage tracker={primaryDomain} trackerIcons={trackerIcons} />,
        })
      } else {
        if (seenDisplayNames.has(lowerTracker)) continue
        seenDisplayNames.add(lowerTracker)

        processed.push({
          label: tracker,
          value: tracker,
          icon: <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />,
        })
      }
    }

    processed.sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }))

    return processed
  }, [trackersQuery.data, trackerCustomizationMaps, trackerIcons])

  const categoryOptions = useMemo(() => {
    if (!categoriesQuery.data) return []
    return buildCategorySelectOptions(categoriesQuery.data)
  }, [categoriesQuery.data])

  const tagOptions = useMemo(() => {
    if (!tagsQuery.data) return []
    return buildTagSelectOptions(tagsQuery.data)
  }, [tagsQuery.data])

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
    if (open) {
      if (rule) {
        const isAllTrackers = rule.trackerPattern === "*"
        const rawDomains = isAllTrackers ? [] : parseTrackerDomains(rule)
        const mappedDomains = mapDomainsToOptionValues(rawDomains)
        const hasConditions = rule.conditions?.schemaVersion != null

        // Parse existing conditions into simplified form
        let actionType: ActionType = "delete"
        let actionCondition: RuleCondition | null = null
        let exprUploadKiB: number | undefined
        let exprDownloadKiB: number | undefined
        let exprDeleteMode: FormState["exprDeleteMode"] = "deleteWithFilesPreserveCrossSeeds"

        if (hasConditions && rule.conditions) {
          if (rule.conditions.speedLimits?.enabled) {
            actionType = "speedLimits"
            actionCondition = rule.conditions.speedLimits.condition ?? null
            exprUploadKiB = rule.conditions.speedLimits.uploadKiB
            exprDownloadKiB = rule.conditions.speedLimits.downloadKiB
          } else if (rule.conditions.pause?.enabled) {
            actionType = "pause"
            actionCondition = rule.conditions.pause.condition ?? null
          } else if (rule.conditions.delete?.enabled) {
            actionType = "delete"
            actionCondition = rule.conditions.delete.condition ?? null
            exprDeleteMode = rule.conditions.delete.mode ?? "deleteWithFilesPreserveCrossSeeds"
          }
        }

        setFormState({
          name: rule.name,
          trackerPattern: rule.trackerPattern,
          trackerDomains: mappedDomains,
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
          useExpressions: hasConditions,
          actionType,
          actionCondition,
          exprUploadKiB,
          exprDownloadKiB,
          exprDeleteMode,
        })
      } else {
        setFormState(emptyFormState)
      }
    }
  }, [open, rule, mapDomainsToOptionValues])

  // Build payload from form state (shared by preview and save)
  const buildPayload = (input: FormState): TrackerRuleInput => {
    const payload: TrackerRuleInput = {
      ...input,
      trackerDomains: input.applyToAllTrackers ? [] : input.trackerDomains.filter(Boolean),
      trackerPattern: input.applyToAllTrackers ? "*" : input.trackerDomains.filter(Boolean).join(","),
      categories: input.categories.filter(Boolean),
      tags: input.tags.filter(Boolean),
    }

    if (input.useExpressions) {
      const conditions: ActionConditions = { schemaVersion: "1" }

      switch (input.actionType) {
        case "speedLimits":
          conditions.speedLimits = {
            enabled: true,
            uploadKiB: input.exprUploadKiB,
            downloadKiB: input.exprDownloadKiB,
            condition: input.actionCondition ?? undefined,
          }
          break
        case "pause":
          conditions.pause = {
            enabled: true,
            condition: input.actionCondition ?? undefined,
          }
          break
        case "delete":
          conditions.delete = {
            enabled: true,
            mode: input.exprDeleteMode,
            condition: input.actionCondition ?? undefined,
          }
          break
      }

      payload.conditions = conditions
      payload.uploadLimitKiB = undefined
      payload.downloadLimitKiB = undefined
      payload.ratioLimit = undefined
      payload.seedingTimeLimitMinutes = undefined
      payload.deleteMode = undefined
      payload.categories = []
      payload.tags = []
      if (input.actionType !== "delete") {
        payload.deleteUnregistered = false
      }
    } else {
      payload.conditions = undefined
    }

    return payload
  }

  // Check if current form state represents a delete rule
  const isDeleteRule = formState.useExpressions
    ? formState.actionType === "delete"
    : !!formState.deleteMode || formState.deleteUnregistered

  const previewMutation = useMutation({
    mutationFn: async (input: FormState) => {
      const payload = buildPayload(input)
      return api.previewTrackerRule(instanceId, payload)
    },
    onSuccess: (result, input) => {
      if (result.totalMatches === 0) {
        // No matches - just save without confirmation
        createOrUpdate.mutate(input)
      } else {
        // Has matches - show confirmation dialog
        setPreviewResult(result)
        setShowConfirmDialog(true)
      }
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to preview rule")
    },
  })

  const createOrUpdate = useMutation({
    mutationFn: async (input: FormState) => {
      const payload = buildPayload(input)
      if (rule) {
        return api.updateTrackerRule(instanceId, rule.id, payload)
      }
      return api.createTrackerRule(instanceId, payload)
    },
    onSuccess: () => {
      toast.success(`Tracker rule ${rule ? "updated" : "created"}`)
      setShowConfirmDialog(false)
      setPreviewResult(null)
      onOpenChange(false)
      void queryClient.invalidateQueries({ queryKey: ["tracker-rules", instanceId] })
      onSuccess?.()
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to save tracker rule")
    },
  })

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

    // Validate based on mode
    if (!formState.useExpressions) {
      // Legacy mode validation
      if (formState.deleteUnregistered && !formState.deleteMode) {
        toast.error("Unregistered cleanup requires a removal action")
        return
      }
    } else {
      // Expression mode validation - speed limits must have at least one limit set
      if (formState.actionType === "speedLimits") {
        if (formState.exprUploadKiB === undefined && formState.exprDownloadKiB === undefined) {
          toast.error("Set at least one speed limit")
          return
        }
      }
    }

    // For delete rules, show preview before saving (even when disabled, for testing)
    if (isDeleteRule) {
      previewMutation.mutate(formState)
    } else {
      createOrUpdate.mutate(formState)
    }
  }

  const handleConfirmSave = () => {
    createOrUpdate.mutate(formState)
  }

  return (
    <>
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl lg:max-w-5xl max-h-[90dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{rule ? "Edit Tracker Rule" : "Add Tracker Rule"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
          <div className="flex-1 overflow-y-auto space-y-3 pr-1">
            {/* Header row: Name + Toggles */}
            <div className="grid gap-3 lg:grid-cols-[1fr_auto_auto] lg:items-end">
              <div className="space-y-1.5">
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
              <div className="flex items-center gap-2 rounded-md border px-3 py-2">
                <Switch
                  id="use-expressions"
                  checked={formState.useExpressions}
                  onCheckedChange={(checked) => setFormState(prev => ({
                    ...prev,
                    useExpressions: checked,
                    ...(checked ? { categories: [], tags: [], tagMatchMode: "any" as const } : {}),
                  }))}
                />
                <Label htmlFor="use-expressions" className="text-sm cursor-pointer whitespace-nowrap">Advanced</Label>
              </div>
            </div>

            {/* Trackers, Categories & Tags */}
            {!formState.useExpressions ? (
              <div className={cn("grid gap-3", formState.applyToAllTrackers ? "sm:grid-cols-2" : "lg:grid-cols-3 sm:grid-cols-2")}>
                {!formState.applyToAllTrackers && (
                  <div className="space-y-1.5 sm:col-span-2 lg:col-span-1">
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
                  </div>
                )}
                <div className="space-y-1.5">
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
                <div className="space-y-1.5">
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
            ) : !formState.applyToAllTrackers && (
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
                />
              </div>
            )}

            {/* Legacy Mode */}
            {!formState.useExpressions && (
              <>
                {/* Speed Limits */}
                <div className="rounded-lg border p-3 space-y-2">
                  <h4 className="text-sm font-medium">Speed limits</h4>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <div className="space-y-1">
                      <Label htmlFor="rule-upload" className="text-xs">Upload (KiB/s)</Label>
                      <Input
                        id="rule-upload"
                        type="number"
                        min={0}
                        value={formState.uploadLimitKiB ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, uploadLimitKiB: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="Blank = no limit"
                      />
                    </div>
                    <div className="space-y-1">
                      <Label htmlFor="rule-download" className="text-xs">Download (KiB/s)</Label>
                      <Input
                        id="rule-download"
                        type="number"
                        min={0}
                        value={formState.downloadLimitKiB ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, downloadLimitKiB: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="Blank = no limit"
                      />
                    </div>
                  </div>
                </div>

                {/* Auto-removal */}
                <div className="rounded-lg border p-3 space-y-3">
                  <h4 className="text-sm font-medium">Auto-removal</h4>

                  {/* Seeding limits inline */}
                  <div className="flex flex-wrap items-end gap-2">
                    <div className="space-y-1">
                      <Label htmlFor="rule-ratio" className="text-xs">Ratio</Label>
                      <Input
                        id="rule-ratio"
                        type="number"
                        step="0.01"
                        min={0.01}
                        className="w-24"
                        value={formState.ratioLimit ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, ratioLimit: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="e.g. 2.0"
                      />
                    </div>
                    <span className="text-xs text-muted-foreground pb-2">OR</span>
                    <div className="space-y-1">
                      <Label htmlFor="rule-seedtime" className="text-xs">Seed time (min)</Label>
                      <Input
                        id="rule-seedtime"
                        type="number"
                        min={1}
                        className="w-28"
                        value={formState.seedingTimeLimitMinutes ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, seedingTimeLimitMinutes: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="e.g. 1440"
                      />
                    </div>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="flex items-center gap-2 ml-auto">
                          <Checkbox
                            id="delete-unregistered"
                            checked={formState.deleteUnregistered ?? false}
                            onCheckedChange={(checked: boolean) => setFormState(prev => ({
                              ...prev,
                              deleteUnregistered: checked,
                              deleteMode: checked && !prev.deleteMode ? "deleteWithFilesPreserveCrossSeeds" : prev.deleteMode,
                            }))}
                          />
                          <Label htmlFor="delete-unregistered" className="text-sm cursor-pointer">Unregistered</Label>
                        </div>
                      </TooltipTrigger>
                      <TooltipContent side="top" className="max-w-xs">
                        <p>Remove torrents the tracker reports as unregistered (deleted from tracker, trumped, or invalid)</p>
                      </TooltipContent>
                    </Tooltip>
                  </div>

                  {/* Action */}
                  <div className="flex items-center gap-3 pt-2 border-t">
                    <Label htmlFor="rule-delete-mode" className="text-sm shrink-0">Action:</Label>
                    <Select
                      value={formState.deleteMode ?? "none"}
                      onValueChange={(value) => setFormState(prev => ({
                        ...prev,
                        deleteMode: value === "none" ? undefined : value as "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds",
                      }))}
                    >
                      <SelectTrigger id="rule-delete-mode" className={cn("flex-1", formState.deleteMode && "text-destructive")}>
                        <SelectValue placeholder="Select action" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">Pause torrent</SelectItem>
                        <SelectItem value="delete" className="text-destructive focus:text-destructive">Remove (keep files)</SelectItem>
                        <SelectItem value="deleteWithFiles" className="text-destructive focus:text-destructive">Remove with files</SelectItem>
                        <SelectItem value="deleteWithFilesPreserveCrossSeeds" className="text-destructive focus:text-destructive">Remove with files (preserve cross-seeds)</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  {formState.deleteUnregistered && !formState.deleteMode && (
                    <p className="text-xs text-amber-500">Unregistered cleanup requires a removal action</p>
                  )}
                </div>
              </>
            )}

            {/* Expression-based Mode */}
            {formState.useExpressions && (
              <div className="space-y-3">
                {/* Query Builder */}
                <div className="space-y-1.5">
                  <Label>When conditions match</Label>
                  <QueryBuilder
                    condition={formState.actionCondition}
                    onChange={(condition) => setFormState(prev => ({ ...prev, actionCondition: condition }))}
                  />
                </div>

                {/* Action row */}
                {formState.actionType === "pause" && (
                  <div className="space-y-1 w-fit">
                    <Label className="text-xs">Action</Label>
                    <Select
                      value={formState.actionType}
                      onValueChange={(value: ActionType) => setFormState(prev => ({ ...prev, actionType: value }))}
                    >
                      <SelectTrigger className="w-[140px]">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="speedLimits">Speed limits</SelectItem>
                        <SelectItem value="pause">Pause</SelectItem>
                        <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                )}

                {formState.actionType === "speedLimits" && (
                  <div className="grid grid-cols-[auto_1fr_1fr] gap-3 items-end">
                    <div className="space-y-1">
                      <Label className="text-xs">Action</Label>
                      <Select
                        value={formState.actionType}
                        onValueChange={(value: ActionType) => setFormState(prev => ({ ...prev, actionType: value }))}
                      >
                        <SelectTrigger className="w-[140px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="speedLimits">Speed limits</SelectItem>
                          <SelectItem value="pause">Pause</SelectItem>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Upload (KiB/s)</Label>
                      <Input
                        type="number"
                        min={0}
                        value={formState.exprUploadKiB ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, exprUploadKiB: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="No limit"
                      />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Download (KiB/s)</Label>
                      <Input
                        type="number"
                        min={0}
                        value={formState.exprDownloadKiB ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, exprDownloadKiB: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="No limit"
                      />
                    </div>
                  </div>
                )}

                {formState.actionType === "delete" && (
                  <div className="grid grid-cols-[auto_1fr_auto] gap-3 items-end">
                    <div className="space-y-1">
                      <Label className="text-xs">Action</Label>
                      <Select
                        value={formState.actionType}
                        onValueChange={(value: ActionType) => setFormState(prev => ({ ...prev, actionType: value }))}
                      >
                        <SelectTrigger className="w-[140px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="speedLimits">Speed limits</SelectItem>
                          <SelectItem value="pause">Pause</SelectItem>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Mode</Label>
                      <Select
                        value={formState.exprDeleteMode}
                        onValueChange={(value: FormState["exprDeleteMode"]) => setFormState(prev => ({ ...prev, exprDeleteMode: value }))}
                      >
                        <SelectTrigger className="text-destructive">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Remove (keep files)</SelectItem>
                          <SelectItem value="deleteWithFiles" className="text-destructive focus:text-destructive">Remove with files</SelectItem>
                          <SelectItem value="deleteWithFilesPreserveCrossSeeds" className="text-destructive focus:text-destructive">Remove with files (preserve cross-seeds)</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs invisible">Unregistered</Label>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="flex items-center gap-2 h-9 px-3 rounded-md border bg-background">
                            <Checkbox
                              id="delete-unregistered-expr"
                              checked={formState.deleteUnregistered ?? false}
                              onCheckedChange={(checked: boolean) => setFormState(prev => ({ ...prev, deleteUnregistered: checked }))}
                            />
                            <Label htmlFor="delete-unregistered-expr" className="text-sm cursor-pointer whitespace-nowrap">
                              + Unregistered
                            </Label>
                          </div>
                        </TooltipTrigger>
                        <TooltipContent side="top" className="max-w-xs">
                          <p>Also remove torrents the tracker reports as unregistered (deleted from tracker, trumped, or invalid)</p>
                        </TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>

          <div className="flex items-center justify-between pt-3 border-t mt-3">
            <div className="flex items-center gap-2">
              <Switch
                id="rule-enabled"
                checked={formState.enabled ?? true}
                onCheckedChange={(checked) => setFormState(prev => ({ ...prev, enabled: checked }))}
              />
              <Label htmlFor="rule-enabled" className="text-sm font-normal cursor-pointer">Enabled</Label>
            </div>
            <div className="flex gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={createOrUpdate.isPending || previewMutation.isPending}>
                {(createOrUpdate.isPending || previewMutation.isPending) && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {rule ? "Save" : "Create"}
              </Button>
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>

    <TrackerRulePreviewDialog
      open={showConfirmDialog}
      onOpenChange={setShowConfirmDialog}
      title={
        formState.enabled
          ? "Confirm Delete Rule"
          : "Preview Delete Rule"
      }
      description={
        previewResult && previewResult.totalMatches > 0 ? (
          formState.enabled ? (
            <p className="text-destructive font-medium">
              This rule will affect {previewResult.totalMatches} torrent{previewResult.totalMatches !== 1 ? "s" : ""} that currently match
            </p>
          ) : (
            <p className="text-muted-foreground">
              {previewResult.totalMatches} torrent{previewResult.totalMatches !== 1 ? "s" : ""} would match this rule if enabled
            </p>
          )
        ) : (
          <p>No torrents currently match this rule.</p>
        )
      }
      preview={previewResult}
      onConfirm={handleConfirmSave}
      confirmLabel="Save Rule"
      isConfirming={createOrUpdate.isPending}
      destructive={formState.enabled}
    />
    </>
  )
}
