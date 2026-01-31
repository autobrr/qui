/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { cn, copyTextToClipboard, formatErrorReason, formatRelativeTime } from "@/lib/utils"
import type { Instance, InstanceFormData, InstanceReannounceActivity, InstanceReannounceSettings } from "@/types"
import { useQueries, useQueryClient } from "@tanstack/react-query"
import { ChevronDown, Copy, Info, RefreshCcw, Search, Settings2 } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

interface ReannounceOverviewProps {
  onConfigureInstance?: (instanceId: number) => void
  expandedInstances?: string[]
  onExpandedInstancesChange?: (values: string[]) => void
}

interface InstanceStats {
  successToday: number
  failedToday: number
  lastActivity?: Date
}

function computeStats(events: InstanceReannounceActivity[]): InstanceStats {
  const now = new Date()
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())

  let successToday = 0
  let failedToday = 0
  let lastActivity: Date | undefined

  for (const event of events) {
    const eventDate = new Date(event.timestamp)
    if (!lastActivity || eventDate > lastActivity) {
      lastActivity = eventDate
    }
    if (eventDate >= startOfToday) {
      if (event.outcome === "succeeded") {
        successToday++
      } else if (event.outcome === "failed") {
        failedToday++
      }
    }
  }

  return { successToday, failedToday, lastActivity }
}

