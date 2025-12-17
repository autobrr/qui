/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { cn } from "@/lib/utils"
import type { Instance, InstanceCrossSeedCompletionSettings } from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Info, Loader2 } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

interface CompletionFormState {
  enabled: boolean
  categories: string[]
  tags: string[]
  excludeCategories: string[]
  excludeTags: string[]
}

const DEFAULT_COMPLETION_FORM: CompletionFormState = {
  enabled: false,
  categories: [],
  tags: [],
  excludeCategories: [],
  excludeTags: [],
}

function settingsToForm(settings: InstanceCrossSeedCompletionSettings | undefined): CompletionFormState {
  if (!settings) return DEFAULT_COMPLETION_FORM
  return {
    enabled: settings.enabled,
    categories: settings.categories ?? [],
    tags: settings.tags ?? [],
    excludeCategories: settings.excludeCategories ?? [],
    excludeTags: settings.excludeTags ?? [],
  }
}

function formToSettings(form: CompletionFormState): Omit<InstanceCrossSeedCompletionSettings, "instanceId"> {
  return {
    enabled: form.enabled,
    categories: form.categories,
    tags: form.tags,
    excludeCategories: form.excludeCategories,
    excludeTags: form.excludeTags,
  }
}

export function CompletionOverview() {
  const queryClient = useQueryClient()
  const { instances } = useInstances()
  const [expandedInstances, setExpandedInstances] = useState<string[]>([])
  const [formMap, setFormMap] = useState<Record<number, CompletionFormState>>({})
  const [dirtyMap, setDirtyMap] = useState<Record<number, boolean>>({})

  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )

  // Fetch completion settings for all active instances
  const settingsQueries = useQueries({
    queries: activeInstances.map((instance) => ({
      queryKey: ["cross-seed", "completion", instance.id],
      queryFn: () => api.getInstanceCompletionSettings(instance.id),
      staleTime: 30000,
    })),
  })

  // Fetch categories/tags for all active instances
  const metadataQueries = useQueries({
    queries: activeInstances.map((instance) => ({
      queryKey: ["instance-metadata", instance.id],
      queryFn: async () => {
        const [categories, tags] = await Promise.all([
          api.getCategories(instance.id),
          api.getTags(instance.id),
        ])
        return { categories, tags }
      },
      staleTime: 5 * 60 * 1000,
    })),
  })

  // Mutation for updating completion settings
  const updateMutation = useMutation({
    mutationFn: ({ instanceId, settings }: { instanceId: number; settings: Omit<InstanceCrossSeedCompletionSettings, "instanceId"> }) =>
      api.updateInstanceCompletionSettings(instanceId, settings),
    onSuccess: (data, variables) => {
      queryClient.invalidateQueries({ queryKey: ["cross-seed", "completion", variables.instanceId] })
      setFormMap((prev) => ({
        ...prev,
        [variables.instanceId]: settingsToForm(data),
      }))
      setDirtyMap((prev) => ({
        ...prev,
        [variables.instanceId]: false,
      }))
      toast.success("Settings saved", {
        description: activeInstances.find((i) => i.id === variables.instanceId)?.name,
      })
    },
    onError: (error) => {
      toast.error("Failed to save settings", {
        description: error instanceof Error ? error.message : "Unknown error",
      })
    },
  })

  const handleToggleEnabled = (instance: Instance, enabled: boolean, queryIndex: number) => {
    const query = settingsQueries[queryIndex]
    // Don't allow toggle if settings haven't loaded successfully
    if (query?.isError || (!query?.data && !formMap[instance.id])) {
      toast.error("Cannot toggle - settings failed to load")
      return
    }

    const currentForm = formMap[instance.id] ?? settingsToForm(query?.data)
    updateMutation.mutate({
      instanceId: instance.id,
      settings: formToSettings({ ...currentForm, enabled }),
    })
  }

  const handleFormChange = (
    instanceId: number,
    field: keyof CompletionFormState,
    value: string[] | boolean,
    currentForm: CompletionFormState
  ) => {
    setFormMap((prev) => ({
      ...prev,
      [instanceId]: {
        ...(prev[instanceId] ?? currentForm),
        [field]: value,
      },
    }))
    setDirtyMap((prev) => ({
      ...prev,
      [instanceId]: true,
    }))
  }

  const handleSave = (instance: Instance, queryIndex: number) => {
    const query = settingsQueries[queryIndex]
    // Don't allow save if settings haven't loaded successfully
    if (query?.isError || (!query?.data && !formMap[instance.id])) {
      toast.error("Cannot save - settings failed to load")
      return
    }

    const form = formMap[instance.id] ?? settingsToForm(query?.data)
    updateMutation.mutate({
      instanceId: instance.id,
      settings: formToSettings(form),
    })
  }

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">Auto-search on completion</CardTitle>
          <CardDescription>
            No instances configured. Add one in Settings to use this feature.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (activeInstances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">Auto-search on completion</CardTitle>
          <CardDescription>
            No active instances. Enable an instance in Settings to use this feature.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-lg font-semibold">Auto-search on completion</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px]">
              <p>
                Automatically trigger a cross-seed search when torrents complete downloading.
                Torrents already tagged <span className="font-semibold">cross-seed</span> are skipped.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          Kick off a cross-seed search the moment a torrent finishes.
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
            const query = settingsQueries[index]
            const metadataQuery = metadataQueries[index]
            const isLoading = query?.isLoading ?? false
            const isError = query?.isError ?? false
            const isMetadataError = metadataQuery?.isError ?? false
            const form = formMap[instance.id] ?? settingsToForm(query?.data)
            const isEnabled = form.enabled
            const isDirty = dirtyMap[instance.id] ?? false
            const isSaving = updateMutation.isPending && updateMutation.variables?.instanceId === instance.id

            const categoryOptions = buildCategorySelectOptions(
              metadataQuery?.data?.categories ?? {},
              form.categories,
              form.excludeCategories
            )
            const tagOptions = buildTagSelectOptions(
              metadataQuery?.data?.tags ?? [],
              form.tags,
              form.excludeTags
            )

            return (
              <AccordionItem key={instance.id} value={String(instance.id)}>
                <AccordionTrigger className="px-6 py-4 hover:no-underline group">
                  <div className="flex items-center justify-between w-full pr-4">
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="font-medium truncate">{instance.name}</span>
                      {isLoading && (
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                      )}
                      {isError && (
                        <AlertCircle className="h-4 w-4 text-destructive" />
                      )}
                    </div>

                    <div className="flex items-center gap-4">
                      <div
                        className="flex items-center gap-2"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <span className={cn(
                          "text-xs font-medium",
                          isEnabled ? "text-emerald-500" : "text-muted-foreground"
                        )}>
                          {isEnabled ? "On" : "Off"}
                        </span>
                        <Switch
                          checked={isEnabled}
                          onCheckedChange={(enabled) => handleToggleEnabled(instance, enabled, index)}
                          disabled={isLoading || isSaving || isError}
                          className="scale-90"
                        />
                      </div>
                    </div>
                  </div>
                </AccordionTrigger>

                <AccordionContent className="px-6 pb-4">
                  <div className="space-y-4">
                    {/* Error state */}
                    {isError && (
                      <div className="flex items-center gap-2 p-3 rounded-lg border border-destructive/30 bg-destructive/10">
                        <AlertCircle className="h-4 w-4 text-destructive shrink-0" />
                        <p className="text-sm text-destructive">
                          Failed to load settings. Please try refreshing the page.
                        </p>
                      </div>
                    )}

                    {/* Settings form */}
                    {!isError && isEnabled && (
                      <>
                        {/* Metadata warning */}
                        {isMetadataError && (
                          <div className="flex items-center gap-2 p-3 rounded-lg border border-yellow-500/30 bg-yellow-500/10">
                            <AlertCircle className="h-4 w-4 text-yellow-500 shrink-0" />
                            <p className="text-sm text-yellow-600 dark:text-yellow-400">
                              Could not load categories and tags from qBittorrent. You can still type custom values.
                            </p>
                          </div>
                        )}

                        <div className="grid gap-4 md:grid-cols-2">
                          <div className="rounded-md border border-border/50 bg-muted/30 p-3 space-y-3">
                            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Include filters</p>
                            <div className="space-y-2">
                              <Label className="text-xs">Categories</Label>
                              <MultiSelect
                                options={categoryOptions}
                                selected={form.categories}
                                onChange={(values) => handleFormChange(instance.id, "categories", values, form)}
                                placeholder="All categories"
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">
                                {form.categories.length === 0
                                  ? "All categories will be included."
                                  : `Only ${form.categories.length} selected ${form.categories.length === 1 ? "category" : "categories"} will be matched.`}
                              </p>
                            </div>
                            <div className="space-y-2">
                              <Label className="text-xs">Tags</Label>
                              <MultiSelect
                                options={tagOptions}
                                selected={form.tags}
                                onChange={(values) => handleFormChange(instance.id, "tags", values, form)}
                                placeholder="All tags"
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">
                                {form.tags.length === 0
                                  ? "All tags will be included."
                                  : `Only ${form.tags.length} selected ${form.tags.length === 1 ? "tag" : "tags"} will be matched.`}
                              </p>
                            </div>
                          </div>

                          <div className="rounded-md border border-border/50 bg-muted/30 p-3 space-y-3">
                            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Exclude filters</p>
                            <div className="space-y-2">
                              <Label className="text-xs">Categories</Label>
                              <MultiSelect
                                options={categoryOptions}
                                selected={form.excludeCategories}
                                onChange={(values) => handleFormChange(instance.id, "excludeCategories", values, form)}
                                placeholder="None"
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">Skip torrents in these categories.</p>
                            </div>
                            <div className="space-y-2">
                              <Label className="text-xs">Tags</Label>
                              <MultiSelect
                                options={tagOptions}
                                selected={form.excludeTags}
                                onChange={(values) => handleFormChange(instance.id, "excludeTags", values, form)}
                                placeholder="None"
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">Skip torrents with these tags.</p>
                            </div>
                          </div>
                        </div>

                        <div className="flex justify-end">
                          <Button
                            onClick={() => handleSave(instance, index)}
                            disabled={isSaving || !isDirty}
                            size="sm"
                          >
                            {isSaving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                            {isDirty ? "Save changes" : "Saved"}
                          </Button>
                        </div>
                      </>
                    )}

                    {/* Disabled state */}
                    {!isError && !isEnabled && (
                      <div className="flex flex-col items-center justify-center py-6 text-center space-y-2 border border-dashed rounded-lg">
                        <p className="text-sm text-muted-foreground">
                          Enable auto-search to configure filters for this instance.
                        </p>
                      </div>
                    )}
                  </div>
                </AccordionContent>
              </AccordionItem>
            )
          })}
        </Accordion>
      </CardContent>
    </Card>
  )
}
