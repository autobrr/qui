/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  DEFAULT_RSS_INTERVAL_MINUTES,
  MIN_RSS_INTERVAL_MINUTES,
  normalizeNumberList,
  useActiveInstances,
  useAggregatedInstanceMetadata,
  useCrossSeedSettings,
  useCrossSeedStatus,
  useEnabledIndexers,
  useFormatDateValue,
  useManualRunCooldown,
  useMissingIndexersToast,
  usePatchCrossSeedSettings
} from "@/components/cross-seed/cross-seed-settings"
import { RSSRunItem } from "@/components/cross-seed/RssRunItem"
import { AutoResumeSwitch, SourceTagsField } from "@/components/cross-seed/SourceCardFields"
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
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import type { CrossSeedAutomationSettings, CrossSeedRun } from "@/types"
import { useMutation, useQuery } from "@tanstack/react-query"
import { ChevronDown, Clock, History, Loader2, Play, XCircle, Zap } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface RssFormState {
  enabled: boolean
  runIntervalMinutes: number
  targetInstanceIds: number[]
  targetIndexerIds: number[]
  rssSourceCategories: string[]
  rssSourceTags: string[]
  rssSourceExcludeCategories: string[]
  rssSourceExcludeTags: string[]
  rssAutomationTags: string[]
  skipAutoResumeRss: boolean
}

const DEFAULT_RSS_FORM: RssFormState = {
  enabled: false,
  runIntervalMinutes: DEFAULT_RSS_INTERVAL_MINUTES,
  targetInstanceIds: [],
  targetIndexerIds: [],
  rssSourceCategories: [],
  rssSourceTags: [],
  rssSourceExcludeCategories: [],
  rssSourceExcludeTags: [],
  rssAutomationTags: ["cross-seed"],
  skipAutoResumeRss: false,
}

function seedRssForm(settings: CrossSeedAutomationSettings): RssFormState {
  return {
    enabled: settings.enabled,
    runIntervalMinutes: settings.runIntervalMinutes,
    targetInstanceIds: settings.targetInstanceIds,
    targetIndexerIds: settings.targetIndexerIds,
    rssSourceCategories: settings.rssSourceCategories ?? [],
    rssSourceTags: settings.rssSourceTags ?? [],
    rssSourceExcludeCategories: settings.rssSourceExcludeCategories ?? [],
    rssSourceExcludeTags: settings.rssSourceExcludeTags ?? [],
    rssAutomationTags: settings.rssAutomationTags,
    skipAutoResumeRss: settings.skipAutoResumeRss,
  }
}

export function RssTab() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return <RssCard settings={settings} />
}

function RssCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const formatDateValue = useFormatDateValue()
  const { activeInstances } = useActiveInstances()
  const enabledIndexers = useEnabledIndexers()
  const hasEnabledIndexers = enabledIndexers.length > 0
  const { notifyMissingIndexers, handleIndexerError } = useMissingIndexersToast()
  const patchSettings = usePatchCrossSeedSettings()

  const [form, setForm] = useState<RssFormState>(() => seedRssForm(settings))
  const [dryRun, setDryRun] = useState(false)
  const [validationErrors, setValidationErrors] = useState<Record<string, string>>({})
  const [rssRunsOpen, setRssRunsOpen] = useState(false)
  const [showCancelDialog, setShowCancelDialog] = useState(false)

  const { data: status, refetch: refetchStatus } = useCrossSeedStatus()
  const { data: runs, refetch: refetchRuns } = useQuery({
    queryKey: ["cross-seed", "runs"],
    queryFn: () => api.listCrossSeedRuns({ limit: 10 }),
  })
  const { data: sourceMetadata } = useAggregatedInstanceMetadata(form.targetInstanceIds)

  const triggerRunMutation = useMutation({
    mutationFn: (payload: { dryRun?: boolean }) => api.triggerCrossSeedRun(payload),
    onSuccess: () => {
      toast.success(t("toast.automationRunStarted"))
      refetchStatus()
      refetchRuns()
    },
    onError: (error: Error) => {
      if (handleIndexerError(error, t("automation.requiresTorznabIndexer"))) {
        return
      }
      toast.error(error.message)
    },
  })

  const cancelRunMutation = useMutation({
    mutationFn: () => api.cancelCrossSeedAutomationRun(),
    onSuccess: () => {
      toast.success(t("toast.rssAutomationRunCanceled"))
      refetchStatus()
      refetchRuns()
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const handleSave = () => {
    setValidationErrors(prev => ({ ...prev, runIntervalMinutes: "", targetInstanceIds: "" }))

    if (form.enabled && form.targetInstanceIds.length === 0) {
      setValidationErrors(prev => ({ ...prev, targetInstanceIds: t("toast.selectAtLeastOneInstance") }))
      return
    }

    if (form.runIntervalMinutes < MIN_RSS_INTERVAL_MINUTES) {
      setValidationErrors(prev => ({ ...prev, runIntervalMinutes: t("validation.minInterval", { min: MIN_RSS_INTERVAL_MINUTES }) }))
      return
    }

    patchSettings.mutate({ ...form })
  }

  const latestRun: CrossSeedRun | null | undefined = status?.lastRun
  const automationRunning = status?.running ?? false
  const cooldown = useManualRunCooldown(form.runIntervalMinutes, latestRun?.startedAt)
  const hasTargets = form.targetInstanceIds.length > 0

  const runButtonDisabled = triggerRunMutation.isPending || automationRunning || cooldown.active || !hasEnabledIndexers || !hasTargets
  const runButtonDisabledReason = useMemo(() => {
    if (!hasEnabledIndexers) {
      return t("automation.requiresTorznabIndexer")
    }
    if (!hasTargets) {
      return t("toast.selectAtLeastOneInstance")
    }
    if (automationRunning) {
      return t("automation.runDisabledAlreadyRunning")
    }
    if (cooldown.active) {
      return t("automation.runDisabledCooldown", { minutes: cooldown.enforcedRunIntervalMinutes, remaining: cooldown.display })
    }
    return undefined
  }, [automationRunning, cooldown.active, cooldown.display, cooldown.enforcedRunIntervalMinutes, hasEnabledIndexers, hasTargets, t])

  const handleTriggerRun = () => {
    if (!hasEnabledIndexers) {
      notifyMissingIndexers(t("automation.requiresTorznabIndexer"))
      return
    }
    if (!hasTargets) {
      setValidationErrors(prev => ({ ...prev, targetInstanceIds: t("toast.selectAtLeastOneInstance") }))
      toast.error(t("toast.pickInstanceBeforeRunning"))
      return
    }
    const savedTargets = [...(settings.targetInstanceIds ?? [])].sort((a, b) => a - b)
    const currentTargets = [...form.targetInstanceIds].sort((a, b) => a - b)
    const targetsMatchSaved =
      savedTargets.length === currentTargets.length &&
      savedTargets.every((value, index) => value === currentTargets[index])
    if (!targetsMatchSaved) {
      toast.error(t("toast.saveSettingsFirst"))
      return
    }
    triggerRunMutation.mutate({ dryRun })
  }

  const instanceOptions = useMemo(
    () => activeInstances.map(instance => ({ label: instance.name, value: String(instance.id) })),
    [activeInstances]
  )

  const indexerOptions = useMemo(
    () => enabledIndexers.map(indexer => ({ label: indexer.name, value: String(indexer.id) })),
    [enabledIndexers]
  )

  const sourceTagNames = useMemo(() => sourceMetadata?.tags ?? [], [sourceMetadata])

  const sourceCategorySelectOptions = useMemo(
    () => buildCategorySelectOptions(
      sourceMetadata?.categories ?? {},
      form.rssSourceCategories,
      form.rssSourceExcludeCategories
    ),
    [form.rssSourceCategories, form.rssSourceExcludeCategories, sourceMetadata?.categories]
  )

  const sourceTagSelectOptions = useMemo(
    () => buildTagSelectOptions(
      sourceTagNames,
      form.rssSourceTags,
      form.rssSourceExcludeTags
    ),
    [sourceTagNames, form.rssSourceTags, form.rssSourceExcludeTags]
  )

  const groupedRuns = useMemo(() => {
    const result = {
      scheduled: [] as CrossSeedRun[],
      manual: [] as CrossSeedRun[],
      other: [] as CrossSeedRun[],
    }
    if (!runs) {
      return result
    }
    for (const run of runs) {
      if (run.triggeredBy === "scheduler") {
        result.scheduled.push(run)
      } else if (run.triggeredBy === "api") {
        result.manual.push(run)
      } else {
        result.other.push(run)
      }
    }
    // Limit each group to 5 most recent runs for cleaner display
    return {
      scheduled: result.scheduled.slice(0, 5),
      manual: result.manual.slice(0, 5),
      other: result.other.slice(0, 5),
    }
  }, [runs])

  const runSummaryStats = useMemo(() => {
    if (!runs || runs.length === 0) {
      return { totalAdded: 0, totalFailed: 0, totalRuns: 0 }
    }
    return {
      totalAdded: runs.reduce((sum, run) => sum + run.crossSeedsAdded, 0),
      totalFailed: runs.reduce((sum, run) => sum + run.candidatesFailed, 0),
      totalRuns: runs.length,
    }
  }, [runs])

  const hasTargetsSelected = form.targetInstanceIds.length > 0

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>{t("automation.title")}</CardTitle>
          <CardDescription>{t("automation.description")}</CardDescription>
          {settings.enabled && status?.nextRunAt && (
            <p className="text-xs text-muted-foreground">{t("automation.nextRun", { date: formatDateValue(status.nextRunAt) })}</p>
          )}
        </CardHeader>
        <CardContent className="space-y-5">

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="automation-enabled" className="flex items-center gap-2">
                <Switch
                  id="automation-enabled"
                  checked={form.enabled}
                  onCheckedChange={value => {
                    if (value && !hasEnabledIndexers) {
                      notifyMissingIndexers(t("toast.enableAutomationAfterIndexers"))
                      return
                    }
                    setForm(prev => ({ ...prev, enabled: !!value }))
                    if (!value && validationErrors.targetInstanceIds) {
                      setValidationErrors(prev => ({ ...prev, targetInstanceIds: "" }))
                    }
                  }}
                />
                {t("automation.enableSwitch")}
              </Label>
            </div>
          </div>

          <div className="grid gap-4">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Label htmlFor="automation-interval">{t("automation.intervalLabel")}</Label>
                <FieldHelp>{t("automation.intervalTooltip", { min: MIN_RSS_INTERVAL_MINUTES })}</FieldHelp>
              </div>
              <Input
                id="automation-interval"
                type="number"
                min={MIN_RSS_INTERVAL_MINUTES}
                value={form.runIntervalMinutes}
                onChange={event => {
                  setForm(prev => ({ ...prev, runIntervalMinutes: Number(event.target.value) }))
                  if (validationErrors.runIntervalMinutes) {
                    setValidationErrors(prev => ({ ...prev, runIntervalMinutes: "" }))
                  }
                }}
                className={validationErrors.runIntervalMinutes ? "border-destructive" : ""}
              />
              {validationErrors.runIntervalMinutes && (
                <p className="text-sm text-destructive">{validationErrors.runIntervalMinutes}</p>
              )}
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label>{t("automation.targetInstances")}</Label>
              <MultiSelect
                options={instanceOptions}
                selected={form.targetInstanceIds.map(String)}
                onChange={values => {
                  const nextIds = normalizeNumberList(values)
                  setForm(prev => ({ ...prev, targetInstanceIds: nextIds }))
                  if (nextIds.length > 0 && validationErrors.targetInstanceIds) {
                    setValidationErrors(prev => ({ ...prev, targetInstanceIds: "" }))
                  }
                }}
                placeholder={instanceOptions.length ? t("automation.selectInstances") : t("automation.noActiveInstances")}
                disabled={!instanceOptions.length}
              />
              <p className="text-xs text-muted-foreground">
                {instanceOptions.length === 0? t("automation.noInstancesAvailable"): form.targetInstanceIds.length === 0? t("automation.pickInstance"): t("automation.instancesSelected", { count: form.targetInstanceIds.length })}
              </p>
              {validationErrors.targetInstanceIds && (
                <p className="text-sm text-destructive">{validationErrors.targetInstanceIds}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label>{t("automation.targetIndexers")}</Label>
              <MultiSelect
                options={indexerOptions}
                selected={form.targetIndexerIds.map(String)}
                onChange={values => setForm(prev => ({ ...prev, targetIndexerIds: normalizeNumberList(values) }))}
                placeholder={indexerOptions.length ? t("automation.allEnabledIndexers") : t("automation.noIndexersConfigured")}
                disabled={!indexerOptions.length}
              />
              <p className="text-xs text-muted-foreground">
                {indexerOptions.length === 0? t("automation.noIndexersConfiguredDot"): form.targetIndexerIds.length === 0? t("automation.allIndexersEligible"): t("automation.selectedIndexersPolled", { count: form.targetIndexerIds.length })}
              </p>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-3">
              <Label>{t("automation.includeCategories")}</Label>
              <MultiSelect
                options={sourceCategorySelectOptions}
                selected={form.rssSourceCategories}
                onChange={values => setForm(prev => ({ ...prev, rssSourceCategories: values }))}
                placeholder={
                  hasTargetsSelected ? sourceCategorySelectOptions.length ? t("automation.allCategories") : t("automation.typeToAddCategories") : t("automation.selectInstancesToLoadCategories")
                }
                creatable
                disabled={!hasTargetsSelected}
              />
              <p className="text-xs text-muted-foreground">
                {form.rssSourceCategories.length === 0 ? t("automation.allCategoriesIncluded") : t("automation.selectedCategoriesMatched", { count: form.rssSourceCategories.length })}
              </p>
            </div>

            <div className="space-y-3">
              <Label>{t("automation.includeTags")}</Label>
              <MultiSelect
                options={sourceTagSelectOptions}
                selected={form.rssSourceTags}
                onChange={values => setForm(prev => ({ ...prev, rssSourceTags: values }))}
                placeholder={
                  hasTargetsSelected ? sourceTagSelectOptions.length ? t("automation.allTags") : t("automation.typeToAddTags") : t("automation.selectInstancesToLoadTags")
                }
                creatable
                disabled={!hasTargetsSelected}
              />
              <p className="text-xs text-muted-foreground">
                {form.rssSourceTags.length === 0 ? t("automation.allTagsIncluded") : t("automation.selectedTagsMatched", { count: form.rssSourceTags.length })}
              </p>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-3">
              <Label>{t("automation.excludeCategories")}</Label>
              <MultiSelect
                options={sourceCategorySelectOptions}
                selected={form.rssSourceExcludeCategories}
                onChange={values => setForm(prev => ({ ...prev, rssSourceExcludeCategories: values }))}
                placeholder={hasTargetsSelected ? t("automation.none") : t("automation.selectInstancesToLoadCategories")}
                creatable
                disabled={!hasTargetsSelected}
              />
              <p className="text-xs text-muted-foreground">
                {form.rssSourceExcludeCategories.length === 0 ? t("automation.noCategoriesExcluded") : t("automation.categoriesSkipped", { count: form.rssSourceExcludeCategories.length })}
              </p>
            </div>

            <div className="space-y-3">
              <Label>{t("automation.excludeTags")}</Label>
              <MultiSelect
                options={sourceTagSelectOptions}
                selected={form.rssSourceExcludeTags}
                onChange={values => setForm(prev => ({ ...prev, rssSourceExcludeTags: values }))}
                placeholder={hasTargetsSelected ? t("automation.none") : t("automation.selectInstancesToLoadTags")}
                creatable
                disabled={!hasTargetsSelected}
              />
              <p className="text-xs text-muted-foreground">
                {form.rssSourceExcludeTags.length === 0 ? t("automation.noTagsExcluded") : t("automation.tagsSkipped", { count: form.rssSourceExcludeTags.length })}
              </p>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <SourceTagsField
              id="rss-automation-tags"
              suggestions={[{ label: t("rules.tagging.tagRss"), value: "rss" }]}
              selected={form.rssAutomationTags}
              onChange={rssAutomationTags => setForm(prev => ({ ...prev, rssAutomationTags }))}
              placeholder={t("rules.tagging.selectRssTags")}
              help={t("rules.tagging.rssTagsDescription")}
            />
            <AutoResumeSwitch
              id="auto-resume-rss"
              skip={form.skipAutoResumeRss}
              onSkipChange={skipAutoResumeRss => setForm(prev => ({ ...prev, skipAutoResumeRss }))}
            />
          </div>

          <Separator />

          <Collapsible open={rssRunsOpen} onOpenChange={setRssRunsOpen}>
            <div className="rounded-xl border bg-card text-card-foreground shadow-sm">
              <CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-4 hover:cursor-pointer text-left hover:bg-muted/50 transition-colors rounded-xl">
                <div className="flex items-center gap-2">
                  <History className="h-4 w-4 text-muted-foreground" />
                  <span className="text-sm font-medium">{t("automation.recentRssRuns")}</span>
                  {runs && runs.length > 0 ? (
                    <Badge variant="secondary" className="text-xs">
                      {runSummaryStats.totalFailed > 0? t("automation.runSummaryWithFailures", { runs: runSummaryStats.totalRuns, added: runSummaryStats.totalAdded, failed: runSummaryStats.totalFailed }): t("automation.runSummary", { runs: runSummaryStats.totalRuns, added: runSummaryStats.totalAdded })}
                    </Badge>
                  ) : (
                    <span className="text-xs text-muted-foreground">{t("automation.noRunsYet")}</span>
                  )}
                </div>
                <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${rssRunsOpen ? "rotate-180" : ""}`} />
              </CollapsibleTrigger>

              <CollapsibleContent>
                <div className="px-4 pb-3 space-y-3">
                  {runs && runs.length > 0 ? (
                    <div className="space-y-4">
                      {groupedRuns.scheduled.length > 0 && (
                        <div className="space-y-2">
                          <div className="flex items-center gap-2 text-sm font-medium">
                            <Clock className="h-4 w-4 text-blue-500" />
                            {t("automation.scheduled")} ({groupedRuns.scheduled.length})
                          </div>
                          <div className="space-y-1">
                            {groupedRuns.scheduled.map(run => (
                              <RSSRunItem key={run.id} run={run} formatDateValue={formatDateValue} />
                            ))}
                          </div>
                        </div>
                      )}

                      {groupedRuns.manual.length > 0 && (
                        <div className="space-y-2">
                          <div className="flex items-center gap-2 text-sm font-medium">
                            <Zap className="h-4 w-4 text-yellow-500" />
                            {t("automation.manual")} ({groupedRuns.manual.length})
                          </div>
                          <div className="space-y-1">
                            {groupedRuns.manual.map(run => (
                              <RSSRunItem key={run.id} run={run} formatDateValue={formatDateValue} />
                            ))}
                          </div>
                        </div>
                      )}

                      {groupedRuns.other.length > 0 && (
                        <div className="space-y-2">
                          <div className="flex items-center gap-2 text-sm font-medium">
                            <History className="h-4 w-4 text-muted-foreground" />
                            {t("automation.other")} ({groupedRuns.other.length})
                          </div>
                          <div className="space-y-1">
                            {groupedRuns.other.map(run => (
                              <RSSRunItem key={run.id} run={run} formatDateValue={formatDateValue} />
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="text-center py-2 text-xs text-muted-foreground">
                      {t("automation.noRunsRecorded")}
                    </div>
                  )}
                </div>
              </CollapsibleContent>
            </div>
          </Collapsible>
        </CardContent>
        <CardFooter className="flex flex-col-reverse gap-3 md:flex-row md:items-center md:justify-between">
          <div className="flex items-center gap-2 text-xs">
            <Switch id="automation-dry-run" checked={dryRun} onCheckedChange={value => setDryRun(!!value)} />
            <Label htmlFor="automation-dry-run">{t("automation.dryRun")}</Label>
          </div>
          <div className="flex flex-col gap-2 w-full md:w-auto md:flex-row">
            {automationRunning ? (
              <Button
                variant="outline"
                className="min-h-11 md:min-h-9"
                onClick={() => setShowCancelDialog(true)}
                disabled={cancelRunMutation.isPending}
              >
                {cancelRunMutation.isPending ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    {t("automation.stopping")}
                  </>
                ) : (
                  <>
                    <XCircle className="mr-2 h-4 w-4" />
                    {t("automation.cancel")}
                  </>
                )}
              </Button>
            ) : (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    className="min-h-11 md:min-h-9 disabled:cursor-not-allowed disabled:pointer-events-auto"
                    onClick={handleTriggerRun}
                    disabled={runButtonDisabled}
                  >
                    {triggerRunMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Play className="mr-2 h-4 w-4" />}
                    {t("automation.runNow")}
                  </Button>
                </TooltipTrigger>
                {runButtonDisabledReason && (
                  <TooltipContent align="end" className="max-w-xs text-xs">
                    {runButtonDisabledReason}
                  </TooltipContent>
                )}
              </Tooltip>
            )}
            <Button className="min-h-11 md:min-h-9" onClick={handleSave} disabled={patchSettings.isPending}>
              {patchSettings.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t("automation.saveSettings")}
            </Button>
            <Button variant="outline" className="min-h-11 md:min-h-9" onClick={() => setForm(DEFAULT_RSS_FORM)}>
              {t("automation.reset")}
            </Button>
          </div>
        </CardFooter>
      </Card>

      <AlertDialog open={showCancelDialog} onOpenChange={setShowCancelDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("cancelDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("cancelDialog.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("cancelDialog.keepRunning")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => cancelRunMutation.mutate()}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("cancelDialog.cancelRun")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
