/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  CROSS_SEED_SEARCH_SETTINGS_KEY,
  CROSS_SEED_SETTINGS_KEY,
  CROSS_SEED_STATUS_KEY,
  normalizeNumberList,
  normalizeStringList,
  useActiveInstances,
  useAggregatedInstanceMetadata,
  useCrossSeedSearchSettings,
  useCrossSeedSearchStatus,
  useCrossSeedSettings,
  useEnabledIndexers,
  useFormatDateValue,
  useMissingIndexersToast
} from "@/components/cross-seed/cross-seed-settings"
import { AutoResumeSwitch, SourceTagsField } from "@/components/cross-seed/SourceCardFields"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { parseNonNegativeInt } from "@/lib/cross-seed-utils"
import type {
  CrossSeedAutomationSettings,
  CrossSeedSearchResult,
  CrossSeedSearchSettings,
  Instance
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, CheckCircle2, ChevronDown, Clock, History, Loader2, Rocket, XCircle } from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const MIN_SEEDED_SEARCH_INTERVAL_SECONDS = 60
const MIN_GAZELLE_ONLY_SEARCH_INTERVAL_SECONDS = 5  // Gazelle-only seeded search: still be polite; per-torrent work can trigger multiple API calls
const MIN_SEEDED_SEARCH_COOLDOWN_MINUTES = 720

function isGazelleOnlyTorznabIndexer(indexerName: string, indexerID: string, baseURL: string) {
  const haystack = `${indexerName} ${indexerID} ${baseURL}`.toLowerCase()
  return /(^|[^a-z0-9])(ops|orpheus|opsfet|redacted|flacsfor)([^a-z0-9]|$)/.test(haystack)
}

function isCrossSeedSearchFailure(result: CrossSeedSearchResult): boolean {
  return result.status === "failed"
}

function isCrossSeedSearchSkipped(result: CrossSeedSearchResult): boolean {
  return result.status === "skipped"
}

interface LibraryTabProps {
  onOpenGazelleSettings: () => void
}

export function LibraryTab({ onOpenGazelleSettings }: LibraryTabProps) {
  const { data: settings } = useCrossSeedSettings()
  const { data: searchSettings } = useCrossSeedSearchSettings()
  const { instances } = useActiveInstances()
  if (!settings || !searchSettings || !instances) {
    return null
  }
  return (
    <LibraryCard
      settings={settings}
      searchSettings={searchSettings}
      instances={instances}
      onOpenGazelleSettings={onOpenGazelleSettings}
    />
  )
}

interface LibraryCardProps extends LibraryTabProps {
  settings: CrossSeedAutomationSettings
  searchSettings: CrossSeedSearchSettings
  instances: Instance[]
}

