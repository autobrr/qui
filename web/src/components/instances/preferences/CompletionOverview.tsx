/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { cn } from "@/lib/utils"
import type { Instance, InstanceCrossSeedCompletionSettings } from "@/types"
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Info, Loader2 } from "lucide-react"
import { useMemo, useState } from "react"
import { Trans, useTranslation } from "react-i18next"
import { toast } from "sonner"

const MAX_COMPLETION_DELAY_SECONDS = 600

interface CompletionFormState {
  enabled: boolean
  categories: string[]
  tags: string[]
  excludeCategories: string[]
  excludeTags: string[]
  indexerIds: number[]
  bypassTorznabCache: boolean
  delaySeconds: number
}

const DEFAULT_COMPLETION_FORM: CompletionFormState = {
  enabled: false,
  categories: [],
  tags: [],
  excludeCategories: [],
  excludeTags: [],
  indexerIds: [],
  bypassTorznabCache: false,
  delaySeconds: 0,
}

function settingsToForm(settings: InstanceCrossSeedCompletionSettings | undefined): CompletionFormState {
  if (!settings) return DEFAULT_COMPLETION_FORM
  return {
    enabled: settings.enabled,
    categories: settings.categories ?? [],
    tags: settings.tags ?? [],
    excludeCategories: settings.excludeCategories ?? [],
    excludeTags: settings.excludeTags ?? [],
    indexerIds: settings.indexerIds ?? [],
    bypassTorznabCache: settings.bypassTorznabCache ?? false,
    delaySeconds: settings.delaySeconds ?? 0,
  }
}

function formToSettings(form: CompletionFormState): Omit<InstanceCrossSeedCompletionSettings, "instanceId"> {
  return {
    enabled: form.enabled,
    categories: form.categories,
    tags: form.tags,
    excludeCategories: form.excludeCategories,
    excludeTags: form.excludeTags,
    indexerIds: form.indexerIds,
    bypassTorznabCache: form.bypassTorznabCache,
    delaySeconds: form.delaySeconds,
  }
}

function normalizeNumberList(values: Array<string | number>): number[] {
  const normalized: number[] = []
  const seen = new Set<number>()
  values.forEach((value) => {
    const parsed = typeof value === "number" ? value : Number(value)
    if (!Number.isFinite(parsed) || parsed <= 0 || seen.has(parsed)) return
    seen.add(parsed)
    normalized.push(parsed)
  })
  return normalized
}

