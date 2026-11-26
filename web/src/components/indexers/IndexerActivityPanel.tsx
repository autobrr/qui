/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { Clock, Pause, Zap } from "lucide-react"
import { useTranslation } from "react-i18next"

import { ResourceList, type ResourceRow } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import type { IndexerCooldownStatus, SchedulerTaskStatus } from "@/types"

const QUEUE_SHOWN = 10

type ActivityItem =
  | { kind: "running" | "queued"; task: SchedulerTaskStatus }
  | { kind: "cooldown"; cooldown: IndexerCooldownStatus }

export function IndexerActivityPanel() {
  const { t } = useTranslation("settings")
  useActivityStream(true)
  const query = useQuery({
    queryKey: ["indexer-activity"],
    queryFn: () => api.getIndexerActivityStatus(),
    refetchInterval: false,
  })

  const scheduler = query.data?.scheduler
  const queued = scheduler?.queuedTasks ?? []
  const items: ActivityItem[] = [
    ...(scheduler?.inFlightTasks ?? []).map((task) => ({ kind: "running" as const, task })),
    ...(query.data?.cooldownIndexers ?? []).map((cooldown) => ({ kind: "cooldown" as const, cooldown })),
    ...queued.slice(0, QUEUE_SHOWN).map((task) => ({ kind: "queued" as const, task })),
  ]

  const priorityLabel = (priority: string) => t([`indexers.priorityLabels.${priority}`, "common:status.unknown"])

  const row = (item: ActivityItem): ResourceRow => {
    if (item.kind === "cooldown") {
      const end = new Date(item.cooldown.cooldownEnd)
      const ready = end.getTime() <= Date.now()
      return {
        title: item.cooldown.indexerName,
        icon: Pause,
        status: { tone: "warn", label: t("indexers.activity.rateLimited") },
        meta: [ready ? t("indexers.activity.ready") : t("indexers.activity.left", { time: formatRelativeTime(end, false) })],
      }
    }
    const { task } = item
    return {
      title: task.indexerName,
      icon: item.kind === "running" ? Zap : Clock,
      status: item.kind === "running"
        ? { tone: "ok", label: t("indexers.activity.running") }
        : { tone: "off", label: t("indexers.activity.queued") },
      meta: [
        priorityLabel(task.priority),
        ...(task.isRss && task.priority !== "rss" ? [t("indexers.priorityLabels.rss")] : []),
        formatRelativeTime(new Date(task.createdAt)),
      ],
    }
  }

  return (
    <Section
      title={t("indexers.activity.title")}
      description={t("indexers.activity.description")}
      footer={scheduler && t("indexers.activity.workersStatus", { used: scheduler.workersInUse, total: scheduler.workerCount })}
      queries={[query]}
    >
      <ResourceList
        label={t("indexers.activity.title")}
        items={items}
        itemKey={(item) => item.kind === "cooldown" ? `cooldown-${item.cooldown.indexerId}` : `${item.kind}-${item.task.taskId}`}
        row={row}
        empty={{ title: t("indexers.activity.noActiveTasks") }}
      />
      {queued.length > QUEUE_SHOWN && (
        <p className="-mt-4 text-xs text-muted-foreground">{t("indexers.activity.andMore", { count: queued.length - QUEUE_SHOWN })}</p>
      )}
    </Section>
  )
}
