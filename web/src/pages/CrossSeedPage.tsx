/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { buildCategoryTree, type CategoryNode } from "@/components/torrents/CategoryTree"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle
} from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import type {
  CrossSeedAutomationSettingsPatch,
  CrossSeedAutomationStatus,
  CrossSeedCompletionSettings,
  CrossSeedRun
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  AlertTriangle,
  ChevronDown,
  FlameIcon,
  Info,
  Loader2,
  Play,
  Rocket,
  XCircle
} from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

// RSS Automation settings
interface AutomationFormState {
  enabled: boolean
  runIntervalMinutes: number  // RSS Automation: interval between RSS feed polls (min: 30 minutes)
  targetInstanceIds: number[]
  targetIndexerIds: number[]
}

// Global cross-seed settings (apply to both RSS Automation and Seeded Torrent Search)
interface GlobalCrossSeedSettings {
  findIndividualEpisodes: boolean
  sizeMismatchTolerancePercent: number
  useCategoryFromIndexer: boolean
  useCrossCategorySuffix: boolean
  runExternalProgramId?: number | null
  ignorePatterns: string
  // Source-specific tagging
  rssAutomationTags: string[]
  seededSearchTags: string[]
  completionSearchTags: string[]
  webhookTags: string[]
  inheritSourceTags: boolean
}

interface CompletionFormState {
  enabled: boolean
  categories: string
  tags: string
  excludeCategories: string
  excludeTags: string
}

// RSS Automation constants
const MIN_RSS_INTERVAL_MINUTES = 30   // RSS: minimum interval between RSS feed polls
const DEFAULT_RSS_INTERVAL_MINUTES = 120  // RSS: default interval (2 hours)
const MIN_SEEDED_SEARCH_INTERVAL_SECONDS = 60  // Seeded Search: minimum interval between torrents
const MIN_SEEDED_SEARCH_COOLDOWN_MINUTES = 720  // Seeded Search: minimum cooldown (12 hours)

// RSS Automation defaults
const DEFAULT_AUTOMATION_FORM: AutomationFormState = {
  enabled: false,
  runIntervalMinutes: DEFAULT_RSS_INTERVAL_MINUTES,
  targetInstanceIds: [],
  targetIndexerIds: [],
}

const DEFAULT_GLOBAL_SETTINGS: GlobalCrossSeedSettings = {
  findIndividualEpisodes: false,
  sizeMismatchTolerancePercent: 5.0,
  useCategoryFromIndexer: false,
  useCrossCategorySuffix: true,
  runExternalProgramId: null,
  ignorePatterns: "",
  // Source-specific tagging defaults
  rssAutomationTags: ["cross-seed"],
  seededSearchTags: ["cross-seed"],
  completionSearchTags: ["cross-seed"],
  webhookTags: ["cross-seed"],
  inheritSourceTags: false,
}

const DEFAULT_COMPLETION_SETTINGS: CrossSeedCompletionSettings = {
  enabled: false,
  categories: [],
  tags: [],
  excludeCategories: [],
  excludeTags: [],
}

const DEFAULT_COMPLETION_FORM: CompletionFormState = {
  enabled: false,
  categories: "",
  tags: "",
  excludeCategories: "",
  excludeTags: "",
}

function parseList(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map(item => item.trim())
    .filter(Boolean)
}

function normalizeStringList(values: string[]): string[] {
  return Array.from(new Set(values.map(item => item.trim()).filter(Boolean)))
}

function normalizeNumberList(values: Array<string | number>): number[] {
  return Array.from(new Set(
    values
      .map(value => Number(value))
      .filter(value => !Number.isNaN(value) && value > 0)
  ))
}

function normalizeIgnorePatterns(patterns: string): string[] {
  return parseList(patterns.replace(/\r/g, ""))
}

function validateIgnorePatterns(raw: string): string {
  const text = raw.replace(/\r/g, "")
  const parts = text.split(/\n|,/)
  for (const part of parts) {
    const pattern = part.trim()
    if (!pattern) continue
    if (pattern.length > 256) {
      return "Ignore patterns must be shorter than 256 characters"
    }
  }
  return ""
}

