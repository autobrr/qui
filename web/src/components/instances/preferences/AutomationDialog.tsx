/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryBuilder } from "@/components/query-builder"
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
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { buildCategorySelectOptions } from "@/lib/category-utils"
import { parseTrackerDomains } from "@/lib/utils"
import type {
  ActionConditions,
  Automation,
  AutomationInput,
  AutomationPreviewResult,
  RuleCondition,
} from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Folder, Loader2 } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { AutomationPreviewDialog } from "./AutomationPreviewDialog"

interface AutomationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  /** Rule to edit, or null to create a new rule */
  rule: Automation | null
  onSuccess?: () => void
}

type ActionType = "speedLimits" | "shareLimits" | "pause" | "delete" | "tag" | "category"

type FormState = {
  name: string
  trackerPattern: string
  trackerDomains: string[]
  applyToAllTrackers: boolean
  enabled: boolean
  sortOrder?: number
  // Single action with condition
  actionType: ActionType
  actionCondition: RuleCondition | null
  // Speed limits settings
  exprUploadKiB?: number
  exprDownloadKiB?: number
  // Share limits settings
  exprRatioLimit?: number
  exprSeedingTimeMinutes?: number
  // Delete settings
  exprDeleteMode: "delete" | "deleteWithFiles" | "deleteWithFilesPreserveCrossSeeds"
  // Tag action settings
  exprTags: string[]
  exprTagMode: "full" | "add" | "remove"
  // Category action settings
  exprCategory: string
  exprIncludeCrossSeeds: boolean
}

const emptyFormState: FormState = {
  name: "",
  trackerPattern: "",
  trackerDomains: [],
  applyToAllTrackers: false,
  enabled: false,
  actionType: "pause",
  actionCondition: null,
  exprUploadKiB: undefined,
  exprDownloadKiB: undefined,
  exprRatioLimit: undefined,
  exprSeedingTimeMinutes: undefined,
  exprDeleteMode: "delete",
  exprTags: [],
  exprTagMode: "full",
  exprCategory: "",
  exprIncludeCrossSeeds: false,
}