function LibraryCard({ settings, searchSettings, instances, onOpenGazelleSettings }: LibraryCardProps) {
  const { t } = useTranslation("crossseed")
  const queryClient = useQueryClient()
  const formatDateValue = useFormatDateValue()
  const enabledIndexers = useEnabledIndexers()
  const hasEnabledIndexers = enabledIndexers.length > 0
  const { handleIndexerError } = useMissingIndexersToast()

  const [searchInstanceId, setSearchInstanceId] = useState<number | null>(searchSettings.instanceId ?? instances[0]?.id ?? null)
  const [searchCategories, setSearchCategories] = useState(() => normalizeStringList(searchSettings.categories ?? []))
  const [searchTags, setSearchTags] = useState(() => normalizeStringList(searchSettings.tags ?? []))
  const [searchIndexerIds, setSearchIndexerIds] = useState<number[]>(searchSettings.indexerIds ?? [])
  const [searchIntervalSeconds, setSearchIntervalSeconds] = useState(searchSettings.intervalSeconds ?? MIN_SEEDED_SEARCH_INTERVAL_SECONDS)
  const [searchCooldownMinutes, setSearchCooldownMinutes] = useState(searchSettings.cooldownMinutes ?? MIN_SEEDED_SEARCH_COOLDOWN_MINUTES)
  const [seededSearchTags, setSeededSearchTags] = useState<string[]>(settings.seededSearchTags)
  const [skipAutoResume, setSkipAutoResume] = useState(settings.skipAutoResumeSeededSearch)
  const [seededSearchTorznabEnabled, setSeededSearchTorznabEnabled] = useState(true)
  const [skipIndividualEpisodes, setSkipIndividualEpisodes] = useState(false)
  const [maxAddedAgeDays, setMaxAddedAgeDays] = useState(0)
  const [validationErrors, setValidationErrors] = useState<Record<string, string>>({})
  const [searchResultsOpen, setSearchResultsOpen] = useState(false)

  const { data: searchStatus, refetch: refetchSearchStatus } = useCrossSeedSearchStatus()

  const searchRunsRefetchInterval =
    searchStatus?.running && searchStatus.run?.instanceId === searchInstanceId ? 5_000 : false

  const { data: searchRuns, refetch: refetchSearchRuns } = useQuery({
    queryKey: ["cross-seed", "search-runs", searchInstanceId],
    queryFn: () => searchInstanceId ? api.listCrossSeedSearchRuns(searchInstanceId, { limit: 10 }) : Promise.resolve([]),
    enabled: !!searchInstanceId,
    refetchInterval: searchRunsRefetchInterval,
  })

  const activeSearchInstanceIdRef = useRef<number | null>(null)

  useEffect(() => {
    const isRunning = searchStatus?.running ?? false
    const activeInstanceId = searchStatus?.run?.instanceId

    if (isRunning && activeInstanceId != null) {
      activeSearchInstanceIdRef.current = activeInstanceId
      return
    }

    if (activeSearchInstanceIdRef.current != null && activeSearchInstanceIdRef.current === searchInstanceId) {
      void refetchSearchRuns()
    }
    activeSearchInstanceIdRef.current = null
  }, [refetchSearchRuns, searchInstanceId, searchStatus?.running, searchStatus?.run?.instanceId])

  const searchInstanceIds = useMemo(() => searchInstanceId ? [searchInstanceId] : [], [searchInstanceId])
  const { data: searchMetadata } = useAggregatedInstanceMetadata(searchInstanceIds)

  const saveMutation = useMutation({
    mutationFn: async () => {
      const [search, global] = await Promise.all([
        api.patchCrossSeedSearchSettings({
          instanceId: searchInstanceId,
          categories: searchCategories,
          tags: searchTags,
          indexerIds: seededSearchEffectiveIndexerIds,
          intervalSeconds: searchIntervalSeconds,
          cooldownMinutes: searchCooldownMinutes,
        }),
        api.patchCrossSeedSettings({
          seededSearchTags,
          skipAutoResumeSeededSearch: skipAutoResume,
        }),
      ])
      return { search, global }
    },
    onSuccess: ({ search, global }) => {
      toast.success(t("toast.settingsUpdated"))
      queryClient.setQueryData(CROSS_SEED_SEARCH_SETTINGS_KEY, search)
      queryClient.setQueryData(CROSS_SEED_SETTINGS_KEY, global)
      void queryClient.refetchQueries({ queryKey: CROSS_SEED_STATUS_KEY })
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const startSearchRunMutation = useMutation({
    mutationFn: (payload: Parameters<typeof api.startCrossSeedSearchRun>[0]) => api.startCrossSeedSearchRun(payload),
    onSuccess: () => {
      toast.success(t("toast.searchRunStarted"))
      refetchSearchStatus()
      refetchSearchRuns()
    },
    onError: (error: Error) => {
      if (handleIndexerError(error, t("scan.searchNeedsTorznabUnlessGazelle"))) {
        return
      }
      toast.error(error.message)
    },
  })

  const cancelSearchRunMutation = useMutation({
    mutationFn: () => api.cancelCrossSeedSearchRun(),
    onSuccess: () => {
      toast.success(t("toast.searchRunCanceled"))
      refetchSearchStatus()
      refetchSearchRuns()
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const searchRunning = searchStatus?.running ?? false
  const activeSearchRun = searchStatus?.run

  const gazelleSavedEnabled = settings.gazelleEnabled ?? false
  const gazelleSavedHasOpsKey = Boolean((settings.orpheusApiKey ?? "").trim())
  const gazelleSavedHasRedKey = Boolean((settings.redactedApiKey ?? "").trim())
  const gazelleSavedConfigured = gazelleSavedEnabled && (gazelleSavedHasOpsKey || gazelleSavedHasRedKey)
  const gazelleSavedFullyConfigured = gazelleSavedEnabled && gazelleSavedHasOpsKey && gazelleSavedHasRedKey

  const seededSearchForceGazelleOnly = useMemo(() => {
    if (!seededSearchTorznabEnabled) {
      return false
    }
    if (!gazelleSavedFullyConfigured) {
      return false
    }
    if (searchIndexerIds.length === 0) {
      return false
    }

    const selected = new Set(searchIndexerIds)
    let hasSelection = false
    for (const idx of enabledIndexers) {
      if (!selected.has(idx.id)) {
        continue
      }
      hasSelection = true
      if (!isGazelleOnlyTorznabIndexer(idx.name, idx.indexer_id, idx.base_url)) {
        return false
      }
    }
    return hasSelection
  }, [enabledIndexers, gazelleSavedFullyConfigured, searchIndexerIds, seededSearchTorznabEnabled])

  const seededSearchTorznabEffectiveEnabled = seededSearchTorznabEnabled && !seededSearchForceGazelleOnly

  const startSearchRunDisabled = !searchInstanceId || startSearchRunMutation.isPending || searchRunning || (seededSearchTorznabEffectiveEnabled ? (!hasEnabledIndexers && !gazelleSavedConfigured) : !gazelleSavedConfigured)
  const startSearchRunDisabledReason = useMemo(() => {
    if (!seededSearchTorznabEffectiveEnabled && !gazelleSavedConfigured) {
      return t("toast.enableGazelleDescription")
    }
    if (!hasEnabledIndexers && !gazelleSavedConfigured) {
      return t("toast.configureTorznabOrGazelle")
    }
    return undefined
  }, [gazelleSavedConfigured, hasEnabledIndexers, seededSearchTorznabEffectiveEnabled, t])
  const seededSearchIntervalMinimum = useMemo(() => {
    if (!seededSearchTorznabEffectiveEnabled && gazelleSavedConfigured) {
      return MIN_GAZELLE_ONLY_SEARCH_INTERVAL_SECONDS
    }
    return MIN_SEEDED_SEARCH_INTERVAL_SECONDS
  }, [gazelleSavedConfigured, seededSearchTorznabEffectiveEnabled])

  useEffect(() => {
    setSearchIntervalSeconds(prev => (prev < seededSearchIntervalMinimum ? seededSearchIntervalMinimum : prev))
  }, [seededSearchIntervalMinimum])

  const seededSearchIndexerExclusions = useMemo(() => {
    const disallowedIDs = new Set<number>()
    if (!gazelleSavedFullyConfigured) {
      return disallowedIDs
    }
    for (const idx of enabledIndexers) {
      if (isGazelleOnlyTorznabIndexer(idx.name, idx.indexer_id, idx.base_url)) {
        disallowedIDs.add(idx.id)
      }
    }
    return disallowedIDs
  }, [enabledIndexers, gazelleSavedFullyConfigured])

  const seededSearchIndexerOptions = useMemo(
    () => (gazelleSavedFullyConfigured ? enabledIndexers.filter(idx => !isGazelleOnlyTorznabIndexer(idx.name, idx.indexer_id, idx.base_url)) : enabledIndexers)
      .map(indexer => ({ label: indexer.name, value: String(indexer.id) })),
    [enabledIndexers, gazelleSavedFullyConfigured]
  )

  const seededSearchHasOnlyGazelleIndexers = useMemo(() => (
    enabledIndexers.length > 0 &&
    seededSearchIndexerOptions.length === 0 &&
    seededSearchIndexerExclusions.size > 0
  ), [enabledIndexers.length, seededSearchIndexerOptions.length, seededSearchIndexerExclusions.size])

  const seededSearchIndexerPlaceholder = useMemo(() => {
    if (!seededSearchTorznabEffectiveEnabled) {
      if (seededSearchForceGazelleOnly) {
        return t("scan.indexers.placeholderTorznabSkippedGazelleOnly")
      }
      return gazelleSavedConfigured ? t("scan.indexers.placeholderTorznabDisabledGazelleOnly") : t("scan.indexers.placeholderTorznabDisabledEnableGazelle")
    }
    if (seededSearchIndexerOptions.length > 0) {
      return gazelleSavedFullyConfigured ? t("scan.indexers.placeholderAllEnabledNonOpsRed") : t("scan.indexers.placeholderAllEnabled")
    }
    if (seededSearchHasOnlyGazelleIndexers) {
      return t("scan.indexers.placeholderOnlyOpsRedEnabled")
    }
    return t("scan.indexers.placeholderNoTorznabConfigured")
  }, [gazelleSavedConfigured, gazelleSavedFullyConfigured, seededSearchForceGazelleOnly, seededSearchHasOnlyGazelleIndexers, seededSearchIndexerOptions.length, seededSearchTorznabEffectiveEnabled, t])

  const seededSearchEffectiveIndexerIds = useMemo(() => {
    const allAllowed = enabledIndexers
      .filter(idx => !seededSearchIndexerExclusions.has(idx.id))
      .map(idx => idx.id)

    if (searchIndexerIds.length === 0) {
      if (seededSearchIndexerExclusions.size === 0) {
        return []
      }
      // Backend treats [] as "all enabled"; when exclusions exist, send an explicit list.
      return allAllowed
    }
    if (seededSearchIndexerExclusions.size === 0) {
      return searchIndexerIds
    }

    const filtered = searchIndexerIds.filter(id => !seededSearchIndexerExclusions.has(id))
    if (filtered.length === 0) {
      // Keep empty selection so we can preserve user intent by running Gazelle-only.
      return []
    }
    return filtered
  }, [enabledIndexers, searchIndexerIds, seededSearchIndexerExclusions])

  const seededSearchIndexerHelpText = useMemo(() => {
    if (!seededSearchTorznabEffectiveEnabled) {
      if (seededSearchForceGazelleOnly) {
        return t("scan.indexers.helpTorznabSkippedGazelleOnly")
      }
      if (gazelleSavedConfigured) {
        return t("scan.indexers.helpTorznabDisabledGazelleCheck")
      }
      return t("scan.indexers.helpTorznabDisabledEnableGazelle")
    }

    if (seededSearchIndexerOptions.length === 0) {
      if (seededSearchHasOnlyGazelleIndexers) {
        return t("scan.indexers.helpOnlyOpsRedEnabled")
      }

      if (gazelleSavedConfigured) {
        return t("scan.indexers.helpNoNonOpsRedTorznab")
      }
      return t("scan.indexers.helpNoTorznabConfigured")
    }

    if (seededSearchEffectiveIndexerIds.length === 0) {
      if (gazelleSavedConfigured) {
        return t("scan.indexers.helpAllEnabledNonOpsRedQueried")
      }
      return t("scan.indexers.helpAllEnabledTorznabQueried")
    }
    if (gazelleSavedConfigured) {
      return t("scan.indexers.helpSelectedTorznabQueriedGazelle", { count: seededSearchEffectiveIndexerIds.length })
    }
    return t("scan.indexers.helpSelectedQueried", { count: seededSearchEffectiveIndexerIds.length })
  }, [gazelleSavedConfigured, seededSearchEffectiveIndexerIds.length, seededSearchForceGazelleOnly, seededSearchHasOnlyGazelleIndexers, seededSearchIndexerOptions.length, seededSearchTorznabEffectiveEnabled, t])

  const seededSearchGazelleStatus = useMemo(() => {
    if (!settings.gazelleEnabled) {
      return t("scan.gazelleStatus.disabled")
    }
    const ops = (settings.orpheusApiKey ?? "").trim() !== ""
    const red = (settings.redactedApiKey ?? "").trim() !== ""
    if (ops && red) return t("scan.gazelleStatus.enabledOpsRed")
    if (ops) return t("scan.gazelleStatus.enabledOpsMissingRed")
    if (red) return t("scan.gazelleStatus.enabledRedMissingOps")
    return t("scan.gazelleStatus.enabledKeysMissing")
  }, [settings, t])
  const seededSearchGazelleOnlyMode = !seededSearchTorznabEffectiveEnabled && gazelleSavedConfigured

  const seededSearchIntervalPresets = seededSearchGazelleOnlyMode ? [10, 30, 60] : [60, 120, 300]

  const seededSearchFlowSummary = gazelleSavedConfigured ? (gazelleSavedFullyConfigured ? t("overview.seededSearch.gazelleFullDescription") : t("overview.seededSearch.gazellePartialDescription")) : t("overview.seededSearch.noGazelleDescription")

  const searchTagNames = useMemo(() => searchMetadata?.tags ?? [], [searchMetadata])

  const searchCategorySelectOptions = useMemo(
    () => buildCategorySelectOptions(searchMetadata?.categories ?? {}, searchCategories),
    [searchCategories, searchMetadata?.categories]
  )

  const searchTagSelectOptions = useMemo(
    () => buildTagSelectOptions(searchTagNames, searchTags),
    [searchTagNames, searchTags]
  )

  const validateTiming = (): boolean => {
    const errors: Record<string, string> = {}
    if (searchIntervalSeconds < seededSearchIntervalMinimum) {
      errors.searchIntervalSeconds = t("validation.minIntervalSeconds", { min: seededSearchIntervalMinimum })
    }
    if (searchCooldownMinutes < MIN_SEEDED_SEARCH_COOLDOWN_MINUTES) {
      errors.searchCooldownMinutes = t("validation.minCooldown", { min: MIN_SEEDED_SEARCH_COOLDOWN_MINUTES })
    }
    setValidationErrors(errors)
    return Object.keys(errors).length === 0
  }

  const handleSave = () => {
    if (!validateTiming()) return
    saveMutation.mutate()
  }

  const handleStartSearchRun = () => {
    setValidationErrors({})

    if (!seededSearchTorznabEffectiveEnabled && !gazelleSavedConfigured) {
      toast.error(t("toast.seededSearchNeedsGazelle"), {
        description: t("toast.enableGazelleDescription"),
      })
      return
    }

    if (!hasEnabledIndexers && !gazelleSavedConfigured) {
      toast.error(t("toast.seededSearchNeedsTorznabOrGazelle"), {
        description: t("toast.configureTorznabOrGazelle"),
      })
      return
    }

    if (!searchInstanceId) {
      toast.error(t("toast.selectInstanceToRun"))
      return
    }

    if (!validateTiming()) return

    startSearchRunMutation.mutate({
      instanceId: searchInstanceId,
      categories: searchCategories,
      tags: searchTags,
      intervalSeconds: searchIntervalSeconds,
      indexerIds: seededSearchTorznabEffectiveEnabled ? seededSearchEffectiveIndexerIds : [],
      disableTorznab: !seededSearchTorznabEffectiveEnabled,
      cooldownMinutes: searchCooldownMinutes,
      skipIndividualEpisodes,
      maxAddedAgeDays,
    })
  }

  const estimatedCompletionInfo = useMemo(() => {
    if (!activeSearchRun) {
      return null
    }
    const total = activeSearchRun.totalTorrents ?? 0
    const interval = searchStatus?.effectiveIntervalSeconds ?? activeSearchRun.intervalSeconds ?? 0
    if (total === 0 || interval <= 0) {
      return null
    }
    const remaining = Math.max(total - activeSearchRun.processed, 0)
    if (remaining === 0) {
      return null
    }
    const eta = new Date(Date.now() + remaining * interval * 1000)
    return { eta, remaining, interval }
  }, [activeSearchRun, searchStatus?.effectiveIntervalSeconds])

  const searchRunStats = useMemo(() => {
    if (!searchRuns || searchRuns.length === 0) {
      return { totalAdded: 0, totalFailed: 0, totalRuns: 0 }
    }
    return {
      totalAdded: searchRuns.reduce((sum, run) => sum + run.crossSeedsAdded, 0),
      totalFailed: searchRuns.reduce((sum, run) => sum + run.torrentsFailed, 0),
      totalRuns: searchRuns.length,
    }
  }, [searchRuns])

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("scan.title")}</CardTitle>
        <CardDescription>{t("scan.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <Alert className="border-destructive/20 bg-destructive/10 text-destructive mb-8">
          <AlertTriangle className="h-4 w-4 !text-destructive" />
          <AlertTitle>{t("scan.runSparingly")}</AlertTitle>
          <AlertDescription>
            {t("scan.runSparinglyDescription")}
          </AlertDescription>
        </Alert>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="search-interval">{t("scan.intervalLabel")}</Label>
              <FieldHelp>
                {t("scan.waitTimeDescription", { min: seededSearchIntervalMinimum })}
                {seededSearchGazelleOnlyMode && t("scan.gazelleOnlyNote")}
              </FieldHelp>
            </div>
            <Input
              id="search-interval"
              type="number"
              min={seededSearchIntervalMinimum}
              value={searchIntervalSeconds}
              onChange={event => {
                setSearchIntervalSeconds(Number(event.target.value) || seededSearchIntervalMinimum)
                if (validationErrors.searchIntervalSeconds) {
                  setValidationErrors(prev => ({ ...prev, searchIntervalSeconds: "" }))
                }
              }}
              className={validationErrors.searchIntervalSeconds ? "border-destructive" : ""}
            />
            {validationErrors.searchIntervalSeconds && (
              <p className="text-sm text-destructive">{validationErrors.searchIntervalSeconds}</p>
            )}
            <div className="flex flex-wrap gap-2">
              {seededSearchIntervalPresets.map(seconds => (
                <Button
                  key={seconds}
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setSearchIntervalSeconds(seconds)}
                  disabled={seconds < seededSearchIntervalMinimum}
                >
                  {seconds}s
                </Button>
              ))}
            </div>
          </div>
          <div className="space-y-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="search-cooldown">{t("scan.cooldownLabel")}</Label>
              <FieldHelp>{t("scan.cooldownDescription", { min: MIN_SEEDED_SEARCH_COOLDOWN_MINUTES })}</FieldHelp>
            </div>
            <Input
              id="search-cooldown"
              type="number"
              min={MIN_SEEDED_SEARCH_COOLDOWN_MINUTES}
              value={searchCooldownMinutes}
              onChange={event => {
                setSearchCooldownMinutes(Number(event.target.value) || MIN_SEEDED_SEARCH_COOLDOWN_MINUTES)
                if (validationErrors.searchCooldownMinutes) {
                  setValidationErrors(prev => ({ ...prev, searchCooldownMinutes: "" }))
                }
              }}
              className={validationErrors.searchCooldownMinutes ? "border-destructive" : ""}
            />
            {validationErrors.searchCooldownMinutes && (
              <p className="text-sm text-destructive">{validationErrors.searchCooldownMinutes}</p>
            )}
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-3">
            <Label>{t("scan.categories")}</Label>
            <MultiSelect
              options={searchCategorySelectOptions}
              selected={searchCategories}
              onChange={values => setSearchCategories(normalizeStringList(values))}
              placeholder={
                searchInstanceId ? searchCategorySelectOptions.length ? t("scan.allCategoriesAll") : t("automation.typeToAddCategories") : t("scan.selectInstanceToLoadCategories")
              }
              creatable
              onCreateOption={value => setSearchCategories(prev => normalizeStringList([...prev, value]))}
              disabled={!searchInstanceId}
            />
            <p className="text-xs text-muted-foreground">
              {searchInstanceId && searchCategorySelectOptions.length === 0 ? t("scan.categoriesLoadAfterInstance") : searchCategories.length === 0 ? t("scan.allCategoriesIncluded") : t("scan.selectedCategoriesScanned", { count: searchCategories.length })}
            </p>
          </div>

          <div className="space-y-3">
            <Label>{t("scan.tags")}</Label>
            <MultiSelect
              options={searchTagSelectOptions}
              selected={searchTags}
              onChange={values => setSearchTags(normalizeStringList(values))}
              placeholder={
                searchInstanceId ? searchTagSelectOptions.length ? t("scan.allTagsAll") : t("automation.typeToAddTags") : t("scan.selectInstanceToLoadTags")
              }
              creatable
              onCreateOption={value => setSearchTags(prev => normalizeStringList([...prev, value]))}
              disabled={!searchInstanceId}
            />
            <p className="text-xs text-muted-foreground">
              {searchInstanceId && searchTagSelectOptions.length === 0 ? t("scan.tagsLoadAfterInstance") : searchTags.length === 0 ? t("scan.allTagsIncluded") : t("scan.selectedTagsScanned", { count: searchTags.length })}
            </p>
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-3">
            <Label>{t("scan.sourceInstance")}</Label>
            <Select
              value={searchInstanceId ? String(searchInstanceId) : ""}
              onValueChange={(value) => setSearchInstanceId(Number(value))}
              disabled={!instances.length}
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder={t("scan.selectAnInstance")} />
              </SelectTrigger>
              <SelectContent>
                {instances.map(instance => (
                  <SelectItem key={instance.id} value={String(instance.id)}>
                    {instance.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {!instances.length && (
              <p className="text-xs text-muted-foreground">{t("scan.addInstanceToSearch")}</p>
            )}
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <Label>
                {gazelleSavedConfigured ? t("scan.torznabIndexersNonOpsRed") : t("scan.torznabIndexers")}
              </Label>
              <div className="flex items-center gap-2">
                <span className="text-xs text-muted-foreground">{t("scan.torznab")}</span>
                <Switch checked={seededSearchTorznabEnabled} onCheckedChange={value => setSeededSearchTorznabEnabled(!!value)} />
              </div>
            </div>
            <MultiSelect
              options={seededSearchIndexerOptions}
              selected={seededSearchEffectiveIndexerIds.map(String)}
              onChange={values => setSearchIndexerIds(normalizeNumberList(values))}
              placeholder={seededSearchIndexerPlaceholder}
              disabled={!seededSearchIndexerOptions.length || !seededSearchTorznabEnabled}
            />
            <p className="text-xs text-muted-foreground">
              {seededSearchIndexerHelpText}
            </p>
            <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
              <span>{seededSearchFlowSummary}</span>
              <button
                type="button"
                onClick={onOpenGazelleSettings}
                className="underline underline-offset-2 hover:text-foreground"
              >
                {seededSearchGazelleStatus}
              </button>
            </div>
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="flex items-center justify-between gap-3">
            <div className="space-y-0.5">
              <div className="flex items-center gap-1.5">
                <Label htmlFor="skip-individual-episodes" className="font-medium">{t("scan.skipIndividualEpisodesLabel")}</Label>
                <FieldHelp>{t("scan.skipIndividualEpisodesDescription")}</FieldHelp>
              </div>
              {skipIndividualEpisodes && settings.seasonPackAutomationEnabled === false && (
                <p className="text-xs text-muted-foreground">{t("scan.skipIndividualEpisodesAutomationOff")}</p>
              )}
            </div>
            <Switch
              id="skip-individual-episodes"
              checked={skipIndividualEpisodes}
              onCheckedChange={value => setSkipIndividualEpisodes(!!value)}
            />
          </div>
          <div className="space-y-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="max-added-age-days">{t("scan.maxAddedAgeLabel")}</Label>
              <FieldHelp>{t("scan.maxAddedAgeDescription")}</FieldHelp>
            </div>
            <Input
              id="max-added-age-days"
              type="number"
              min={0}
              value={maxAddedAgeDays}
              onChange={event => setMaxAddedAgeDays(parseNonNegativeInt(event.target.value))}
            />
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <SourceTagsField
            id="seeded-search-tags"
            suggestions={[{ label: t("rules.tagging.tagSeededSearch"), value: "seeded-search" }]}
            selected={seededSearchTags}
            onChange={setSeededSearchTags}
            placeholder={t("rules.tagging.selectSeededTags")}
            help={t("rules.tagging.seededTagsDescription")}
          />
          <AutoResumeSwitch
            id="auto-resume-seeded-search"
            skip={skipAutoResume}
            onSkipChange={setSkipAutoResume}
            help={t("rules.postInjection.seededSearchDescription")}
          />
        </div>

        <Separator />

        {activeSearchRun && (
          <div className="rounded-lg border bg-muted/50 p-4 space-y-3">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">{t("scan.status")}</p>
              <Badge variant={searchRunning ? "default" : "secondary"}>{searchRunning ? t("scan.runningUpper") : t("scan.idleUpper")}</Badge>
            </div>
            {searchStatus?.currentTorrent && (
              <div className="text-xs">
                <span className="text-muted-foreground">{t("scan.currentlyProcessing")}</span>{" "}
                <span className="font-medium">{searchStatus.currentTorrent.torrentName}</span>
              </div>
            )}
            <div className="grid gap-2 text-xs">
              <div className="flex items-center gap-4">
                <span className="text-muted-foreground flex items-center gap-1">
                  {t("scan.progress")}
                  <FieldHelp>{t("scan.dueCandidatesHelp")}</FieldHelp>
                </span>
                <span className="font-medium">{t("scan.torrentsProgress", { processed: activeSearchRun.processed, total: activeSearchRun.totalTorrents || "?" })}</span>
              </div>
              <div className="flex items-center gap-4">
                <span className="text-muted-foreground">{t("scan.results")}</span>
                <span className="font-medium">
                  {t("scan.resultsDetail", { added: activeSearchRun.torrentsWithCrossSeeds, skipped: activeSearchRun.torrentsSkipped, failed: activeSearchRun.torrentsFailed })}
                </span>
              </div>
              <div className="flex items-center gap-4">
                <span className="text-muted-foreground">{t("scan.crossSeedsAdded")}</span>
                <span className="font-medium">{activeSearchRun.crossSeedsAdded}</span>
              </div>
              <div className="flex items-center gap-4">
                <span className="text-muted-foreground">{t("scan.started")}</span>
                <span className="font-medium">{formatDateValue(activeSearchRun.startedAt)}</span>
              </div>
              {estimatedCompletionInfo && (
                <div className="flex items-center gap-4">
                  <span className="text-muted-foreground">{t("scan.estCompletion")}</span>
                  <span className="font-medium">
                    {formatDateValue(estimatedCompletionInfo.eta)}
                    <span className="text-xs text-muted-foreground font-normal ml-2">
                      {t("scan.etaDetail", { remaining: estimatedCompletionInfo.remaining, interval: estimatedCompletionInfo.interval })}
                    </span>
                  </span>
                </div>
              )}
            </div>
          </div>
        )}

        <Collapsible open={searchResultsOpen} onOpenChange={setSearchResultsOpen}>
          <div className="rounded-xl border bg-card text-card-foreground shadow-sm">
            <CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-4 hover:cursor-pointer text-left hover:bg-muted/50 transition-colors rounded-xl">
              <div className="flex items-center gap-2">
                <History className="h-4 w-4 text-muted-foreground" />
                <span className="text-sm font-medium">{t("scan.recentRuns")}</span>
                {searchRunStats.totalRuns > 0 ? (
                  <Badge variant="secondary" className="text-xs">
                    {searchRunStats.totalFailed > 0? t("scan.runSummaryWithFailures", { runs: searchRunStats.totalRuns, added: searchRunStats.totalAdded, failed: searchRunStats.totalFailed }): t("scan.runSummary", { runs: searchRunStats.totalRuns, added: searchRunStats.totalAdded })}
                  </Badge>
                ) : (
                  <span className="text-xs text-muted-foreground">{t("scan.noRunsYet")}</span>
                )}
              </div>
              <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${searchResultsOpen ? "rotate-180" : ""}`} />
            </CollapsibleTrigger>

            <CollapsibleContent>
              <div className="px-4 pb-3 space-y-2">
                {searchRuns && searchRuns.length > 0 ? (
                  <div className="space-y-1">
                    {searchRuns.map(run => {
                      const successResults = run.results?.filter(r => r.status === "added") ?? []
                      const failedResults = run.results?.filter(isCrossSeedSearchFailure) ?? []
                      const skippedResults = run.results?.filter(isCrossSeedSearchSkipped) ?? []
                      const hasResults = (run.results?.length ?? 0) > 0
                      return (
                        <Collapsible key={run.id}>
                          <CollapsibleTrigger asChild disabled={!hasResults}>
                            <div className={`flex items-center justify-between gap-2 p-2 rounded bg-muted/30 text-sm ${hasResults ? "hover:bg-muted/50 cursor-pointer" : ""}`}>
                              <div className="flex items-center gap-2 min-w-0">
                                {run.status === "success" && <CheckCircle2 className="h-3 w-3 text-primary shrink-0" />}
                                {run.status === "running" && <Loader2 className="h-3 w-3 animate-spin text-yellow-500 shrink-0" />}
                                {run.status === "failed" && <XCircle className="h-3 w-3 text-destructive shrink-0" />}
                                {run.status === "canceled" && <Clock className="h-3 w-3 text-muted-foreground shrink-0" />}
                                <span className="text-xs text-muted-foreground">
                                  {t("scan.torrentsCount", { count: run.status === "running" ? `${run.processed}/${run.totalTorrents}` : run.totalTorrents })}
                                </span>
                                {run.errorMessage && (
                                  <span className="min-w-0 truncate text-xs text-muted-foreground" title={run.errorMessage}>
                                    {t("scan.runError", { message: run.errorMessage })}
                                  </span>
                                )}
                              </div>
                              <div className="flex items-center gap-2 shrink-0">
                                <Badge variant="secondary" className="text-xs">{t("scan.crossSeedsAddedBadge", { count: run.crossSeedsAdded })}</Badge>
                                {run.torrentsFailed > 0 && (
                                  <Badge variant="destructive" className="text-xs">{t("scan.failedCount", { count: run.torrentsFailed })}</Badge>
                                )}
                                <span className="text-xs text-muted-foreground">{formatDateValue(run.startedAt)}</span>
                                {hasResults && <ChevronDown className="h-3 w-3 text-muted-foreground" />}
                              </div>
                            </div>
                          </CollapsibleTrigger>
                          {hasResults && (
                            <CollapsibleContent>
                              <div className="pl-5 pr-2 py-2 space-y-1 border-l-2 border-muted ml-1.5 mt-1 max-h-48 overflow-y-auto">
                                {successResults.map((result, i) => (
                                  <div key={`success-${result.torrentHash}-${i}`} className="flex items-center gap-2 text-xs">
                                    <Badge variant="default" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName || t("common:status.unknown")}</Badge>
                                    <span className="truncate text-muted-foreground">{result.torrentName}</span>
                                  </div>
                                ))}
                                {successResults.length === 0 && failedResults.length === 0 && skippedResults.length === 0 && run.results && run.results.length > 0 && (
                                  <span className="text-xs text-muted-foreground">{t("scan.noResultsWithDetails")}</span>
                                )}
                                {skippedResults.length > 0 && (
                                  <div className="mt-2 pt-2 border-t border-border/50 space-y-1">
                                    <span className="text-[10px] text-muted-foreground font-medium">{t("scan.skippedLabel")}</span>
                                    {skippedResults.map((result, i) => (
                                      <div key={`skipped-${result.torrentHash}-${i}`} className="flex flex-col gap-0.5 text-xs">
                                        <div className="flex items-center gap-2">
                                          <Badge variant="secondary" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName || t("common:status.unknown")}</Badge>
                                          <span className="truncate text-muted-foreground">{result.torrentName}</span>
                                        </div>
                                        {result.message && (
                                          <span className="text-muted-foreground/70 pl-[104px] text-[10px]">{result.message}</span>
                                        )}
                                      </div>
                                    ))}
                                  </div>
                                )}
                                {failedResults.length > 0 && (
                                  <div className="mt-2 pt-2 border-t border-border/50 space-y-1">
                                    <span className="text-[10px] text-muted-foreground font-medium">{t("scan.failed")}</span>
                                    {failedResults.map((result, i) => (
                                      <div key={`failed-${result.torrentHash}-${i}`} className="flex flex-col gap-0.5 text-xs">
                                        <div className="flex items-center gap-2">
                                          <Badge variant="destructive" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName || t("common:status.unknown")}</Badge>
                                          <span className="truncate text-muted-foreground">{result.torrentName}</span>
                                        </div>
                                        <span className="text-muted-foreground/70 pl-[104px] text-[10px]">{result.message || t("scan.noMessageProvided")}</span>
                                      </div>
                                    ))}
                                  </div>
                                )}
                              </div>
                            </CollapsibleContent>
                          )}
                        </Collapsible>
                      )
                    })}
                  </div>
                ) : (
                  <div className="text-center py-2 text-xs text-muted-foreground">
                    {t("scan.noSearchRunsRecorded")}
                  </div>
                )}
              </div>
            </CollapsibleContent>
          </div>
        </Collapsible>
      </CardContent>
      <CardFooter className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          {searchRunning ? (
            <Button
              variant="outline"
              className="min-h-11 sm:min-h-9"
              onClick={() => cancelSearchRunMutation.mutate()}
              disabled={cancelSearchRunMutation.isPending}
            >
              {cancelSearchRunMutation.isPending ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  {t("automation.stopping")}
                </>
              ) : (
                <>
                  <XCircle className="mr-2 h-4 w-4" />
                  {t("common:actions.cancel")}
                </>
              )}
            </Button>
          ) : (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  className="min-h-11 sm:min-h-9 disabled:cursor-not-allowed disabled:pointer-events-auto"
                  onClick={handleStartSearchRun}
                  disabled={startSearchRunDisabled}
                >
                  {startSearchRunMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Rocket className="mr-2 h-4 w-4" />}
                  {t("scan.startRun")}
                </Button>
              </TooltipTrigger>
              {startSearchRunDisabledReason && (
                <TooltipContent align="start" className="max-w-xs text-xs">
                  {startSearchRunDisabledReason}
                </TooltipContent>
              )}
            </Tooltip>
          )}
        </div>
        <Button className="min-h-11 sm:min-h-9" onClick={handleSave} disabled={saveMutation.isPending}>
          {saveMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("rules.saveChanges")}
        </Button>
      </CardFooter>
    </Card>
  )
}
