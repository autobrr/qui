/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
import { Checkbox } from "@/components/ui/checkbox"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/utils"
import type {
  CrossSeedAutomationSettings,
  CrossSeedAutomationStatus,
  CrossSeedRun
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  Info,
  Loader2,
  Play,
  Rocket,
  XCircle
} from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

interface AutomationFormState {
  enabled: boolean
  runIntervalMinutes: number
  startPaused: boolean
  category: string
  tags: string
  ignorePatterns: string
  targetInstanceIds: number[]
  targetIndexerIds: number[]
  maxResultsPerRun: number
}

interface GlobalCrossSeedSettings {
  findIndividualEpisodes: boolean
  sizeMismatchTolerancePercent: number
}

const DEFAULT_AUTOMATION_FORM: AutomationFormState = {
  enabled: false,
  runIntervalMinutes: 120,
  startPaused: true,
  category: "",
  tags: "",
  ignorePatterns: "",
  targetInstanceIds: [],
  targetIndexerIds: [],
  maxResultsPerRun: 50,
}

const DEFAULT_GLOBAL_SETTINGS: GlobalCrossSeedSettings = {
  findIndividualEpisodes: false,
  sizeMismatchTolerancePercent: 5.0,
}

function parseList(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map(item => item.trim())
    .filter(Boolean)
}

