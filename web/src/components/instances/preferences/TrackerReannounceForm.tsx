/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect, type Option } from "@/components/ui/multi-select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { api } from "@/lib/api"
import { copyTextToClipboard } from "@/lib/utils"
import type { InstanceFormData, InstanceReannounceActivity, InstanceReannounceSettings } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { ChevronDown, Copy, RefreshCcw } from "lucide-react"
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
  monitorAll: false,
  categories: [],
  tags: [],
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
  const [activityOpen, setActivityOpen] = useState(false)
  const [hideSkipped, setHideSkipped] = useState(true)

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
      password: "",
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

  if (!instance) {
    return <p className="text-sm text-muted-foreground">Instance not found. Please close and reopen the dialog.</p>
  }

  const activityQuery = useQuery({
    queryKey: ["instance-reannounce-activity", instanceId],
    queryFn: () => api.getInstanceReannounceActivity(instanceId, 50),
    enabled: Boolean(instance && settings.enabled),
    refetchInterval: activityOpen ? 15000 : false,
  })

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
    <form className="space-y-4 sm:space-y-6" onSubmit={handleSubmit}>
      <div className="rounded-lg border border-border/60 bg-muted/30 p-3 sm:p-4 space-y-3 sm:space-y-4">
        <div className="flex items-center justify-between gap-3 sm:gap-4">
          <div className="space-y-1 flex-1">
            <Label className="text-base">Automatic tracker reannounce</Label>
            <p className="text-sm mb-2 text-muted-foreground">
              qui will reannounce torrents whose trackers report "unregistered" or outage errors. Requests are debounced so
              trackers are not spammed.
            </p>
            <p className="text-xs text-muted-foreground">
              Background scan interval: <code>{GLOBAL_SCAN_INTERVAL_SECONDS} seconds</code>.
            </p>
          </div>
          <Switch
            id="tracker-monitoring"
            checked={settings.enabled}
            onCheckedChange={(enabled) => setSettings((prev) => ({ ...prev, enabled }))}
            className="shrink-0"
          />
        </div>

        {settings.enabled && (
          <div className="space-y-3 sm:space-y-4">
            <div className="grid gap-3 sm:gap-4 md:grid-cols-3">
              <NumberField
                id="initial-wait"
                label="Initial tracker wait (seconds)"
                min={MIN_INITIAL_WAIT}
                value={settings.initialWaitSeconds}
                onChange={(value) => setSettings((prev) => ({ ...prev, initialWaitSeconds: value }))}
              />
              <NumberField
                id="reannounce-interval"
                label="Reannounce interval (seconds)"
                min={MIN_INTERVAL}
                value={settings.reannounceIntervalSeconds}
                onChange={(value) => setSettings((prev) => ({ ...prev, reannounceIntervalSeconds: value }))}
              />
              <NumberField
                id="max-age"
                label="Monitor torrents added within (seconds)"
                min={MIN_MAX_AGE}
                value={settings.maxAgeSeconds}
                onChange={(value) => setSettings((prev) => ({ ...prev, maxAgeSeconds: value }))}
              />
            </div>

            <div className="flex items-center justify-between gap-3 sm:gap-4">
              <div className="space-y-0.5 flex-1">
                <Label className="mb-0">Monitor scope</Label>
                <p className="text-sm text-muted-foreground">
                  Enable to monitor all torrents, or configure specific categories, tags, or tracker domains below.
                </p>
              </div>
              <Switch
                id="monitor-all"
                checked={settings.monitorAll}
                onCheckedChange={(monitorAll) => setSettings((prev) => ({ ...prev, monitorAll }))}
                className="shrink-0"
              />
            </div>

            {!settings.monitorAll && (
              <div className="space-y-3 sm:space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="scope-categories">Categories</Label>
                  <MultiSelect
                    options={categoryOptions}
                    selected={settings.categories}
                    onChange={(values) => setSettings((prev) => ({ ...prev, categories: values }))}
                    placeholder="Select or type categories..."
                    creatable
                    onCreateOption={(value) => appendUniqueValue("categories", value)}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="scope-tags">Tags</Label>
                  <MultiSelect
                    options={tagOptions}
                    selected={settings.tags}
                    onChange={(values) => setSettings((prev) => ({ ...prev, tags: values }))}
                    placeholder="Select or type tags..."
                    creatable
                    onCreateOption={(value) => appendUniqueValue("tags", value)}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="scope-trackers">Tracker domains</Label>
                  <MultiSelect
                    options={trackerOptions}
                    selected={settings.trackers}
                    onChange={(values) => setSettings((prev) => ({ ...prev, trackers: values }))}
                    placeholder="Select or type tracker domains..."
                    creatable
                    onCreateOption={(value) => appendUniqueValue("trackers", value)}
                  />
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      <Collapsible open={activityOpen} onOpenChange={setActivityOpen} className="rounded-lg border border-border/60 bg-muted/30">
        <CollapsibleTrigger className="flex w-full items-center justify-between px-3 sm:px-4 py-3 text-left text-sm font-medium hover:bg-muted/40 transition-colors">
          <span>Recent reannounce activity</span>
          <ChevronDown className={`h-4 w-4 transition-transform ${activityOpen ? "rotate-180" : ""}`} />
        </CollapsibleTrigger>
        <CollapsibleContent className="px-3 sm:px-4 pb-3 sm:pb-4 pt-2 space-y-3">
          {activityEnabled ? (
            <>
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                <p className="text-sm text-muted-foreground">Events refresh automatically while open.</p>
                <div className="flex items-center gap-3 self-start sm:self-auto">
                  <div className="flex items-center gap-2">
                    <Label htmlFor="hide-skipped" className="text-sm font-normal cursor-pointer">
                      Hide skipped
                    </Label>
                    <Switch id="hide-skipped" checked={hideSkipped} onCheckedChange={setHideSkipped} />
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={activityQuery.isFetching}
                    onClick={() => activityQuery.refetch()}
                    className="h-9 w-9 sm:h-8 sm:w-8 p-0"
                  >
                    <RefreshCcw className={`h-4 w-4 ${activityQuery.isFetching ? "animate-spin" : ""}`} />
                  </Button>
                </div>
              </div>
              {activityQuery.isLoading ? (
                <p className="text-sm text-muted-foreground">Loading activity…</p>
              ) : activityEvents.length === 0 ? (
                <p className="text-sm text-muted-foreground">No activity yet. Reannounce attempts will appear here.</p>
              ) : (
                <ScrollArea className="max-h-[300px] sm:max-h-[400px] pr-3 sm:pr-4">
                  <div className="space-y-2">
                    {activityEvents.map((event, index) => (
                      <div key={`${event.hash}-${index}-${event.timestamp}`} className="rounded-md border bg-background px-3 sm:px-4 py-2 sm:py-3 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 sm:gap-3">
                        <div className="space-y-1 overflow-hidden flex-1">
                          <div className="flex items-center gap-2 text-sm font-medium flex-wrap">
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="truncate max-w-[250px] sm:max-w-[350px] md:max-w-[500px] lg:max-w-[650px] cursor-help">
                                  {event.torrentName || event.hash}
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>
                                <p className="font-semibold">{event.torrentName || "N/A"}</p>
                              </TooltipContent>
                            </Tooltip>
                            <Badge variant="outline" className={outcomeClasses[event.outcome]}>
                              {event.outcome}
                            </Badge>
                          </div>
                          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                            <span className="font-mono">{event.hash.substring(0, 7)}...</span>
                             <button
                              type="button"
                              className="hover:text-foreground transition-colors p-1 -m-1 min-w-[44px] min-h-[44px] flex items-center justify-center sm:min-w-0 sm:min-h-0 sm:p-0 sm:m-0"
                              onClick={() => {
                                copyTextToClipboard(event.hash)
                                toast.success("Hash copied to clipboard")
                              }}
                              title="Copy hash"
                            >
                              <Copy className="h-3 w-3" />
                            </button>
                          </div>
                          {event.trackers && (
                            <p className="text-xs text-muted-foreground truncate">Tracker: <span className="text-primary">{event.trackers}</span></p>
                          )}
                          {event.reason && <p className="text-xs text-muted-foreground truncate">{event.reason}</p>}
                        </div>
                        <div className="text-left sm:text-right text-xs text-muted-foreground sm:shrink-0 mt-1 sm:mt-0">{formatTimestamp(event.timestamp)}</div>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              )}
            </>
          ) : (
            <p className="text-sm text-muted-foreground">Enable automatic tracker reannounce to track activity.</p>
          )}
        </CollapsibleContent>
      </Collapsible>

      <div className="flex justify-end">
        <Button type="submit" disabled={isUpdating}>
          {isUpdating ? "Saving..." : "Save Changes"}
        </Button>
      </div>
    </form>
  )
}

interface NumberFieldProps {
  id: string
  label: string
  value: number
  min: number
  onChange: (value: number) => void
}

function NumberField({ id, label, value, min, onChange }: NumberFieldProps) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id} className="text-sm sm:text-base">{label}</Label>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        min={min}
        value={value}
        onChange={(event) => onChange(Math.max(min, Number(event.target.value) || min))}
        className="h-10 sm:h-9"
      />
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
    categories: [...settings.categories],
    tags: [...settings.tags],
    trackers: [...settings.trackers],
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
    categories: normalizeList(settings.categories),
    tags: normalizeList(settings.tags),
    trackers: normalizeList(settings.trackers),
  }
}
