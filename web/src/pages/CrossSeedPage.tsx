/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { CompletionOverview } from "@/components/instances/preferences/CompletionOverview"
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
  CrossSeedRun
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  Clock,
  FlameIcon,
  History,
  Info,
  Loader2,
  Play,
  Rocket,
  XCircle,
  Zap
} from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

// RSS Automation settings
interface AutomationFormState {
  enabled: boolean
  runIntervalMinutes: number  // RSS Automation: interval between RSS feed polls (min: 30 minutes)
  targetInstanceIds: number[]
  targetIndexerIds: number[]
  // RSS source filtering: filter which local torrents to search when checking RSS feeds
  rssSourceCategories: string[]
  rssSourceTags: string[]
  rssSourceExcludeCategories: string[]
  rssSourceExcludeTags: string[]
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
  // Skip auto-resume settings per source mode
  skipAutoResumeRss: boolean
  skipAutoResumeSeededSearch: boolean
  skipAutoResumeCompletion: boolean
  skipAutoResumeWebhook: boolean
  skipRecheck: boolean
  // Webhook source filtering: filter which local torrents to search when checking webhook requests
  webhookSourceCategories: string[]
  webhookSourceTags: string[]
  webhookSourceExcludeCategories: string[]
  webhookSourceExcludeTags: string[]
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
  rssSourceCategories: [],
  rssSourceTags: [],
  rssSourceExcludeCategories: [],
  rssSourceExcludeTags: [],
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
  // Skip auto-resume defaults (off = preserve existing behavior)
  skipAutoResumeRss: false,
  skipAutoResumeSeededSearch: false,
  skipAutoResumeCompletion: false,
  skipAutoResumeWebhook: false,
  skipRecheck: false,
  // Webhook source filtering defaults - empty means no filtering (all torrents)
  webhookSourceCategories: [],
  webhookSourceTags: [],
  webhookSourceExcludeCategories: [],
  webhookSourceExcludeTags: [],
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

/** Aggregate categories and tags from multiple qBittorrent instances */
function aggregateInstanceMetadata(
  results: Array<{ categories: Record<string, { name: string; savePath: string }>; tags: string[] }>
): { categories: Record<string, { name: string; savePath: string }>; tags: string[] } {
  const allCategories: Record<string, { name: string; savePath: string }> = {}
  const allTags = new Set<string>()
  for (const result of results) {
    for (const [name, cat] of Object.entries(result.categories)) {
      allCategories[name] = cat
    }
    for (const tag of result.tags) {
      allTags.add(tag)
    }
  }
  return { categories: allCategories, tags: Array.from(allTags) }
}

interface CrossSeedPageProps {
  activeTab: "automation" | "search" | "global"
  onTabChange: (tab: "automation" | "search" | "global") => void
}

export function CrossSeedPage({ activeTab, onTabChange }: CrossSeedPageProps) {
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  // RSS Automation state
  const [automationForm, setAutomationForm] = useState<AutomationFormState>(DEFAULT_AUTOMATION_FORM)
  const [globalSettings, setGlobalSettings] = useState<GlobalCrossSeedSettings>(DEFAULT_GLOBAL_SETTINGS)
  const [formInitialized, setFormInitialized] = useState(false)
  const [globalSettingsInitialized, setGlobalSettingsInitialized] = useState(false)
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

  // Fetch categories/tags from all RSS Automation target instances (aggregated)
  const { data: rssSourceMetadata } = useQuery({
    queryKey: ["cross-seed", "rss-source-metadata", automationForm.targetInstanceIds],
    queryFn: async () => {
      if (automationForm.targetInstanceIds.length === 0) return null
      const results = await Promise.all(
        automationForm.targetInstanceIds.map(async (instanceId) => {
          const [categories, tags] = await Promise.all([
            api.getCategories(instanceId),
            api.getTags(instanceId),
          ])
          return { categories, tags }
        })
      )
      return aggregateInstanceMetadata(results)
    },
    enabled: automationForm.targetInstanceIds.length > 0,
    staleTime: 5 * 60 * 1000,
  })

  // Fetch categories/tags from ALL active instances (for webhook source filters)
  const { data: webhookSourceMetadata } = useQuery({
    queryKey: ["cross-seed", "webhook-source-metadata", activeInstances.map(i => i.id)],
    queryFn: async () => {
      if (activeInstances.length === 0) return null
      const results = await Promise.all(
        activeInstances.map(async (instance) => {
          const [categories, tags] = await Promise.all([
            api.getCategories(instance.id),
            api.getTags(instance.id),
          ])
          return { categories, tags }
        })
      )
      return aggregateInstanceMetadata(results)
    },
    enabled: activeInstances.length > 0,
    staleTime: 5 * 60 * 1000,
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
        rssSourceCategories: settings.rssSourceCategories ?? [],
        rssSourceTags: settings.rssSourceTags ?? [],
        rssSourceExcludeCategories: settings.rssSourceExcludeCategories ?? [],
        rssSourceExcludeTags: settings.rssSourceExcludeTags ?? [],
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
        // Skip auto-resume settings
        skipAutoResumeRss: settings.skipAutoResumeRss ?? false,
        skipAutoResumeSeededSearch: settings.skipAutoResumeSeededSearch ?? false,
        skipAutoResumeCompletion: settings.skipAutoResumeCompletion ?? false,
        skipAutoResumeWebhook: settings.skipAutoResumeWebhook ?? false,
        skipRecheck: settings.skipRecheck ?? false,
        // Webhook source filtering
        webhookSourceCategories: settings.webhookSourceCategories ?? [],
        webhookSourceTags: settings.webhookSourceTags ?? [],
        webhookSourceExcludeCategories: settings.webhookSourceExcludeCategories ?? [],
        webhookSourceExcludeTags: settings.webhookSourceExcludeTags ?? [],
      })
      setGlobalSettingsInitialized(true)
    }
  }, [settings, globalSettingsInitialized])

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
        rssSourceCategories: settings.rssSourceCategories ?? [],
        rssSourceTags: settings.rssSourceTags ?? [],
        rssSourceExcludeCategories: settings.rssSourceExcludeCategories ?? [],
        rssSourceExcludeTags: settings.rssSourceExcludeTags ?? [],
      }

    return {
      enabled: automationSource.enabled,
      runIntervalMinutes: automationSource.runIntervalMinutes,
      targetInstanceIds: automationSource.targetInstanceIds,
      targetIndexerIds: automationSource.targetIndexerIds,
      rssSourceCategories: automationSource.rssSourceCategories,
      rssSourceTags: automationSource.rssSourceTags,
      rssSourceExcludeCategories: automationSource.rssSourceExcludeCategories,
      rssSourceExcludeTags: automationSource.rssSourceExcludeTags,
    }
  }, [settings, automationForm, formInitialized])

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
        skipAutoResumeRss: settings.skipAutoResumeRss ?? false,
        skipAutoResumeSeededSearch: settings.skipAutoResumeSeededSearch ?? false,
        skipAutoResumeCompletion: settings.skipAutoResumeCompletion ?? false,
        skipAutoResumeWebhook: settings.skipAutoResumeWebhook ?? false,
        skipRecheck: settings.skipRecheck ?? false,
        webhookSourceCategories: settings.webhookSourceCategories ?? [],
        webhookSourceTags: settings.webhookSourceTags ?? [],
        webhookSourceExcludeCategories: settings.webhookSourceExcludeCategories ?? [],
        webhookSourceExcludeTags: settings.webhookSourceExcludeTags ?? [],
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
      // Skip auto-resume settings
      skipAutoResumeRss: globalSource.skipAutoResumeRss,
      skipAutoResumeSeededSearch: globalSource.skipAutoResumeSeededSearch,
      skipAutoResumeCompletion: globalSource.skipAutoResumeCompletion,
      skipAutoResumeWebhook: globalSource.skipAutoResumeWebhook,
      skipRecheck: globalSource.skipRecheck,
      // Webhook source filtering
      webhookSourceCategories: globalSource.webhookSourceCategories,
      webhookSourceTags: globalSource.webhookSourceTags,
      webhookSourceExcludeCategories: globalSource.webhookSourceExcludeCategories,
      webhookSourceExcludeTags: globalSource.webhookSourceExcludeTags,
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
    () => buildCategorySelectOptions(searchMetadata?.categories ?? {}, searchCategories),
    [searchCategories, searchMetadata?.categories]
  )

  const searchTagSelectOptions = useMemo(
    () => buildTagSelectOptions(searchTagNames, searchTags),
    [searchTagNames, searchTags]
  )

  // RSS Source filter select options (aggregated from all target instances)
  const rssSourceTagNames = useMemo(() => rssSourceMetadata?.tags ?? [], [rssSourceMetadata])

  const rssSourceCategorySelectOptions = useMemo(
    () => buildCategorySelectOptions(
      rssSourceMetadata?.categories ?? {},
      automationForm.rssSourceCategories,
      automationForm.rssSourceExcludeCategories
    ),
    [automationForm.rssSourceCategories, automationForm.rssSourceExcludeCategories, rssSourceMetadata?.categories]
  )

  const rssSourceTagSelectOptions = useMemo(
    () => buildTagSelectOptions(
      rssSourceTagNames,
      automationForm.rssSourceTags,
      automationForm.rssSourceExcludeTags
    ),
    [rssSourceTagNames, automationForm.rssSourceTags, automationForm.rssSourceExcludeTags]
  )

  // Webhook Source filter select options (aggregated from ALL active instances)
  const webhookSourceTagNames = useMemo(() => webhookSourceMetadata?.tags ?? [], [webhookSourceMetadata])

  const webhookSourceCategorySelectOptions = useMemo(
    () => buildCategorySelectOptions(
      webhookSourceMetadata?.categories ?? {},
      globalSettings.webhookSourceCategories,
      globalSettings.webhookSourceExcludeCategories
    ),
    [globalSettings.webhookSourceCategories, globalSettings.webhookSourceExcludeCategories, webhookSourceMetadata?.categories]
  )

  const webhookSourceTagSelectOptions = useMemo(
    () => buildTagSelectOptions(
      webhookSourceTagNames,
      globalSettings.webhookSourceTags,
      globalSettings.webhookSourceExcludeTags
    ),
    [webhookSourceTagNames, globalSettings.webhookSourceTags, globalSettings.webhookSourceExcludeTags]
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

  const runSummaryStats = useMemo(() => {
    if (!runs || runs.length === 0) {
      return { totalAdded: 0, totalFailed: 0, totalRuns: 0 }
    }
    return {
      totalAdded: runs.reduce((sum, run) => sum + run.torrentsAdded, 0),
      totalFailed: runs.reduce((sum, run) => sum + run.torrentsFailed, 0),
      totalRuns: runs.length,
    }
  }, [runs])


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

      <Tabs value={activeTab} onValueChange={(v) => onTabChange(v as typeof activeTab)} className="space-y-4">
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

              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-3">
                  <Label>Include categories</Label>
                  <MultiSelect
                    options={rssSourceCategorySelectOptions}
                    selected={automationForm.rssSourceCategories}
                    onChange={values => setAutomationForm(prev => ({ ...prev, rssSourceCategories: values }))}
                    placeholder={
                      automationForm.targetInstanceIds.length > 0
                        ? rssSourceCategorySelectOptions.length ? "All categories (leave empty for all)" : "Type to add categories"
                        : "Select target instances to load categories"
                    }
                    creatable
                    disabled={automationForm.targetInstanceIds.length === 0}
                  />
                  <p className="text-xs text-muted-foreground">
                    {automationForm.rssSourceCategories.length === 0
                      ? "All categories will be included."
                      : `Only ${automationForm.rssSourceCategories.length} selected categor${automationForm.rssSourceCategories.length === 1 ? "y" : "ies"} will be matched.`}
                  </p>
                </div>

                <div className="space-y-3">
                  <Label>Include tags</Label>
                  <MultiSelect
                    options={rssSourceTagSelectOptions}
                    selected={automationForm.rssSourceTags}
                    onChange={values => setAutomationForm(prev => ({ ...prev, rssSourceTags: values }))}
                    placeholder={
                      automationForm.targetInstanceIds.length > 0
                        ? rssSourceTagSelectOptions.length ? "All tags (leave empty for all)" : "Type to add tags"
                        : "Select target instances to load tags"
                    }
                    creatable
                    disabled={automationForm.targetInstanceIds.length === 0}
                  />
                  <p className="text-xs text-muted-foreground">
                    {automationForm.rssSourceTags.length === 0
                      ? "All tags will be included."
                      : `Only ${automationForm.rssSourceTags.length} selected tag${automationForm.rssSourceTags.length === 1 ? "" : "s"} will be matched.`}
                  </p>
                </div>
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-3">
                  <Label>Exclude categories</Label>
                  <MultiSelect
                    options={rssSourceCategorySelectOptions}
                    selected={automationForm.rssSourceExcludeCategories}
                    onChange={values => setAutomationForm(prev => ({ ...prev, rssSourceExcludeCategories: values }))}
                    placeholder={
                      automationForm.targetInstanceIds.length > 0
                        ? "None"
                        : "Select target instances to load categories"
                    }
                    creatable
                    disabled={automationForm.targetInstanceIds.length === 0}
                  />
                  <p className="text-xs text-muted-foreground">
                    {automationForm.rssSourceExcludeCategories.length === 0
                      ? "No categories excluded."
                      : `${automationForm.rssSourceExcludeCategories.length} categor${automationForm.rssSourceExcludeCategories.length === 1 ? "y" : "ies"} will be skipped.`}
                  </p>
                </div>

                <div className="space-y-3">
                  <Label>Exclude tags</Label>
                  <MultiSelect
                    options={rssSourceTagSelectOptions}
                    selected={automationForm.rssSourceExcludeTags}
                    onChange={values => setAutomationForm(prev => ({ ...prev, rssSourceExcludeTags: values }))}
                    placeholder={
                      automationForm.targetInstanceIds.length > 0
                        ? "None"
                        : "Select target instances to load tags"
                    }
                    creatable
                    disabled={automationForm.targetInstanceIds.length === 0}
                  />
                  <p className="text-xs text-muted-foreground">
                    {automationForm.rssSourceExcludeTags.length === 0
                      ? "No tags excluded."
                      : `${automationForm.rssSourceExcludeTags.length} tag${automationForm.rssSourceExcludeTags.length === 1 ? "" : "s"} will be skipped.`}
                  </p>
                </div>
              </div>

              <Separator />

              <Collapsible open={rssRunsOpen} onOpenChange={setRssRunsOpen}>
                <div className="rounded-xl border bg-card text-card-foreground shadow-sm">
                  <CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-4 hover:cursor-pointer text-left hover:bg-muted/50 transition-colors rounded-xl">
                    <div className="flex items-center gap-2">
                      <History className="h-4 w-4 text-muted-foreground" />
                      <span className="text-sm font-medium">Recent RSS runs</span>
                      {runs && runs.length > 0 ? (
                        <Badge variant="secondary" className="text-xs">
                          {runSummaryStats.totalRuns} runs • +{runSummaryStats.totalAdded}
                          {runSummaryStats.totalFailed > 0 && ` • ${runSummaryStats.totalFailed} failed`}
                        </Badge>
                      ) : (
                        <span className="text-xs text-muted-foreground">No runs yet</span>
                      )}
                    </div>
                    <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${rssRunsOpen ? "rotate-180" : ""}`} />
                  </CollapsibleTrigger>

                  <CollapsibleContent>
                    <div className="px-4 pb-3 space-y-3">
                      {/* Grouped runs */}
                      {runs && runs.length > 0 ? (
                        <div className="space-y-4">
                          {/* Scheduled runs */}
                          {groupedRuns.scheduled.length > 0 && (
                            <div className="space-y-2">
                              <div className="flex items-center gap-2 text-sm font-medium">
                                <Clock className="h-4 w-4 text-blue-500" />
                                Scheduled ({groupedRuns.scheduled.length})
                              </div>
                              <div className="space-y-1">
                                {groupedRuns.scheduled.map(run => {
                                  const hasResults = run.results && run.results.length > 0
                                  const successResults = run.results?.filter(r => r.success) ?? []
                                  return (
                                    <Collapsible key={run.id}>
                                      <CollapsibleTrigger asChild disabled={!hasResults}>
                                        <div className={`flex items-center justify-between gap-2 p-2 rounded bg-muted/30 text-sm ${hasResults ? "hover:bg-muted/50 cursor-pointer" : ""}`}>
                                          <div className="flex items-center gap-2 min-w-0">
                                            {run.status === "success" && <CheckCircle2 className="h-3 w-3 text-primary shrink-0" />}
                                            {run.status === "running" && <Loader2 className="h-3 w-3 animate-spin text-yellow-500 shrink-0" />}
                                            {run.status === "failed" && <XCircle className="h-3 w-3 text-destructive shrink-0" />}
                                            {run.status === "partial" && <AlertTriangle className="h-3 w-3 text-yellow-500 shrink-0" />}
                                            {run.status === "pending" && <Clock className="h-3 w-3 text-muted-foreground shrink-0" />}
                                            <span className="text-xs text-muted-foreground">{run.totalFeedItems} items</span>
                                          </div>
                                          <div className="flex items-center gap-2 shrink-0">
                                            <Badge variant="secondary" className="text-xs">+{run.torrentsAdded}</Badge>
                                            {run.torrentsFailed > 0 && (
                                              <Badge variant="destructive" className="text-xs">{run.torrentsFailed} failed</Badge>
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
                                              <div key={`${result.instanceId}-${i}`} className="flex items-center gap-2 text-xs">
                                                <Badge variant="default" className="text-[10px] shrink-0 w-20 justify-center truncate" title={result.instanceName}>{result.instanceName}</Badge>
                                                {result.indexerName && (
                                                  <Badge variant="secondary" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName}</Badge>
                                                )}
                                                <span className="truncate text-muted-foreground">{result.matchedTorrentName}</span>
                                              </div>
                                            ))}
                                            {successResults.length === 0 && run.results && run.results.length > 0 && (
                                              <span className="text-xs text-muted-foreground">No successful additions</span>
                                            )}
                                          </div>
                                        </CollapsibleContent>
                                      )}
                                    </Collapsible>
                                  )
                                })}
                              </div>
                            </div>
                          )}

                          {/* Manual runs */}
                          {groupedRuns.manual.length > 0 && (
                            <div className="space-y-2">
                              <div className="flex items-center gap-2 text-sm font-medium">
                                <Zap className="h-4 w-4 text-yellow-500" />
                                Manual ({groupedRuns.manual.length})
                              </div>
                              <div className="space-y-1">
                                {groupedRuns.manual.map(run => {
                                  const hasResults = run.results && run.results.length > 0
                                  const successResults = run.results?.filter(r => r.success) ?? []
                                  return (
                                    <Collapsible key={run.id}>
                                      <CollapsibleTrigger asChild disabled={!hasResults}>
                                        <div className={`flex items-center justify-between gap-2 p-2 rounded bg-muted/30 text-sm ${hasResults ? "hover:bg-muted/50 cursor-pointer" : ""}`}>
                                          <div className="flex items-center gap-2 min-w-0">
                                            {run.status === "success" && <CheckCircle2 className="h-3 w-3 text-primary shrink-0" />}
                                            {run.status === "running" && <Loader2 className="h-3 w-3 animate-spin text-yellow-500 shrink-0" />}
                                            {run.status === "failed" && <XCircle className="h-3 w-3 text-destructive shrink-0" />}
                                            {run.status === "partial" && <AlertTriangle className="h-3 w-3 text-yellow-500 shrink-0" />}
                                            {run.status === "pending" && <Clock className="h-3 w-3 text-muted-foreground shrink-0" />}
                                            <span className="text-xs text-muted-foreground">{run.totalFeedItems} items</span>
                                          </div>
                                          <div className="flex items-center gap-2 shrink-0">
                                            <Badge variant="secondary" className="text-xs">+{run.torrentsAdded}</Badge>
                                            {run.torrentsFailed > 0 && (
                                              <Badge variant="destructive" className="text-xs">{run.torrentsFailed} failed</Badge>
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
                                              <div key={`${result.instanceId}-${i}`} className="flex items-center gap-2 text-xs">
                                                <Badge variant="default" className="text-[10px] shrink-0 w-20 justify-center truncate" title={result.instanceName}>{result.instanceName}</Badge>
                                                {result.indexerName && (
                                                  <Badge variant="secondary" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName}</Badge>
                                                )}
                                                <span className="truncate text-muted-foreground">{result.matchedTorrentName}</span>
                                              </div>
                                            ))}
                                            {successResults.length === 0 && run.results && run.results.length > 0 && (
                                              <span className="text-xs text-muted-foreground">No successful additions</span>
                                            )}
                                          </div>
                                        </CollapsibleContent>
                                      )}
                                    </Collapsible>
                                  )
                                })}
                              </div>
                            </div>
                          )}

                          {/* Other runs */}
                          {groupedRuns.other.length > 0 && (
                            <div className="space-y-2">
                              <div className="flex items-center gap-2 text-sm font-medium">
                                <History className="h-4 w-4 text-muted-foreground" />
                                Other ({groupedRuns.other.length})
                              </div>
                              <div className="space-y-1">
                                {groupedRuns.other.map(run => {
                                  const hasResults = run.results && run.results.length > 0
                                  const successResults = run.results?.filter(r => r.success) ?? []
                                  return (
                                    <Collapsible key={run.id}>
                                      <CollapsibleTrigger asChild disabled={!hasResults}>
                                        <div className={`flex items-center justify-between gap-2 p-2 rounded bg-muted/30 text-sm ${hasResults ? "hover:bg-muted/50 cursor-pointer" : ""}`}>
                                          <div className="flex items-center gap-2 min-w-0">
                                            {run.status === "success" && <CheckCircle2 className="h-3 w-3 text-primary shrink-0" />}
                                            {run.status === "running" && <Loader2 className="h-3 w-3 animate-spin text-yellow-500 shrink-0" />}
                                            {run.status === "failed" && <XCircle className="h-3 w-3 text-destructive shrink-0" />}
                                            {run.status === "partial" && <AlertTriangle className="h-3 w-3 text-yellow-500 shrink-0" />}
                                            {run.status === "pending" && <Clock className="h-3 w-3 text-muted-foreground shrink-0" />}
                                            <span className="text-xs text-muted-foreground">{run.totalFeedItems} items</span>
                                          </div>
                                          <div className="flex items-center gap-2 shrink-0">
                                            <Badge variant="secondary" className="text-xs">+{run.torrentsAdded}</Badge>
                                            {run.torrentsFailed > 0 && (
                                              <Badge variant="destructive" className="text-xs">{run.torrentsFailed} failed</Badge>
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
                                              <div key={`${result.instanceId}-${i}`} className="flex items-center gap-2 text-xs">
                                                <Badge variant="default" className="text-[10px] shrink-0 w-20 justify-center truncate" title={result.instanceName}>{result.instanceName}</Badge>
                                                {result.indexerName && (
                                                  <Badge variant="secondary" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName}</Badge>
                                                )}
                                                <span className="truncate text-muted-foreground">{result.matchedTorrentName}</span>
                                              </div>
                                            ))}
                                            {successResults.length === 0 && run.results && run.results.length > 0 && (
                                              <span className="text-xs text-muted-foreground">No successful additions</span>
                                            )}
                                          </div>
                                        </CollapsibleContent>
                                      )}
                                    </Collapsible>
                                  )
                                })}
                              </div>
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="text-center py-2 text-xs text-muted-foreground">
                          No RSS automation runs recorded yet.
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

          <CompletionOverview />

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
                      <div className="flex items-center gap-1.5">
                        <Label htmlFor="global-use-cross-category-suffix" className="font-medium">Add .cross category suffix</Label>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button type="button" className="text-muted-foreground hover:text-foreground" aria-label="Category suffix help">
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent align="start" className="max-w-xs text-xs">
                            Creates isolated categories (e.g., tv.cross) with the same save path as the base category. Cross-seeds inherit autoTMM from the matched torrent and are saved to the same location as the original files.
                          </TooltipContent>
                        </Tooltip>
                      </div>
                      <p className="text-xs text-muted-foreground">Keeps cross-seeds separate from *arr applications to prevent import loops.</p>
                    </div>
                    <Switch
                      id="global-use-cross-category-suffix"
                      checked={globalSettings.useCrossCategorySuffix}
                      disabled={globalSettings.useCategoryFromIndexer}
                      onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, useCrossCategorySuffix: !!value }))}
                    />
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <div className="space-y-0.5">
                      <div className="flex items-center gap-1.5">
                        <Label htmlFor="global-use-category-from-indexer" className="font-medium">Use indexer name as category</Label>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button type="button" className="text-muted-foreground hover:text-foreground" aria-label="Indexer category help">
                              <Info className="h-3.5 w-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent align="start" className="max-w-xs text-xs">
                            Creates a category named after the indexer. Save path and autoTMM are inherited from the matched torrent. Useful for tracking which indexer provided each cross-seed.
                          </TooltipContent>
                        </Tooltip>
                      </div>
                      <p className="text-xs text-muted-foreground">Set category to indexer name. Cannot be used with .cross suffix.</p>
                    </div>
                    <Switch
                      id="global-use-category-from-indexer"
                      checked={globalSettings.useCategoryFromIndexer}
                      disabled={globalSettings.useCrossCategorySuffix}
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

              <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-4">
                <div className="space-y-1">
                  <p className="text-sm font-medium leading-none">Auto-resume behavior</p>
                  <p className="text-xs text-muted-foreground">
                    Control whether cross-seed torrents are automatically resumed after hash check.
                    When enabled, torrents remain paused for manual review.
                  </p>
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <div className="flex items-center justify-between gap-3">
                    <div className="space-y-0.5">
                      <Label htmlFor="skip-auto-resume-rss" className="font-medium">Skip for RSS</Label>
                      <p className="text-xs text-muted-foreground">Keep RSS automation torrents paused</p>
                    </div>
                    <Switch
                      id="skip-auto-resume-rss"
                      checked={globalSettings.skipAutoResumeRss}
                      onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, skipAutoResumeRss: !!value }))}
                    />
                  </div>

                  <div className="flex items-center justify-between gap-3">
                    <div className="space-y-0.5">
                      <Label htmlFor="skip-auto-resume-seeded" className="font-medium">Skip for Seeded Search</Label>
                      <p className="text-xs text-muted-foreground">Keep seeded search & interactive dialog torrents paused</p>
                    </div>
                    <Switch
                      id="skip-auto-resume-seeded"
                      checked={globalSettings.skipAutoResumeSeededSearch}
                      onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, skipAutoResumeSeededSearch: !!value }))}
                    />
                  </div>

                  <div className="flex items-center justify-between gap-3">
                    <div className="space-y-0.5">
                      <Label htmlFor="skip-auto-resume-completion" className="font-medium">Skip for Completion</Label>
                      <p className="text-xs text-muted-foreground">Keep completion-triggered torrents paused</p>
                    </div>
                    <Switch
                      id="skip-auto-resume-completion"
                      checked={globalSettings.skipAutoResumeCompletion}
                      onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, skipAutoResumeCompletion: !!value }))}
                    />
                  </div>

                  <div className="flex items-center justify-between gap-3">
                    <div className="space-y-0.5">
                      <Label htmlFor="skip-auto-resume-webhook" className="font-medium">Skip for Webhook</Label>
                      <p className="text-xs text-muted-foreground">Keep /apply webhook torrents paused</p>
                    </div>
                    <Switch
                      id="skip-auto-resume-webhook"
                      checked={globalSettings.skipAutoResumeWebhook}
                      onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, skipAutoResumeWebhook: !!value }))}
                    />
                  </div>
                </div>
              </div>

              <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
                <div className="space-y-1">
                  <p className="text-sm font-medium leading-none">Recheck behavior</p>
                  <p className="text-xs text-muted-foreground">
                    Control whether cross-seeds requiring disk verification are skipped.
                  </p>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <div className="space-y-0.5">
                    <Label htmlFor="skip-recheck" className="font-medium">Skip recheck-required matches</Label>
                    <p className="text-xs text-muted-foreground">Skip matches needing rename alignment or extra files</p>
                  </div>
                  <Switch
                    id="skip-recheck"
                    checked={globalSettings.skipRecheck}
                    onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, skipRecheck: !!value }))}
                  />
                </div>
              </div>

              <Collapsible className="rounded-lg border border-border/70 bg-muted/40">
                <CollapsibleTrigger className="flex w-full items-center justify-between p-4 font-medium [&[data-state=open]>svg]:rotate-180">
                  <span>Webhook Source Filters</span>
                  <ChevronDown className="h-4 w-4 transition-transform duration-200" />
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className="border-t border-border/70 p-4 pt-4 space-y-4">
                    <p className="text-xs text-muted-foreground">
                      Filter which local torrents are considered when autobrr calls the webhook endpoint.
                      Empty filters mean all torrents are checked. If you configure both category and tag filters, torrents must match both.
                    </p>

                    <div className="grid gap-4 md:grid-cols-2">
                      <div className="space-y-3">
                        <Label>Exclude categories</Label>
                        <MultiSelect
                          options={webhookSourceCategorySelectOptions}
                          selected={globalSettings.webhookSourceExcludeCategories}
                          onChange={values => setGlobalSettings(prev => ({ ...prev, webhookSourceExcludeCategories: values }))}
                          placeholder={webhookSourceCategorySelectOptions.length ? "None" : "Type to add categories"}
                          creatable
                        />
                        <p className="text-xs text-muted-foreground">
                          {globalSettings.webhookSourceExcludeCategories.length === 0
                            ? "No categories excluded."
                            : `${globalSettings.webhookSourceExcludeCategories.length} categor${globalSettings.webhookSourceExcludeCategories.length === 1 ? "y" : "ies"} will be skipped.`}
                        </p>
                      </div>

                      <div className="space-y-3">
                        <Label>Exclude tags</Label>
                        <MultiSelect
                          options={webhookSourceTagSelectOptions}
                          selected={globalSettings.webhookSourceExcludeTags}
                          onChange={values => setGlobalSettings(prev => ({ ...prev, webhookSourceExcludeTags: values }))}
                          placeholder={webhookSourceTagSelectOptions.length ? "None" : "Type to add tags"}
                          creatable
                        />
                        <p className="text-xs text-muted-foreground">
                          {globalSettings.webhookSourceExcludeTags.length === 0
                            ? "No tags excluded."
                            : `${globalSettings.webhookSourceExcludeTags.length} tag${globalSettings.webhookSourceExcludeTags.length === 1 ? "" : "s"} will be skipped.`}
                        </p>
                      </div>
                    </div>

                    <div className="grid gap-4 md:grid-cols-2">
                      <div className="space-y-3">
                        <Label>Include categories</Label>
                        <MultiSelect
                          options={webhookSourceCategorySelectOptions}
                          selected={globalSettings.webhookSourceCategories}
                          onChange={values => setGlobalSettings(prev => ({ ...prev, webhookSourceCategories: values }))}
                          placeholder={webhookSourceCategorySelectOptions.length ? "All categories (leave empty for all)" : "Type to add categories"}
                          creatable
                        />
                        <p className="text-xs text-muted-foreground">
                          {globalSettings.webhookSourceCategories.length === 0
                            ? "All categories will be included."
                            : `Only ${globalSettings.webhookSourceCategories.length} selected categor${globalSettings.webhookSourceCategories.length === 1 ? "y" : "ies"} will be matched.`}
                        </p>
                      </div>

                      <div className="space-y-3">
                        <Label>Include tags</Label>
                        <MultiSelect
                          options={webhookSourceTagSelectOptions}
                          selected={globalSettings.webhookSourceTags}
                          onChange={values => setGlobalSettings(prev => ({ ...prev, webhookSourceTags: values }))}
                          placeholder={webhookSourceTagSelectOptions.length ? "All tags (leave empty for all)" : "Type to add tags"}
                          creatable
                        />
                        <p className="text-xs text-muted-foreground">
                          {globalSettings.webhookSourceTags.length === 0
                            ? "All tags will be included."
                            : `Only ${globalSettings.webhookSourceTags.length} selected tag${globalSettings.webhookSourceTags.length === 1 ? "" : "s"} will be matched.`}
                        </p>
                      </div>
                    </div>
                  </div>
                </CollapsibleContent>
              </Collapsible>

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
