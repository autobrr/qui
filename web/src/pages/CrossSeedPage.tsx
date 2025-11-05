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
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import type {
  CrossSeedAutomationSettings,
  CrossSeedAutomationStatus,
  CrossSeedFindCandidatesResponse,
  CrossSeedResponse,
  CrossSeedRun,
  InstanceResponse
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Check,
  Loader2,
  Play,
  Rocket,
  UploadCloud,
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
}

function parseList(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map(item => item.trim())
    .filter(Boolean)
}

async function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      const base64 = result.includes(",") ? result.split(",")[1] : result
      resolve(base64)
    }
    reader.onerror = () => reject(reader.error ?? new Error("Failed to read file"))
    reader.readAsDataURL(file)
  })
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
      })
      setGlobalSettingsInitialized(true)
    }
  }, [settings, globalSettingsInitialized])

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

  const [findTorrentName, setFindTorrentName] = useState("")
  const [findIgnorePatterns, setFindIgnorePatterns] = useState("")
  const [candidateResult, setCandidateResult] = useState<CrossSeedFindCandidatesResponse | null>(null)

  const findCandidatesMutation = useMutation({
    mutationFn: (payload: { torrentName: string; ignorePatterns: string[] }) =>
      api.findCrossSeedCandidates({
        torrentName: payload.torrentName,
        ignorePatterns: payload.ignorePatterns,
        findIndividualEpisodes: globalSettings.findIndividualEpisodes,
      }),
    onSuccess: (data) => {
      setCandidateResult(data)
    },
    onError: (error: Error) => {
      toast.error(error.message)
      setCandidateResult(null)
    },
  })

  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [targetInstanceIds, setTargetInstanceIds] = useState<number[]>([])
  const [crossSeedCategory, setCrossSeedCategory] = useState("")
  const [crossSeedTags, setCrossSeedTags] = useState("")
  const [crossSeedStartPaused, setCrossSeedStartPaused] = useState(true)
  const [crossSeedResult, setCrossSeedResult] = useState<CrossSeedResponse | null>(null)

  const crossSeedMutation = useMutation({
    mutationFn: async () => {
      if (!selectedFile) {
        throw new Error("Select a .torrent file first")
      }
      const torrentData = await fileToBase64(selectedFile)
      return api.crossSeed({
        torrentData,
        targetInstanceIds: targetInstanceIds.length > 0 ? targetInstanceIds : undefined,
        category: crossSeedCategory.trim() || undefined,
        tags: parseList(crossSeedTags),
        startPaused: crossSeedStartPaused,
        findIndividualEpisodes: globalSettings.findIndividualEpisodes,
      })
    },
    onSuccess: (data) => {
      toast.success("Cross-seed request submitted")
      setCrossSeedResult(data)
    },
    onError: (error: Error) => {
      toast.error(error.message)
      setCrossSeedResult(null)
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
      // Only update the global setting
      findIndividualEpisodes: globalSettings.findIndividualEpisodes,
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
    }
    updateSettingsMutation.mutate(payload)
  }

  const automationStatus: CrossSeedAutomationStatus | undefined = status
  const latestRun: CrossSeedRun | null | undefined = automationStatus?.lastRun

  const instanceMap = useMemo(() => {
    const map = new Map<number, InstanceResponse>()
    instances?.forEach(inst => map.set(inst.id, inst))
    return map
  }, [instances])

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

  const runSummary = useMemo(() => {
    if (!latestRun) return "No runs yet"
    return `${latestRun.status.toUpperCase()} • Added ${latestRun.torrentsAdded} / Failed ${latestRun.torrentsFailed} • ${formatDate(latestRun.startedAt)}`
  }, [latestRun])

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

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Find Candidates</CardTitle>
            <CardDescription>UTTER USELESS FOR NOW. COULD A PROWLARR SEARCH. Analyse an incoming release name and discover existing torrents that can seed it.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="torrent-name">Torrent name</Label>
              <Input
                id="torrent-name"
                placeholder="Example.Show.S01E01.1080p.WEB.x264-GROUP"
                value={findTorrentName}
                onChange={event => setFindTorrentName(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ignore-patterns">Ignore patterns</Label>
              <Textarea
                id="ignore-patterns"
                placeholder="*.nfo\n*sample*.mkv"
                value={findIgnorePatterns}
                onChange={event => setFindIgnorePatterns(event.target.value)}
                rows={3}
              />
            </div>
            <Button
              onClick={() => {
                if (!findTorrentName.trim()) {
                  toast.error("Enter a torrent name to analyse")
                  return
                }
                findCandidatesMutation.mutate({
                  torrentName: findTorrentName.trim(),
                  ignorePatterns: parseList(findIgnorePatterns.replace(/\r/g, "")),
                })
              }}
              disabled={findCandidatesMutation.isPending}
            >
              {findCandidatesMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Find candidates
            </Button>

            {candidateResult && (
              <div className="rounded-md border p-4 space-y-4">
                <div>
                  <p className="text-sm font-medium">Source torrent</p>
                  <p className="text-xs text-muted-foreground break-all">{candidateResult.sourceTorrent?.name ?? findTorrentName}</p>
                </div>
                {candidateResult.candidates.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No matching torrents found across your instances.</p>
                ) : (
                  <div className="space-y-3">
                    {candidateResult.candidates.map(candidate => (
                      <div key={`${candidate.instanceId}-${candidate.matchType ?? "unknown"}`} className="rounded border p-3">
                        <div className="flex items-center gap-2">
                          <p className="font-medium text-sm">
                            {instanceMap.get(candidate.instanceId)?.name ?? candidate.instanceName}
                          </p>
                          <Badge variant="outline" className="text-xs capitalize">{(candidate.matchType ?? "unknown").replace(/-/g, " ")}</Badge>
                        </div>
                        <ul className="mt-2 space-y-1 text-xs text-muted-foreground">
                          {candidate.torrents.map(torrent => (
                            <li key={torrent.hash} className="flex items-center justify-between gap-2">
                              <span className="truncate">{torrent.name}</span>
                              <span>{(torrent.progress * 100).toFixed(0)}%</span>
                            </li>
                          ))}
                        </ul>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Cross-seed Manually</CardTitle>
            <CardDescription>Upload a .torrent file and automatically map it to existing data.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="torrent-file">Torrent file</Label>
              <Input
                id="torrent-file"
                type="file"
                accept=".torrent"
                onChange={event => {
                  const file = event.target.files?.[0]
                  setSelectedFile(file ?? null)
                }}
              />
              {selectedFile && (
                <p className="text-xs text-muted-foreground">{selectedFile.name}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label>Target instances</Label>
              <div className="flex flex-wrap gap-2">
                {instances?.map(instance => {
                  const checked = targetInstanceIds.includes(instance.id)
                  return (
                    <Label key={instance.id} className="flex items-center gap-2 text-xs font-medium border rounded-md px-2 py-1 cursor-pointer">
                      <Checkbox
                        checked={checked}
                        onCheckedChange={value => {
                          setTargetInstanceIds(prev => {
                            if (value) return Array.from(new Set([...prev, instance.id]))
                            return prev.filter(id => id !== instance.id)
                          })
                        }}
                      />
                      {instance.name}
                    </Label>
                  )
                })}
                {(!instances || instances.length === 0) && (
                  <p className="text-xs text-muted-foreground">Add an instance first to cross-seed.</p>
                )}
              </div>
              <p className="text-[10px] text-muted-foreground">Leave empty to target any instance with matching data.</p>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="crossseed-category">Category</Label>
                <Input
                  id="crossseed-category"
                  placeholder="Optional category"
                  value={crossSeedCategory}
                  onChange={event => setCrossSeedCategory(event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="crossseed-tags">Tags</Label>
                <Input
                  id="crossseed-tags"
                  placeholder="Comma separated"
                  value={crossSeedTags}
                  onChange={event => setCrossSeedTags(event.target.value)}
                />
              </div>
            </div>

            <div className="flex items-center gap-2">
              <Switch
                id="crossseed-start-paused"
                checked={crossSeedStartPaused}
                onCheckedChange={value => setCrossSeedStartPaused(!!value)}
              />
              <Label htmlFor="crossseed-start-paused" className="text-sm">Start torrents paused</Label>
            </div>
          </CardContent>
          <CardFooter className="flex items-center gap-3">
            <Button
              onClick={() => crossSeedMutation.mutate()}
              disabled={crossSeedMutation.isPending}
            >
              {crossSeedMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              <UploadCloud className="mr-2 h-4 w-4" />
              Cross-seed
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                setSelectedFile(null)
                setTargetInstanceIds([])
                setCrossSeedCategory("")
                setCrossSeedTags("")
                setCrossSeedStartPaused(true)
                setCrossSeedResult(null)
              }}
            >
              Reset
            </Button>
          </CardFooter>
          {crossSeedResult && (
            <CardContent>
              <div className="rounded-md border p-4 space-y-3">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-sm">Results</span>
                  <Badge variant={crossSeedResult.success ? "default" : "destructive"} className="text-xs">
                    {crossSeedResult.success ? "SUCCESS" : "PARTIAL"}
                  </Badge>
                </div>
                <div className="space-y-2">
                  {crossSeedResult.results.map(result => (
                    <div key={`${result.instanceId}-${result.instanceName}`} className="flex items-center justify-between text-sm">
                      <div className="flex items-center gap-2">
                        {result.success ? (
                          <Check className="h-4 w-4 text-green-500" />
                        ) : result.status === "exists" ? (
                          <Rocket className="h-4 w-4 text-blue-500" />
                        ) : (
                          <XCircle className="h-4 w-4 text-destructive" />
                        )}
                        <span>{result.instanceName}</span>
                      </div>
                      <span className="text-xs text-muted-foreground">{result.message || result.status}</span>
                    </div>
                  ))}
                </div>
              </div>
            </CardContent>
          )}
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Automation</CardTitle>
          <CardDescription>Configure scheduled cross-seed scans and run them on-demand.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <p className="text-sm font-medium">Scheduler</p>
              <p className="text-xs text-muted-foreground">
                {automationStatus?.running ? "Automation run in progress" : automationStatus?.nextRunAt ? `Next run: ${formatDate(automationStatus.nextRunAt)}` : "Scheduler idle"}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <div className="flex items-center gap-2 text-xs">
                <Switch id="automation-dry-run" checked={dryRun} onCheckedChange={value => setDryRun(!!value)} />
                <Label htmlFor="automation-dry-run">Dry run</Label>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => triggerRunMutation.mutate({ limit: automationForm.maxResultsPerRun, dryRun })}
                disabled={triggerRunMutation.isPending}
              >
                {triggerRunMutation.isPending ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : (
                  <Play className="mr-2 h-4 w-4" />
                )}
                Run now
              </Button>
            </div>
          </div>

          <Separator />

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="automation-enabled" className="flex items-center gap-2">
                <Switch
                  id="automation-enabled"
                  checked={automationForm.enabled}
                  onCheckedChange={value => setAutomationForm(prev => ({ ...prev, enabled: !!value }))}
                />
                Enable automation
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
              <Label htmlFor="automation-interval">Run interval (minutes)</Label>
              <Input
                id="automation-interval"
                type="number"
                min={5}
                value={automationForm.runIntervalMinutes}
                onChange={event => setAutomationForm(prev => ({ ...prev, runIntervalMinutes: Number(event.target.value) }))}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="automation-max-results">Max results per run</Label>
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
              <Label htmlFor="automation-category">Category</Label>
              <Input
                id="automation-category"
                placeholder="Optional"
                value={automationForm.category}
                onChange={event => setAutomationForm(prev => ({ ...prev, category: event.target.value }))}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="automation-tags">Tags</Label>
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

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label>Target instances</Label>
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
            </div>
            <div className="space-y-2">
              <Label>Target indexers</Label>
              <div className="flex flex-wrap gap-2">
                {indexers?.map(indexer => {
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
                })}
                {(!indexers || indexers.length === 0) && (
                  <p className="text-xs text-muted-foreground">No Torznab indexers configured.</p>
                )}
              </div>
            </div>
          </div>
        </CardContent>
        <CardFooter className="flex items-center gap-3">
          <Button
            onClick={handleAutomationSave}
            disabled={updateSettingsMutation.isPending}
          >
            {updateSettingsMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Save settings
          </Button>
          <Button
            variant="ghost"
            onClick={() => {
              // Reset to defaults without triggering reinitialization
              setAutomationForm(DEFAULT_AUTOMATION_FORM)
            }}
          >
            Reset
          </Button>
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
            <p className="text-sm text-muted-foreground">No automation runs recorded yet.</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
