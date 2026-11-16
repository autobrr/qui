/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChevronDown, CircleAlert, CircleCheck, CircleX, Clock, type LucideIcon } from "lucide-react"
import { useId, useState } from "react"
import { useTranslation } from "react-i18next"

import { EmptyTile } from "@/components/kit/EmptyTile"
import { Facts, type Fact } from "@/components/kit/Facts"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useSearchHistory } from "@/hooks/useSearchHistory"
import { formatRelativeTime, formatTimeHMS } from "@/lib/dateTimeUtils"
import { cn } from "@/lib/utils"
import type { SearchHistoryEntry } from "@/types"

// Torznab standard category mappings, from Prowlarr's NewznabStandardCategory.
const CATEGORY_MAP: Record<string, string> = {
  // Parent categories
  "1000": "Console",
  "2000": "Movies",
  "3000": "Audio",
  "4000": "PC",
  "5000": "TV",
  "6000": "XXX",
  "7000": "Books",
  "8000": "Other",
  // Movies subcategories
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
  // Audio subcategories
  "3010": "Audio MP3",
  "3020": "Audio Video",
  "3030": "Audio Audiobook",
  "3040": "Audio Lossless",
  "3050": "Audio Other",
  "3060": "Audio Foreign",
  // PC subcategories
  "4010": "PC 0day",
  "4020": "PC ISO",
  "4030": "PC Mac",
  "4040": "PC Mobile Other",
  "4050": "PC Games",
  "4060": "PC Mobile iOS",
  "4070": "PC Mobile Android",
  // TV subcategories
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
  // XXX subcategories
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
  // Books subcategories
  "7010": "Books Mags",
  "7020": "Books EBook",
  "7030": "Books Comics",
  "7040": "Books Technical",
  "7050": "Books Other",
  "7060": "Books Foreign",
  // Other subcategories
  "8010": "Other Misc",
  "8020": "Other Hashed",
}

interface ParamBadge {
  label: string
  value?: string
}

// Transform raw torznab params into semantic badges
function transformParams(params: Record<string, string>, t: (key: string) => string): ParamBadge[] {
  const badges: ParamBadge[] = []
  const consumed = new Set<string>()

  // Season and episode as separate badges
  if (params.season) {
    badges.push({ label: t("indexers.searchHistory.detail.season"), value: params.season })
    consumed.add("season")
  }
  if (params.ep) {
    badges.push({ label: t("indexers.searchHistory.detail.episode"), value: params.ep })
    consumed.add("ep")
  }

  // Map each category ID to a name with number in parentheses
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

  // Year as standalone badge
  if (params.year) {
    badges.push({ label: params.year })
    consumed.add("year")
  }

  // External IDs with labels
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

  // Skip redundant params (already shown elsewhere)
  consumed.add("t") // Shown as Mode in footer
  consumed.add("q") // Already filtered out

  // Remaining params with key: value format
  for (const [key, value] of Object.entries(params)) {
    if (!consumed.has(key)) {
      badges.push({ label: key, value })
    }
  }

  return badges
}

// The list shows the latest searches first, and the rest on request.
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

/** The latest searches that qui sent to the indexers. A row opens to show the details of its search. */
export function SearchHistoryPanel() {
  const { t } = useTranslation("settings")
  // The stream sends the activity events that refresh the history, so it needs no polling.
  useActivityStream(true)
  const query = useSearchHistory({ limit: 50, enabled: true, refetchInterval: false })
  const [openId, setOpenId] = useState<number | null>(null)
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

  return (
    <Section
      title={t("indexers.searchHistory.title")}
      description={t("indexers.searchHistory.description")}
      footer={entries.length > 0 ? summary : undefined}
      queries={[query]}
    >
      {entries.length === 0 ? (
        <EmptyTile title={t("indexers.searchHistory.noSearchesYet")} />
      ) : (
        <div className="space-y-2">
          <ul aria-label={t("indexers.searchHistory.title")} className="flex flex-col gap-1.5">
            {shown.map((entry) => (
              <HistoryRow key={entry.id} entry={entry} open={openId === entry.id} onToggle={() => setOpenId(openId === entry.id ? null : entry.id)} />
            ))}
          </ul>
          {entries.length > SHOWN && (
            <Button variant="ghost" size="sm" className="w-full text-muted-foreground max-sm:h-10" onClick={() => setShowAll(!showAll)}>
              {showAll ? t("indexers.searchHistory.showFewer") : t("indexers.searchHistory.showAll", { count: entries.length })}
              <ChevronDown className={cn("transition-transform", showAll && "rotate-180")} />
            </Button>
          )}
        </div>
      )}
    </Section>
  )
}

