/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { useClientSetting } from "@/lib/client-settings"
import type { SeasonPackRun } from "@/types"
import { ChevronDown, History, Loader2, RefreshCw, Search } from "lucide-react"
import { Fragment, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

interface SeasonPackRunsPanelProps {
  runs: SeasonPackRun[]
  isLoading: boolean
  isFetching: boolean
  error: unknown
  onRefresh: () => void
  formatDateValue: (date: string | undefined) => string
}

const STATUS_FILTERS = ["applied", "ready", "skipped", "failed"] as const
type StatusFilter = typeof STATUS_FILTERS[number]

const DEFAULT_STATUS_FILTERS: StatusFilter[] = ["applied"]

const STATUS_FILTER_LABEL_KEYS = {
  applied: "seasonPackRuns.statusLabels.applied",
  ready: "seasonPackRuns.statusLabels.ready",
  skipped: "seasonPackRuns.statusLabels.skipped",
  failed: "seasonPackRuns.statusLabels.failed",
} as const satisfies Record<StatusFilter, string>

const SHOW_STATUS_KEYS = {
  applied: "seasonPackRuns.showStatus.applied",
  ready: "seasonPackRuns.showStatus.ready",
  skipped: "seasonPackRuns.showStatus.skipped",
  failed: "seasonPackRuns.showStatus.failed",
} as const satisfies Record<StatusFilter, string>

function parseStatusFilters(raw: string): StatusFilter[] {
  const parsed: unknown = JSON.parse(raw)
  if (!Array.isArray(parsed) || parsed.some(item => !STATUS_FILTERS.includes(item))) {
    throw new Error("invalid season pack run status filters")
  }
  return parsed
}

function statusFilterOf(status: SeasonPackRun["status"]): StatusFilter {
  return status === "ready_no_threshold" ? "ready" : status
}

function seasonPackStatusVariant(status: SeasonPackRun["status"]) {
  switch (status) {
    case "ready":
    case "applied":
      return "default"
    case "failed":
      return "destructive"
    default:
      return "secondary"
  }
}

function formatSeasonPackCoverage(run: SeasonPackRun) {
  if (run.totalEpisodes <= 0) {
    return "—"
  }
  return `${Math.round(run.coverage * 100)}%`
}

function formatSeasonPackReason(run: SeasonPackRun): string | null {
  const reason = run.reason?.trim()
  if (!reason) return null
  if (reason.toLowerCase() === run.status) return null
  return reason.replace(/_/g, " ")
}

export function SeasonPackRunsPanel({
  runs,
  isLoading,
  isFetching,
  error,
  onRefresh,
  formatDateValue,
}: SeasonPackRunsPanelProps) {
  const { t } = useTranslation("crossseed")
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [statusFilters, setStatusFilters] = useClientSetting<StatusFilter[]>("qui-season-pack-run-status-filters", {
    defaultValue: DEFAULT_STATUS_FILTERS,
    parse: parseStatusFilters,
  })

  const statusCounts = useMemo(() => {
    const counts: Record<StatusFilter, number> = { applied: 0, ready: 0, skipped: 0, failed: 0 }
    for (const run of runs) counts[statusFilterOf(run.status)]++
    return counts
  }, [runs])

  const filteredRuns = useMemo(() => {
    const query = search.trim().toLowerCase()
    return runs.filter(run => {
      if (!statusFilters.includes(statusFilterOf(run.status))) return false
      if (query && !run.torrentName.toLowerCase().includes(query)) return false
      return true
    })
  }, [runs, search, statusFilters])

  const toggleStatusFilter = (filter: StatusFilter) => {
    setStatusFilters(prev => prev.includes(filter) ? prev.filter(item => item !== filter) : [...prev, filter])
  }

  const hiddenStatusesWithRuns = STATUS_FILTERS.filter(filter => !statusFilters.includes(filter) && statusCounts[filter] > 0)

  return (
    <div className="pt-3 border-t border-border/50">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger className="flex w-full items-center justify-between gap-3 text-left hover:cursor-pointer">
          <div className="flex flex-wrap items-center gap-2 min-w-0">
            <History className="h-4 w-4 text-muted-foreground shrink-0" />
            <span className="text-sm font-medium">{t("seasonPackRuns.title")}</span>
            {runs.length > 0 ? (
              <Badge variant="secondary" className="text-xs">
                {t("seasonPackRuns.runsCount", { count: runs.length })}
                {statusCounts.applied > 0 && ` • ${t("seasonPackRuns.appliedCount", { count: statusCounts.applied })}`}
                {statusCounts.failed > 0 && ` • ${t("seasonPackRuns.failedCount", { count: statusCounts.failed })}`}
              </Badge>
            ) : (
              <span className="text-xs text-muted-foreground">{t("seasonPackRuns.noRunsYet")}</span>
            )}
          </div>
          <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform shrink-0 ${open ? "rotate-180" : ""}`} />
        </CollapsibleTrigger>

        <CollapsibleContent>
          <div className="space-y-3 pt-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
              <div role="group" aria-label={t("seasonPackRuns.statusFilter")} className="flex flex-wrap items-center gap-1.5 sm:mr-auto">
                {STATUS_FILTERS.map(filter => {
                  const selected = statusFilters.includes(filter)
                  return (
                    <Button
                      key={filter}
                      type="button"
                      size="sm"
                      variant={selected ? "secondary" : "outline"}
                      aria-pressed={selected}
                      onClick={() => toggleStatusFilter(filter)}
                      className={`h-7 gap-1.5 px-2.5 text-xs ${selected ? "" : "text-muted-foreground"}`}
                    >
                      <span>{t(STATUS_FILTER_LABEL_KEYS[filter])}</span>{" "}
                      <span className="tabular-nums opacity-70">{statusCounts[filter]}</span>
                    </Button>
                  )
                })}
              </div>
              <div className="relative w-full sm:w-56">
                <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  type="search"
                  value={search}
                  onChange={event => setSearch(event.target.value)}
                  placeholder={t("seasonPackRuns.filterByName")}
                  className="h-8 pl-8 text-xs"
                />
              </div>
              <Button type="button" variant="outline" size="sm" onClick={onRefresh} disabled={isFetching} className="self-end sm:self-auto">
                <RefreshCw className={`mr-2 h-3.5 w-3.5 ${isFetching ? "animate-spin" : ""}`} />
                {t("seasonPackRuns.refresh")}
              </Button>
            </div>

            {error ? (
              <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
                {t("seasonPackRuns.loadError")}
              </div>
            ) : isLoading ? (
              <div className="flex items-center gap-2 rounded-md border border-border/70 bg-background/50 p-3 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                {t("seasonPackRuns.loading")}
              </div>
            ) : runs.length === 0 ? (
              <div className="rounded-md border border-border/70 bg-background/50 p-3 text-sm text-muted-foreground">
                {t("seasonPackRuns.noActivity")}
              </div>
            ) : filteredRuns.length === 0 ? (
              <div className="rounded-md border border-border/70 bg-background/50 p-3 text-sm text-muted-foreground">
                {t("seasonPackRuns.noMatches")}
                {hiddenStatusesWithRuns.map((filter, index) => (
                  <Fragment key={filter}>
                    {index === 0 ? " " : " · "}
                    <button
                      type="button"
                      onClick={() => toggleStatusFilter(filter)}
                      className="text-foreground underline-offset-2 hover:underline"
                    >
                      {t(SHOW_STATUS_KEYS[filter], { total: statusCounts[filter] })}
                    </button>
                  </Fragment>
                ))}
              </div>
            ) : (
              <div className="overflow-hidden rounded-md border border-border/70">
                <div className="max-h-[420px] divide-y divide-border/70 overflow-y-auto">
                  {filteredRuns.map(run => {
                    const reasonLabel = formatSeasonPackReason(run)
                    return (
                      <div key={run.id} className="grid gap-2 bg-background/50 p-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
                        <div className="min-w-0 space-y-1">
                          <div className="flex min-w-0 flex-wrap items-center gap-2">
                            <Badge variant={seasonPackStatusVariant(run.status)} className="capitalize">{t(`seasonPackRuns.statusLabels.${run.status}`, run.status)}</Badge>
                            <Badge variant="outline" className="uppercase">{t(`seasonPackRuns.phases.${run.phase}`, run.phase)}</Badge>
                            {reasonLabel && <span className="text-xs text-muted-foreground">· {reasonLabel}</span>}
                          </div>
                          <p className="truncate text-sm font-medium" title={run.torrentName}>{run.torrentName}</p>
                          {run.message && <p className="line-clamp-2 text-xs text-muted-foreground">{run.message}</p>}
                        </div>
                        <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground md:min-w-72 md:grid-cols-3">
                          <span>{t("seasonPackRuns.coverage")} <span className="font-medium text-foreground">{formatSeasonPackCoverage(run)}</span></span>
                          <span>{t("seasonPackRuns.episodes")} <span className="font-medium text-foreground">{run.matchedEpisodes}/{run.totalEpisodes || "?"}</span></span>
                          <span>{t("seasonPackRuns.instance")} <span className="font-medium text-foreground">{run.instanceId ?? "—"}</span></span>
                          <span>{t("seasonPackRuns.mode")} <span className="font-medium text-foreground">{run.linkMode || "—"}</span></span>
                          <span className="col-span-2 md:col-span-2">{formatDateValue(run.createdAt)}</span>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}
