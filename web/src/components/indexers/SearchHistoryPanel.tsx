/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChevronDown, CircleAlert, CircleCheck, CircleX, Clock, type LucideIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { Facts, type Fact } from "@/components/kit/Facts"
import { ResourceList, type ResourceRow } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useSearchHistory } from "@/hooks/useSearchHistory"
import { formatRelativeTime, formatTimeHMS } from "@/lib/dateTimeUtils"
import { cn } from "@/lib/utils"
import type { SearchHistoryEntry } from "@/types"

const CATEGORY_MAP: Record<string, string> = {
  "1000": "Console",
  "2000": "Movies",
  "3000": "Audio",
  "4000": "PC",
  "5000": "TV",
  "6000": "XXX",
  "7000": "Books",
  "8000": "Other",
  "2010": "Movies Foreign",
  "2020": "Movies Other",
  "2030": "Movies SD",
  "2040": "Movies HD",
  "2045": "Movies UHD",
  "2050": "Movies BluRay",
  "2060": "Movies 3D",
  "2070": "Movies DVD",
  "2080": "Movies Web",
  "2090": "Movies x265",
  "3010": "Audio MP3",
  "3020": "Audio Video",
  "3030": "Audio Audiobook",
  "3040": "Audio Lossless",
  "3050": "Audio Other",
  "3060": "Audio Foreign",
  "4010": "PC 0day",
  "4020": "PC ISO",
  "4030": "PC Mac",
  "4040": "PC Mobile Other",
  "4050": "PC Games",
  "4060": "PC Mobile iOS",
  "4070": "PC Mobile Android",
  "5010": "TV Web",
  "5020": "TV Foreign",
  "5030": "TV SD",
  "5040": "TV HD",
  "5045": "TV UHD",
  "5050": "TV Other",
  "5060": "TV Sport",
  "5070": "TV Anime",
  "5080": "TV Documentary",
  "5090": "TV x265",
  "6010": "XXX DVD",
  "6020": "XXX WMV",
  "6030": "XXX XviD",
  "6040": "XXX x264",
  "6045": "XXX UHD",
  "6050": "XXX Pack",
  "6060": "XXX ImageSet",
  "6070": "XXX Other",
  "6080": "XXX SD",
  "6090": "XXX Web",
  "7010": "Books Mags",
  "7020": "Books EBook",
  "7030": "Books Comics",
  "7040": "Books Technical",
  "7050": "Books Other",
  "7060": "Books Foreign",
  "8010": "Other Misc",
  "8020": "Other Hashed",
}

interface ParamBadge {
  label: string
  value?: string
}

function transformParams(params: Record<string, string>, t: (key: string) => string): ParamBadge[] {
  const badges: ParamBadge[] = []
  const consumed = new Set<string>()

  if (params.season) {
    badges.push({ label: t("indexers.searchHistory.detail.season"), value: params.season })
    consumed.add("season")
  }
  if (params.ep) {
    badges.push({ label: t("indexers.searchHistory.detail.episode"), value: params.ep })
    consumed.add("ep")
  }

  if (params.cat) {
    const cats = params.cat.split(",").map((c) => c.trim()).filter(Boolean)
    for (const cat of cats) {
      let catName = CATEGORY_MAP[cat]
      if (!catName && cat.length >= 4) {
        const parentCat = `${cat.slice(0, 1)}000`
        catName = CATEGORY_MAP[parentCat]
      }
      badges.push({ label: catName ? `${catName} (${cat})` : cat })
    }
    consumed.add("cat")
  }

  if (params.year) {
    badges.push({ label: params.year })
    consumed.add("year")
  }

  const idMappings: [string, string][] = [
    ["imdbid", "IMDB"],
    ["tmdbid", "TMDB"],
    ["tvdbid", "TVDB"],
    ["tvmazeid", "TVMaze"],
    ["rid", "TVRage"],
  ]
  for (const [key, label] of idMappings) {
    if (params[key]) {
      badges.push({ label, value: params[key] })
      consumed.add(key)
    }
  }

  consumed.add("t")
  consumed.add("q")

  for (const [key, value] of Object.entries(params)) {
    if (!consumed.has(key)) {
      badges.push({ label: key, value })
    }
  }

  return badges
}

const SHOWN = 10

const STATUS_ICON: Record<SearchHistoryEntry["status"], { icon: LucideIcon; className: string }> = {
  success: { icon: CircleCheck, className: "text-emerald-500" },
  error: { icon: CircleX, className: "text-destructive" },
  rate_limited: { icon: CircleAlert, className: "text-amber-500" },
  skipped: { icon: Clock, className: "text-muted-foreground" },
}

const STATUS_LABEL_KEY: Record<SearchHistoryEntry["status"], string> = {
  success: "indexers.searchHistory.detail.statusSuccess",
  error: "indexers.searchHistory.detail.statusFailed",
  rate_limited: "indexers.searchHistory.detail.statusRateLimited",
  skipped: "indexers.searchHistory.detail.statusSkipped",
}