// A disclosure row, not a ResourceList row: it opens its details in place and has no steps, and no other list needs that yet.
function HistoryRow({ entry, open, onToggle }: { entry: SearchHistoryEntry; open: boolean; onToggle: () => void }) {
  const { t } = useTranslation("settings")
  const detailId = useId()
  const status = STATUS_ICON[entry.status] ?? STATUS_ICON.error
  const statusLabel = t(STATUS_LABEL_KEY[entry.status] ?? STATUS_LABEL_KEY.error)
  const priority = t([`indexers.priorityLabels.${entry.priority}`, "common:status.unknown"])
  // An RSS search has no content type, and "unknown" says nothing.
  const contentType = entry.contentType && entry.contentType !== "unknown"
    ? t([`indexers.contentTypeLabels.${entry.contentType}`, "common:status.unknown"])
    : undefined
  const added = entry.outcome === "added" ? entry.addedCount || 1 : 0

  const meta = [
    entry.status === "success" ? t("indexers.searchHistory.results", { count: entry.resultCount }) : statusLabel,
    entry.status !== "success" && entry.errorMessage,
    contentType,
    priority,
    formatDuration(entry.durationMs, 1),
    formatRelativeTime(new Date(entry.completedAt)),
  ].filter(Boolean).join(" · ")

  const params = entry.params ? transformParams(entry.params, t) : []
  const facts: (Fact | false | undefined)[] = [
    { label: t("indexers.table.status"), value: statusLabel },
    entry.query ? { label: t("indexers.searchHistory.detail.query"), value: entry.query } : undefined,
    entry.releaseName ? { label: t("indexers.searchHistory.detail.release"), value: entry.releaseName, mono: true, copy: entry.releaseName } : undefined,
    { label: t("indexers.searchHistory.detail.results"), value: entry.resultCount },
    added > 0 && { label: t("indexers.searchHistory.detail.crossSeed"), value: t("indexers.searchHistory.detail.added", { count: added }) },
    entry.errorMessage ? { label: t("indexers.searchHistory.detail.error"), value: <span className="text-destructive">{entry.errorMessage}</span> } : undefined,
    params.length > 0 && {
      label: t("indexers.searchHistory.detail.searchParameters"),
      value: params.map((param) => param.value ? `${param.label}: ${param.value}` : param.label).join(" · "),
    },
    entry.searchMode ? { label: t("indexers.searchHistory.detail.mode"), value: entry.searchMode, mono: true } : undefined,
    contentType ? { label: t("indexers.searchHistory.detail.type"), value: contentType } : undefined,
    { label: t("indexers.searchHistory.detail.priority"), value: priority },
    { label: t("indexers.searchHistory.detail.duration"), value: formatDuration(entry.durationMs, 2) },
    {
      label: t("indexers.searchHistory.detail.finished"),
      value: formatTimeHMS(new Date(entry.completedAt)),
      hint: t("indexers.searchHistory.detail.job", { id: entry.jobId }),
    },
  ]

  return (
    <li className="rounded-lg border">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={open ? detailId : undefined}
        onClick={onToggle}
        className="flex min-h-14 w-full items-center gap-3 rounded-lg px-3 py-2 text-left outline-none transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
          <status.icon className={cn("size-4", status.className)} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 flex-col sm:flex-row sm:items-baseline sm:gap-2">
            <span className="text-sm font-medium [overflow-wrap:anywhere] sm:max-w-[50%] sm:shrink-0 sm:truncate">{entry.indexerName}</span>
            {entry.query && <span className="min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere] sm:truncate">"{entry.query}"</span>}
          </span>
          <span className="block text-xs text-muted-foreground [overflow-wrap:anywhere] max-sm:line-clamp-2 sm:truncate">{meta}</span>
        </span>
        {added > 0 && (
          <span className="shrink-0 rounded-md bg-primary/10 px-1.5 py-0.5 text-xs font-medium tabular-nums text-primary">+{added}</span>
        )}
        <ChevronDown aria-hidden="true" className={cn("size-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <div id={detailId} className="border-t px-3 py-3">
          <Facts items={facts.filter((fact): fact is Fact => Boolean(fact))} />
        </div>
      )}
    </li>
  )
}
