/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { cn, copyTextToClipboard, formatRelativeTime } from "@/lib/utils"
import type { TrackerRuleActivity } from "@/types"
import { useQueries, useQueryClient } from "@tanstack/react-query"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Copy, Info, RefreshCcw, Search, Settings2, Trash2 } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

interface TrackerRulesActivityOverviewProps {
  onConfigureInstance?: (instanceId: number) => void
}

interface InstanceStats {
  deletionsToday: number
  failedToday: number
  lastActivity?: Date
}

function computeStats(events: TrackerRuleActivity[]): InstanceStats {
  const now = new Date()
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())

  let deletionsToday = 0
  let failedToday = 0
  let lastActivity: Date | undefined

  for (const event of events) {
    const eventDate = new Date(event.createdAt)
    if (!lastActivity || eventDate > lastActivity) {
      lastActivity = eventDate
    }
    if (eventDate >= startOfToday) {
      if (event.outcome === "success") {
        deletionsToday++
      } else if (event.outcome === "failed") {
        failedToday++
      }
    }
  }

  return { deletionsToday, failedToday, lastActivity }
}

function formatAction(action: TrackerRuleActivity["action"]): string {
  switch (action) {
    case "deleted_ratio":
      return "Ratio limit"
    case "deleted_seeding":
      return "Seeding time"
    case "deleted_unregistered":
      return "Unregistered"
    case "delete_failed":
      return "Delete failed"
    case "limit_failed":
      return "Limit failed"
    default:
      return action
  }
}