const formatDuration = (ms: number, digits: number) => ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(digits)}s`

interface EntryLabels {
  status: string
  priority: string
  contentType?: string
  added: number
}

export function SearchHistoryPanel() {
  const { t } = useTranslation("settings")
  useActivityStream(true)
  const query = useSearchHistory({ limit: 50, enabled: true, refetchInterval: false })
  const [showAll, setShowAll] = useState(false)

  const entries = query.data?.entries ?? []
  const shown = showAll ? entries : entries.slice(0, SHOWN)
  const successCount = entries.filter((entry) => entry.status === "success").length
  const errorCount = entries.filter((entry) => entry.status === "error" || entry.status === "rate_limited").length
  const summary = [
    t("indexers.searchHistory.searches", { count: query.data?.total ?? 0 }),
    t("indexers.searchHistory.successful", { count: successCount }),
    errorCount > 0 && t("indexers.searchHistory.failed", { count: errorCount }),
    query.data?.source && t("indexers.searchHistory.source", { source: query.data.source }),
  ].filter(Boolean).join(" · ")

  const row = (entry: SearchHistoryEntry): ResourceRow => {
    const status = STATUS_ICON[entry.status] ?? STATUS_ICON.error
    const labels: EntryLabels = {
      status: t(STATUS_LABEL_KEY[entry.status] ?? STATUS_LABEL_KEY.error),
      priority: t([`indexers.priorityLabels.${entry.priority}`, "common:status.unknown"]),
      contentType: entry.contentType && entry.contentType !== "unknown"
        ? t([`indexers.contentTypeLabels.${entry.contentType}`, "common:status.unknown"])
        : undefined,
      added: entry.outcome === "added" ? entry.addedCount || 1 : 0,
    }
    return {
      title: entry.indexerName,
      icon: <status.icon className={cn("size-4", status.className)} />,
      meta: [
        entry.query && `"${entry.query}"`,
        entry.status === "success" ? t("indexers.searchHistory.results", { count: entry.resultCount }) : labels.status,
        labels.added > 0 && <span className="font-medium text-primary">{t("indexers.searchHistory.detail.added", { count: labels.added })}</span>,
        entry.status !== "success" && entry.errorMessage,
        labels.contentType,
        labels.priority,
        formatDuration(entry.durationMs, 1),
        formatRelativeTime(new Date(entry.completedAt)),
      ].filter(Boolean),
      expand: <HistoryDetail entry={entry} labels={labels} />,
    }
  }

  return (
    <Section
      title={t("indexers.searchHistory.title")}
      description={t("indexers.searchHistory.description")}
      footer={entries.length > 0 ? summary : undefined}
      queries={[query]}
    >
      <div className="space-y-2">
        <ResourceList
          label={t("indexers.searchHistory.title")}
          items={shown}
          itemKey={(entry) => entry.id}
          row={row}
          empty={{ title: t("indexers.searchHistory.noSearchesYet") }}
        />
        {entries.length > SHOWN && (
          <Button variant="ghost" size="sm" className="w-full text-muted-foreground max-sm:h-10" onClick={() => setShowAll(!showAll)}>
            {showAll ? t("indexers.searchHistory.showFewer") : t("indexers.searchHistory.showAll", { count: entries.length })}
            <ChevronDown className={cn("transition-transform", showAll && "rotate-180")} />
          </Button>
        )}
      </div>
    </Section>
  )
}

function HistoryDetail({ entry, labels }: { entry: SearchHistoryEntry; labels: EntryLabels }) {
  const { t } = useTranslation("settings")
  const params = entry.params ? transformParams(entry.params, t) : []
  const facts: (Fact | false | undefined)[] = [
    { label: t("indexers.table.status"), value: labels.status },
    entry.query ? { label: t("indexers.searchHistory.detail.query"), value: entry.query } : undefined,
    entry.releaseName ? { label: t("indexers.searchHistory.detail.release"), value: entry.releaseName, mono: true, copy: entry.releaseName } : undefined,
    { label: t("indexers.searchHistory.detail.results"), value: entry.resultCount },
    labels.added > 0 && { label: t("indexers.searchHistory.detail.crossSeed"), value: t("indexers.searchHistory.detail.added", { count: labels.added }) },
    entry.errorMessage ? { label: t("indexers.searchHistory.detail.error"), value: <span className="text-destructive">{entry.errorMessage}</span> } : undefined,
    params.length > 0 && {
      label: t("indexers.searchHistory.detail.searchParameters"),
      value: params.map((param) => param.value ? `${param.label}: ${param.value}` : param.label).join(" · "),
    },
    entry.searchMode ? { label: t("indexers.searchHistory.detail.mode"), value: entry.searchMode, mono: true } : undefined,
    labels.contentType ? { label: t("indexers.searchHistory.detail.type"), value: labels.contentType } : undefined,
    { label: t("indexers.searchHistory.detail.priority"), value: labels.priority },
    { label: t("indexers.searchHistory.detail.duration"), value: formatDuration(entry.durationMs, 2) },
    {
      label: t("indexers.searchHistory.detail.finished"),
      value: formatTimeHMS(new Date(entry.completedAt)),
      hint: t("indexers.searchHistory.detail.job", { id: entry.jobId }),
    },
  ]

  return <Facts items={facts.filter((fact): fact is Fact => Boolean(fact))} />
}
