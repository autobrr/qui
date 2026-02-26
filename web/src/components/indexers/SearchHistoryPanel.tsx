/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { useSearchHistory } from "@/hooks/useSearchHistory"
import { formatRelativeTime, formatTimeHMS } from "@/lib/dateTimeUtils"
import type { SearchHistoryEntry } from "@/types"
import { AlertCircle, CheckCircle2, ChevronDown, Clock, History, Loader2, Plus, XCircle } from "lucide-react"
import { type ReactNode, useState } from "react"
import { useTranslation } from "react-i18next"

// Torznab standard category mappings (synced with pkg/gojackett/constants.go)
const CATEGORY_KEY_MAP: Record<string, string> = {
  // Parent categories
  "1000": "console",
  "2000": "movies",
  "3000": "audio",
  "4000": "pc",
  "5000": "tv",
  "6000": "xxx",
  "7000": "other",
  "8000": "books",
  // Movies subcategories
  "2010": "moviesForeign",
  "2020": "moviesOther",
  "2030": "moviesSd",
  "2040": "moviesHd",
  "2045": "moviesUhd",
  "2050": "moviesBluRay",
  "2060": "movies3d",
  "2070": "moviesWeb",
  // Audio subcategories
  "3010": "audioMp3",
  "3020": "audioVideo",
  "3030": "audiobook",
  "3040": "audioLossless",
  "3050": "audioOther",
  "3060": "audioForeign",
  // PC subcategories
  "4010": "pc0day",
  "4020": "pcIso",
  "4030": "pcMac",
  "4040": "pcPhoneOther",
  "4050": "pcGames",
  "4060": "pcPhoneIos",
  "4070": "pcPhoneAndroid",
  // TV subcategories
  "5010": "tvWeb",
  "5020": "tvForeign",
  "5030": "tvSd",
  "5040": "tvHd",
  "5045": "tvUhd",
  "5060": "tvSport",
  "5070": "tvAnime",
  "5080": "tvDocumentary",
  "5090": "tvOther",
  // XXX subcategories
  "6010": "xxxDvd",
  "6020": "xxxWmv",
  "6030": "xxxXvid",
  "6040": "xxxX264",
  "6045": "xxxUhd",
  "6050": "xxxPack",
  "6060": "xxxImageSet",
  "6070": "xxxOther",
  "6080": "xxxSd",
  "6090": "xxxWeb",
  // Other subcategories
  "7010": "otherMisc",
  "7020": "otherHashed",
  // Books subcategories
  "8010": "booksMags",
  "8020": "booksEbook",
  "8030": "booksComics",
  "8040": "booksTechnical",
  "8050": "booksForeign",
  "8060": "booksOther",
}

type TranslateFn = (key: string, options?: Record<string, unknown>) => string

function formatSearchDuration(durationMs: number, secondsPrecision: number): string {
  if (durationMs < 1000) {
    return `${durationMs}ms`
  }
  return `${(durationMs / 1000).toFixed(secondsPrecision)}s`
}

interface ParamBadge {
  label: string
  value?: string
}