export function CompletionOverview() {
  const { t } = useTranslation("instances")
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

  const indexersQuery = useQuery({
    queryKey: ["torznab-indexers"],
    queryFn: () => api.listTorznabIndexers(),
    staleTime: 5 * 60 * 1000,
  })

  const enabledIndexers = useMemo(
    () => (indexersQuery.data ?? []).filter((indexer) => indexer.enabled),
    [indexersQuery.data]
  )
  const indexerOptions = useMemo(
    () => enabledIndexers.map((indexer) => ({ label: indexer.name, value: String(indexer.id) })),
    [enabledIndexers]
  )
  const hasEnabledIndexers = enabledIndexers.length > 0

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
      toast.success(t("preferences.completionOverview.toast.settingsSaved"), {
        description: activeInstances.find((i) => i.id === variables.instanceId)?.name,
      })
    },
    onError: (error) => {
      toast.error(t("preferences.completionOverview.toast.saveFailed"), {
        description: error instanceof Error ? error.message : "Unknown error",
      })
    },
  })

  const handleToggleEnabled = (instance: Instance, enabled: boolean, queryIndex: number) => {
    const query = settingsQueries[queryIndex]
    // Don't allow toggle if settings haven't loaded successfully
    if (query?.isError || (!query?.data && !formMap[instance.id])) {
      toast.error(t("preferences.completionOverview.cannotToggle"))
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
    value: string[] | number[] | boolean | number,
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
      toast.error(t("preferences.completionOverview.cannotSave"))
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
          <CardTitle className="text-lg font-semibold">{t("preferences.completionOverview.title")}</CardTitle>
          <CardDescription>
            {t("preferences.completionOverview.noInstancesDescription")}
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (activeInstances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">{t("preferences.completionOverview.title")}</CardTitle>
          <CardDescription>
            {t("preferences.completionOverview.noActiveInstances")}
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-lg font-semibold">{t("preferences.completionOverview.title")}</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px]">
              <p>
                <Trans
                  ns="instances"
                  i18nKey="preferences.completionOverview.tooltip"
                  components={{ strong: <span className="font-semibold" /> }}
                />
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          {t("preferences.completionOverview.description")}
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
                          {isEnabled ? t("preferences.completionOverview.on") : t("preferences.completionOverview.off")}
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
                          {t("preferences.completionOverview.failedToLoadSettings")}
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
                              {t("preferences.completionOverview.metadataWarning")}
                            </p>
                          </div>
                        )}
                        {indexersQuery.isError && (
                          <div className="flex items-center gap-2 p-3 rounded-lg border border-yellow-500/30 bg-yellow-500/10">
                            <AlertCircle className="h-4 w-4 text-yellow-500 shrink-0" />
                            <p className="text-sm text-yellow-600 dark:text-yellow-400">
                              {t("preferences.completionOverview.indexerWarning")}
                            </p>
                          </div>
                        )}
                        {!indexersQuery.isError && !indexersQuery.isPending && !hasEnabledIndexers && (
                          <div className="flex items-center gap-2 p-3 rounded-lg border border-yellow-500/30 bg-yellow-500/10">
                            <AlertCircle className="h-4 w-4 text-yellow-500 shrink-0" />
                            <p className="text-sm text-yellow-600 dark:text-yellow-400">
                              {t("preferences.completionOverview.noIndexersWarning")}
                            </p>
                          </div>
                        )}

                        <div className="flex items-center justify-between rounded-md border border-border/50 bg-muted/30 p-3">
                          <div className="space-y-0.5">
                            <Label className="text-sm font-medium">{t("preferences.completionOverview.bypassTorznabCache")}</Label>
                            <p className="text-xs text-muted-foreground">
                              {t("preferences.completionOverview.bypassTorznabCacheDescription")}
                            </p>
                          </div>
                          <Switch
                            checked={form.bypassTorznabCache}
                            onCheckedChange={(checked) => handleFormChange(instance.id, "bypassTorznabCache", checked, form)}
                            disabled={isSaving}
                          />
                        </div>

                        <div className="flex items-center justify-between gap-4 rounded-md border border-border/50 bg-muted/30 p-3">
                          <div className="space-y-0.5">
                            <Label htmlFor={`completion-delay-${instance.id}`} className="text-sm font-medium">
                              {t("preferences.completionOverview.searchDelay")}
                            </Label>
                            <p className="text-xs text-muted-foreground">
                              {t("preferences.completionOverview.searchDelayDescription")}
                            </p>
                          </div>
                          <Input
                            id={`completion-delay-${instance.id}`}
                            type="number"
                            min={0}
                            max={MAX_COMPLETION_DELAY_SECONDS}
                            step={1}
                            value={form.delaySeconds}
                            onChange={(event) => {
                              const raw = event.target.value
                              const parsed = raw === "" ? 0 : Number(raw)
                              if (!Number.isFinite(parsed)) return
                              const clamped = Math.min(MAX_COMPLETION_DELAY_SECONDS, Math.max(0, Math.floor(parsed)))
                              handleFormChange(instance.id, "delaySeconds", clamped, form)
                            }}
                            disabled={isSaving}
                            className="w-24 text-right"
                          />
                        </div>

                        <div className="grid gap-4 md:grid-cols-2">
                          <div className="rounded-md border border-border/50 bg-muted/30 p-3 space-y-3">
                            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("preferences.completionOverview.includeFilters")}</p>
                            <div className="space-y-2">
                              <Label className="text-xs">{t("preferences.completionOverview.categories")}</Label>
                              <MultiSelect
                                options={categoryOptions}
                                selected={form.categories}
                                onChange={(values) => handleFormChange(instance.id, "categories", values, form)}
                                placeholder={t("preferences.completionOverview.allCategories")}
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">
                                {form.categories.length === 0? t("preferences.completionOverview.allCategoriesIncluded"): t("preferences.completionOverview.selectedCategories", { count: form.categories.length })}
                              </p>
                            </div>
                            <div className="space-y-2">
                              <Label className="text-xs">{t("preferences.completionOverview.tags")}</Label>
                              <MultiSelect
                                options={tagOptions}
                                selected={form.tags}
                                onChange={(values) => handleFormChange(instance.id, "tags", values, form)}
                                placeholder={t("preferences.completionOverview.allTags")}
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">
                                {form.tags.length === 0? t("preferences.completionOverview.allTagsIncluded"): t("preferences.completionOverview.selectedTags", { count: form.tags.length })}
                              </p>
                            </div>
                            <div className="space-y-2">
                              <Label className="text-xs">{t("preferences.completionOverview.indexers")}</Label>
                              <MultiSelect
                                options={indexerOptions}
                                selected={form.indexerIds.map(String)}
                                onChange={(values) => handleFormChange(instance.id, "indexerIds", normalizeNumberList(values), form)}
                                placeholder={t("preferences.completionOverview.allIndexers")}
                                disabled={isSaving || indexersQuery.isPending || (!hasEnabledIndexers && !indexersQuery.isPending)}
                              />
                              <p className="text-xs text-muted-foreground">
                                {form.indexerIds.length === 0? t("preferences.completionOverview.allIndexersSearched"): t("preferences.completionOverview.selectedIndexers", { count: form.indexerIds.length })}
                              </p>
                            </div>
                          </div>

                          <div className="rounded-md border border-border/50 bg-muted/30 p-3 space-y-3">
                            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("preferences.completionOverview.excludeFilters")}</p>
                            <div className="space-y-2">
                              <Label className="text-xs">{t("preferences.completionOverview.categories")}</Label>
                              <MultiSelect
                                options={categoryOptions}
                                selected={form.excludeCategories}
                                onChange={(values) => handleFormChange(instance.id, "excludeCategories", values, form)}
                                placeholder={t("preferences.completionOverview.none")}
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">{t("preferences.completionOverview.skipCategoriesDescription")}</p>
                            </div>
                            <div className="space-y-2">
                              <Label className="text-xs">{t("preferences.completionOverview.tags")}</Label>
                              <MultiSelect
                                options={tagOptions}
                                selected={form.excludeTags}
                                onChange={(values) => handleFormChange(instance.id, "excludeTags", values, form)}
                                placeholder={t("preferences.completionOverview.none")}
                                creatable
                                disabled={isSaving}
                              />
                              <p className="text-xs text-muted-foreground">{t("preferences.completionOverview.skipTagsDescription")}</p>
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
                            {isDirty ? t("preferences.completionOverview.saveChanges") : t("preferences.completionOverview.saved")}
                          </Button>
                        </div>
                      </>
                    )}

                    {/* Disabled state */}
                    {!isError && !isEnabled && (
                      <div className="flex flex-col items-center justify-center py-6 text-center space-y-2 border border-dashed rounded-lg">
                        <p className="text-sm text-muted-foreground">
                          {t("preferences.completionOverview.enableAutoSearchMessage")}
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
