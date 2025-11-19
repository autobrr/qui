/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect, type Option } from "@/components/ui/multi-select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { api } from "@/lib/api"
import { cn, copyTextToClipboard } from "@/lib/utils"
import type { InstanceFormData, InstanceReannounceActivity, InstanceReannounceSettings } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Copy, Info, RefreshCcw } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

interface TrackerReannounceFormProps {
  instanceId: number
  onSuccess?: () => void
}

const DEFAULT_SETTINGS: InstanceReannounceSettings = {
  enabled: false,
  initialWaitSeconds: 15,
  reannounceIntervalSeconds: 7,
  maxAgeSeconds: 600,
  aggressive: false,
  monitorAll: false,
  excludeCategories: false,
  categories: [],
  excludeTags: false,
  tags: [],
  excludeTrackers: false,
  trackers: [],
}

const MIN_INITIAL_WAIT = 5
const MIN_INTERVAL = 5
const MIN_MAX_AGE = 60
const GLOBAL_SCAN_INTERVAL_SECONDS = 7

type MonitorScopeField = keyof Pick<InstanceReannounceSettings, "categories" | "tags" | "trackers">

export function TrackerReannounceForm({ instanceId, onSuccess }: TrackerReannounceFormProps) {
  const { instances, updateInstance, isUpdating } = useInstances()
  const instance = useMemo(() => instances?.find((item) => item.id === instanceId), [instances, instanceId])
  const [settings, setSettings] = useState<InstanceReannounceSettings>(() => cloneSettings(instance?.reannounceSettings))
  const [hideSkipped, setHideSkipped] = useState(true)
  const [activeTab, setActiveTab] = useState("settings")

  const trackersQuery = useInstanceTrackers(instanceId, { enabled: !!instance })

  const categoriesQuery = useQuery({
    queryKey: ["instance-categories", instanceId],
    queryFn: () => api.getCategories(instanceId),
    enabled: !!instance,
    staleTime: 1000 * 60 * 5,
  })

  const tagsQuery = useQuery({
    queryKey: ["instance-tags", instanceId],
    queryFn: () => api.getTags(instanceId),
    enabled: !!instance,
    staleTime: 1000 * 60 * 5,
  })

  const trackerOptions: Option[] = useMemo(() => {
    if (!trackersQuery.data) return []
    
    // The API returns Record<string, string> where key is domain, value is full URL or similar.
    // We're interested in the domains (keys).
    return Object.keys(trackersQuery.data).map((domain) => ({
      label: domain,
      value: domain,
    })).sort((a, b) => a.label.localeCompare(b.label))
  }, [trackersQuery.data])

  const categoryOptions: Option[] = useMemo(() => {
    if (!categoriesQuery.data) return []
    return Object.values(categoriesQuery.data)
      .map((category) => ({
        label: category.name,
        value: category.name,
      }))
      .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }))
  }, [categoriesQuery.data])

  const tagOptions: Option[] = useMemo(() => {
    if (!tagsQuery.data) return []
    return tagsQuery.data
      .map((tag) => ({
        label: tag,
        value: tag,
      }))
      .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }))
  }, [tagsQuery.data])

  const appendUniqueValue = (field: MonitorScopeField, rawValue: string) => {
    const trimmed = rawValue.trim()
    if (!trimmed) return
    const normalized = trimmed.toLowerCase()
    setSettings((prev) => {
      const values = prev[field]
      if (values.some((entry) => entry.toLowerCase() === normalized)) {
        return prev
      }
      return {
        ...prev,
        [field]: [...values, trimmed],
      }
    })
  }

  useEffect(() => {
    setSettings(cloneSettings(instance?.reannounceSettings))
  }, [instance?.reannounceSettings, instanceId])

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!instance) {
      toast.error("Instance missing", { description: "Please close and reopen the dialog." })
      return
    }
    const sanitized = sanitizeSettings(settings)

    const payload: Partial<InstanceFormData> = {
      name: instance.name,
      host: instance.host,
      username: instance.username,
      tlsSkipVerify: instance.tlsSkipVerify,
      reannounceSettings: sanitized,
    }

    if (instance.basicUsername !== undefined) {
      payload.basicUsername = instance.basicUsername
    }

    updateInstance(
      { id: instanceId, data: payload },
      {
        onSuccess: () => {
          toast.success("Tracker monitoring updated", { description: "Settings saved successfully." })
          onSuccess?.()
        },
        onError: (error) => {
          toast.error("Update failed", { description: error instanceof Error ? error.message : "Unable to update settings" })
        },
      },
    )
  }

  const activityQuery = useQuery({
    queryKey: ["instance-reannounce-activity", instanceId],
    queryFn: () => api.getInstanceReannounceActivity(instanceId, 50),
    enabled: Boolean(instance && settings.enabled),
    refetchInterval: activeTab === "activity" ? 5000 : false,
  })

  if (!instance) {
    return <p className="text-sm text-muted-foreground">Instance not found. Please close and reopen the dialog.</p>
  }

  const allActivityEvents: InstanceReannounceActivity[] = (activityQuery.data ?? []).slice().reverse()
  const activityEvents = hideSkipped ? allActivityEvents.filter((event) => event.outcome !== "skipped") : allActivityEvents
  const activityEnabled = Boolean(instance && settings.enabled)

  const outcomeClasses: Record<InstanceReannounceActivity["outcome"], string> = {
    succeeded: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20",
    failed: "bg-destructive/10 text-destructive border-destructive/30",
    skipped: "bg-muted text-muted-foreground border-border/60",
  }

  const formatTimestamp = (timestamp: string) => {
    try {
      return new Intl.DateTimeFormat(undefined, {
        dateStyle: "short",
        timeStyle: "short",
      }).format(new Date(timestamp))
    } catch {
      return timestamp
    }
  }

  return (
    <form onSubmit={handleSubmit}>
      <Card className="w-full border-none shadow-none bg-transparent p-0">
        <CardHeader className="px-0 pt-0 pb-0 space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-1">
              <CardTitle className="text-lg font-semibold">Automatic Tracker Reannounce</CardTitle>
              <CardDescription>
                qui monitors <strong>stalled</strong> torrents and reannounces them if trackers report "unregistered" or errors.
                Background scan runs every {GLOBAL_SCAN_INTERVAL_SECONDS} seconds.
              </CardDescription>
            </div>
            <div className="flex items-center gap-2 bg-muted/40 p-2 rounded-lg border border-border/40">
              <Label htmlFor="tracker-monitoring" className="font-medium text-sm cursor-pointer">
                {settings.enabled ? "Enabled" : "Disabled"}
              </Label>
              <Switch
                id="tracker-monitoring"
                checked={settings.enabled}
                onCheckedChange={(enabled) => setSettings((prev) => ({ ...prev, enabled }))}
              />
            </div>
          </div>
        </CardHeader>

        <CardContent className="px-0 pb-0">
          <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
            <div className="flex items-center justify-between mb-4">
              <TabsList className="grid w-full grid-cols-2 lg:w-[400px]">
                <TabsTrigger value="settings">Settings</TabsTrigger>
                <TabsTrigger value="activity">Activity Log</TabsTrigger>
              </TabsList>
            </div>

            <TabsContent value="settings" className="space-y-6 mt-0">
              {settings.enabled ? (
                <>
                  <div className="space-y-4">
                    <div className="flex items-center gap-2">
                      <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Timing & Behavior</h3>
                      <Separator className="flex-1" />
                    </div>
                    
                    <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
                      <NumberField
                        id="initial-wait"
                        label="Initial Wait"
                        description="Seconds before first check"
                        tooltip="How long to wait after a torrent is added before checking its status. Gives the tracker time to register it naturally."
                        min={MIN_INITIAL_WAIT}
                        value={settings.initialWaitSeconds}
                        onChange={(value) => setSettings((prev) => ({ ...prev, initialWaitSeconds: value }))}
                      />
                      <NumberField
                        id="reannounce-interval"
                        label="Retry Interval"
                        description="Seconds between retries"
                        tooltip="How often to retry reannouncing if the previous attempt failed. Avoid setting this too low to prevent tracker spam."
                        min={MIN_INTERVAL}
                        value={settings.reannounceIntervalSeconds}
                        onChange={(value) => setSettings((prev) => ({ ...prev, reannounceIntervalSeconds: value }))}
                      />
                      <NumberField
                        id="max-age"
                        label="Max Torrent Age"
                        description="Stop monitoring after (s)"
                        tooltip="Stop monitoring torrents older than this (in seconds). Prevents checking old torrents that are permanently dead."
                        min={MIN_MAX_AGE}
                        value={settings.maxAgeSeconds}
                        onChange={(value) => setSettings((prev) => ({ ...prev, maxAgeSeconds: value }))}
                      />
                    </div>

                    <div className="flex items-center justify-between rounded-lg border border-border/60 p-3 bg-muted/20">
                      <div className="space-y-0.5">
                        <div className="flex items-center gap-2">
                          <Label htmlFor="aggressive-mode" className="text-base">Aggressive Mode</Label>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent className="max-w-[300px]">
                              <p>
                                Normally, we wait 2 minutes between attempts to be polite. 
                                Enable this to retry immediately upon failure. 
                              </p>
                            </TooltipContent>
                          </Tooltip>
                        </div>
                        <p className="text-sm text-muted-foreground">
                          Bypass wait times on failure
                        </p>
                      </div>
                      <Switch
                        id="aggressive-mode"
                        checked={settings.aggressive}
                        onCheckedChange={(aggressive) => setSettings((prev) => ({ ...prev, aggressive }))}
                      />
                    </div>
                  </div>

                  <div className="space-y-4">
                    <div className="flex items-center gap-2">
                      <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Scope & Filtering</h3>
                      <Separator className="flex-1" />
                    </div>

                    <div className="space-y-4">
                      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                        <Label className="text-base">Monitoring Scope</Label>
                        <Tabs
                          value={settings.monitorAll ? "all" : "custom"}
                          onValueChange={(v) => setSettings(prev => ({ ...prev, monitorAll: v === "all" }))}
                          className="w-full sm:w-auto"
                        >
                          <TabsList className="grid w-full grid-cols-2 sm:w-[300px]">
                            <TabsTrigger value="all">Monitor All</TabsTrigger>
                            <TabsTrigger value="custom">Custom Filter</TabsTrigger>
                          </TabsList>
                        </Tabs>
                      </div>

                      {!settings.monitorAll && (
                        <div className="grid gap-6 pt-2 animate-in fade-in slide-in-from-top-2 duration-200">
                          {/* Categories */}
                          <div className="space-y-3">
                            <div className="flex items-center justify-between">
                              <Label htmlFor="scope-categories">Categories</Label>
                              <Tabs
                                value={settings.excludeCategories ? "exclude" : "include"}
                                onValueChange={(v) => setSettings(prev => ({ ...prev, excludeCategories: v === "exclude" }))}
                                className="h-7"
                              >
                                <TabsList className="h-7">
                                  <TabsTrigger value="include" className="text-xs h-5 px-2">Include</TabsTrigger>
                                  <TabsTrigger value="exclude" className="text-xs h-5 px-2">Exclude</TabsTrigger>
                                </TabsList>
                              </Tabs>
                            </div>
                            <MultiSelect
                              options={categoryOptions}
                              selected={settings.categories}
                              onChange={(values) => setSettings((prev) => ({ ...prev, categories: values }))}
                              placeholder="Select categories..."
                              creatable
                              onCreateOption={(value) => appendUniqueValue("categories", value)}
                            />
                          </div>

                          {/* Tags */}
                          <div className="space-y-3">
                            <div className="flex items-center justify-between">
                              <Label htmlFor="scope-tags">Tags</Label>
                              <Tabs
                                value={settings.excludeTags ? "exclude" : "include"}
                                onValueChange={(v) => setSettings(prev => ({ ...prev, excludeTags: v === "exclude" }))}
                                className="h-7"
                              >
                                <TabsList className="h-7">
                                  <TabsTrigger value="include" className="text-xs h-5 px-2">Include</TabsTrigger>
                                  <TabsTrigger value="exclude" className="text-xs h-5 px-2">Exclude</TabsTrigger>
                                </TabsList>
                              </Tabs>
                            </div>
                            <MultiSelect
                              options={tagOptions}
                              selected={settings.tags}
                              onChange={(values) => setSettings((prev) => ({ ...prev, tags: values }))}
                              placeholder="Select tags..."
                              creatable
                              onCreateOption={(value) => appendUniqueValue("tags", value)}
                            />
                          </div>

                          {/* Trackers */}
                          <div className="space-y-3">
                            <div className="flex items-center justify-between">
                              <Label htmlFor="scope-trackers">Tracker Domains</Label>
                              <Tabs
                                value={settings.excludeTrackers ? "exclude" : "include"}
                                onValueChange={(v) => setSettings(prev => ({ ...prev, excludeTrackers: v === "exclude" }))}
                                className="h-7"
                              >
                                <TabsList className="h-7">
                                  <TabsTrigger value="include" className="text-xs h-5 px-2">Include</TabsTrigger>
                                  <TabsTrigger value="exclude" className="text-xs h-5 px-2">Exclude</TabsTrigger>
                                </TabsList>
                              </Tabs>
                            </div>
                            <MultiSelect
                              options={trackerOptions}
                              selected={settings.trackers}
                              onChange={(values) => setSettings((prev) => ({ ...prev, trackers: values }))}
                              placeholder="Select tracker domains..."
                              creatable
                              onCreateOption={(value) => appendUniqueValue("trackers", value)}
                            />
                          </div>
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="flex justify-end pt-4">
                    <Button type="submit" disabled={isUpdating}>
                      {isUpdating ? "Saving..." : "Save Changes"}
                    </Button>
                  </div>
                </>
              ) : (
                <div className="flex flex-col items-center justify-center py-12 text-center space-y-3 border-2 border-dashed rounded-lg">
                  <div className="p-3 rounded-full bg-muted/50">
                    <RefreshCcw className="h-6 w-6 text-muted-foreground/50" />
                  </div>
                  <div className="space-y-1">
                    <h3 className="font-medium text-muted-foreground">Monitoring Disabled</h3>
                    <p className="text-sm text-muted-foreground/60 max-w-xs mx-auto">
                      Enable automatic reannouncing to configure settings and start monitoring stalled torrents.
                    </p>
                  </div>
                  <Button variant="outline" onClick={() => setSettings(prev => ({ ...prev, enabled: true }))}>
                    Enable Monitoring
                  </Button>
                </div>
              )}
            </TabsContent>

            <TabsContent value="activity" className="mt-0 space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-1">
                  <h3 className="text-sm font-medium leading-none">Recent Activity</h3>
                  <p className="text-sm text-muted-foreground">
                    {activityEnabled 
                      ? "Real-time log of reannounce attempts and results."
                      : "Monitoring is disabled. No new activity will be recorded."}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <div className="flex items-center gap-2 mr-2">
                    <Switch 
                      id="hide-skipped" 
                      checked={hideSkipped} 
                      onCheckedChange={setHideSkipped} 
                      className="scale-75"
                    />
                    <Label htmlFor="hide-skipped" className="text-sm font-normal cursor-pointer">
                      Hide skipped
                    </Label>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={activityQuery.isFetching}
                    onClick={() => activityQuery.refetch()}
                    className="h-8 px-2 lg:px-3"
                  >
                    <RefreshCcw className={cn("h-3.5 w-3.5 mr-2", activityQuery.isFetching && "animate-spin")} />
                    Refresh
                  </Button>
                </div>
              </div>

              {activityQuery.isLoading ? (
                 <div className="h-[300px] flex items-center justify-center border rounded-lg bg-muted/10">
                    <p className="text-sm text-muted-foreground">Loading activity...</p>
                 </div>
              ) : activityEvents.length === 0 ? (
                <div className="h-[300px] flex flex-col items-center justify-center border rounded-lg bg-muted/10 text-center p-6">
                  <p className="text-sm text-muted-foreground">No activity recorded yet.</p>
                  {activityEnabled && (
                    <p className="text-xs text-muted-foreground/60 mt-1">
                      Events will appear here when stalled torrents are detected.
                    </p>
                  )}
                </div>
              ) : (
                <ScrollArea className="h-[400px] rounded-md border">
                  <div className="divide-y divide-border/40">
                    {activityEvents.map((event, index) => (
                      <div key={`${event.hash}-${index}-${event.timestamp}`} className="p-4 hover:bg-muted/20 transition-colors">
                        <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
                          <div className="space-y-1.5 flex-1 min-w-0">
                            <div className="flex items-center gap-2 flex-wrap">
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="font-medium text-sm truncate max-w-[300px] sm:max-w-[400px] cursor-help">
                                    {event.torrentName || event.hash}
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent>
                                  <p className="font-semibold">{event.torrentName || "N/A"}</p>
                                </TooltipContent>
                              </Tooltip>
                              <Badge variant="outline" className={cn("capitalize text-[10px] px-1.5 py-0 h-5", outcomeClasses[event.outcome])}>
                                {event.outcome}
                              </Badge>
                            </div>
                            
                            <div className="flex items-center gap-3 text-xs text-muted-foreground">
                              <div className="flex items-center gap-1 bg-muted/50 px-1.5 py-0.5 rounded">
                                <span className="font-mono">{event.hash.substring(0, 7)}</span>
                                <button
                                  type="button"
                                  className="hover:text-foreground transition-colors"
                                  onClick={() => {
                                    copyTextToClipboard(event.hash)
                                    toast.success("Hash copied")
                                  }}
                                  title="Copy hash"
                                >
                                  <Copy className="h-3 w-3" />
                                </button>
                              </div>
                              <span className="text-muted-foreground/40">•</span>
                              <span>{formatTimestamp(event.timestamp)}</span>
                            </div>

                            {(event.trackers || event.reason) && (
                              <div className="mt-2 space-y-1 bg-muted/30 p-2 rounded text-xs">
                                {event.trackers && (
                                  <div className="flex items-start gap-2">
                                    <span className="font-medium text-muted-foreground shrink-0">Trackers:</span>
                                    <span className="text-foreground break-all">{event.trackers}</span>
                                  </div>
                                )}
                                {event.reason && (
                                  <div className="flex items-start gap-2">
                                    <span className="font-medium text-muted-foreground shrink-0">Reason:</span>
                                    <span className="text-foreground break-all">{event.reason}</span>
                                  </div>
                                )}
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              )}
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </form>
  )
}

interface NumberFieldProps {
  id: string
  label: string
  description?: string
  tooltip?: string
  value: number
  min: number
  onChange: (value: number) => void
}

function NumberField({ id, label, description, tooltip, value, min, onChange }: NumberFieldProps) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Label htmlFor={id} className="text-sm font-medium">{label}</Label>
        {tooltip && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[250px]">
              <p>{tooltip}</p>
            </TooltipContent>
          </Tooltip>
        )}
      </div>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        min={min}
        value={value}
        onChange={(event) => onChange(Math.max(min, Number(event.target.value) || min))}
        className="h-9"
      />
      {description && <p className="text-[11px] text-muted-foreground">{description}</p>}
    </div>
  )
}

function cloneSettings(settings?: InstanceReannounceSettings): InstanceReannounceSettings {
  if (!settings) {
    return { ...DEFAULT_SETTINGS }
  }
  return {
    enabled: settings.enabled,
    initialWaitSeconds: settings.initialWaitSeconds,
    reannounceIntervalSeconds: settings.reannounceIntervalSeconds,
    maxAgeSeconds: settings.maxAgeSeconds,
    monitorAll: settings.monitorAll,
    excludeCategories: settings.excludeCategories,
    categories: [...settings.categories],
    excludeTags: settings.excludeTags,
    tags: [...settings.tags],
    excludeTrackers: settings.excludeTrackers,
    trackers: [...settings.trackers],
    aggressive: settings.aggressive,
  }
}

function sanitizeSettings(settings: InstanceReannounceSettings): InstanceReannounceSettings {
  const clamp = (value: number, fallback: number, min: number) => {
    const parsed = Number.isFinite(value) ? Math.floor(value) : fallback
    return Math.max(min, parsed)
  }
  const normalizeList = (values: string[]) => values.map((value) => value.trim()).filter(Boolean)

  return {
    enabled: settings.enabled,
    initialWaitSeconds: clamp(settings.initialWaitSeconds, DEFAULT_SETTINGS.initialWaitSeconds, MIN_INITIAL_WAIT),
    reannounceIntervalSeconds: clamp(settings.reannounceIntervalSeconds, DEFAULT_SETTINGS.reannounceIntervalSeconds, MIN_INTERVAL),
    maxAgeSeconds: clamp(settings.maxAgeSeconds, DEFAULT_SETTINGS.maxAgeSeconds, MIN_MAX_AGE),
    monitorAll: settings.monitorAll,
    excludeCategories: settings.excludeCategories,
    categories: normalizeList(settings.categories),
    excludeTags: settings.excludeTags,
    tags: normalizeList(settings.tags),
    excludeTrackers: settings.excludeTrackers,
    trackers: normalizeList(settings.trackers),
    aggressive: settings.aggressive,
  }
}
