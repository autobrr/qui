/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
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
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { cn, parseTrackerDomains } from "@/lib/utils"
import type { TrackerRule, TrackerRuleInput } from "@/types"
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

export function TrackerRuleDialog({ open, onOpenChange, instanceId, rule, onSuccess }: TrackerRuleDialogProps) {
  const queryClient = useQueryClient()
  const [formState, setFormState] = useState<FormState>(emptyFormState)

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
        // Secondary domains (not the first one) should be hidden/merged
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

      // Skip secondary domains - they're merged into their primary
      if (secondaryDomains.has(lowerTracker)) {
        continue
      }

      const customization = domainToCustomization.get(lowerTracker)

      if (customization) {
        // Use displayName as uniqueness key for merged trackers
        const displayKey = customization.displayName.toLowerCase()
        if (seenDisplayNames.has(displayKey)) continue
        seenDisplayNames.add(displayKey)

        const primaryDomain = customization.domains[0]
        processed.push({
          label: customization.displayName,
          // Store all domains as comma-separated value for the rule pattern
          value: customization.domains.join(","),
          icon: <TrackerIconImage tracker={primaryDomain} trackerIcons={trackerIcons} />,
        })
      } else {
        // No customization - use domain as-is
        if (seenDisplayNames.has(lowerTracker)) continue
        seenDisplayNames.add(lowerTracker)

        processed.push({
          label: tracker,
          value: tracker,
          icon: <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />,
        })
      }
    }

    // Sort by display name (case-insensitive)
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
  // e.g., if a rule has "domain1,domain2" and there's a merged option with value "domain1,domain2",
  // this returns ["domain1,domain2"] instead of ["domain1", "domain2"]
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
          // Use the merged value (all domains comma-separated)
          const mergedValue = customization.domains.join(",")
          if (!result.includes(mergedValue)) {
            result.push(mergedValue)
          }
          // Mark all domains in this group as processed
          for (const d of customization.domains) {
            processed.add(d.toLowerCase())
          }
        } else {
          // Not part of a merged group, use as-is
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
        // Map to merged option values if customizations are loaded
        const mappedDomains = mapDomainsToOptionValues(rawDomains)
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
        })
      } else {
        setFormState(emptyFormState)
      }
    }
  }, [open, rule, mapDomainsToOptionValues])

  const createOrUpdate = useMutation({
    mutationFn: async (input: FormState) => {
      if (rule) {
        return api.updateTrackerRule(instanceId, rule.id, input)
      }
      return api.createTrackerRule(instanceId, input)
    },
    onSuccess: () => {
      toast.success(`Tracker rule ${rule ? "updated" : "created"}`)
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
    const payload = {
      ...formState,
      trackerDomains: formState.applyToAllTrackers ? [] : selectedTrackers,
      trackerPattern: formState.applyToAllTrackers ? "*" : selectedTrackers.join(","),
      categories: formState.categories.filter(Boolean),
      tags: formState.tags.filter(Boolean),
    }
    createOrUpdate.mutate(payload)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl max-h-[90dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{rule ? "Edit Tracker Rule" : "Add Tracker Rule"}</DialogTitle>
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
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createOrUpdate.isPending}>
                {createOrUpdate.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {rule ? "Save changes" : "Create rule"}
              </Button>
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
