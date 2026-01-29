/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog"
import { useSearchHistory } from "@/hooks/useSearchHistory"
import { formatRelativeTime, formatTimeHMS } from "@/lib/dateTimeUtils"
import type { SearchHistoryEntry } from "@/types"
import { AlertCircle, CheckCircle2, ChevronDown, Clock, History, Loader2, Plus, XCircle } from "lucide-react"
import { type ReactNode, useState } from "react"

// Torznab standard category mappings (synced with pkg/gojackett/constants.go)
const CATEGORY_MAP: Record<string, string> = {
  // Parent categories
  "1000": "Console",
  "2000": "Movies",
  "3000": "Audio",
  "4000": "PC",
  "5000": "TV",
  "6000": "XXX",
  "7000": "Other",
  "8000": "Books",
  // Movies subcategories
  "2010": "Movies Foreign",
  "2020": "Movies Other",
  "2030": "Movies SD",
  "2040": "Movies HD",
  "2045": "Movies UHD",
  "2050": "Movies BluRay",
  "2060": "Movies 3D",
  "2070": "Movies Web",
  // Audio subcategories
  "3010": "Audio MP3",
  "3020": "Audio Video",
  "3030": "Audiobook",
  "3040": "Audio Lossless",
  "3050": "Audio Other",
  "3060": "Audio Foreign",
  // PC subcategories
  "4010": "PC 0day",
  "4020": "PC ISO",
  "4030": "PC Mac",
  "4040": "PC Phone Other",
  "4050": "PC Games",
  "4060": "PC Phone iOS",
  "4070": "PC Phone Android",
  // TV subcategories
  "5010": "TV Web",
  "5020": "TV Foreign",
  "5030": "TV SD",
  "5040": "TV HD",
  "5045": "TV UHD",
  "5060": "TV Sport",
  "5070": "TV Anime",
  "5080": "TV Documentary",
  "5090": "TV Other",
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
  // Other subcategories
  "7010": "Other Misc",
  "7020": "Other Hashed",
  // Books subcategories
  "8010": "Books Mags",
  "8020": "Books EBook",
  "8030": "Books Comics",
  "8040": "Books Technical",
  "8050": "Books Foreign",
  "8060": "Books Other",
}

interface ParamBadge {
  label: string
  value?: string
}

// Transform raw torznab params into semantic badges
function transformParams(params: Record<string, string>): ParamBadge[] {
  const badges: ParamBadge[] = []
  const consumed = new Set<string>()

  // Season and episode as separate badges
  if (params.season) {
    badges.push({ label: "Season", value: params.season })
    consumed.add("season")
  }
  if (params.ep) {
    badges.push({ label: "Episode", value: params.ep })
    consumed.add("ep")
  }

  // Map category ID to name with number in parentheses
  if (params.cat) {
    // Handle comma-separated categories, use first one for display
    const firstCat = params.cat.split(",")[0]
    // Try exact match first, then fall back to parent category (e.g., 5030 -> 5000 -> TV)
    let catName = CATEGORY_MAP[firstCat]
    if (!catName && firstCat.length >= 4) {
      const parentCat = `${firstCat.slice(0, 1)}000`
      catName = CATEGORY_MAP[parentCat]
    }
    const catLabel = catName ? `${catName} (${firstCat})` : firstCat
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

export function SearchHistoryPanel() {
  const [isOpen, setIsOpen] = useState(true)
  const [selectedEntry, setSelectedEntry] = useState<SearchHistoryEntry | null>(null)
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
              <span className="text-sm font-medium">Search History</span>
              {isLoading ? (
                <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
              ) : total > 0 ? (
                <Badge variant="secondary" className="text-xs">
                  {total} searches
                  {errorCount > 0 && `, ${errorCount} errors`}
                </Badge>
              ) : (
                <span className="text-xs text-muted-foreground">No searches yet</span>
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
                    {successCount} successful
                  </span>
                  {errorCount > 0 && (
                    <span className="flex items-center gap-1">
                      <XCircle className="h-3 w-3 text-destructive" />
                      {errorCount} failed
                    </span>
                  )}
                  {data?.source && (
                    <span className="ml-auto">
                      Source: {data.source}
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
  const statusIcons: Record<string, ReactNode> = {
    success: <CheckCircle2 className="h-3 w-3 text-primary shrink-0" />,
    error: <XCircle className="h-3 w-3 text-destructive shrink-0" />,
    skipped: <Clock className="h-3 w-3 text-muted-foreground shrink-0" />,
    rate_limited: <AlertCircle className="h-3 w-3 text-destructive shrink-0" />,
  }

  const durationStr = entry.durationMs < 1000
    ? `${entry.durationMs}ms`
    : `${(entry.durationMs / 1000).toFixed(1)}s`

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
            {entry.resultCount} results
          </span>
        )}
        {entry.status === "error" && entry.errorMessage && (
          <span className="text-xs text-destructive truncate max-w-32" title={entry.errorMessage}>
            {entry.errorMessage}
          </span>
        )}
        <span className="text-xs text-muted-foreground">
          {entry.priority}
        </span>
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
  if (!entry) return null

  const statusLabels: Record<string, string> = {
    success: "Success",
    error: "Failed",
    skipped: "Skipped",
    rate_limited: "Rate Limited",
  }

  const isSuccess = entry.status === "success"
  const isError = entry.status === "error" || entry.status === "rate_limited"

  const durationStr = entry.durationMs < 1000
    ? `${entry.durationMs}ms`
    : `${(entry.durationMs / 1000).toFixed(2)}s`

  // Transform params into semantic badges
  const paramBadges = entry.params ? transformParams(entry.params) : []

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
              {statusLabels[entry.status] ?? entry.status}
            </Badge>
          </div>
        </div>

        <div className="px-5 py-4 space-y-4">
          {/* Release Name - Hero Section */}
          {entry.releaseName && (
            <div className="rounded-lg border border-border bg-muted/40 p-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-1.5">
                Release
              </div>
              <div className="font-mono text-[13px] leading-relaxed break-all">
                {entry.releaseName}
              </div>
            </div>
          )}

          {/* Stats Row */}
          <div className="flex items-center gap-6 text-sm">
            <div>
              <span className="text-muted-foreground">Results </span>
              <span className={entry.resultCount > 0 ? "font-semibold text-primary" : "text-muted-foreground"}>
                {entry.resultCount}
              </span>
            </div>
            <div>
              <span className="text-muted-foreground">Duration </span>
              <span className="font-medium">{durationStr}</span>
            </div>
            <div>
              <span className="text-muted-foreground">Priority </span>
              <span className="font-medium">{entry.priority}</span>
            </div>
            {/* Cross-seed outcome - only show successful adds */}
            {entry.outcome === "added" && (
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">Cross-seed </span>
                <Badge className="bg-primary/10 text-primary border-primary/30">
                  Added {entry.addedCount || 1}
                </Badge>
              </div>
            )}
          </div>

          {/* Error Message */}
          {entry.errorMessage && (
            <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3">
              <div className="text-[11px] uppercase tracking-wide text-destructive/80 mb-1">
                Error
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
                Search Parameters
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
                  Mode: <span className="text-foreground/70">{entry.searchMode}</span>
                </span>
              )}
              {entry.contentType && entry.contentType !== "unknown" && (
                <span>
                  Type: <span className="text-foreground/70">{entry.contentType}</span>
                </span>
              )}
            </div>
            <div className="font-mono text-[10px] text-muted-foreground/60">
              {formatTimeHMS(new Date(entry.completedAt))} · Job {entry.jobId}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