// Transform raw torznab params into semantic badges
function transformParams(params: Record<string, string>, tr: TranslateFn): ParamBadge[] {
  const badges: ParamBadge[] = []
  const consumed = new Set<string>()

  // Season and episode as separate badges
  if (params.season) {
    badges.push({ label: tr("searchHistoryPanel.params.season"), value: params.season })
    consumed.add("season")
  }
  if (params.ep) {
    badges.push({ label: tr("searchHistoryPanel.params.episode"), value: params.ep })
    consumed.add("ep")
  }

  // Map category ID to name with number in parentheses
  if (params.cat) {
    // Handle comma-separated categories, use first one for display
    const firstCat = params.cat.split(",")[0]
    // Try exact match first, then fall back to parent category (e.g., 5030 -> 5000 -> TV)
    let catKey = CATEGORY_KEY_MAP[firstCat]
    if (!catKey && firstCat.length >= 4) {
      const parentCat = `${firstCat.slice(0, 1)}000`
      catKey = CATEGORY_KEY_MAP[parentCat]
    }
    const catName = catKey ? tr(`searchHistoryPanel.params.categories.${catKey}`) : ""
    let catLabel = firstCat
    if (catName) {
      catLabel = tr("searchHistoryPanel.params.categoryWithId", { category: catName, id: firstCat })
    }
    badges.push({ label: catLabel })
    consumed.add("cat")
  }

  // Year as standalone badge
  if (params.year) {
    badges.push({ label: params.year })
    consumed.add("year")
  }

  // External IDs with labels
  const idMappings: [string, string][] = [
    ["imdbid", tr("searchHistoryPanel.params.ids.imdb")],
    ["tmdbid", tr("searchHistoryPanel.params.ids.tmdb")],
    ["tvdbid", tr("searchHistoryPanel.params.ids.tvdb")],
    ["tvmazeid", tr("searchHistoryPanel.params.ids.tvmaze")],
    ["rid", tr("searchHistoryPanel.params.ids.tvrage")],
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

export function SearchHistoryPanel() {
  const [isOpen, setIsOpen] = useState(true)
  const [selectedEntry, setSelectedEntry] = useState<SearchHistoryEntry | null>(null)
  const { t } = useTranslation()
  const tr: TranslateFn = (key, options) => String(t(key as never, options as never))
  const { data, isLoading } = useSearchHistory({
    limit: 50,
    enabled: true,
    refetchInterval: isOpen ? 3000 : false,
  })

  const entries = data?.entries ?? []
  const total = data?.total ?? 0

  const successCount = entries.filter(e => e.status === "success").length
  const errorCount = entries.filter(e => e.status === "error" || e.status === "rate_limited").length

  return (
    <>
      <Collapsible open={isOpen} onOpenChange={setIsOpen}>
        <div className="rounded-xl border bg-card text-card-foreground shadow-sm">
          <CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-4 hover:cursor-pointer text-left hover:bg-muted/50 transition-colors rounded-xl">
            <div className="flex items-center gap-2">
              <History className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm font-medium">{tr("searchHistoryPanel.panel.title")}</span>
              {isLoading ? (
                <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
              ) : total > 0 ? (
                <Badge variant="secondary" className="text-xs">
                  {tr("searchHistoryPanel.panel.summary.searches", { count: total })}
                  {errorCount > 0 && tr("searchHistoryPanel.panel.summary.errorsInline", { count: errorCount })}
                </Badge>
              ) : (
                <span className="text-xs text-muted-foreground">{tr("searchHistoryPanel.panel.summary.none")}</span>
              )}
            </div>
            <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${isOpen ? "rotate-180" : ""}`} />
          </CollapsibleTrigger>

          <CollapsibleContent>
            <div className="px-4 pb-3 space-y-3">
              {/* Summary stats */}
              {entries.length > 0 && (
                <div className="flex items-center gap-4 text-xs text-muted-foreground border-b pb-2">
                  <span className="flex items-center gap-1">
                    <CheckCircle2 className="h-3 w-3 text-primary" />
                    {tr("searchHistoryPanel.panel.summary.successful", { count: successCount })}
                  </span>
                  {errorCount > 0 && (
                    <span className="flex items-center gap-1">
                      <XCircle className="h-3 w-3 text-destructive" />
                      {tr("searchHistoryPanel.panel.summary.failed", { count: errorCount })}
                    </span>
                  )}
                  {data?.source && (
                    <span className="ml-auto">
                      {tr("searchHistoryPanel.panel.summary.source", { source: data.source })}
                    </span>
                  )}
                </div>
              )}

              {/* History entries */}
              {entries.length > 0 && (
                <div className="space-y-1 max-h-80 overflow-y-auto">
                  {entries.map((entry) => (
                    <HistoryRow
                      key={entry.id}
                      entry={entry}
                      onClick={() => setSelectedEntry(entry)}
                    />
                  ))}
                </div>
              )}
            </div>
          </CollapsibleContent>
        </div>
      </Collapsible>

      <SearchDetailDialog
        entry={selectedEntry}
        open={!!selectedEntry}
        onClose={() => setSelectedEntry(null)}
      />
    </>
  )
}

interface HistoryRowProps {
  entry: SearchHistoryEntry
  onClick: () => void
}

function HistoryRow({ entry, onClick }: HistoryRowProps) {
  const { t } = useTranslation()
  const tr: TranslateFn = (key, options) => String(t(key as never, options as never))

  const statusIcons: Record<string, ReactNode> = {
    success: <CheckCircle2 className="h-3 w-3 text-primary shrink-0" />,
    error: <XCircle className="h-3 w-3 text-destructive shrink-0" />,
    skipped: <Clock className="h-3 w-3 text-muted-foreground shrink-0" />,
    rate_limited: <AlertCircle className="h-3 w-3 text-destructive shrink-0" />,
  }
  const priorityLabelKeys: Record<string, string> = {
    interactive: "searchHistoryPanel.priority.interactive",
    rss: "searchHistoryPanel.priority.rss",
    completion: "searchHistoryPanel.priority.completion",
    background: "searchHistoryPanel.priority.background",
  }

  const durationStr = formatSearchDuration(entry.durationMs, 1)
  let priorityLabel = entry.priority
  const priorityLabelKey = priorityLabelKeys[entry.priority]
  if (priorityLabelKey) {
    priorityLabel = tr(priorityLabelKey)
  }

  // Hide "unknown" content type - it's noise for RSS searches
  const showContentType = entry.contentType && entry.contentType !== "unknown"

  return (
    <div
      className="flex flex-col gap-1.5 p-2 rounded bg-muted/30 text-sm hover:bg-muted/50 cursor-pointer transition-colors md:flex-row md:items-center md:justify-between md:gap-2"
      onClick={onClick}
    >
      <div className="flex items-center gap-2 min-w-0 flex-1">
        {statusIcons[entry.status] ?? statusIcons.error}
        <span className="truncate font-medium">{entry.indexerName}</span>
        {entry.query && (
          <span className="truncate text-muted-foreground text-xs">
            "{entry.query}"
          </span>
        )}
        {showContentType && (
          <Badge variant="outline" className="text-xs shrink-0">
            {entry.contentType}
          </Badge>
        )}
        {/* Cross-seed outcome badge - only show successful adds */}
        {entry.outcome === "added" && (
          <Badge className="text-xs shrink-0 gap-0.5 bg-primary/10 text-primary border-primary/30 hover:bg-primary/15">
            <Plus className="h-2.5 w-2.5" />
            {entry.addedCount || 1}
          </Badge>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2 shrink-0 pl-5 md:pl-0">
        {entry.status === "success" && (
          <span className={`text-xs ${entry.resultCount > 0 ? "text-primary" : "text-muted-foreground"}`}>
            {tr("searchHistoryPanel.row.results", { count: entry.resultCount })}
          </span>
        )}
        {entry.status === "error" && entry.errorMessage && (
          <span className="text-xs text-destructive truncate max-w-32" title={entry.errorMessage}>
            {entry.errorMessage}
          </span>
        )}
        <span className="text-xs text-muted-foreground">{priorityLabel}</span>
        <span className="text-xs text-muted-foreground">
          {durationStr}
        </span>
        <span className="text-xs text-muted-foreground ml-auto md:ml-0">
          {formatRelativeTime(new Date(entry.completedAt))}
        </span>
      </div>
    </div>
  )
}

interface SearchDetailDialogProps {
  entry: SearchHistoryEntry | null
  open: boolean
  onClose: () => void
}

function SearchDetailDialog({ entry, open, onClose }: SearchDetailDialogProps) {
  const { t } = useTranslation()
  const tr: TranslateFn = (key, options) => String(t(key as never, options as never))
  if (!entry) return null

  const statusLabelKeys: Record<string, string> = {
    success: "searchHistoryPanel.status.success",
    error: "searchHistoryPanel.status.error",
    skipped: "searchHistoryPanel.status.skipped",
    rate_limited: "searchHistoryPanel.status.rateLimited",
  }
  const priorityLabelKeys: Record<string, string> = {
    interactive: "searchHistoryPanel.priority.interactive",
    rss: "searchHistoryPanel.priority.rss",
    completion: "searchHistoryPanel.priority.completion",
    background: "searchHistoryPanel.priority.background",
  }

  const isSuccess = entry.status === "success"
  const isError = entry.status === "error" || entry.status === "rate_limited"

  const durationStr = formatSearchDuration(entry.durationMs, 2)
  let priorityLabel = entry.priority
  const priorityLabelKey = priorityLabelKeys[entry.priority]
  if (priorityLabelKey) {
    priorityLabel = tr(priorityLabelKey)
  }

  // Transform params into semantic badges
  const paramBadges = entry.params ? transformParams(entry.params, tr) : []

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="!max-w-2xl gap-0 p-0 overflow-hidden">
        {/* Header */}
        <div className="px-5 pt-5">
          <div className="flex items-start justify-between gap-3 pr-8">
            <div className="space-y-1 min-w-0">
              <DialogTitle className="text-base font-semibold truncate">
                {entry.indexerName}
              </DialogTitle>
            </div>
            <Badge
              variant={isSuccess ? "default" : isError ? "destructive" : "secondary"}
              className="shrink-0"
            >
              {statusLabelKeys[entry.status] ? tr(statusLabelKeys[entry.status]) : entry.status}
            </Badge>
          </div>
        </div>

        <div className="px-5 py-4 space-y-4">
          {/* Release Name - Hero Section */}
          {entry.releaseName && (
            <div className="rounded-lg border border-border bg-muted/40 p-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-1.5">
                {tr("searchHistoryPanel.dialog.sections.release")}
              </div>
              <div className="font-mono text-[13px] leading-relaxed break-all">
                {entry.releaseName}
              </div>
            </div>
          )}

          {/* Stats Row */}
          <div className="flex items-center gap-6 text-sm">
            <div>
              <span className="text-muted-foreground">{tr("searchHistoryPanel.dialog.stats.results")} </span>
              <span className={entry.resultCount > 0 ? "font-semibold text-primary" : "text-muted-foreground"}>
                {entry.resultCount}
              </span>
            </div>
            <div>
              <span className="text-muted-foreground">{tr("searchHistoryPanel.dialog.stats.duration")} </span>
              <span className="font-medium">{durationStr}</span>
            </div>
            <div>
              <span className="text-muted-foreground">{tr("searchHistoryPanel.dialog.stats.priority")} </span>
              <span className="font-medium">{priorityLabel}</span>
            </div>
            {/* Cross-seed outcome - only show successful adds */}
            {entry.outcome === "added" && (
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">{tr("searchHistoryPanel.dialog.stats.crossSeed")} </span>
                <Badge className="bg-primary/10 text-primary border-primary/30">
                  {tr("searchHistoryPanel.dialog.badges.added", { count: entry.addedCount || 1 })}
                </Badge>
              </div>
            )}
          </div>

          {/* Error Message */}
          {entry.errorMessage && (
            <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3">
              <div className="text-[11px] uppercase tracking-wide text-destructive/80 mb-1">
                {tr("searchHistoryPanel.dialog.sections.error")}
              </div>
              <div className="text-sm text-destructive break-all">
                {entry.errorMessage}
              </div>
            </div>
          )}

          {/* Request Parameters */}
          {paramBadges.length > 0 && (
            <div className="rounded-lg border border-dashed border-border bg-muted/20 p-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-2">
                {tr("searchHistoryPanel.dialog.sections.searchParameters")}
              </div>
              <div className="flex flex-wrap gap-1.5">
                {paramBadges.map((badge, i) => (
                  <Badge key={i} variant="outline" className="text-xs font-normal">
                    {badge.value ? `${badge.label}: ${badge.value}` : badge.label}
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Footer - Technical Details */}
        <div className="px-5 py-3 bg-muted/30 border-t border-border/50">
          <div className="flex items-center justify-between text-[11px] text-muted-foreground">
            <div className="flex items-center gap-4">
              {entry.searchMode && (
                <span>
                  {tr("searchHistoryPanel.dialog.footer.mode")}: <span className="text-foreground/70">{entry.searchMode}</span>
                </span>
              )}
              {entry.contentType && entry.contentType !== "unknown" && (
                <span>
                  {tr("searchHistoryPanel.dialog.footer.type")}: <span className="text-foreground/70">{entry.contentType}</span>
                </span>
              )}
            </div>
            <div className="font-mono text-[10px] text-muted-foreground/60">
              {formatTimeHMS(new Date(entry.completedAt))} · {tr("searchHistoryPanel.dialog.footer.job", { id: entry.jobId })}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