function getDurationParts(ms: number): { hours: number; minutes: number; seconds: number } {
  if (ms <= 0) {
    return { hours: 0, minutes: 0, seconds: 0 }
  }
  const totalSeconds = Math.ceil(ms / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  return { hours, minutes, seconds }
}

function formatDurationShort(ms: number): string {
  const { hours, minutes, seconds } = getDurationParts(ms)
  const parts: string[] = []
  if (hours > 0) {
    parts.push(`${hours}h`)
  }
  parts.push(`${String(minutes).padStart(2, "0")}m`)
  parts.push(`${String(seconds).padStart(2, "0")}s`)
  return parts.join(" ")
}

export function CrossSeedPage() {
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  // RSS Automation state
  const [automationForm, setAutomationForm] = useState<AutomationFormState>(DEFAULT_AUTOMATION_FORM)
  const [globalSettings, setGlobalSettings] = useState<GlobalCrossSeedSettings>(DEFAULT_GLOBAL_SETTINGS)
  const [completionForm, setCompletionForm] = useState<CompletionFormState>(DEFAULT_COMPLETION_FORM)
  const [formInitialized, setFormInitialized] = useState(false)
  const [globalSettingsInitialized, setGlobalSettingsInitialized] = useState(false)
  const [completionFormInitialized, setCompletionFormInitialized] = useState(false)
  const [dryRun, setDryRun] = useState(false)
  const [validationErrors, setValidationErrors] = useState<Record<string, string>>({})

  // Seeded Torrent Search state (separate from RSS Automation)
  const [searchInstanceId, setSearchInstanceId] = useState<number | null>(null)
  const [searchCategories, setSearchCategories] = useState<string[]>([])
  const [searchTags, setSearchTags] = useState<string[]>([])
  const [searchIndexerIds, setSearchIndexerIds] = useState<number[]>([])
  const [searchIntervalSeconds, setSearchIntervalSeconds] = useState(MIN_SEEDED_SEARCH_INTERVAL_SECONDS)
  const [searchCooldownMinutes, setSearchCooldownMinutes] = useState(MIN_SEEDED_SEARCH_COOLDOWN_MINUTES)
  const [searchSettingsInitialized, setSearchSettingsInitialized] = useState(false)
  const [searchResultsOpen, setSearchResultsOpen] = useState(false)
  const [rssRunsOpen, setRssRunsOpen] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const formatDateValue = useCallback((value?: string | Date | null) => {
    if (!value) {
      return "—"
    }
    const date = value instanceof Date ? value : new Date(value)
    if (Number.isNaN(date.getTime())) {
      return "—"
    }
    return formatDate(date)
  }, [formatDate])

  const { data: settings } = useQuery({
    queryKey: ["cross-seed", "settings"],
    queryFn: () => api.getCrossSeedSettings(),
    staleTime: 5 * 60 * 1000, // 5 minutes
    gcTime: 10 * 60 * 1000, // 10 minutes
  })

  const { data: status, refetch: refetchStatus } = useQuery({
    queryKey: ["cross-seed", "status"],
    queryFn: () => api.getCrossSeedStatus(),
    refetchInterval: 30_000,
  })

  const { data: searchSettings } = useQuery({
    queryKey: ["cross-seed", "search", "settings"],
    queryFn: () => api.getCrossSeedSearchSettings(),
    staleTime: 5 * 60 * 1000, // 5 minutes
    gcTime: 10 * 60 * 1000, // 10 minutes
  })

  const { data: runs, refetch: refetchRuns } = useQuery({
    queryKey: ["cross-seed", "runs"],
    queryFn: () => api.listCrossSeedRuns({ limit: 20 }),
  })

  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })
  const activeInstances = useMemo(
    () => (instances ?? []).filter(instance => instance.isActive),
    [instances]
  )

  const { data: indexers } = useQuery({
    queryKey: ["torznab", "indexers"],
    queryFn: () => api.listTorznabIndexers(),
  })

  const enabledIndexers = useMemo(
    () => (indexers ?? []).filter(indexer => indexer.enabled),
    [indexers]
  )

  const hasEnabledIndexers = enabledIndexers.length > 0

  const notifyMissingIndexers = useCallback((context: string) => {
    toast.error("No Torznab indexers configured", {
      description: `${context} Add at least one enabled indexer in Settings → Indexers.`,
    })
  }, [])

  const handleIndexerError = useCallback((error: Error, context: string) => {
    const normalized = error.message?.toLowerCase?.() ?? ""
    if (normalized.includes("torznab indexers")) {
      notifyMissingIndexers(context)
      return true
    }
    return false
  }, [notifyMissingIndexers])

  const { data: externalPrograms } = useQuery({
    queryKey: ["external-programs"],
    queryFn: () => api.listExternalPrograms(),
  })
  const enabledExternalPrograms = useMemo(
    () => (externalPrograms ?? []).filter(program => program.enabled),
    [externalPrograms]
  )

  const { data: searchStatus, refetch: refetchSearchStatus } = useQuery({
    queryKey: ["cross-seed", "search-status"],
    queryFn: () => api.getCrossSeedSearchStatus(),
    refetchInterval: 5_000,
  })

  const { data: searchMetadata } = useQuery({
    queryKey: ["cross-seed", "search-metadata", searchInstanceId],
    queryFn: async () => {
      if (!searchInstanceId) return null
      const [categories, tags] = await Promise.all([
        api.getCategories(searchInstanceId),
        api.getTags(searchInstanceId),
      ])
      return { categories, tags }
    },
    enabled: !!searchInstanceId,
  })

  const { data: searchCacheStats } = useQuery({
    queryKey: ["torznab", "search-cache", "stats", "cross-seed"],
    queryFn: () => api.getTorznabSearchCacheStats(),
    staleTime: 60 * 1000,
  })

  const formatCacheTimestamp = useCallback((value?: string | null) => {
    if (!value) {
      return "—"
    }
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) {
      return "—"
    }
    return formatDateValue(parsed)
  }, [formatDateValue])

  useEffect(() => {
    if (settings && !formInitialized) {
      setAutomationForm({
        enabled: settings.enabled,
        runIntervalMinutes: settings.runIntervalMinutes,
        targetInstanceIds: settings.targetInstanceIds,
        targetIndexerIds: settings.targetIndexerIds,
      })
      setFormInitialized(true)
    }
  }, [settings, formInitialized])

  useEffect(() => {
    if (settings && !globalSettingsInitialized) {
      setGlobalSettings({
        findIndividualEpisodes: settings.findIndividualEpisodes,
        sizeMismatchTolerancePercent: settings.sizeMismatchTolerancePercent ?? 5.0,
        useCategoryFromIndexer: settings.useCategoryFromIndexer ?? false,
        useCrossCategorySuffix: settings.useCrossCategorySuffix ?? true,
        runExternalProgramId: settings.runExternalProgramId ?? null,
        ignorePatterns: Array.isArray(settings.ignorePatterns)
          ? settings.ignorePatterns.join("\n")
          : "",
        // Source-specific tagging
        rssAutomationTags: settings.rssAutomationTags ?? ["cross-seed"],
        seededSearchTags: settings.seededSearchTags ?? ["cross-seed"],
        completionSearchTags: settings.completionSearchTags ?? ["cross-seed"],
        webhookTags: settings.webhookTags ?? ["cross-seed"],
        inheritSourceTags: settings.inheritSourceTags ?? false,
      })
      setGlobalSettingsInitialized(true)
    }
  }, [settings, globalSettingsInitialized])

  useEffect(() => {
    if (settings && !completionFormInitialized) {
      const completion = settings.completion ?? DEFAULT_COMPLETION_SETTINGS
      setCompletionForm({
        enabled: completion.enabled,
        categories: completion.categories.join(", "),
        tags: completion.tags.join(", "),
        excludeCategories: completion.excludeCategories.join(", "),
        excludeTags: completion.excludeTags.join(", "),
      })
      setCompletionFormInitialized(true)
    }
  }, [settings, completionFormInitialized])

  useEffect(() => {
    if (!searchSettings || searchSettingsInitialized) {
      return
    }
    setSearchInstanceId(searchSettings.instanceId ?? null)
    setSearchCategories(normalizeStringList(searchSettings.categories ?? []))
    setSearchTags(normalizeStringList(searchSettings.tags ?? []))
    setSearchIndexerIds(searchSettings.indexerIds ?? [])
    setSearchIntervalSeconds(searchSettings.intervalSeconds ?? MIN_SEEDED_SEARCH_INTERVAL_SECONDS)
    setSearchCooldownMinutes(searchSettings.cooldownMinutes ?? MIN_SEEDED_SEARCH_COOLDOWN_MINUTES)
    setSearchSettingsInitialized(true)
  }, [searchSettings, searchSettingsInitialized])

  const ignorePatternError = useMemo(
    () => validateIgnorePatterns(globalSettings.ignorePatterns),
    [globalSettings.ignorePatterns]
  )

  useEffect(() => {
    setValidationErrors(prev => {
      const current = prev.ignorePatterns ?? ""
      if (current === ignorePatternError) {
        return prev
      }
      return { ...prev, ignorePatterns: ignorePatternError }
    })
  }, [ignorePatternError])

  useEffect(() => {
    if (!searchInstanceId && instances && instances.length > 0) {
      setSearchInstanceId(instances[0].id)
    }
  }, [instances, searchInstanceId])

  const buildAutomationPatch = useCallback((): CrossSeedAutomationSettingsPatch | null => {
    if (!settings) return null

    const automationSource = formInitialized
      ? automationForm
      : {
          enabled: settings.enabled,
          runIntervalMinutes: settings.runIntervalMinutes,
          targetInstanceIds: settings.targetInstanceIds,
          targetIndexerIds: settings.targetIndexerIds,
        }

    return {
      enabled: automationSource.enabled,
      runIntervalMinutes: automationSource.runIntervalMinutes,
      targetInstanceIds: automationSource.targetInstanceIds,
      targetIndexerIds: automationSource.targetIndexerIds,
    }
  }, [settings, automationForm, formInitialized])

  const buildCompletionPatch = useCallback((): CrossSeedAutomationSettingsPatch | null => {
    if (!settings) return null

    const completionSource = settings.completion ?? DEFAULT_COMPLETION_SETTINGS
    const completionState = completionFormInitialized
      ? completionForm
      : {
          enabled: completionSource.enabled,
          categories: completionSource.categories.join(", "),
          tags: completionSource.tags.join(", "),
          excludeCategories: completionSource.excludeCategories.join(", "),
          excludeTags: completionSource.excludeTags.join(", "),
        }

    return {
      completion: {
        enabled: completionState.enabled,
        categories: parseList(completionState.categories),
        tags: parseList(completionState.tags),
        excludeCategories: parseList(completionState.excludeCategories),
        excludeTags: parseList(completionState.excludeTags),
      },
    }
  }, [settings, completionForm, completionFormInitialized])

  const buildGlobalPatch = useCallback((): CrossSeedAutomationSettingsPatch | null => {
    if (!settings) return null

    const ignorePatterns = Array.isArray(settings.ignorePatterns) ? settings.ignorePatterns : []

    const globalSource = globalSettingsInitialized
      ? globalSettings
      : {
          findIndividualEpisodes: settings.findIndividualEpisodes,
          sizeMismatchTolerancePercent: settings.sizeMismatchTolerancePercent,
          useCategoryFromIndexer: settings.useCategoryFromIndexer,
          useCrossCategorySuffix: settings.useCrossCategorySuffix ?? true,
          runExternalProgramId: settings.runExternalProgramId ?? null,
          ignorePatterns: ignorePatterns.length > 0 ? ignorePatterns.join(", ") : "",
          rssAutomationTags: settings.rssAutomationTags ?? ["cross-seed"],
          seededSearchTags: settings.seededSearchTags ?? ["cross-seed"],
          completionSearchTags: settings.completionSearchTags ?? ["cross-seed"],
          webhookTags: settings.webhookTags ?? ["cross-seed"],
          inheritSourceTags: settings.inheritSourceTags ?? false,
        }

    return {
      findIndividualEpisodes: globalSource.findIndividualEpisodes,
      sizeMismatchTolerancePercent: globalSource.sizeMismatchTolerancePercent,
      useCategoryFromIndexer: globalSource.useCategoryFromIndexer,
      useCrossCategorySuffix: globalSource.useCrossCategorySuffix,
      runExternalProgramId: globalSource.runExternalProgramId,
      ignorePatterns: normalizeIgnorePatterns(globalSource.ignorePatterns),
      // Source-specific tagging
      rssAutomationTags: globalSource.rssAutomationTags,
      seededSearchTags: globalSource.seededSearchTags,
      completionSearchTags: globalSource.completionSearchTags,
      webhookTags: globalSource.webhookTags,
      inheritSourceTags: globalSource.inheritSourceTags,
    }
  }, [
    settings,
    globalSettings,
    globalSettingsInitialized,
  ])

  const patchSettingsMutation = useMutation({
    mutationFn: (payload: CrossSeedAutomationSettingsPatch) => api.patchCrossSeedSettings(payload),
    onSuccess: (data) => {
      toast.success("Settings updated")
      // Don't reinitialize the form since we just saved it
      queryClient.setQueryData(["cross-seed", "settings"], data)
      refetchStatus()
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const startSearchRunMutation = useMutation({
    mutationFn: (payload: Parameters<typeof api.startCrossSeedSearchRun>[0]) => api.startCrossSeedSearchRun(payload),
    onSuccess: () => {
      toast.success("Search run started")
      refetchSearchStatus()
    },
    onError: (error: Error) => {
      if (handleIndexerError(error, "Seeded Torrent Search cannot run without Torznab indexers.")) {
        return
      }
      toast.error(error.message)
    },
  })

  const cancelSearchRunMutation = useMutation({
    mutationFn: () => api.cancelCrossSeedSearchRun(),
    onSuccess: () => {
      toast.success("Search run canceled")
      refetchSearchStatus()
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const triggerRunMutation = useMutation({
    mutationFn: (payload: { dryRun?: boolean }) => api.triggerCrossSeedRun(payload),
    onSuccess: () => {
      toast.success("Automation run started")
      refetchStatus()
      refetchRuns()
    },
    onError: (error: Error) => {
      if (handleIndexerError(error, "RSS automation runs require at least one Torznab indexer.")) {
        return
      }
      toast.error(error.message)
    },
  })

  const handleSaveAutomation = () => {
    setValidationErrors(prev => ({ ...prev, runIntervalMinutes: "", targetInstanceIds: "" }))

    if (automationForm.enabled && automationForm.targetInstanceIds.length === 0) {
      setValidationErrors(prev => ({ ...prev, targetInstanceIds: "Select at least one instance for RSS automation." }))
      return
    }

    if (automationForm.runIntervalMinutes < MIN_RSS_INTERVAL_MINUTES) {
      setValidationErrors(prev => ({ ...prev, runIntervalMinutes: `Must be at least ${MIN_RSS_INTERVAL_MINUTES} minutes` }))
      return
    }

    const payload = buildAutomationPatch()
    if (!payload) return

    patchSettingsMutation.mutate(payload)
  }

  const handleSaveCompletion = () => {
    const payload = buildCompletionPatch()
    if (!payload) return

    patchSettingsMutation.mutate(payload)
  }

  const handleSaveGlobal = () => {
    if (ignorePatternError) {
      setValidationErrors(prev => ({ ...prev, ignorePatterns: ignorePatternError }))
      return
    }

    if (validationErrors.ignorePatterns) {
      setValidationErrors(prev => ({ ...prev, ignorePatterns: "" }))
    }

    const payload = buildGlobalPatch()
    if (!payload) return

    patchSettingsMutation.mutate(payload)
  }

  const automationStatus: CrossSeedAutomationStatus | undefined = status
  const latestRun: CrossSeedRun | null | undefined = automationStatus?.lastRun
  const automationRunning = automationStatus?.running ?? false
  const effectiveRunIntervalMinutes = formInitialized
    ? automationForm.runIntervalMinutes
    : settings?.runIntervalMinutes ?? DEFAULT_RSS_INTERVAL_MINUTES
  const enforcedRunIntervalMinutes = Math.max(effectiveRunIntervalMinutes, MIN_RSS_INTERVAL_MINUTES)
  const automationTargetInstanceCount = formInitialized
    ? automationForm.targetInstanceIds.length
    : settings?.targetInstanceIds?.length ?? 0
  const hasAutomationTargets = automationTargetInstanceCount > 0

  const nextManualRunAt = useMemo(() => {
    if (!latestRun?.startedAt) {
      return null
    }
    const startedAt = new Date(latestRun.startedAt)
    if (Number.isNaN(startedAt.getTime())) {
      return null
    }
    const intervalMs = enforcedRunIntervalMinutes * 60 * 1000
    return new Date(startedAt.getTime() + intervalMs)
  }, [enforcedRunIntervalMinutes, latestRun?.startedAt])

  const manualCooldownRemainingMs = useMemo(() => {
    if (!nextManualRunAt) {
      return 0
    }
    const remaining = nextManualRunAt.getTime() - now
    return remaining > 0 ? remaining : 0
  }, [nextManualRunAt, now])

  const manualCooldownActive = manualCooldownRemainingMs > 0
  const manualCooldownDisplay = manualCooldownActive ? formatDurationShort(manualCooldownRemainingMs) : ""
  const runButtonDisabled = triggerRunMutation.isPending || automationRunning || manualCooldownActive || !hasEnabledIndexers || !hasAutomationTargets
  const runButtonDisabledReason = useMemo(() => {
    if (!hasEnabledIndexers) {
      return "Configure at least one Torznab indexer before running RSS automation."
    }
    if (!hasAutomationTargets) {
      return "Select at least one instance before running RSS automation."
    }
    if (automationRunning) {
      return "Automation run is already in progress."
    }
    if (manualCooldownActive) {
      return `Manual runs are limited to every ${enforcedRunIntervalMinutes}-minute interval. Try again in ${manualCooldownDisplay}.`
    }
    return undefined
  }, [automationRunning, enforcedRunIntervalMinutes, hasAutomationTargets, hasEnabledIndexers, manualCooldownActive, manualCooldownDisplay])

  const handleTriggerAutomationRun = () => {
    if (!hasEnabledIndexers) {
      notifyMissingIndexers("RSS automation runs require at least one Torznab indexer.")
      return
    }
    if (!hasAutomationTargets) {
      setValidationErrors(prev => ({ ...prev, targetInstanceIds: "Select at least one instance for RSS automation." }))
      toast.error("Pick at least one instance to receive cross-seeds before running RSS automation.")
      return
    }
    if (formInitialized && settings) {
      const savedTargets = [...(settings.targetInstanceIds ?? [])].sort((a, b) => a - b)
      const currentTargets = [...automationForm.targetInstanceIds].sort((a, b) => a - b)
      const targetsMatchSaved =
        savedTargets.length === currentTargets.length &&
        savedTargets.every((value, index) => value === currentTargets[index])
      if (!targetsMatchSaved) {
        toast.error("Save RSS automation settings to apply the updated target instances before running.")
        return
      }
    }
    triggerRunMutation.mutate({ dryRun })
  }

  const searchRunning = searchStatus?.running ?? false
  const activeSearchRun = searchStatus?.run
  const recentSearchResults = searchStatus?.recentResults ?? []
  const recentAddedResults = useMemo(
    () => recentSearchResults.filter(result => result.added),
    [recentSearchResults]
  )

  const startSearchRunDisabled = !searchInstanceId || startSearchRunMutation.isPending || searchRunning || !hasEnabledIndexers
  const startSearchRunDisabledReason = useMemo(() => {
    if (!hasEnabledIndexers) {
      return "Configure at least one Torznab indexer before running Seeded Torrent Search."
    }
    return undefined
  }, [hasEnabledIndexers])

  useEffect(() => {
    if (typeof window === "undefined") {
      return
    }
    if (!manualCooldownActive || !nextManualRunAt) {
      return
    }
    const tick = () => setNow(Date.now())
    tick()
    const interval = window.setInterval(tick, 1_000)
    return () => window.clearInterval(interval)
  }, [manualCooldownActive, nextManualRunAt])

  const instanceOptions = useMemo(
    () => activeInstances.map(instance => ({ label: instance.name, value: String(instance.id) })),
    [activeInstances]
  )

  const indexerOptions = useMemo(
    () => enabledIndexers.map(indexer => ({ label: indexer.name, value: String(indexer.id) })),
    [enabledIndexers]
  )

  const searchTagNames = useMemo(() => searchMetadata?.tags ?? [], [searchMetadata])

  const searchCategorySelectOptions = useMemo(
    () => {
      // Build tree from available categories for indentation
      const categories = searchMetadata?.categories ?? {}
      const tree = buildCategoryTree(categories, {})
      const flattened: { label: string; value: string; level: number }[] = []

      const visitNodes = (nodes: CategoryNode[]) => {
        for (const node of nodes) {
          flattened.push({
            label: node.displayName,
            value: node.name,
            level: node.level,
          })
          visitNodes(node.children)
        }
      }

      visitNodes(tree)

      // Add any extra categories that were manually typed but not in the list
      const extras = searchCategories.filter(category => !flattened.some(opt => opt.value === category))
      for (const extra of extras) {
        flattened.push({ label: extra, value: extra, level: 0 })
      }

      return flattened
    },
    [searchCategories, searchMetadata?.categories]
  )

  const searchTagSelectOptions = useMemo(
    () => {
      const extras = searchTags.filter(tag => !searchTagNames.includes(tag))
      return Array.from(new Set([...searchTagNames, ...extras])).map(tag => ({
        label: tag,
        value: tag,
      }))
    },
    [searchTagNames, searchTags]
  )

  const handleStartSearchRun = () => {
    // Clear previous validation errors
    setValidationErrors({})

    if (!hasEnabledIndexers) {
      notifyMissingIndexers("Seeded Torrent Search requires at least one Torznab indexer.")
      return
    }

    if (!searchInstanceId) {
      toast.error("Select an instance to run against")
      return
    }

    // Validate search interval and cooldown
    const errors: Record<string, string> = {}
    if (searchIntervalSeconds < MIN_SEEDED_SEARCH_INTERVAL_SECONDS) {
      errors.searchIntervalSeconds = `Must be at least ${MIN_SEEDED_SEARCH_INTERVAL_SECONDS} seconds`
    }
    if (searchCooldownMinutes < MIN_SEEDED_SEARCH_COOLDOWN_MINUTES) {
      errors.searchCooldownMinutes = `Must be at least ${MIN_SEEDED_SEARCH_COOLDOWN_MINUTES} minutes`
    }

    if (Object.keys(errors).length > 0) {
      setValidationErrors(errors)
      return
    }

    startSearchRunMutation.mutate({
      instanceId: searchInstanceId,
      categories: searchCategories,
      tags: searchTags,
      intervalSeconds: searchIntervalSeconds,
      indexerIds: searchIndexerIds,
      cooldownMinutes: searchCooldownMinutes,
    })
  }

  const estimatedCompletionInfo = useMemo(() => {
    if (!activeSearchRun) {
      return null
    }
    const total = activeSearchRun.totalTorrents ?? 0
    const interval = activeSearchRun.intervalSeconds ?? 0
    if (total === 0 || interval <= 0) {
      return null
    }
    const remaining = Math.max(total - activeSearchRun.processed, 0)
    if (remaining === 0) {
      return null
    }
    const eta = new Date(Date.now() + remaining * interval * 1000)
    return { eta, remaining, interval }
  }, [activeSearchRun])

  const automationEnabled = formInitialized ? automationForm.enabled : settings?.enabled ?? false
  const completionEnabled = completionFormInitialized
    ? completionForm.enabled
    : settings?.completion?.enabled ?? false

  const searchInstanceName = useMemo(
    () => instances?.find(instance => instance.id === searchInstanceId)?.name ?? "No instance selected",
    [instances, searchInstanceId]
  )

  const currentSearchInstanceName = useMemo(
    () => {
      if (searchRunning && activeSearchRun) {
        return instances?.find(instance => instance.id === activeSearchRun.instanceId)?.name ?? `Instance ${activeSearchRun.instanceId}`
      }
      return searchInstanceName
    },
    [instances, searchInstanceId, searchRunning, activeSearchRun]
  )

  const ignorePatternCount = useMemo(
    () => normalizeIgnorePatterns(globalSettings.ignorePatterns).length,
    [globalSettings.ignorePatterns]
  )

  const automationStatusLabel = automationRunning ? "RUNNING" : automationEnabled ? "SCHEDULED" : "DISABLED"
  const automationStatusVariant: "default" | "secondary" | "destructive" | "outline" =
    automationRunning ? "default" : automationEnabled ? "secondary" : "destructive"
  const searchStatusLabel = searchRunning ? "RUNNING" : "IDLE"
  const searchStatusVariant: "default" | "secondary" | "destructive" | "outline" =
    searchRunning ? "default" : "secondary"

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

  const getRunStatusVariant = (status: CrossSeedRun["status"]) => {
    switch (status) {
      case "success":
        return "default"
      case "running":
      case "partial":
        return "secondary"
      case "failed":
        return "destructive"
      case "pending":
      default:
        return "outline"
    }
  }


  return (
    <div className="space-y-6 p-4 lg:p-6 pb-16">
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Cross-Seed</h1>
          <p className="text-sm text-muted-foreground">
            Identify compatible torrents and automate cross-seeding across your instances.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Badge variant={automationEnabled ? "default" : "secondary"}>
            Automation {automationEnabled ? "on" : "off"}
          </Badge>
          <Badge variant={completionEnabled ? "default" : "secondary"}>
            On completion {completionEnabled ? "on" : "off"}
          </Badge>
        </div>
      </div>

      {!hasEnabledIndexers && (
        <Alert className="border-border rounded-xl bg-card">
          <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400" />
          <AlertTitle>Torznab indexers required</AlertTitle>
          <AlertDescription className="space-y-1">
            <p>Automation runs and Seeded Torrent Search need at least one enabled Torznab indexer.</p>
            <p>
              <Link to="/settings" search={{ tab: "indexers" }} className="font-medium text-primary underline-offset-4 hover:underline">
                Manage indexers in Settings
              </Link>{" "}
              to add or enable one.
            </p>
          </AlertDescription>
        </Alert>
      )}

      <Alert className="border-border rounded-xl bg-card">
        <Info className="h-4 w-4 text-blue-600 dark:text-blue-400" />
        <AlertTitle>How cross-seeding works</AlertTitle>
        <AlertDescription className="space-y-1">
          <p>
            qui inherits the <strong>Automatic Torrent Management (AutoTMM)</strong> state from the matched torrent.
            If the source uses AutoTMM, the cross-seed will too; if the source has a custom save path, the cross-seed uses the same path.
            Files are reused directly without hardlinking.
          </p>
          <p className="text-muted-foreground">
            <a
              href="https://github.com/autobrr/qui#how-qui-differs-from-cross-seed"
              target="_blank"
              rel="noopener noreferrer"
              className="font-medium text-primary underline-offset-4 hover:underline"
            >
              Learn more
            </a>
          </p>
        </AlertDescription>
      </Alert>

      <div className="grid gap-4 md:grid-cols-2 mb-6">
        <Card className="h-full">
          <CardHeader className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <CardTitle className="text-base">RSS automation</CardTitle>
              <Badge variant={automationStatusVariant}>
                {automationStatusLabel}
              </Badge>
            </div>
            <CardDescription>Hands-free polling of tracker RSS feeds using your rules.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Next run</span>
              <span className="font-medium">
                {automationEnabled
                  ? automationStatus?.nextRunAt
                    ? formatDateValue(automationStatus.nextRunAt)
                    : "—"
                  : "Disabled"}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Manual trigger</span>
              <span className="font-medium">{manualCooldownActive ? `Cooldown ${manualCooldownDisplay}` : "Ready"}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Last run</span>
              <span className="font-medium">
                {latestRun ? `${latestRun.status.toUpperCase()} • ${formatDateValue(latestRun.startedAt)}` : "No runs yet"}
              </span>
            </div>
          </CardContent>
        </Card>

        <Card className="h-full">
          <CardHeader className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <CardTitle className="text-base">Seeded torrent search</CardTitle>
              <Badge variant={searchStatusVariant}>{searchStatusLabel}</Badge>
            </div>
            <CardDescription>Deep scan the torrents you already seed to backfill gaps.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Instance</span>
              <span className="font-medium truncate text-right max-w-[180px]">{currentSearchInstanceName}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Recent additions</span>
              <span className="font-medium">{recentAddedResults.length}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Now</span>
              <span className="font-medium">
                {searchRunning
                  ? activeSearchRun
                    ? `${activeSearchRun.processed}/${activeSearchRun.totalTorrents ?? "?"} scanned`
                    : "Running..."
                  : "Idle"}
              </span>
            </div>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="automation" className="space-y-4">
        <TabsList className="grid w-full grid-cols-3 gap-2 md:w-auto">
          <TabsTrigger value="automation">Automation</TabsTrigger>
          <TabsTrigger value="search">Seeded search</TabsTrigger>
          <TabsTrigger value="global">Global rules</TabsTrigger>
        </TabsList>

        <TabsContent value="automation" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>RSS Automation</CardTitle>
              <CardDescription>Poll tracker RSS feeds on a fixed interval and add matching cross-seeds automatically.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-5">

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="automation-enabled" className="flex items-center gap-2">
                <Switch
                  id="automation-enabled"
                  checked={automationForm.enabled}
                  onCheckedChange={value => {
                    if (value && !hasEnabledIndexers) {
                      notifyMissingIndexers("Enable RSS automation only after configuring Torznab indexers.")
                      return
                    }
                    setAutomationForm(prev => ({ ...prev, enabled: !!value }))
                    if (!value && validationErrors.targetInstanceIds) {
                      setValidationErrors(prev => ({ ...prev, targetInstanceIds: "" }))
                    }
                  }}
                />
                Enable RSS automation
              </Label>
            </div>
          </div>

          <div className="grid gap-4">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Label htmlFor="automation-interval">RSS run interval (minutes)</Label>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground"
                      aria-label="RSS interval help"
                    >
                      <Info className="h-4 w-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent align="start" className="max-w-xs text-xs">
                    Automation processes the full feed from every enabled Torznab indexer on each run. Minimum interval is {MIN_RSS_INTERVAL_MINUTES} minutes to avoid hammering indexers.
                  </TooltipContent>
                </Tooltip>
              </div>
              <Input
                id="automation-interval"
                type="number"
                min={MIN_RSS_INTERVAL_MINUTES}
                value={automationForm.runIntervalMinutes}
                onChange={event => {
                  setAutomationForm(prev => ({ ...prev, runIntervalMinutes: Number(event.target.value) }))
                  // Clear validation error when user changes the value
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
              <Label>Target instances</Label>
              <MultiSelect
                options={instanceOptions}
                selected={automationForm.targetInstanceIds.map(String)}
                onChange={values => {
                  const nextIds = normalizeNumberList(values)
                  setAutomationForm(prev => ({
                    ...prev,
                    targetInstanceIds: nextIds,
                  }))
                  if (nextIds.length > 0 && validationErrors.targetInstanceIds) {
                    setValidationErrors(prev => ({ ...prev, targetInstanceIds: "" }))
                  }
                }}
                placeholder={instanceOptions.length ? "Select qBittorrent instances" : "No active instances available"}
                disabled={!instanceOptions.length}
              />
              <p className="text-xs text-muted-foreground">
                {instanceOptions.length === 0
                  ? "No instances available."
                  : automationForm.targetInstanceIds.length === 0
                    ? "Pick at least one instance to receive cross-seeds."
                    : `${automationForm.targetInstanceIds.length} instance${automationForm.targetInstanceIds.length === 1 ? "" : "s"} selected.`}
              </p>
              {validationErrors.targetInstanceIds && (
                <p className="text-sm text-destructive">{validationErrors.targetInstanceIds}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label>Target indexers</Label>
              <MultiSelect
                options={indexerOptions}
                selected={automationForm.targetIndexerIds.map(String)}
                onChange={values => setAutomationForm(prev => ({
                  ...prev,
                  targetIndexerIds: normalizeNumberList(values),
                }))}
                placeholder={indexerOptions.length ? "All enabled indexers (leave empty for all)" : "No Torznab indexers configured"}
                disabled={!indexerOptions.length}
              />
              <p className="text-xs text-muted-foreground">
                {indexerOptions.length === 0
                  ? "No Torznab indexers configured."
                  : automationForm.targetIndexerIds.length === 0
                    ? "All enabled Torznab indexers are eligible for RSS automation."
                    : `Only ${automationForm.targetIndexerIds.length} selected indexer${automationForm.targetIndexerIds.length === 1 ? "" : "s"} will be polled.`}
              </p>
            </div>
          </div>

          <Separator />

          <Collapsible open={rssRunsOpen} onOpenChange={setRssRunsOpen} className="rounded-md border px-3 py-3 text-sm">
            <CollapsibleTrigger className="flex w-full items-center justify-between text-sm font-medium hover:cursor-pointer">
              <span className="flex items-center gap-2">
                Recent RSS runs
                <ChevronDown className={`h-4 w-4 transition-transform ${rssRunsOpen ? "" : "-rotate-90"}`} />
              </span>
              <div className="flex items-center gap-1.5">
                {groupedRuns.scheduled.length > 0 && (
                  <Badge variant="secondary" className="text-xs">{groupedRuns.scheduled.length} scheduled</Badge>
                )}
                {groupedRuns.manual.length > 0 && (
                  <Badge variant="outline" className="text-xs">{groupedRuns.manual.length} manual</Badge>
                )}
              </div>
            </CollapsibleTrigger>
            <CollapsibleContent className="pt-2 space-y-4">
              {runs && runs.length > 0 ? (
                <div className="space-y-4">
                  {(["scheduled", "manual", "other"] as const).map(group => {
                    const data = groupedRuns[group]
                    if (!data || data.length === 0) return null
                    const title =
                      group === "scheduled" ? "Scheduled" : group === "manual" ? "Manual" : "Other"
                    return (
                      <div key={group} className="space-y-2">
                        <div className="flex items-center gap-2 text-xs uppercase tracking-wide text-muted-foreground">
                          <span>{title}</span>
                          <span className="text-xs normal-case tracking-normal">(last {data.length})</span>
                        </div>
                        <div className="space-y-2">
                          {data.map(run => (
                            <div key={run.id} className="rounded border p-3 space-y-2 bg-muted/40">
                              <div className="flex items-center justify-between text-sm">
                                <Badge variant={getRunStatusVariant(run.status)} className="uppercase text-xs tracking-wide">
                                  {run.status}
                                </Badge>
                                <span className="text-xs text-muted-foreground">{formatDateValue(run.startedAt)}</span>
                              </div>
                              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                                <Badge variant="secondary" className="text-xs">
                                  Added {run.torrentsAdded}
                                </Badge>
                                <Badge variant="outline" className="text-xs">
                                  Skipped {run.torrentsSkipped}
                                </Badge>
                                <Badge variant={run.torrentsFailed > 0 ? "destructive" : "outline"} className="text-xs">
                                  Failed {run.torrentsFailed}
                                </Badge>
                                <span className="text-xs">{run.totalFeedItems} feed items</span>
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )
                  })}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">No RSS automation runs recorded yet.</p>
              )}
            </CollapsibleContent>
          </Collapsible>
        </CardContent>
        <CardFooter className="flex flex-col-reverse gap-3 md:flex-row md:items-center md:justify-between">
          <div className="flex items-center gap-2 text-xs">
            <Switch id="automation-dry-run" checked={dryRun} onCheckedChange={value => setDryRun(!!value)} />
            <Label htmlFor="automation-dry-run">Dry run</Label>
          </div>
          <div className="flex flex-col gap-2 w-full md:w-auto md:flex-row">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  onClick={handleTriggerAutomationRun}
                  disabled={runButtonDisabled}
                  className="disabled:cursor-not-allowed disabled:pointer-events-auto"
                >
                  {triggerRunMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Play className="mr-2 h-4 w-4" />}
                  Run now
                </Button>
              </TooltipTrigger>
              {runButtonDisabledReason && (
                <TooltipContent align="end" className="max-w-xs text-xs">
                  {runButtonDisabledReason}
                </TooltipContent>
              )}
            </Tooltip>
            <Button
              onClick={handleSaveAutomation}
              disabled={patchSettingsMutation.isPending}
            >
              {patchSettingsMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Save RSS automation settings
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                // Reset to defaults without triggering reinitialization
                setAutomationForm(DEFAULT_AUTOMATION_FORM)
              }}
            >
              Reset
            </Button>
          </div>
        </CardFooter>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Auto-search on completion</CardTitle>
          <CardDescription>Kick off a cross-seed search the moment a torrent finishes, using simple category and tag filters. Torrents already tagged <span className="font-semibold text-foreground">cross-seed</span> are skipped automatically.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="flex gap-4">
            <Label htmlFor="completion-enabled">Enable on completion</Label>
            <Switch
              id="completion-enabled"
              checked={completionForm.enabled}
              onCheckedChange={value => setCompletionForm(prev => ({ ...prev, enabled: !!value }))}
            />
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="completion-categories">Categories (allow list)</Label>
              <Input
                id="completion-categories"
                placeholder="Comma separated"
                value={completionForm.categories}
                onChange={event => setCompletionForm(prev => ({ ...prev, categories: event.target.value }))}
              />
              <p className="text-xs text-muted-foreground">Only run for these categories. Leave blank to include all categories.</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="completion-exclude-categories">Exclude categories</Label>
              <Input
                id="completion-exclude-categories"
                placeholder="Comma separated"
                value={completionForm.excludeCategories}
                onChange={event => setCompletionForm(prev => ({ ...prev, excludeCategories: event.target.value }))}
              />
              <p className="text-xs text-muted-foreground">Stop completion searches for matching categories.</p>
            </div>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="completion-tags">Tags (allow list)</Label>
              <Input
                id="completion-tags"
                placeholder="Comma separated"
                value={completionForm.tags}
                onChange={event => setCompletionForm(prev => ({ ...prev, tags: event.target.value }))}
              />
              <p className="text-xs text-muted-foreground">Require at least one matching tag. Leave blank to include all tags.</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="completion-exclude-tags">Exclude tags</Label>
              <Input
                id="completion-exclude-tags"
                placeholder="Comma separated"
                value={completionForm.excludeTags}
                onChange={event => setCompletionForm(prev => ({ ...prev, excludeTags: event.target.value }))}
              />
              <p className="text-xs text-muted-foreground">Skip completion searches when any of these tags are present.</p>
            </div>
          </div>
        </CardContent>
        <CardFooter className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
          <Button
            onClick={handleSaveCompletion}
            disabled={patchSettingsMutation.isPending}
          >
            {patchSettingsMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Save completion settings
          </Button>
        </CardFooter>
      </Card>

        </TabsContent>

        <TabsContent value="search" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Seeded Torrent Search</CardTitle>
              <CardDescription>Walk the torrents you already seed on the selected instance, collapse identical content down to the oldest copy, and query Torznab feeds once per unique release while skipping trackers you already have it from.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-5">
          <Alert className="border-destructive/20 bg-destructive/10 text-destructive mb-8">
            <AlertTriangle className="h-4 w-4 !text-destructive" />
            <AlertTitle>Run sparingly</AlertTitle>
            <AlertDescription>
              This deep scan touches every torrent you seed and can stress trackers despite the built-in cooldowns. Prefer autobrr announces or RSS automation for routine coverage and reserve manual search runs for occasional catch-up passes.
            </AlertDescription>
          </Alert>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-3">
              <Label htmlFor="search-interval">Interval between torrents (seconds)</Label>
              <Input
                id="search-interval"
                type="number"
                min={MIN_SEEDED_SEARCH_INTERVAL_SECONDS}
                value={searchIntervalSeconds}
                onChange={event => {
                  setSearchIntervalSeconds(Number(event.target.value) || MIN_SEEDED_SEARCH_INTERVAL_SECONDS)
                  // Clear validation error when user changes the value
                  if (validationErrors.searchIntervalSeconds) {
                    setValidationErrors(prev => ({ ...prev, searchIntervalSeconds: "" }))
                  }
                }}
                className={validationErrors.searchIntervalSeconds ? "border-destructive" : ""}
              />
              {validationErrors.searchIntervalSeconds && (
                <p className="text-sm text-destructive">{validationErrors.searchIntervalSeconds}</p>
              )}
              <p className="text-xs text-muted-foreground">Wait time before scanning the next seeded torrent. Minimum {MIN_SEEDED_SEARCH_INTERVAL_SECONDS} seconds.</p>
            </div>
            <div className="space-y-3">
              <Label htmlFor="search-cooldown">Cooldown (minutes)</Label>
              <Input
                id="search-cooldown"
                type="number"
                min={MIN_SEEDED_SEARCH_COOLDOWN_MINUTES}
                value={searchCooldownMinutes}
                onChange={event => {
                  setSearchCooldownMinutes(Number(event.target.value) || MIN_SEEDED_SEARCH_COOLDOWN_MINUTES)
                  // Clear validation error when user changes the value
                  if (validationErrors.searchCooldownMinutes) {
                    setValidationErrors(prev => ({ ...prev, searchCooldownMinutes: "" }))
                  }
                }}
                className={validationErrors.searchCooldownMinutes ? "border-destructive" : ""}
              />
              {validationErrors.searchCooldownMinutes && (
                <p className="text-sm text-destructive">{validationErrors.searchCooldownMinutes}</p>
              )}
              <p className="text-xs text-muted-foreground">Skip seeded torrents that were searched more recently than this window. Minimum {MIN_SEEDED_SEARCH_COOLDOWN_MINUTES} minutes.</p>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-3">
              <Label>Categories</Label>
              <MultiSelect
                options={searchCategorySelectOptions}
                selected={searchCategories}
                onChange={values => setSearchCategories(normalizeStringList(values))}
                placeholder={
                  searchInstanceId
                    ? searchCategorySelectOptions.length ? "All categories (leave empty for all)" : "Type to add categories"
                    : "Select an instance to load categories"
                }
                creatable
                onCreateOption={value => setSearchCategories(prev => normalizeStringList([...prev, value]))}
                disabled={!searchInstanceId}
              />
              <p className="text-xs text-muted-foreground">
                {searchInstanceId && searchCategorySelectOptions.length === 0
                  ? "Categories load after selecting an instance; you can still type a category name."
                  : searchCategories.length === 0
                    ? "All categories will be included in the scan."
                    : `Only ${searchCategories.length} selected categor${searchCategories.length === 1 ? "y" : "ies"} will be scanned.`}
              </p>
            </div>

            <div className="space-y-3">
              <Label>Tags</Label>
              <MultiSelect
                options={searchTagSelectOptions}
                selected={searchTags}
                onChange={values => setSearchTags(normalizeStringList(values))}
                placeholder={
                  searchInstanceId
                    ? searchTagSelectOptions.length ? "All tags (leave empty for all)" : "Type to add tags"
                    : "Select an instance to load tags"
                }
                creatable
                onCreateOption={value => setSearchTags(prev => normalizeStringList([...prev, value]))}
                disabled={!searchInstanceId}
              />
              <p className="text-xs text-muted-foreground">
                {searchInstanceId && searchTagSelectOptions.length === 0
                  ? "Tags load after selecting an instance; you can still type a tag."
                  : searchTags.length === 0
                  ? "All tags will be included in the scan."
                  : `Only ${searchTags.length} selected tag${searchTags.length === 1 ? "" : "s"} will be scanned.`}
              </p>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-3">
              <Label>Source instance</Label>
              <Select
                value={searchInstanceId ? String(searchInstanceId) : ""}
                onValueChange={(value) => setSearchInstanceId(Number(value))}
                disabled={!instances?.length}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Select an instance" />
                </SelectTrigger>
                <SelectContent>
                  {instances?.map(instance => (
                    <SelectItem key={instance.id} value={String(instance.id)}>
                      {instance.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {!instances?.length && (
                <p className="text-xs text-muted-foreground">Add an instance to search the torrents you already seed.</p>
              )}
            </div>

            <div className="space-y-3">
              <Label>Indexers</Label>
              <MultiSelect
                options={indexerOptions}
                selected={searchIndexerIds.map(String)}
                onChange={values => setSearchIndexerIds(normalizeNumberList(values))}
                placeholder={indexerOptions.length ? "All enabled indexers (leave empty for all)" : "No Torznab indexers configured"}
                disabled={!indexerOptions.length}
              />
              <p className="text-xs text-muted-foreground">
                {indexerOptions.length === 0
                  ? "No Torznab indexers configured."
                  : searchIndexerIds.length === 0
                    ? "All enabled Torznab indexers will be queried for matches."
                    : `Only ${searchIndexerIds.length} selected indexer${searchIndexerIds.length === 1 ? "" : "s"} will be queried.`}
              </p>
            </div>
          </div>

          <Separator />

          {activeSearchRun && (
            <div className="rounded-lg border bg-muted/50 p-4 space-y-3">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium">Status</p>
                <Badge variant={searchRunning ? "default" : "secondary"}>{searchRunning ? "RUNNING" : "IDLE"}</Badge>
              </div>
              {searchStatus?.currentTorrent && (
                <div className="text-xs">
                  <span className="text-muted-foreground">Currently processing:</span>{" "}
                  <span className="font-medium">{searchStatus.currentTorrent.torrentName}</span>
                </div>
              )}
              <div className="grid gap-2 text-xs">
                <div className="flex items-center gap-4">
                  <span className="text-muted-foreground">Progress:</span>
                  <span className="font-medium">{activeSearchRun.processed} / {activeSearchRun.totalTorrents || "?"} torrents</span>
                </div>
                <div className="flex items-center gap-4">
                  <span className="text-muted-foreground">Results:</span>
                  <span className="font-medium">
                    {activeSearchRun.torrentsAdded} added • {activeSearchRun.torrentsSkipped} skipped • {activeSearchRun.torrentsFailed} failed
                  </span>
                </div>
                <div className="flex items-center gap-4">
                  <span className="text-muted-foreground">Started:</span>
                  <span className="font-medium">{formatDateValue(activeSearchRun.startedAt)}</span>
                </div>
                {estimatedCompletionInfo && (
                  <div className="flex items-center gap-4">
                    <span className="text-muted-foreground">Est. completion:</span>
                    <span className="font-medium">
                      {formatDateValue(estimatedCompletionInfo.eta)}
                      <span className="text-xs text-muted-foreground font-normal ml-2">
                        ≈ {estimatedCompletionInfo.remaining} torrents remaining @ {estimatedCompletionInfo.interval}s intervals
                      </span>
                    </span>
                  </div>
                )}
              </div>
            </div>
          )}

          <Collapsible open={searchResultsOpen} onOpenChange={setSearchResultsOpen} className="border rounded-md mb-4">
            <CollapsibleTrigger className="flex w-full items-center justify-between px-3 py-2 text-sm font-medium hover:cursor-pointer">
              <span className="flex items-center gap-2">
                Recent search additions
                <ChevronDown className={`h-4 w-4 transition-transform ${searchResultsOpen ? "" : "-rotate-90"}`} />
              </span>
              <Badge variant="outline">{recentAddedResults.length}</Badge>
            </CollapsibleTrigger>
            <CollapsibleContent className="px-3 pb-3 space-y-2">
              {recentAddedResults.length === 0 ? (
                <p className="text-xs text-muted-foreground">No added cross-seed results recorded yet.</p>
              ) : (
                <ul className="space-y-2">
                  {recentAddedResults.map(result => (
                    <li key={`${result.torrentHash}-${result.processedAt}`} className="flex items-start justify-between gap-3 rounded border px-3 py-3 bg-muted/40">
                      <div className="space-y-1.5 max-w-[80%]">
                        <div className="flex items-center gap-2">
                          <p className="text-sm font-medium leading-tight">{result.torrentName}</p>
                          <Badge variant="secondary" className="text-xs">{result.indexerName || "Indexer"}</Badge>
                        </div>
                        <p className="text-xs text-muted-foreground">{formatDateValue(result.processedAt)}</p>
                      </div>
                      <Badge variant="default">Added</Badge>
                    </li>
                  ))}
                </ul>
              )}
              <p className="text-xs text-muted-foreground">Shows the last 10 additions during this run. List clears when the run stops.</p>
            </CollapsibleContent>
          </Collapsible>
        </CardContent>
        <CardFooter className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2">
            {searchRunning ? (
              <Button
                variant="outline"
                onClick={() => cancelSearchRunMutation.mutate()}
                disabled={cancelSearchRunMutation.isPending}
              >
                {cancelSearchRunMutation.isPending ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Stopping...
                  </>
                ) : (
                  <>
                    <XCircle className="mr-2 h-4 w-4" />
                    Cancel
                  </>
                )}
              </Button>
            ) : (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    onClick={handleStartSearchRun}
                    disabled={startSearchRunDisabled}
                    className="disabled:cursor-not-allowed disabled:pointer-events-auto"
                  >
                    {startSearchRunMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Rocket className="mr-2 h-4 w-4" />}
                    Start run
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
        </CardFooter>
          </Card>

        </TabsContent>

        <TabsContent value="global" className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Global Cross-Seed Settings</CardTitle>
          <CardDescription>Settings that apply to all cross-seed operations.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {searchCacheStats && (
          <div className="rounded-lg border border-dashed border-border/70 bg-muted/60 p-3 text-xs text-muted-foreground">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={searchCacheStats.enabled ? "secondary" : "outline"}>
                {searchCacheStats.enabled ? "Cache enabled" : "Cache disabled"}
              </Badge>
              <span>TTL {searchCacheStats.ttlMinutes} min</span>
              <span>{searchCacheStats.entries} cached searches</span>
              <span>Last used {formatCacheTimestamp(searchCacheStats.lastUsedAt)}</span>
              <Button variant="link" size="xs" className="px-0 ml-auto" asChild>
                <Link to="/settings" search={{ tab: "search-cache" }}>
                  Manage cache settings
                </Link>
              </Button>
            </div>
          </div>
          )}

          <div className="grid gap-4 md:grid-cols-2">
            <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div className="space-y-1">
                  <p className="text-sm font-medium leading-none">Matching</p>
                  <p className="text-xs text-muted-foreground">Tune how releases are matched and filtered.</p>
                </div>
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Label htmlFor="global-find-individual-episodes" className="cursor-pointer">Find individual episodes</Label>
                  <Switch
                    id="global-find-individual-episodes"
                    checked={globalSettings.findIndividualEpisodes}
                    onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, findIndividualEpisodes: !!value }))}
                  />
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                When enabled, season packs also match individual episodes. When disabled, season packs only match other season packs.
              </p>
              <p className="flex items-center pb-2 text-sm text-destructive">
                <FlameIcon className="h-4 w-4 mr-2" aria-hidden="true" /> Episodes are added with Auto Torrent Management disabled to prevent save path conflicts.
              </p>
              <div className="space-y-2">
                <Label htmlFor="global-size-tolerance">Size mismatch tolerance (%)</Label>
                <Input
                  id="global-size-tolerance"
                  type="number"
                  min="0"
                  max="100"
                  step="0.1"
                  value={globalSettings.sizeMismatchTolerancePercent}
                  onChange={event => setGlobalSettings(prev => ({ 
                    ...prev, 
                    sizeMismatchTolerancePercent: Math.max(0, Math.min(100, Number(event.target.value) || 0))
                  }))}
                />
                <p className="text-xs text-muted-foreground">
                  Filters out results with sizes differing by more than this percentage. Also determines the auto-resume threshold after recheck completes (e.g., 5% tolerance auto-resumes if recheck finishes at 95% or higher). Set to 0 for exact size matching.
                </p>
              </div>
            </div>

            <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
              <div className="space-y-1">
                <p className="text-sm font-medium leading-none">Categories & automation</p>
                <p className="text-xs text-muted-foreground">Control categories and post-processing for injected torrents.</p>
              </div>
              <div className="flex items-center justify-between gap-3">
                <div className="space-y-0.5">
                  <Label htmlFor="global-use-cross-category-suffix" className="font-medium">Add .cross category suffix</Label>
                  <p className="text-xs text-muted-foreground">Append .cross to categories (e.g., movies → movies.cross) to prevent Sonarr/Radarr import loops. Disable for full TMM support.</p>
                </div>
                <Switch
                  id="global-use-cross-category-suffix"
                  checked={globalSettings.useCrossCategorySuffix}
                  onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, useCrossCategorySuffix: !!value }))}
                />
              </div>
              <div className="flex items-center justify-between gap-3">
                <div className="space-y-0.5">
                  <Label htmlFor="global-use-category-from-indexer" className="font-medium">Use indexer name as category</Label>
                  <p className="text-xs text-muted-foreground">Automatically set qBittorrent category to the indexer name. Save path is inherited by the matched torrent.</p>
                </div>
                <Switch
                  id="global-use-category-from-indexer"
                  checked={globalSettings.useCategoryFromIndexer}
                  onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, useCategoryFromIndexer: !!value }))}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="global-external-program">Run external program after injection</Label>
                <Select
                value={globalSettings.runExternalProgramId ? String(globalSettings.runExternalProgramId) : "none"}
                onValueChange={(value) => setGlobalSettings(prev => ({ 
                  ...prev, 
                  runExternalProgramId: value === "none" ? null : Number(value) 
                }))}
                disabled={!enabledExternalPrograms.length}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={
                      !enabledExternalPrograms.length 
                        ? "No external programs available" 
                        : "Select external program (optional)"
                    } />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">None</SelectItem>
                  {enabledExternalPrograms.map(program => (
                    <SelectItem key={program.id} value={String(program.id)}>
                      {program.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
                <p className="text-xs text-muted-foreground">
                  Optionally run an external program after successfully injecting a cross-seed torrent. Only enabled programs are shown.
                  {!enabledExternalPrograms.length && (
                    <> <Link to="/settings" search={{ tab: "external-programs" }} className="font-medium text-primary underline-offset-4 hover:underline">Configure external programs</Link> to use this feature.</>
                  )}
                </p>
              </div>
            </div>
          </div>

          <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-4">
            <div className="space-y-1">
              <p className="text-sm font-medium leading-none">Source Tagging</p>
              <p className="text-xs text-muted-foreground">Configure tags applied to cross-seed torrents based on how they were discovered.</p>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="rss-automation-tags">RSS Automation Tags</Label>
                <MultiSelect
                  options={[
                    { label: "cross-seed", value: "cross-seed" },
                    { label: "rss", value: "rss" },
                  ]}
                  selected={globalSettings.rssAutomationTags}
                  onChange={values => setGlobalSettings(prev => ({ ...prev, rssAutomationTags: normalizeStringList(values) }))}
                  placeholder="Select tags for RSS automation"
                  creatable
                  onCreateOption={value => setGlobalSettings(prev => ({ ...prev, rssAutomationTags: normalizeStringList([...prev.rssAutomationTags, value]) }))}
                />
                <p className="text-xs text-muted-foreground">Tags applied to torrents added via RSS feed automation.</p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="seeded-search-tags">Seeded Search Tags</Label>
                <MultiSelect
                  options={[
                    { label: "cross-seed", value: "cross-seed" },
                    { label: "seeded-search", value: "seeded-search" },
                  ]}
                  selected={globalSettings.seededSearchTags}
                  onChange={values => setGlobalSettings(prev => ({ ...prev, seededSearchTags: normalizeStringList(values) }))}
                  placeholder="Select tags for seeded search"
                  creatable
                  onCreateOption={value => setGlobalSettings(prev => ({ ...prev, seededSearchTags: normalizeStringList([...prev.seededSearchTags, value]) }))}
                />
                <p className="text-xs text-muted-foreground">Tags applied to torrents added via seeded torrent search.</p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="completion-search-tags">Completion Search Tags</Label>
                <MultiSelect
                  options={[
                    { label: "cross-seed", value: "cross-seed" },
                    { label: "completion", value: "completion" },
                  ]}
                  selected={globalSettings.completionSearchTags}
                  onChange={values => setGlobalSettings(prev => ({ ...prev, completionSearchTags: normalizeStringList(values) }))}
                  placeholder="Select tags for completion search"
                  creatable
                  onCreateOption={value => setGlobalSettings(prev => ({ ...prev, completionSearchTags: normalizeStringList([...prev.completionSearchTags, value]) }))}
                />
                <p className="text-xs text-muted-foreground">Tags applied to torrents added via completion-triggered search.</p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="webhook-tags">Webhook Tags</Label>
                <MultiSelect
                  options={[
                    { label: "cross-seed", value: "cross-seed" },
                    { label: "webhook", value: "webhook" },
                    { label: "autobrr", value: "autobrr" },
                  ]}
                  selected={globalSettings.webhookTags}
                  onChange={values => setGlobalSettings(prev => ({ ...prev, webhookTags: normalizeStringList(values) }))}
                  placeholder="Select tags for webhook/apply"
                  creatable
                  onCreateOption={value => setGlobalSettings(prev => ({ ...prev, webhookTags: normalizeStringList([...prev.webhookTags, value]) }))}
                />
                <p className="text-xs text-muted-foreground">Tags applied to torrents added via /apply webhook (e.g., autobrr).</p>
              </div>
            </div>

            <div className="flex items-center justify-between gap-3 pt-2">
              <div className="space-y-0.5">
                <Label htmlFor="inherit-source-tags" className="font-medium">Inherit source torrent tags</Label>
                <p className="text-xs text-muted-foreground">Also copy tags from the matched source torrent in qBittorrent.</p>
              </div>
              <Switch
                id="inherit-source-tags"
                checked={globalSettings.inheritSourceTags}
                onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, inheritSourceTags: !!value }))}
              />
            </div>
          </div>

          <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <Label htmlFor="global-ignore-patterns">Ignore patterns</Label>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground transition-colors"
                      aria-label="How ignore patterns work"
                    >
                      <Info className="h-4 w-4" aria-hidden="true" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs text-xs">
                    Plain strings act as suffix matches (e.g., <code>.nfo</code> ignores any path ending in <code>.nfo</code>). Globs treat <code>/</code> as a folder separator, so <code>*.nfo</code> only matches files in the top-level folder. To ignore sample folders use <code>*/sample/*</code>. Separate entries with commas or new lines.
                  </TooltipContent>
                </Tooltip>
              </div>
              <Badge variant="outline" className="text-xs">{ignorePatternCount} pattern{ignorePatternCount === 1 ? "" : "s"}</Badge>
            </div>
            <Textarea
              id="global-ignore-patterns"
              placeholder={".nfo, .srr, */sample/*\nor one per line"}
              rows={4}
              value={globalSettings.ignorePatterns}
              onChange={event => {
                const value = event.target.value
                setGlobalSettings(prev => ({ ...prev, ignorePatterns: value }))
                const error = validateIgnorePatterns(value)
                setValidationErrors(prev => ({ ...prev, ignorePatterns: error }))
              }}
              className={validationErrors.ignorePatterns ? "border-destructive" : ""}
            />
            <p className="text-xs text-muted-foreground">
              Applies to RSS automation, autobrr apply requests, and seeded torrent search additions. Plain suffixes (e.g., <code>.nfo</code>) match in any subfolder; glob patterns do not cross <code>/</code>, so use folder-aware globs like <code>*/sample/*</code> for nested paths.
            </p>
            {validationErrors.ignorePatterns && (
              <p className="text-sm text-destructive">{validationErrors.ignorePatterns}</p>
            )}
          </div>
        </CardContent>
        <CardFooter className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
          <Button
            onClick={handleSaveGlobal}
            disabled={patchSettingsMutation.isPending || Boolean(ignorePatternError)}
          >
            {patchSettingsMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Save global cross-seed settings
          </Button>
        </CardFooter>
      </Card>

        </TabsContent>
      </Tabs>
    </div>
  )
}