export function ReannounceOverview({
  onConfigureInstance,
  expandedInstances: controlledExpanded,
  onExpandedInstancesChange,
}: ReannounceOverviewProps) {
  const { instances, updateInstance, isUpdating } = useInstances()
  const queryClient = useQueryClient()
  const { formatISOTimestamp } = useDateTimeFormatters()

  // Internal state for standalone usage
  const [internalExpanded, setInternalExpanded] = useState<string[]>([])

  // Use controlled props if provided, otherwise internal state
  const expandedInstances = controlledExpanded ?? internalExpanded
  const setExpandedInstances = onExpandedInstancesChange ?? setInternalExpanded
  const [hideSkippedMap, setHideSkippedMap] = useState<Record<number, boolean>>({})
  const [searchMap, setSearchMap] = useState<Record<number, string>>({})

  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )

  // Fetch activity for all instances with enabled reannounce
  const activityQueries = useQueries({
    queries: activeInstances.map((instance) => ({
      queryKey: ["instance-reannounce-activity", instance.id],
      queryFn: () => api.getInstanceReannounceActivity(instance.id, 0),
      enabled: instance.reannounceSettings?.enabled ?? false,
      refetchInterval: expandedInstances.includes(String(instance.id)) ? 5000 : 30000,
      staleTime: 5000,
    })),
  })

  const handleToggleEnabled = (instance: Instance, enabled: boolean) => {
    const payload: Partial<InstanceFormData> = {
      name: instance.name,
      host: instance.host,
      username: instance.username,
      tlsSkipVerify: instance.tlsSkipVerify,
      reannounceSettings: {
        ...instance.reannounceSettings,
        enabled,
      },
    }

    if (instance.basicUsername !== undefined) {
      payload.basicUsername = instance.basicUsername
    }

    updateInstance(
      { id: instance.id, data: payload },
      {
        onSuccess: () => {
          toast.success(enabled ? "Monitoring enabled" : "Monitoring disabled", {
            description: instance.name,
          })
        },
        onError: (error) => {
          toast.error("Update failed", {
            description: error instanceof Error ? error.message : "Unable to update settings",
          })
        },
      }
    )
  }

  const outcomeClasses: Record<InstanceReannounceActivity["outcome"], string> = {
    succeeded: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20",
    failed: "bg-destructive/10 text-destructive border-destructive/30",
    skipped: "bg-muted text-muted-foreground border-border/60",
  }

  const getSettingsSummary = (settings: InstanceReannounceSettings | undefined): string => {
    if (!settings) return "Not configured"
    const parts: string[] = []
    parts.push(`Wait ${settings.initialWaitSeconds}s`)
    parts.push(`Retry ${settings.reannounceIntervalSeconds}s`)
    parts.push(`Max ${settings.maxRetries}x`)
    if (settings.aggressive) parts.push("Quick")
    return parts.join(" · ")
  }

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">Reannounce</CardTitle>
          <CardDescription>
            No instances configured. Add one in Settings to use this service.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-lg font-semibold">Reannounce</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px]">
              <p>
                qBittorrent doesn't retry failed announces quickly. When a tracker is slow to
                register a new upload or returns an error, you may be stuck waiting. qui handles
                this automatically while never spamming trackers.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          Monitors <strong>stalled</strong> torrents and reannounces them when no tracker is healthy.
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
            const activityQuery = activityQueries[index]
            const events = activityQuery?.data ?? []
            const stats = computeStats(events)
            const settings = instance.reannounceSettings
            const isEnabled = settings?.enabled ?? false
            const hideSkipped = hideSkippedMap[instance.id] ?? true
            const searchTerm = (searchMap[instance.id] ?? "").toLowerCase().trim()
            // Filter by outcome and search term, limit to 50 events for display
            const filteredEvents = events
              .filter((e) => {
                if (hideSkipped && e.outcome === "skipped") return false
                if (searchTerm) {
                  const nameMatch = e.torrentName?.toLowerCase().includes(searchTerm)
                  const hashMatch = e.hash.toLowerCase().includes(searchTerm)
                  if (!nameMatch && !hashMatch) return false
                }
                return true
              })
              .slice(-50)
              .reverse()

            return (
              <AccordionItem key={instance.id} value={String(instance.id)} className="group/item">
                <div className="grid grid-cols-[1fr_auto] items-center px-6">
                  <AccordionTrigger className="py-4 pr-4 hover:no-underline [&>svg]:hidden">
                    <div className="flex items-center justify-between w-full">
                      <div className="flex items-center gap-3 min-w-0">
                        <span className="font-medium truncate">{instance.name}</span>
                        {isEnabled && stats.successToday > 0 && (
                          <Badge variant="outline" className="bg-emerald-500/10 text-emerald-500 border-emerald-500/20 text-xs">
                            {stats.successToday} today
                          </Badge>
                        )}
                        {isEnabled && stats.failedToday > 0 && (
                          <Badge variant="outline" className="bg-destructive/10 text-destructive border-destructive/30 text-xs">
                            {stats.failedToday} failed
                          </Badge>
                        )}
                      </div>

                      {isEnabled && stats.lastActivity && (
                        <span className="text-xs text-muted-foreground hidden sm:block">
                          {formatRelativeTime(stats.lastActivity)}
                        </span>
                      )}
                    </div>
                  </AccordionTrigger>
                  <div className="flex items-center gap-4 py-4">
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
                        onCheckedChange={(enabled) => handleToggleEnabled(instance, enabled)}
                        disabled={isUpdating}
                        className="scale-90"
                      />
                    </div>
                    <button
                      type="button"
                      onClick={() => {
                        const itemValue = String(instance.id)
                        if (expandedInstances.includes(itemValue)) {
                          setExpandedInstances(expandedInstances.filter((v) => v !== itemValue))
                        } else {
                          setExpandedInstances([...expandedInstances, itemValue])
                        }
                      }}
                      aria-expanded={expandedInstances.includes(String(instance.id))}
                      aria-label={expandedInstances.includes(String(instance.id)) ? "Collapse" : "Expand"}
                    >
                      <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[state=open]/item:rotate-180" />
                    </button>
                  </div>
                </div>

                <AccordionContent className="px-6 pb-4">
                  <div className="space-y-4">
                    {/* Settings summary */}
                    <div className="flex items-center justify-between p-3 rounded-lg bg-muted/40 border">
                      <div className="space-y-0.5">
                        <p className="text-sm text-muted-foreground">
                          {getSettingsSummary(settings)}
                        </p>
                        {settings?.monitorAll ? (
                          <p className="text-xs text-muted-foreground/70">Monitoring all stalled torrents</p>
                        ) : (
                          <p className="text-xs text-muted-foreground/70">
                            {settings?.categories.length || settings?.tags.length || settings?.trackers.length
                              ? "Filtered by categories/tags/trackers"
                              : "No filters configured"}
                          </p>
                        )}
                      </div>
                      {onConfigureInstance && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => onConfigureInstance(instance.id)}
                          className="h-8"
                        >
                          <Settings2 className="h-4 w-4 mr-2" />
                          Configure
                        </Button>
                      )}
                    </div>

                    {/* Activity log */}
                    {isEnabled && (
                      <div className="space-y-3">
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <h4 className="text-sm font-medium">Recent Activity</h4>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="text-xs text-muted-foreground cursor-help">
                                  {filteredEvents.length === events.length
                                    ? `${events.length} events`
                                    : `${filteredEvents.length} of ${events.length}`}
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>
                                <p>Retains last 100 succeeded, 100 failed, 50 skipped</p>
                              </TooltipContent>
                            </Tooltip>
                          </div>
                          <div className="flex items-center gap-2">
                            <button
                              type="button"
                              className={cn(
                                "text-xs px-2 py-1 rounded transition-colors",
                                hideSkipped
                                  ? "bg-muted text-foreground"
                                  : "text-muted-foreground hover:text-foreground"
                              )}
                              onClick={() => setHideSkippedMap((prev) => ({
                                ...prev,
                                [instance.id]: !hideSkipped,
                              }))}
                            >
                              {hideSkipped ? "Show skipped" : "Hide skipped"}
                            </button>
                            <Button
                              type="button"
                              size="sm"
                              variant="ghost"
                              disabled={activityQuery?.isFetching}
                              onClick={() => queryClient.invalidateQueries({
                                queryKey: ["instance-reannounce-activity", instance.id],
                              })}
                              className="h-7 px-2"
                            >
                              <RefreshCcw className={cn(
                                "h-3.5 w-3.5",
                                activityQuery?.isFetching && "animate-spin"
                              )} />
                            </Button>
                          </div>
                        </div>

                        {/* Search filter */}
                        <div className="relative">
                          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                          <Input
                            type="text"
                            placeholder="Filter by name or hash..."
                            value={searchMap[instance.id] ?? ""}
                            onChange={(e) => setSearchMap((prev) => ({
                              ...prev,
                              [instance.id]: e.target.value,
                            }))}
                            className="pl-9 h-8 text-sm"
                          />
                        </div>

                        {activityQuery?.isError ? (
                          <div className="h-[100px] flex flex-col items-center justify-center border border-destructive/30 rounded-lg bg-destructive/10 text-center p-4">
                            <p className="text-sm text-destructive">Failed to load activity</p>
                            <p className="text-xs text-destructive/70 mt-1">
                              Check connection to the instance.
                            </p>
                          </div>
                        ) : activityQuery?.isLoading ? (
                          <div className="h-[150px] flex items-center justify-center border rounded-lg bg-muted/30">
                            <p className="text-sm text-muted-foreground">Loading activity...</p>
                          </div>
                        ) : filteredEvents.length === 0 ? (
                          <div className="h-[100px] flex flex-col items-center justify-center border border-dashed rounded-lg bg-muted/30 text-center p-4">
                            <p className="text-sm text-muted-foreground">
                              {searchTerm ? "No matching events found." : "No activity recorded yet."}
                            </p>
                            <p className="text-xs text-muted-foreground/60 mt-1">
                              {searchTerm
                                ? "Try a different search term or clear the filter."
                                : "Events will appear here when stalled torrents are detected."}
                            </p>
                          </div>
                        ) : (
                          <div className="max-h-[350px] overflow-auto rounded-md border bg-muted/20">
                            <div className="divide-y divide-border">
                              {filteredEvents.map((event, eventIndex) => (
                                <div
                                  key={`${event.hash}-${eventIndex}-${event.timestamp}`}
                                  className="p-3 hover:bg-muted/30 transition-colors"
                                >
                                  <div className="flex flex-col gap-2">
                                    <div className="flex items-center gap-2 flex-wrap">
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <span className="font-medium text-sm truncate max-w-[250px] cursor-help">
                                            {event.torrentName || event.hash}
                                          </span>
                                        </TooltipTrigger>
                                        <TooltipContent>
                                          <p className="font-semibold">{event.torrentName || "N/A"}</p>
                                        </TooltipContent>
                                      </Tooltip>
                                      <Badge
                                        variant="outline"
                                        className={cn(
                                          "capitalize text-[10px] px-1.5 py-0 h-5",
                                          outcomeClasses[event.outcome]
                                        )}
                                      >
                                        {event.outcome}
                                      </Badge>
                                    </div>

                                    <div className="flex items-center gap-3 text-xs text-muted-foreground">
                                      <div className="flex items-center gap-1 bg-muted/60 px-1.5 py-0.5 rounded">
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
                                      <span className="text-muted-foreground/40">·</span>
                                      <span>{formatISOTimestamp(event.timestamp)}</span>
                                    </div>

                                    {event.reason && (
                                      <div className="text-xs bg-muted/40 p-2 rounded">
                                        {formatErrorReason(event.reason) !== event.reason ? (
                                          <Tooltip>
                                            <TooltipTrigger asChild>
                                              <span className="cursor-help">{formatErrorReason(event.reason)}</span>
                                            </TooltipTrigger>
                                            <TooltipContent className="max-w-md">
                                              <p className="break-all">{event.reason}</p>
                                            </TooltipContent>
                                          </Tooltip>
                                        ) : (
                                          <span>{event.reason}</span>
                                        )}
                                      </div>
                                    )}
                                  </div>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                      </div>
                    )}

                    {!isEnabled && (
                      <div className="flex flex-col items-center justify-center py-6 text-center space-y-2 border border-dashed rounded-lg">
                        <div className="p-2 rounded-full bg-muted/50">
                          <RefreshCcw className="h-5 w-5 text-muted-foreground/50" />
                        </div>
                        <p className="text-sm text-muted-foreground">
                          Enable monitoring to start tracking stalled torrents.
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
