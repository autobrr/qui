/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import type { IndexerActivityStatus, IndexerCooldownStatus, SchedulerTaskStatus } from "@/types"
import { Activity, ChevronDown, Clock, Loader2, Pause, Zap } from "lucide-react"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

function useCommonTr() {
  const { t } = useTranslation("common")
  return (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
}

export function IndexerActivityPanel() {
  const tr = useCommonTr()
  const [activity, setActivity] = useState<IndexerActivityStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [isOpen, setIsOpen] = useState(true)

  const fetchActivity = async () => {
    try {
      const data = await api.getIndexerActivityStatus()
      setActivity(data)
    } catch (error) {
      console.error("Failed to fetch activity status:", error)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchActivity()
    // Poll every 2 seconds when open
    const interval = setInterval(() => {
      if (isOpen) {
        fetchActivity()
      }
    }, 2000)
    return () => clearInterval(interval)
  }, [isOpen])

  const hasActivity = activity && (
    (activity.scheduler?.queueLength ?? 0) > 0 ||
    (activity.scheduler?.workersInUse ?? 0) > 0 ||
    activity.cooldownIndexers.length > 0
  )

  const queueLength = activity?.scheduler?.queueLength ?? 0
  const workersInUse = activity?.scheduler?.workersInUse ?? 0
  const workerCount = activity?.scheduler?.workerCount ?? 0
  const cooldownCount = activity?.cooldownIndexers.length ?? 0

  return (
    <Collapsible open={isOpen} onOpenChange={setIsOpen}>
      <div className="rounded-xl border bg-card text-card-foreground shadow-sm">
        <CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-4 hover:cursor-pointer text-left hover:bg-muted/50 transition-colors rounded-xl">
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">{tr("indexerActivity.title")}</span>
            {loading ? (
              <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
            ) : hasActivity ? (
              <Badge variant="secondary" className="text-xs">
                {workersInUse > 0 && tr("indexerActivity.summary.running", { count: workersInUse })}
                {workersInUse > 0 && queueLength > 0 && ", "}
                {queueLength > 0 && tr("indexerActivity.summary.queued", { count: queueLength })}
                {(workersInUse > 0 || queueLength > 0) && cooldownCount > 0 && ", "}
                {cooldownCount > 0 && tr("indexerActivity.summary.cooldown", { count: cooldownCount })}
              </Badge>
            ) : (
              <span className="text-xs text-muted-foreground">
                {tr("indexerActivity.summary.workers", {
                  inUse: workersInUse,
                  total: workerCount,
                })}
              </span>
            )}
          </div>
          <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${isOpen ? "rotate-180" : ""}`} />
        </CollapsibleTrigger>

        <CollapsibleContent>
          <div className="px-4 pb-3 space-y-3">
            {/* In-flight tasks */}
            {activity?.scheduler && activity.scheduler.inFlightTasks.length > 0 && (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Zap className="h-4 w-4 text-yellow-500" />
                  {tr("indexerActivity.sections.running", { count: activity.scheduler.inFlightTasks.length })}
                </div>
                <div className="space-y-1">
                  {activity.scheduler.inFlightTasks.map((task) => (
                    <TaskRow key={task.taskId} task={task} status="running" />
                  ))}
                </div>
              </div>
            )}

            {/* Queued tasks */}
            {activity?.scheduler && activity.scheduler.queuedTasks.length > 0 && (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Clock className="h-4 w-4 text-blue-500" />
                  {tr("indexerActivity.sections.queued", { count: activity.scheduler.queuedTasks.length })}
                </div>
                <div className="space-y-1">
                  {activity.scheduler.queuedTasks.slice(0, 10).map((task) => (
                    <TaskRow key={task.taskId} task={task} status="queued" />
                  ))}
                  {activity.scheduler.queuedTasks.length > 10 && (
                    <div className="text-xs text-muted-foreground pl-2">
                      {tr("indexerActivity.sections.andMore", { count: activity.scheduler.queuedTasks.length - 10 })}
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* Cooldown indexers */}
            {activity?.cooldownIndexers && activity.cooldownIndexers.length > 0 && (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Pause className="h-4 w-4 text-orange-500" />
                  {tr("indexerActivity.sections.rateLimited", { count: activity.cooldownIndexers.length })}
                </div>
                <div className="space-y-1">
                  {activity.cooldownIndexers.map((cooldown) => (
                    <CooldownRow key={cooldown.indexerId} cooldown={cooldown} />
                  ))}
                </div>
              </div>
            )}

            {/* Empty state */}
            {!hasActivity && !loading && (
              <div className="text-center py-2 text-xs text-muted-foreground">
                {tr("indexerActivity.empty.noActivity")}
              </div>
            )}
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}

function TaskRow({ task, status }: { task: SchedulerTaskStatus; status: "running" | "queued" }) {
  const tr = useCommonTr()
  const priorityColors: Record<string, string> = {
    interactive: "text-green-500",
    rss: "text-blue-500",
    completion: "text-purple-500",
    background: "text-gray-500",
  }
  const priorityLabels: Record<string, string> = {
    interactive: tr("indexerActivity.priority.interactive"),
    rss: tr("indexerActivity.priority.rss"),
    completion: tr("indexerActivity.priority.completion"),
    background: tr("indexerActivity.priority.background"),
  }

  return (
    <div className="flex items-center justify-between gap-2 p-2 rounded bg-muted/30 text-sm">
      <div className="flex items-center gap-2 min-w-0">
        {status === "running" ? (
          <Loader2 className="h-3 w-3 animate-spin text-yellow-500 shrink-0" />
        ) : (
          <Clock className="h-3 w-3 text-blue-500 shrink-0" />
        )}
        <span className="truncate font-medium">{task.indexerName}</span>
        {task.isRss && (
          <Badge variant="outline" className="text-xs shrink-0">{tr("indexerActivity.badges.rss")}</Badge>
        )}
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <span className={`text-xs ${priorityColors[task.priority] ?? "text-gray-500"}`}>
          {priorityLabels[task.priority] ?? task.priority}
        </span>
        <span className="text-xs text-muted-foreground">
          {formatRelativeTime(new Date(task.createdAt))}
        </span>
      </div>
    </div>
  )
}

function CooldownRow({ cooldown }: { cooldown: IndexerCooldownStatus }) {
  const tr = useCommonTr()
  const cooldownEnd = new Date(cooldown.cooldownEnd)
  const remaining = cooldownEnd.getTime() - Date.now()
  const isExpired = remaining <= 0

  return (
    <div className="flex items-center justify-between gap-2 p-2 rounded bg-muted/30 text-sm">
      <div className="flex items-center gap-2 min-w-0">
        <Pause className="h-3 w-3 text-orange-500 shrink-0" />
        <span className="truncate font-medium">{cooldown.indexerName}</span>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        {isExpired ? (
          <span className="text-xs text-green-500">{tr("indexerActivity.cooldown.ready")}</span>
        ) : (
          <span className="text-xs text-orange-500">
            {tr("indexerActivity.cooldown.timeLeft", { time: formatRelativeTime(cooldownEnd, false) })}
          </span>
        )}
      </div>
    </div>
  )
}