export function AutomationDialog({ open, onOpenChange, instanceId, rule, onSuccess }: AutomationDialogProps) {
  const queryClient = useQueryClient()
  const [formState, setFormState] = useState<FormState>(emptyFormState)
  const [previewResult, setPreviewResult] = useState<AutomationPreviewResult | null>(null)
  const [previewInput, setPreviewInput] = useState<FormState | null>(null)
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [enabledBeforePreview, setEnabledBeforePreview] = useState<boolean | null>(null)
  const previewPageSize = 25

  const trackersQuery = useInstanceTrackers(instanceId, { enabled: open })
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const { data: metadata } = useInstanceMetadata(instanceId)

  // Build category options for the category action dropdown
  const categoryOptions = useMemo(() => {
    if (!metadata?.categories) return []
    return buildCategorySelectOptions(metadata.categories, [formState.exprCategory].filter(Boolean))
  }, [metadata?.categories, formState.exprCategory])

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

        // Parse existing conditions into simplified form
        let actionType: ActionType = "delete"
        let actionCondition: RuleCondition | null = null
        let exprUploadKiB: number | undefined
        let exprDownloadKiB: number | undefined
        let exprRatioLimit: number | undefined
        let exprSeedingTimeMinutes: number | undefined
        let exprDeleteMode: FormState["exprDeleteMode"] = "deleteWithFilesPreserveCrossSeeds"
        let exprTags: string[] = []
        let exprTagMode: FormState["exprTagMode"] = "full"
        let exprCategory = ""
        let exprIncludeCrossSeeds = false

        const conditions = rule.conditions
        if (conditions) {
          if (conditions.speedLimits?.enabled) {
            actionType = "speedLimits"
            actionCondition = conditions.speedLimits.condition ?? null
            exprUploadKiB = conditions.speedLimits.uploadKiB
            exprDownloadKiB = conditions.speedLimits.downloadKiB
          } else if (conditions.shareLimits?.enabled) {
            actionType = "shareLimits"
            actionCondition = conditions.shareLimits.condition ?? null
            exprRatioLimit = conditions.shareLimits.ratioLimit
            exprSeedingTimeMinutes = conditions.shareLimits.seedingTimeMinutes
          } else if (conditions.pause?.enabled) {
            actionType = "pause"
            actionCondition = conditions.pause.condition ?? null
          } else if (conditions.delete?.enabled) {
            actionType = "delete"
            actionCondition = conditions.delete.condition ?? null
            exprDeleteMode = conditions.delete.mode ?? "deleteWithFilesPreserveCrossSeeds"
          } else if (conditions.tag?.enabled) {
            actionType = "tag"
            actionCondition = conditions.tag.condition ?? null
            exprTags = conditions.tag.tags ?? []
            exprTagMode = conditions.tag.mode ?? "full"
          } else if (conditions.category?.enabled) {
            actionType = "category"
            actionCondition = conditions.category.condition ?? null
            exprCategory = conditions.category.category ?? ""
            exprIncludeCrossSeeds = conditions.category.includeCrossSeeds ?? false
          }
        }

        setFormState({
          name: rule.name,
          trackerPattern: rule.trackerPattern,
          trackerDomains: mappedDomains,
          applyToAllTrackers: isAllTrackers,
          enabled: rule.enabled,
          sortOrder: rule.sortOrder,
          actionType,
          actionCondition,
          exprUploadKiB,
          exprDownloadKiB,
          exprRatioLimit,
          exprSeedingTimeMinutes,
          exprDeleteMode,
          exprTags,
          exprTagMode,
          exprCategory,
          exprIncludeCrossSeeds,
        })
      } else {
        setFormState(emptyFormState)
      }
    }
  }, [open, rule, mapDomainsToOptionValues])

  // Build payload from form state (shared by preview and save)
  const buildPayload = (input: FormState): AutomationInput => {
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
      case "shareLimits":
        conditions.shareLimits = {
          enabled: true,
          ratioLimit: input.exprRatioLimit,
          seedingTimeMinutes: input.exprSeedingTimeMinutes,
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
      case "tag":
        conditions.tag = {
          enabled: true,
          tags: input.exprTags,
          mode: input.exprTagMode,
          condition: input.actionCondition ?? undefined,
        }
        break
      case "category":
        conditions.category = {
          enabled: true,
          category: input.exprCategory,
          includeCrossSeeds: input.exprIncludeCrossSeeds,
          condition: input.actionCondition ?? undefined,
        }
        break
    }

    return {
      name: input.name,
      trackerDomains: input.applyToAllTrackers ? [] : input.trackerDomains.filter(Boolean),
      trackerPattern: input.applyToAllTrackers ? "*" : input.trackerDomains.filter(Boolean).join(","),
      enabled: input.enabled,
      sortOrder: input.sortOrder,
      conditions,
    }
  }

  // Check if current form state represents a delete or category rule (both need previews)
  const isDeleteRule = formState.actionType === "delete"
  const isCategoryRule = formState.actionType === "category"

  const handleActionTypeChange = (value: ActionType) => {
    setFormState(prev => {
      const next: FormState = { ...prev, actionType: value }

      // Safety: when switching to delete in "create new" mode, start disabled.
      if (!rule && value === "delete") {
        next.enabled = false
      }

      return next
    })
  }

  const previewMutation = useMutation({
    mutationFn: async (input: FormState) => {
      const payload = {
        ...buildPayload(input),
        previewLimit: previewPageSize,
        previewOffset: 0,
      }
      return api.previewAutomation(instanceId, payload)
    },
    onSuccess: (result, input) => {
      // Last warning before enabling a delete rule (even if 0 matches right now).
      setPreviewInput(input)
      setPreviewResult(result)
      setShowConfirmDialog(true)
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to preview rule")
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
      }
      return api.previewAutomation(instanceId, payload)
    },
    onSuccess: (result) => {
      setPreviewResult(prev => prev
        ? { ...prev, examples: [...prev.examples, ...result.examples], totalMatches: result.totalMatches }
        : result
      )
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

  const createOrUpdate = useMutation({
    mutationFn: async (input: FormState) => {
      const payload = buildPayload(input)
      if (rule) {
        return api.updateAutomation(instanceId, rule.id, payload)
      }
      return api.createAutomation(instanceId, payload)
    },
    onSuccess: () => {
      toast.success(`Automation ${rule ? "updated" : "created"}`)
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

    // Action-specific validation
    if (formState.actionType === "speedLimits") {
      if (formState.exprUploadKiB === undefined && formState.exprDownloadKiB === undefined) {
        toast.error("Set at least one speed limit")
        return
      }
    }
    if (formState.actionType === "shareLimits") {
      if (formState.exprRatioLimit === undefined && formState.exprSeedingTimeMinutes === undefined) {
        toast.error("Set ratio limit or seeding time")
        return
      }
    }
    if (formState.actionType === "tag") {
      if (formState.exprTags.length === 0) {
        toast.error("Specify at least one tag")
        return
      }
    }
    if (formState.actionType === "category") {
      if (!formState.exprCategory) {
        toast.error("Select a category")
        return
      }
    }

    // For delete and category rules, show preview as a last warning before enabling.
    const needsPreview = (isDeleteRule || isCategoryRule) && formState.enabled
    if (needsPreview) {
      previewMutation.mutate(formState)
    } else {
      createOrUpdate.mutate(formState)
    }
  }

  const handleConfirmSave = () => {
    // Clear the stored value so onOpenChange won't restore it after successful save
    setEnabledBeforePreview(null)
    createOrUpdate.mutate(formState)
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-4xl lg:max-w-5xl max-h-[90dvh] flex flex-col">
          <DialogHeader>
            <DialogTitle>{rule ? "Edit Automation Rule" : "Add Automation Rule"}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
            <div className="flex-1 overflow-y-auto space-y-3 pr-1">
              {/* Header row: Name + All Trackers toggle */}
              <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
                <div className="space-y-1.5">
                  <Label htmlFor="rule-name">Name</Label>
                  <Input
                    id="rule-name"
                    value={formState.name}
                    onChange={(e) => setFormState(prev => ({ ...prev, name: e.target.value }))}
                    required
                    placeholder="Automation rule"
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
                  />
                </div>
              )}

              {/* Condition and Action */}
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
                      onValueChange={handleActionTypeChange}
                    >
                      <SelectTrigger className="w-[140px]">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="speedLimits">Speed limits</SelectItem>
                        <SelectItem value="shareLimits">Share limits</SelectItem>
                        <SelectItem value="pause">Pause</SelectItem>
                        <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                        <SelectItem value="tag">Tag</SelectItem>
                        <SelectItem value="category">Category</SelectItem>
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
                        onValueChange={handleActionTypeChange}
                      >
                        <SelectTrigger className="w-[140px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="speedLimits">Speed limits</SelectItem>
                          <SelectItem value="shareLimits">Share limits</SelectItem>
                          <SelectItem value="pause">Pause</SelectItem>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                          <SelectItem value="tag">Tag</SelectItem>
                          <SelectItem value="category">Category</SelectItem>
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

                {formState.actionType === "shareLimits" && (
                  <div className="grid grid-cols-[auto_1fr_1fr] gap-3 items-end">
                    <div className="space-y-1">
                      <Label className="text-xs">Action</Label>
                      <Select
                        value={formState.actionType}
                        onValueChange={handleActionTypeChange}
                      >
                        <SelectTrigger className="w-[140px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="speedLimits">Speed limits</SelectItem>
                          <SelectItem value="shareLimits">Share limits</SelectItem>
                          <SelectItem value="pause">Pause</SelectItem>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                          <SelectItem value="tag">Tag</SelectItem>
                          <SelectItem value="category">Category</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Ratio limit</Label>
                      <Input
                        type="number"
                        step="0.01"
                        min={0}
                        value={formState.exprRatioLimit ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, exprRatioLimit: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="e.g. 2.0"
                      />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Seed time (min)</Label>
                      <Input
                        type="number"
                        min={0}
                        value={formState.exprSeedingTimeMinutes ?? ""}
                        onChange={(e) => setFormState(prev => ({ ...prev, exprSeedingTimeMinutes: e.target.value ? Number(e.target.value) : undefined }))}
                        placeholder="e.g. 1440"
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
                        onValueChange={handleActionTypeChange}
                      >
                        <SelectTrigger className="w-[140px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="speedLimits">Speed limits</SelectItem>
                          <SelectItem value="shareLimits">Share limits</SelectItem>
                          <SelectItem value="pause">Pause</SelectItem>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                          <SelectItem value="tag">Tag</SelectItem>
                          <SelectItem value="category">Category</SelectItem>
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
                  </div>
                )}

                {formState.actionType === "tag" && (
                  <div className="grid grid-cols-[auto_1fr_auto] gap-3 items-start">
                    <div className="space-y-1">
                      <Label className="text-xs">Action</Label>
                      <Select
                        value={formState.actionType}
                        onValueChange={handleActionTypeChange}
                      >
                        <SelectTrigger className="w-[140px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="speedLimits">Speed limits</SelectItem>
                          <SelectItem value="shareLimits">Share limits</SelectItem>
                          <SelectItem value="pause">Pause</SelectItem>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                          <SelectItem value="tag">Tag</SelectItem>
                          <SelectItem value="category">Category</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Tags</Label>
                      <Input
                        type="text"
                        value={formState.exprTags.join(", ")}
                        onChange={(e) => {
                          const tags = e.target.value.split(",").map(t => t.trim()).filter(Boolean)
                          setFormState(prev => ({ ...prev, exprTags: tags }))
                        }}
                        placeholder="tag1, tag2, ..."
                      />
                    </div>
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
                )}

                {formState.actionType === "category" && (
                  <div className="flex items-center gap-3">
                    <div className="space-y-1">
                      <Label className="text-xs">Action</Label>
                      <Select
                        value={formState.actionType}
                        onValueChange={handleActionTypeChange}
                      >
                        <SelectTrigger className="w-[140px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="speedLimits">Speed limits</SelectItem>
                          <SelectItem value="shareLimits">Share limits</SelectItem>
                          <SelectItem value="pause">Pause</SelectItem>
                          <SelectItem value="delete" className="text-destructive focus:text-destructive">Delete</SelectItem>
                          <SelectItem value="tag">Tag</SelectItem>
                          <SelectItem value="category">Category</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Move to category</Label>
                      <Select
                        value={formState.exprCategory}
                        onValueChange={(value) => setFormState(prev => ({ ...prev, exprCategory: value }))}
                      >
                        <SelectTrigger>
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
                )}
              </div>
            </div>

            <div className="flex items-center justify-between pt-3 border-t mt-3">
              <div className="flex items-center gap-2">
                <Switch
                  id="rule-enabled"
                  checked={formState.enabled ?? true}
                  onCheckedChange={(checked) => {
                    // When enabling a delete or category rule, show preview first
                    if (checked && (isDeleteRule || isCategoryRule)) {
                      setEnabledBeforePreview(formState.enabled)
                      const nextState = { ...formState, enabled: true }
                      setFormState(nextState)
                      previewMutation.mutate(nextState)
                    } else {
                      setFormState(prev => ({ ...prev, enabled: checked }))
                    }
                  }}
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

      <AutomationPreviewDialog
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
          }
          setShowConfirmDialog(open)
        }}
        title={
          isDeleteRule
            ? (formState.enabled ? "Confirm Delete Rule" : "Preview Delete Rule")
            : `Confirm Category Change → ${previewInput?.exprCategory ?? formState.exprCategory}`
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
        onConfirm={handleConfirmSave}
        onLoadMore={handleLoadMore}
        isLoadingMore={loadMorePreview.isPending}
        confirmLabel="Save Rule"
        isConfirming={createOrUpdate.isPending}
        destructive={isDeleteRule && formState.enabled}
        warning={isCategoryRule}
      />
    </>
  )
}