export function TrackerRulesActivityOverview({ onConfigureInstance }: TrackerRulesActivityOverviewProps) {
  const { instances } = useInstances()
  const queryClient = useQueryClient()
  const { formatISOTimestamp } = useDateTimeFormatters()
  const [expandedInstances, setExpandedInstances] = useState<string[]>([])
  const [filterMap, setFilterMap] = useState<Record<number, "all" | "deletions" | "errors">>({})
  const [searchMap, setSearchMap] = useState<Record<number, string>>({})
  const [clearDaysMap, setClearDaysMap] = useState<Record<number, string>>({})

  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )

  // Fetch activity for all active instances
  const activityQueries = useQueries({
    queries: activeInstances.map((instance) => ({
      queryKey: ["tracker-rule-activity", instance.id],
      queryFn: () => api.getTrackerRuleActivity(instance.id, 100),
      refetchInterval: expandedInstances.includes(String(instance.id)) ? 5000 : 30000,
      staleTime: 5000,
    })),
  })

  const handleDeleteOldActivity = async (instanceId: number, days: number) => {
    try {
      const result = await api.deleteTrackerRuleActivity(instanceId, days)
      toast.success(`Deleted ${result.deleted} activity entries`)
      queryClient.invalidateQueries({ queryKey: ["tracker-rule-activity", instanceId] })
    } catch (error) {
      toast.error("Failed to delete activity", {
        description: error instanceof Error ? error.message : "Unknown error",
      })
    }
  }

  const outcomeClasses: Record<TrackerRuleActivity["outcome"], string> = {
    success: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20",
    failed: "bg-destructive/10 text-destructive border-destructive/30",
  }

  const actionClasses: Record<TrackerRuleActivity["action"], string> = {
    deleted_ratio: "bg-blue-500/10 text-blue-500 border-blue-500/20",
    deleted_seeding: "bg-purple-500/10 text-purple-500 border-purple-500/20",
    deleted_unregistered: "bg-orange-500/10 text-orange-500 border-orange-500/20",
    delete_failed: "bg-destructive/10 text-destructive border-destructive/30",
    limit_failed: "bg-yellow-500/10 text-yellow-500 border-yellow-500/20",
  }

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">Tracker Rules Activity</CardTitle>
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
          <CardTitle className="text-lg font-semibold">Tracker Rules Activity</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px]">
              <p>
                History of torrents deleted by tracker rules due to ratio limits, seeding time limits,
                or unregistered status. Use the Clear button to manage retention.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          Persistent history of tracker rule deletions and errors.
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
            const filter = filterMap[instance.id] ?? "all"
            const searchTerm = (searchMap[instance.id] ?? "").toLowerCase().trim()

            // Filter events
            const filteredEvents = events
              .filter((e) => {
                if (filter === "deletions" && e.outcome !== "success") return false
                if (filter === "errors" && e.outcome !== "failed") return false
                if (searchTerm) {
                  const nameMatch = e.torrentName?.toLowerCase().includes(searchTerm)
                  const hashMatch = e.hash.toLowerCase().includes(searchTerm)
                  const ruleMatch = e.ruleName?.toLowerCase().includes(searchTerm)
                  if (!nameMatch && !hashMatch && !ruleMatch) return false
                }
                return true
              })
              .slice(0, 50)

            return (
              <AccordionItem key={instance.id} value={String(instance.id)}>
                <AccordionTrigger className="px-6 py-4 hover:no-underline group">
                  <div className="flex items-center justify-between w-full pr-4">
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="font-medium truncate">{instance.name}</span>
                      {stats.deletionsToday > 0 && (
                        <Badge variant="outline" className="bg-emerald-500/10 text-emerald-500 border-emerald-500/20 text-xs">
                          {stats.deletionsToday} today
                        </Badge>
                      )}
                      {stats.failedToday > 0 && (
                        <Badge variant="outline" className="bg-destructive/10 text-destructive border-destructive/30 text-xs">
                          {stats.failedToday} failed
                        </Badge>
                      )}
                    </div>

                    <div className="flex items-center gap-4">
                      {stats.lastActivity && (
                        <span className="text-xs text-muted-foreground hidden sm:block">
                          {formatRelativeTime(stats.lastActivity)}
                        </span>
                      )}
                    </div>
                  </div>
                </AccordionTrigger>

                <AccordionContent className="px-6 pb-4">
                  <div className="space-y-4">
                    {/* Settings summary */}
                    <div className="flex items-center justify-between p-3 rounded-lg bg-muted/30 border border-border/40">
                      <div className="space-y-0.5">
                        <p className="text-sm text-muted-foreground">
                          {events.length} events in history
                        </p>
                        <p className="text-xs text-muted-foreground/70">
                          Auto-pruned after 7 days (configurable via Clear)
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
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
                        <AlertDialog>
                          <AlertDialogTrigger asChild>
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-8"
                              disabled={events.length === 0}
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              Clear
                            </Button>
                          </AlertDialogTrigger>
                          <AlertDialogContent>
                            <AlertDialogHeader>
                              <AlertDialogTitle>Clear Activity History</AlertDialogTitle>
                              <AlertDialogDescription>
                                Delete activity history older than the selected period.
                                This action cannot be undone.
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <div className="py-4">
                              <label className="text-sm font-medium mb-2 block">
                                Keep activity from the last:
                              </label>
                              <Select
                                value={clearDaysMap[instance.id] ?? "7"}
                                onValueChange={(value) =>
                                  setClearDaysMap((prev) => ({ ...prev, [instance.id]: value }))
                                }
                              >
                                <SelectTrigger className="w-full">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="1">1 day</SelectItem>
                                  <SelectItem value="3">3 days</SelectItem>
                                  <SelectItem value="7">7 days</SelectItem>
                                  <SelectItem value="14">14 days</SelectItem>
                                  <SelectItem value="30">30 days</SelectItem>
                                  <SelectItem value="0">Delete all</SelectItem>
                                </SelectContent>
                              </Select>
                            </div>
                            <AlertDialogFooter>
                              <AlertDialogCancel>Cancel</AlertDialogCancel>
                              <AlertDialogAction
                                onClick={() => handleDeleteOldActivity(
                                  instance.id,
                                  parseInt(clearDaysMap[instance.id] ?? "7", 10)
                                )}
                              >
                                Delete
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      </div>
                    </div>

                    {/* Activity log */}
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          <h4 className="text-sm font-medium">Recent Activity</h4>
                          <span className="text-xs text-muted-foreground">
                            {filteredEvents.length === events.length
                              ? `${events.length} events`
                              : `${filteredEvents.length} of ${events.length}`}
                          </span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Select
                            value={filter}
                            onValueChange={(value: "all" | "deletions" | "errors") =>
                              setFilterMap((prev) => ({ ...prev, [instance.id]: value }))
                            }
                          >
                            <SelectTrigger className="h-7 w-28 text-xs">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="all">All</SelectItem>
                              <SelectItem value="deletions">Deletions</SelectItem>
                              <SelectItem value="errors">Errors</SelectItem>
                            </SelectContent>
                          </Select>
                          <Button
                            type="button"
                            size="sm"
                            variant="ghost"
                            disabled={activityQuery?.isFetching}
                            onClick={() => queryClient.invalidateQueries({
                              queryKey: ["tracker-rule-activity", instance.id],
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
                          placeholder="Filter by name, hash, or rule..."
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
                        <div className="h-[150px] flex items-center justify-center border rounded-lg bg-muted/10">
                          <p className="text-sm text-muted-foreground">Loading activity...</p>
                        </div>
                      ) : filteredEvents.length === 0 ? (
                        <div className="h-[100px] flex flex-col items-center justify-center border border-dashed rounded-lg bg-muted/10 text-center p-4">
                          <p className="text-sm text-muted-foreground">
                            {searchTerm ? "No matching events found." : "No activity recorded yet."}
                          </p>
                          <p className="text-xs text-muted-foreground/60 mt-1">
                            {searchTerm
                              ? "Try a different search term or clear the filter."
                              : "Events will appear here when tracker rules delete torrents."}
                          </p>
                        </div>
                      ) : (
                        <div className="max-h-[350px] overflow-auto rounded-md border">
                          <div className="divide-y divide-border/40">
                            {filteredEvents.map((event) => (
                              <div
                                key={event.id}
                                className="p-3 hover:bg-muted/20 transition-colors"
                              >
                                <div className="flex flex-col gap-2">
                                  <div className="grid grid-cols-[1fr_auto] items-center gap-2">
                                    <div className="min-w-0">
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <span className="font-medium text-sm truncate block cursor-help">
                                            {event.torrentName || event.hash}
                                          </span>
                                        </TooltipTrigger>
                                        <TooltipContent>
                                          <p className="font-semibold">{event.torrentName || "N/A"}</p>
                                        </TooltipContent>
                                      </Tooltip>
                                    </div>
                                    <div className="flex items-center gap-1.5">
                                      <Badge
                                        variant="outline"
                                        className={cn(
                                          "text-[10px] px-1.5 py-0 h-5 shrink-0",
                                          actionClasses[event.action]
                                        )}
                                      >
                                        {formatAction(event.action)}
                                      </Badge>
                                      <Badge
                                        variant="outline"
                                        className={cn(
                                          "capitalize text-[10px] px-1.5 py-0 h-5 shrink-0",
                                          outcomeClasses[event.outcome]
                                        )}
                                      >
                                        {event.outcome}
                                      </Badge>
                                    </div>
                                  </div>

                                  <div className="flex items-center gap-3 text-xs text-muted-foreground flex-wrap">
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
                                    {event.trackerDomain && (
                                      <>
                                        <span className="text-muted-foreground/40">·</span>
                                        <span className="text-xs font-medium">{event.trackerDomain}</span>
                                      </>
                                    )}
                                    {event.ruleName && (
                                      <>
                                        <span className="text-muted-foreground/40">·</span>
                                        <span className="text-xs">Rule: {event.ruleName}</span>
                                      </>
                                    )}
                                    <span className="text-muted-foreground/40">·</span>
                                    <span>{formatISOTimestamp(event.createdAt)}</span>
                                  </div>

                                  {event.reason && (
                                    <div className="text-xs bg-muted/30 p-2 rounded">
                                      <span>{event.reason}</span>
                                    </div>
                                  )}

                                  {event.details && (
                                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                      {event.details.ratio !== undefined && (
                                        <span>Ratio: {event.details.ratio.toFixed(2)}/{event.details.ratioLimit?.toFixed(2)}</span>
                                      )}
                                      {event.details.seedingMinutes !== undefined && (
                                        <span>Seeding: {event.details.seedingMinutes}m/{event.details.seedingLimitMinutes}m</span>
                                      )}
                                      {event.details.filesKept !== undefined && (
                                        <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-5">
                                          {event.details.filesKept ? "Files kept" : "Files deleted"}
                                        </Badge>
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