function formatDate(value?: string | null): string {
  if (!value) return "—"
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

export function CrossSeedPage() {
  const queryClient = useQueryClient()
  const [automationForm, setAutomationForm] = useState<AutomationFormState>(DEFAULT_AUTOMATION_FORM)
  const [globalSettings, setGlobalSettings] = useState<GlobalCrossSeedSettings>(DEFAULT_GLOBAL_SETTINGS)
  const [formInitialized, setFormInitialized] = useState(false)
  const [globalSettingsInitialized, setGlobalSettingsInitialized] = useState(false)
  const [dryRun, setDryRun] = useState(false)
  const [searchInstanceId, setSearchInstanceId] = useState<number | null>(null)
  const [searchCategories, setSearchCategories] = useState<string[]>([])
  const [searchTags, setSearchTags] = useState<string[]>([])
  const [searchIndexerIds, setSearchIndexerIds] = useState<number[]>([])
  const [searchIntervalSeconds, setSearchIntervalSeconds] = useState(60)
  const [searchCooldownMinutes, setSearchCooldownMinutes] = useState(720)
  const [searchResultsOpen, setSearchResultsOpen] = useState(false)
  const [showIndexerFilters, setShowIndexerFilters] = useState(false)
  const [showAutomationIndexerFilters, setShowAutomationIndexerFilters] = useState(false)
  const [showSearchCategories, setShowSearchCategories] = useState(false)
  const [showSearchTags, setShowSearchTags] = useState(false)
  const [showAutomationInstances, setShowAutomationInstances] = useState(false)

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

  const { data: runs, refetch: refetchRuns } = useQuery({
    queryKey: ["cross-seed", "runs"],
    queryFn: () => api.listCrossSeedRuns({ limit: 10 }),
  })

  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })

  const { data: indexers } = useQuery({
    queryKey: ["torznab", "indexers"],
    queryFn: () => api.listTorznabIndexers(),
  })

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

  useEffect(() => {
    if (settings && !formInitialized) {
      setAutomationForm({
        enabled: settings.enabled,
        runIntervalMinutes: settings.runIntervalMinutes,
        startPaused: settings.startPaused,
        category: settings.category ?? "",
        tags: settings.tags.join(", "),
        ignorePatterns: settings.ignorePatterns.join("\n"),
        targetInstanceIds: settings.targetInstanceIds,
        targetIndexerIds: settings.targetIndexerIds,
        maxResultsPerRun: settings.maxResultsPerRun,
      })
      setFormInitialized(true)
    }
  }, [settings, formInitialized])

  useEffect(() => {
    if (settings && !globalSettingsInitialized) {
      setGlobalSettings({
        findIndividualEpisodes: settings.findIndividualEpisodes,
        sizeMismatchTolerancePercent: settings.sizeMismatchTolerancePercent ?? 5.0,
      })
      setGlobalSettingsInitialized(true)
    }
  }, [settings, globalSettingsInitialized])

  useEffect(() => {
    if (!searchInstanceId && instances && instances.length > 0) {
      setSearchInstanceId(instances[0].id)
    }
  }, [instances, searchInstanceId])

  const updateSettingsMutation = useMutation({
    mutationFn: (payload: CrossSeedAutomationSettings) => api.updateCrossSeedSettings(payload),
    onSuccess: (data) => {
      toast.success("Automation settings updated")
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

  const updateGlobalSettingsMutation = useMutation({
    mutationFn: (payload: CrossSeedAutomationSettings) => api.updateCrossSeedSettings(payload),
    onSuccess: (data) => {
      toast.success("Global settings updated")
      // Update the cache and invalidate to ensure fresh data
      queryClient.setQueryData(["cross-seed", "settings"], data)
      queryClient.invalidateQueries({ queryKey: ["cross-seed", "settings"] })
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const triggerRunMutation = useMutation({
    mutationFn: (payload: { limit?: number; dryRun?: boolean }) => api.triggerCrossSeedRun(payload),
    onSuccess: () => {
      toast.success("Automation run started")
      refetchStatus()
      refetchRuns()
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const handleGlobalSettingsSave = () => {
    // Get current settings to merge with global changes
    if (!settings) return
    
    const payload: CrossSeedAutomationSettings = {
      // Use current automation form values if they've been modified, otherwise use saved settings
      enabled: formInitialized ? automationForm.enabled : settings.enabled,
      runIntervalMinutes: formInitialized ? automationForm.runIntervalMinutes : settings.runIntervalMinutes,
      startPaused: formInitialized ? automationForm.startPaused : settings.startPaused,
      category: formInitialized ? (automationForm.category.trim() || null) : settings.category,
      tags: formInitialized ? parseList(automationForm.tags) : settings.tags,
      ignorePatterns: formInitialized ? parseList(automationForm.ignorePatterns.replace(/\r/g, "")) : settings.ignorePatterns,
      targetInstanceIds: formInitialized ? automationForm.targetInstanceIds : settings.targetInstanceIds,
      targetIndexerIds: formInitialized ? automationForm.targetIndexerIds : settings.targetIndexerIds,
      maxResultsPerRun: formInitialized ? automationForm.maxResultsPerRun : settings.maxResultsPerRun,
      // Only update the global settings
      findIndividualEpisodes: globalSettings.findIndividualEpisodes,
      sizeMismatchTolerancePercent: globalSettings.sizeMismatchTolerancePercent,
    }
    updateGlobalSettingsMutation.mutate(payload)
  }

  const handleAutomationSave = () => {
    const payload: CrossSeedAutomationSettings = {
      enabled: automationForm.enabled,
      runIntervalMinutes: automationForm.runIntervalMinutes,
      startPaused: automationForm.startPaused,
      category: automationForm.category.trim() || null,
      tags: parseList(automationForm.tags),
      ignorePatterns: parseList(automationForm.ignorePatterns.replace(/\r/g, "")),
      targetInstanceIds: automationForm.targetInstanceIds,
      targetIndexerIds: automationForm.targetIndexerIds,
      maxResultsPerRun: automationForm.maxResultsPerRun,
      findIndividualEpisodes: globalSettings.findIndividualEpisodes,
      sizeMismatchTolerancePercent: globalSettings.sizeMismatchTolerancePercent,
    }
    updateSettingsMutation.mutate(payload)
  }

  const automationStatus: CrossSeedAutomationStatus | undefined = status
  const latestRun: CrossSeedRun | null | undefined = automationStatus?.lastRun

  const searchCategoryOptions = useMemo(() => {
    if (!searchMetadata?.categories) return [] as string[]
    return Object.keys(searchMetadata.categories).sort()
  }, [searchMetadata])

  const searchTagOptions = useMemo(() => searchMetadata?.tags ?? [], [searchMetadata])

  const handleToggleInstance = (instanceId: number, checked: boolean) => {
    setAutomationForm(prev => {
      const nextIds = checked
        ? Array.from(new Set([...prev.targetInstanceIds, instanceId]))
        : prev.targetInstanceIds.filter(id => id !== instanceId)
      return { ...prev, targetInstanceIds: nextIds }
    })
  }

  const handleToggleIndexer = (indexerId: number, checked: boolean) => {
    setAutomationForm(prev => {
      const nextIds = checked
        ? Array.from(new Set([...prev.targetIndexerIds, indexerId]))
        : prev.targetIndexerIds.filter(id => id !== indexerId)
      return { ...prev, targetIndexerIds: nextIds }
    })
  }

  const toggleSearchCategory = (category: string) => {
    setSearchCategories(prev =>
      prev.includes(category) ? prev.filter(value => value !== category) : [...prev, category]
    )
  }

  const toggleSearchTag = (tag: string) => {
    setSearchTags(prev =>
      prev.includes(tag) ? prev.filter(value => value !== tag) : [...prev, tag]
    )
  }

  const toggleSearchIndexer = (indexerId: number) => {
    setSearchIndexerIds(prev =>
      prev.includes(indexerId) ? prev.filter(value => value !== indexerId) : [...prev, indexerId]
    )
  }

  const handleStartSearchRun = () => {
    if (!searchInstanceId) {
      toast.error("Select an instance to run against")
      return
    }

    startSearchRunMutation.mutate({
      instanceId: searchInstanceId,
      categories: searchCategories,
      tags: searchTags,
      intervalSeconds: Math.max(60, Number(searchIntervalSeconds) || 60),
      indexerIds: searchIndexerIds,
      cooldownMinutes: Math.max(720, Number(searchCooldownMinutes) || 720),
    })
  }

  const runSummary = useMemo(() => {
    if (!latestRun) return "No runs yet"
    return `${latestRun.status.toUpperCase()} • Added ${latestRun.torrentsAdded} / Failed ${latestRun.torrentsFailed} • ${formatDate(latestRun.startedAt)}`
  }, [latestRun])

  const searchRunning = searchStatus?.running ?? false
  const activeSearchRun = searchStatus?.run
  const recentSearchResults = searchStatus?.recentResults ?? []
  const recentAddedResults = useMemo(
    () => recentSearchResults.filter(result => result.added),
    [recentSearchResults]
  )

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

  return (
    <div className="space-y-6 p-6 pb-16">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Cross-Seed</h1>
        <p className="text-sm text-muted-foreground">Identify compatible torrents and automate cross-seeding across your instances.</p>
      </div>

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
                <span>Last used {formatRelativeTime(searchCacheStats.lastUsedAt)}</span>
              </div>
              <Button variant="link" size="xs" className="px-0" asChild>
                <Link to="/settings" search={{ tab: "search-cache" }}>
                  Manage cache settings
                </Link>
              </Button>
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="global-find-individual-episodes" className="flex items-center gap-2">
              <Switch
                id="global-find-individual-episodes"
                checked={globalSettings.findIndividualEpisodes}
                onCheckedChange={value => setGlobalSettings(prev => ({ ...prev, findIndividualEpisodes: !!value }))}
              />
              Find individual episodes
            </Label>
            <p className="text-xs text-muted-foreground">
              When enabled, season packs will also match individual episodes. When disabled, season packs only match other season packs.
            </p>
          </div>
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
              Filters out search results with sizes differing by more than this percentage. Set to 0 for exact size matching.
            </p>
          </div>
      </CardContent>
      <CardFooter className="flex items-center gap-3">
        <Button
          onClick={handleGlobalSettingsSave}
          disabled={updateGlobalSettingsMutation.isPending}
          >
            {updateGlobalSettingsMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Save settings
          </Button>
        </CardFooter>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Seeded Torrent Search</CardTitle>
          <CardDescription>Walk the torrents you already seed on the selected instance, collapse identical content down to the oldest copy, and query Torznab feeds once per unique release while skipping trackers you already have it from.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label>Source instance</Label>
            <Select
              value={searchInstanceId ? String(searchInstanceId) : ""}
              onValueChange={(value) => setSearchInstanceId(Number(value))}
              disabled={!instances?.length}
            >
              <SelectTrigger>
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

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="search-interval">Interval between torrents (seconds)</Label>
              <Input
                id="search-interval"
                type="number"
                min={60}
                value={searchIntervalSeconds}
                onChange={event => setSearchIntervalSeconds(Math.max(60, Number(event.target.value) || 60))}
              />
              <p className="text-xs text-muted-foreground">Wait time before scanning the next seeded torrent. Minimum 60 seconds.</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="search-cooldown">Cooldown (minutes)</Label>
              <Input
                id="search-cooldown"
                type="number"
                min={720}
                value={searchCooldownMinutes}
                onChange={event => setSearchCooldownMinutes(Math.max(720, Number(event.target.value) || 720))}
              />
              <p className="text-xs text-muted-foreground">Skip seeded torrents that were searched more recently than this window. Minimum 720 minutes.</p>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div>
                <Label>Categories</Label>
                <p className="text-xs text-muted-foreground">Limit the seeded-torrent scan to specific qBittorrent categories.</p>
              </div>
              {searchCategoryOptions.length > 0 && (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  onClick={() => setShowSearchCategories(prev => !prev)}
                >
                  {showSearchCategories ? "Hide" : "Customize"}
                </Button>
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              {searchCategories.length === 0
                ? "All categories will be included in the scan."
                : `Only ${searchCategories.length} selected categor${searchCategories.length === 1 ? "y" : "ies"} will be scanned.`}
            </div>
            {showSearchCategories && (
              <div className="flex flex-wrap gap-2">
                {searchCategoryOptions.length > 0 ? (
                  searchCategoryOptions.map(category => (
                    <Label key={category} className="flex items-center gap-2 border rounded-md px-2 py-1 text-xs cursor-pointer">
                      <Checkbox checked={searchCategories.includes(category)} onCheckedChange={() => toggleSearchCategory(category)} />
                      {category}
                    </Label>
                  ))
                ) : (
                  <p className="text-xs text-muted-foreground">Categories load after selecting an instance.</p>
                )}
              </div>
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div>
                <Label>Tags</Label>
                <p className="text-xs text-muted-foreground">Restrict the scan to torrents with at least one of these tags.</p>
              </div>
              {searchTagOptions.length > 0 && (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  onClick={() => setShowSearchTags(prev => !prev)}
                >
                  {showSearchTags ? "Hide" : "Customize"}
                </Button>
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              {searchTags.length === 0
                ? "All tags will be included in the scan."
                : `Only ${searchTags.length} selected tag${searchTags.length === 1 ? "" : "s"} will be scanned.`}
            </div>
            {showSearchTags && (
              <div className="flex flex-wrap gap-2">
                {searchTagOptions.length > 0 ? (
                  searchTagOptions.map(tag => (
                    <Label key={tag} className="flex items-center gap-2 border rounded-md px-2 py-1 text-xs cursor-pointer">
                      <Checkbox checked={searchTags.includes(tag)} onCheckedChange={() => toggleSearchTag(tag)} />
                      {tag}
                    </Label>
                  ))
                ) : (
                  <p className="text-xs text-muted-foreground">No tags found for this instance.</p>
                )}
              </div>
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div>
                <Label>Indexers</Label>
                <p className="text-xs text-muted-foreground">Select Torznab indexers to query. Trackers you already seed from are skipped automatically.</p>
              </div>
              {indexers && indexers.length > 0 && (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  onClick={() => setShowIndexerFilters(prev => !prev)}
                >
                  {showIndexerFilters ? "Hide" : "Customize"}
                </Button>
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              {searchIndexerIds.length === 0
                ? "All enabled Torznab indexers will be queried for matches."
                : `Only ${searchIndexerIds.length} selected indexer${searchIndexerIds.length === 1 ? "" : "s"} will be queried.`}
            </div>
            {showIndexerFilters && (
              <div className="flex flex-wrap gap-2">
                {indexers && indexers.length > 0 ? (
                  indexers.map(indexer => (
                    <Label key={indexer.id} className="flex items-center gap-2 border rounded-md px-2 py-1 text-xs cursor-pointer">
                      <Checkbox
                        checked={searchIndexerIds.includes(indexer.id)}
                        onCheckedChange={() => toggleSearchIndexer(indexer.id)}
                      />
                      {indexer.name}
                    </Label>
                  ))
                ) : (
                  <p className="text-xs text-muted-foreground">No Torznab indexers configured.</p>
                )}
              </div>
            )}
            {!indexers?.length && (
              <p className="text-xs text-muted-foreground">No Torznab indexers configured.</p>
            )}
          </div>

          <Separator />

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
            {activeSearchRun ? (
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
                <span className="font-medium">{formatDate(activeSearchRun.startedAt)}</span>
              </div>
              {estimatedCompletionInfo && (
                <div className="flex items-center gap-4">
                  <span className="text-muted-foreground">Est. completion:</span>
                  <div className="flex flex-col">
                    <span className="font-medium">{formatDate(estimatedCompletionInfo.eta.toISOString())}</span>
                    <span className="text-[10px] text-muted-foreground">
                      ≈ {estimatedCompletionInfo.remaining} torrents remaining @ {estimatedCompletionInfo.interval}s intervals
                    </span>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">No active run</p>
          )}
        </div>

          <Collapsible open={searchResultsOpen} onOpenChange={setSearchResultsOpen} className="border rounded-md">
            <CollapsibleTrigger className="flex w-full items-center justify-between px-3 py-2 text-sm font-medium">
              <span>Recent search additions</span>
              <Badge variant="outline">{recentAddedResults.length}</Badge>
            </CollapsibleTrigger>
            <CollapsibleContent className="px-3 pb-3">
              {recentAddedResults.length === 0 ? (
                <p className="text-xs text-muted-foreground">No added cross-seed results recorded yet.</p>
              ) : (
                <ul className="space-y-2">
                  {recentAddedResults.map(result => (
                    <li key={`${result.torrentHash}-${result.processedAt}`} className="flex items-start justify-between gap-3 rounded border px-2 py-2">
                      <div className="space-y-1">
                        <p className="text-sm font-medium leading-tight">{result.torrentName}</p>
                        <p className="text-xs text-muted-foreground">{result.indexerName} • {result.releaseTitle}</p>
                        {result.message && <p className="text-xs text-muted-foreground">{result.message}</p>}
                        <p className="text-[10px] text-muted-foreground">{formatDate(result.processedAt)}</p>
                      </div>
                      <Badge variant="default">Added</Badge>
                    </li>
                  ))}
                </ul>
              )}
            </CollapsibleContent>
          </Collapsible>
        </CardContent>
        <CardFooter className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-xs text-muted-foreground">
            Next run: {searchStatus?.nextRunAt ? formatDate(searchStatus.nextRunAt) : "—"}
          </p>
          <div className="flex items-center gap-2">
            <Button
              onClick={handleStartSearchRun}
              disabled={!searchInstanceId || startSearchRunMutation.isPending || searchRunning}
            >
              {startSearchRunMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Rocket className="mr-2 h-4 w-4" />}
              Start run
            </Button>
            <Button
              variant="outline"
              onClick={() => cancelSearchRunMutation.mutate()}
              disabled={!searchRunning || cancelSearchRunMutation.isPending}
            >
              {cancelSearchRunMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <XCircle className="mr-2 h-4 w-4" />}
              Cancel
            </Button>
          </div>
        </CardFooter>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>RSS Automation</CardTitle>
          <CardDescription>Poll tracker RSS feeds on a fixed interval and add matching cross-seeds automatically.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="automation-enabled" className="flex items-center gap-2">
                <Switch
                  id="automation-enabled"
                  checked={automationForm.enabled}
                  onCheckedChange={value => setAutomationForm(prev => ({ ...prev, enabled: !!value }))}
                />
                Enable RSS automation
              </Label>
            </div>
            <div className="space-y-2">
              <Label htmlFor="automation-start-paused" className="flex items-center gap-2">
                <Switch
                  id="automation-start-paused"
                  checked={automationForm.startPaused}
                  onCheckedChange={value => setAutomationForm(prev => ({ ...prev, startPaused: !!value }))}
                />
                Start torrents paused
              </Label>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="automation-interval">RSS run interval (minutes)</Label>
              <Input
                id="automation-interval"
                type="number"
                min={5}
                value={automationForm.runIntervalMinutes}
                onChange={event => setAutomationForm(prev => ({ ...prev, runIntervalMinutes: Number(event.target.value) }))}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="automation-max-results">Max RSS results per run</Label>
              <Input
                id="automation-max-results"
                type="number"
                min={1}
                value={automationForm.maxResultsPerRun}
                onChange={event => setAutomationForm(prev => ({ ...prev, maxResultsPerRun: Number(event.target.value) }))}
              />
              <p className="text-xs text-muted-foreground">
                Torznab feeds only deliver the 100 newest results. Use 100 here to scan the entire feed each run.
              </p>
            </div>
          </div>

            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Label htmlFor="automation-category">Category</Label>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        className="text-muted-foreground hover:text-foreground"
                        aria-label="Category help"
                      >
                        <Info className="h-4 w-4" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent align="start" className="max-w-xs text-xs">
                      Leave this blank to reuse the matched torrent&apos;s category. Only set it when every automated add should force a specific qBittorrent category.
                    </TooltipContent>
                  </Tooltip>
                </div>
                <Input
                  id="automation-category"
                  placeholder="Optional"
                  value={automationForm.category}
                  onChange={event => setAutomationForm(prev => ({ ...prev, category: event.target.value }))}
                />
              </div>
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Label htmlFor="automation-tags">Tags</Label>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        className="text-muted-foreground hover:text-foreground"
                        aria-label="Tags help"
                      >
                        <Info className="h-4 w-4" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent align="start" className="max-w-xs text-xs">
                      Comma-separated list applied to every cross-seeded torrent. If left empty the service reuses the source torrent tags and still adds the default <span className="font-semibold">cross-seed</span> tag.
                    </TooltipContent>
                  </Tooltip>
                </div>
                <Input
                  id="automation-tags"
                  placeholder="Comma separated"
                  value={automationForm.tags}
                onChange={event => setAutomationForm(prev => ({ ...prev, tags: event.target.value }))}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="automation-ignore">Ignore patterns</Label>
            <Textarea
              id="automation-ignore"
              placeholder="*.nfo\n*.txt"
              rows={3}
              value={automationForm.ignorePatterns}
              onChange={event => setAutomationForm(prev => ({ ...prev, ignorePatterns: event.target.value }))}
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div>
                <Label>Target instances</Label>
                <p className="text-xs text-muted-foreground">Select which qBittorrent instances will receive cross-seeds.</p>
              </div>
              {instances && instances.length > 0 && (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  onClick={() => setShowAutomationInstances(prev => !prev)}
                >
                  {showAutomationInstances ? "Hide" : "Customize"}
                </Button>
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              {automationForm.targetInstanceIds.length === 0
                ? "No instances selected. Please select at least one instance."
                : `${automationForm.targetInstanceIds.length} instance${automationForm.targetInstanceIds.length === 1 ? "" : "s"} selected.`}
            </div>
            {showAutomationInstances && (
              <div className="flex flex-wrap gap-2">
                {instances?.map(instance => (
                  <Label key={instance.id} className="flex items-center gap-2 text-xs font-medium border rounded-md px-2 py-1 cursor-pointer">
                    <Checkbox
                      checked={automationForm.targetInstanceIds.includes(instance.id)}
                      onCheckedChange={value => handleToggleInstance(instance.id, !!value)}
                    />
                    {instance.name}
                  </Label>
                ))}
                {(!instances || instances.length === 0) && (
                  <p className="text-xs text-muted-foreground">No instances available.</p>
                )}
              </div>
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div>
                <Label>Target indexers</Label>
                <p className="text-xs text-muted-foreground">Select Torznab indexers to poll for RSS feeds.</p>
              </div>
              {indexers && indexers.length > 0 && (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  onClick={() => setShowAutomationIndexerFilters(prev => !prev)}
                >
                  {showAutomationIndexerFilters ? "Hide" : "Customize"}
                </Button>
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              {automationForm.targetIndexerIds.length === 0
                ? "All enabled Torznab indexers are eligible for RSS automation."
                : `Only ${automationForm.targetIndexerIds.length} selected indexer${automationForm.targetIndexerIds.length === 1 ? "" : "s"} will be polled.`}
            </div>
            {showAutomationIndexerFilters && (
              <div className="flex flex-wrap gap-2">
                {indexers && indexers.length > 0 ? (
                  indexers.map(indexer => {
                    const id = Number(indexer.id)
                    return (
                      <Label key={indexer.id} className="flex items-center gap-2 text-xs font-medium border rounded-md px-2 py-1 cursor-pointer">
                        <Checkbox
                          checked={automationForm.targetIndexerIds.includes(id)}
                          onCheckedChange={value => handleToggleIndexer(id, !!value)}
                        />
                        {indexer.name}
                      </Label>
                    )
                  })
                ) : (
                  <p className="text-xs text-muted-foreground">No Torznab indexers configured.</p>
                )}
              </div>
            )}
            {!indexers?.length && (
              <p className="text-xs text-muted-foreground">No Torznab indexers configured.</p>
            )}
          </div>

          <Separator />

          <div className="rounded-lg border bg-muted/50 p-4 space-y-3">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">Status</p>
              <Badge variant={automationStatus?.running ? "default" : "secondary"}>
                {automationStatus?.running ? "RUNNING" : "IDLE"}
              </Badge>
            </div>
            {automationStatus?.running ? (
              <p className="text-xs text-muted-foreground">RSS automation run in progress</p>
            ) : (
              <div className="text-xs">
                <span className="text-muted-foreground">Next run:</span>{" "}
                <span className="font-medium">{automationStatus?.nextRunAt ? formatDate(automationStatus.nextRunAt) : "—"}</span>
              </div>
            )}
          </div>
        </CardContent>
        <CardFooter className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2 text-xs">
            <Switch id="automation-dry-run" checked={dryRun} onCheckedChange={value => setDryRun(!!value)} />
            <Label htmlFor="automation-dry-run">Dry run</Label>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              onClick={() => triggerRunMutation.mutate({ limit: automationForm.maxResultsPerRun, dryRun })}
              disabled={triggerRunMutation.isPending}
            >
              {triggerRunMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Play className="mr-2 h-4 w-4" />}
              Run now
            </Button>
            <Button
              onClick={handleAutomationSave}
              disabled={updateSettingsMutation.isPending}
            >
              {updateSettingsMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Save settings
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
          <CardTitle>Recent Runs</CardTitle>
          <CardDescription>{runSummary}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {runs && runs.length > 0 ? (
            runs.map(run => (
              <div key={run.id} className="rounded border p-3 space-y-1">
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    <Badge variant="outline" className="uppercase text-xs">{run.status}</Badge>
                    <span>{run.triggeredBy}</span>
                  </div>
                  <span className="text-xs text-muted-foreground">{formatDate(run.startedAt)}</span>
                </div>
                <div className="flex items-center gap-3 text-xs text-muted-foreground">
                  <span>Added {run.torrentsAdded}</span>
                  <span>Skipped {run.torrentsSkipped}</span>
                  <span>Failed {run.torrentsFailed}</span>
                </div>
                {run.message && (
                  <p className="text-xs text-muted-foreground">{run.message}</p>
                )}
              </div>
            ))
          ) : (
            <p className="text-sm text-muted-foreground">No RSS automation runs recorded yet.</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
