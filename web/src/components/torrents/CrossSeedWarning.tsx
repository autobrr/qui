/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, GitBranch, Info, Loader2, Search, XCircle } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { CrossSeedSearchState } from "@/hooks/useCrossSeedWarning"
import type { CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"
import { cn } from "@/lib/utils"

interface CrossSeedWarningProps {
  affectedTorrents: CrossSeedTorrent[]
  searchState: CrossSeedSearchState
  hasWarning: boolean
  deleteFiles: boolean
  deleteCrossSeeds: boolean
  onDeleteCrossSeedsChange: (checked: boolean) => void
  onSearch: () => void
  totalToCheck: number
  checkedCount: number
  className?: string
}

/** Extract tracker domain from URL, e.g. "https://tracker.example.com:443/announce" -> "tracker.example.com" */
function getTrackerDomain(trackerUrl: string | undefined): string | null {
  if (!trackerUrl) return null
  try {
    const url = new URL(trackerUrl)
    return url.hostname
  } catch {
    // If not a valid URL, try to extract domain-like pattern
    const match = trackerUrl.match(/(?:https?:\/\/)?([^:/]+)/)
    return match?.[1] || null
  }
}

export function CrossSeedWarning({
  affectedTorrents,
  searchState,
  hasWarning,
  deleteFiles,
  deleteCrossSeeds,
  onDeleteCrossSeedsChange,
  onSearch,
  totalToCheck,
  checkedCount,
  className,
}: CrossSeedWarningProps) {
  const { t } = useTranslation()
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const [isExpanded, setIsExpanded] = useState(false)
  const [incognitoMode] = useIncognitoMode()

  // Idle state - show search prompt with contextual urgency
  if (searchState === "idle") {
    // More prominent when deleting files (higher risk of breaking cross-seeds)
    const isHighRisk = deleteFiles

    return (
      <div
        className={cn(
          "group relative rounded-lg border py-2 px-3 transition-all duration-300",
          isHighRisk? "border-amber-500/40 bg-amber-500/5 hover:border-amber-500/60 hover:bg-amber-500/10": "border-border/50 bg-muted/30 hover:border-border hover:bg-muted/50",
          className
        )}
      >
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            {isHighRisk ? (
              <AlertTriangle className="h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
            ) : (
              <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
            )}
            <p className={cn(
              "text-xs font-medium truncate",
              isHighRisk ? "text-amber-700 dark:text-amber-300" : "text-foreground"
            )}>
              {isHighRisk? tr("crossSeedWarning.idle.highRiskTitle"): tr("crossSeedWarning.idle.defaultTitle")}
            </p>
          </div>

          <Button
            type="button"
            variant={isHighRisk ? "default" : "outline"}
            size="sm"
            onClick={onSearch}
            className={cn(
              "shrink-0 h-7 px-2 text-xs transition-all",
              isHighRisk? "bg-amber-600 hover:bg-amber-700 text-white shadow-sm": "hover:bg-accent"
            )}
          >
            <Search className="h-3 w-3 mr-1" />
            {tr("crossSeedWarning.idle.checkAction")}
          </Button>
        </div>
      </div>
    )
  }

  // Searching state - show progress
  if (searchState === "searching") {
    return (
      <div className={cn("flex items-center gap-2 py-2 text-xs text-muted-foreground", className)}>
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span>
          {tr("crossSeedWarning.searching.title")}
          {totalToCheck > 1 && ` (${checkedCount}/${totalToCheck})`}
          ...
        </span>
      </div>
    )
  }

  // Error state
  if (searchState === "error") {
    return (
      <div className={cn("flex items-center gap-2 py-2 text-xs text-muted-foreground", className)}>
        <XCircle className="h-3.5 w-3.5 text-destructive" />
        <span>{tr("crossSeedWarning.error.title")}</span>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onSearch}
          className="h-6 px-2 text-xs"
        >
          {tr("crossSeedWarning.error.retryAction")}
        </Button>
      </div>
    )
  }

  // Complete state - no cross-seeds found
  if (searchState === "complete" && !hasWarning) {
    return (
      <div className={cn("flex items-center gap-2 py-2 text-xs text-muted-foreground", className)}>
        <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />
        <span>{tr("crossSeedWarning.complete.noneFound")}</span>
      </div>
    )
  }

  // Complete state - cross-seeds found, show warning
  // Group by instance
  const byInstance = affectedTorrents.reduce<Record<string, CrossSeedTorrent[]>>(
    (acc, torrent) => {
      const key = torrent.instanceName || tr("crossSeedWarning.instances.fallback", { id: torrent.instanceId })
      if (!acc[key]) {
        acc[key] = []
      }
      acc[key].push(torrent)
      return acc
    },
    {}
  )

  const instanceCount = Object.keys(byInstance).length
  // Show destructive styling if deleting files OR if user opted to delete cross-seeds
  const isDestructive = deleteFiles || deleteCrossSeeds

  // Collect unique trackers for summary
  const uniqueTrackers = new Set<string>()
  affectedTorrents.forEach((t) => {
    const domain = getTrackerDomain(t.tracker)
    if (domain) uniqueTrackers.add(domain)
  })

  const shareSummary = affectedTorrents.length === 1? tr("crossSeedWarning.summary.shareSingle", { count: affectedTorrents.length }): tr("crossSeedWarning.summary.sharePlural", { count: affectedTorrents.length })
  const trackerSummary = uniqueTrackers.size === 1? tr("crossSeedWarning.summary.onSingleTracker", { tracker: Array.from(uniqueTrackers)[0] }): tr("crossSeedWarning.summary.onMultipleTrackers", { count: uniqueTrackers.size })

  return (
    <div
      className={cn(
        "rounded-lg border py-3 px-4 overflow-hidden",
        isDestructive? "border-destructive/40 bg-destructive/5": "border-blue-500/30 bg-blue-500/5",
        className
      )}
    >
      {/* Header */}
      <div className="flex items-start gap-3">
        {isDestructive ? (
          <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0 text-destructive" />
        ) : (
          <Info className="mt-0.5 h-4 w-4 flex-shrink-0 text-blue-500" />
        )}
        <div className="flex-1 min-w-0">
          <p className={cn(
            "text-sm font-medium",
            isDestructive ? "text-destructive" : "text-blue-600 dark:text-blue-400"
          )}>
            {deleteCrossSeeds? tr("crossSeedWarning.warning.deleteCrossSeeds"): deleteFiles? tr("crossSeedWarning.warning.deleteFiles"): tr("crossSeedWarning.warning.preserveData")}
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {shareSummary}
            {instanceCount > 1 && ` ${tr("crossSeedWarning.summary.acrossInstances", { count: instanceCount })}`}
            {uniqueTrackers.size > 0 && (
              <span className="ml-1">
                {trackerSummary}
              </span>
            )}
            {deleteCrossSeeds? tr("crossSeedWarning.summary.tail.deleteCrossSeeds"): deleteFiles? tr("crossSeedWarning.summary.tail.deleteFiles"): tr("crossSeedWarning.summary.tail.preserve")}
          </p>
        </div>
      </div>

      {/* Delete cross-seeds option */}
      <div className="mt-3 flex items-center gap-2">
        <Checkbox
          id="deleteCrossSeeds"
          checked={deleteCrossSeeds}
          onCheckedChange={(checked) => onDeleteCrossSeedsChange(checked === true)}
        />
        <label
          htmlFor="deleteCrossSeeds"
          className="text-xs cursor-pointer select-none"
        >
          {tr("crossSeedWarning.options.deleteCrossSeedsLabel")}
        </label>
      </div>

      {/* Expandable torrent list */}
      <div className="mt-3">
        <button
          type="button"
          onClick={() => setIsExpanded(!isExpanded)}
          className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          {isExpanded ? (
            <ChevronDown className="h-3 w-3" />
          ) : (
            <ChevronRight className="h-3 w-3" />
          )}
          <GitBranch className="h-3 w-3" />
          <span>
            {isExpanded? tr("crossSeedWarning.list.hide"): tr("crossSeedWarning.list.show")} {tr("crossSeedWarning.list.affectedTorrents")}
          </span>
        </button>

        {isExpanded && (
          <div className="mt-2 space-y-2 overflow-hidden">
            {Object.entries(byInstance).map(([instanceName, torrents]) => (
              <div key={instanceName} className="min-w-0">
                {instanceCount > 1 && (
                  <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wide mb-1">
                    {instanceName}
                  </p>
                )}
                <div className="space-y-1 min-w-0">
                  {torrents.slice(0, 8).map((torrent) => {
                    const trackerDomain = getTrackerDomain(torrent.tracker)
                    return (
                      <div
                        key={`${torrent.hash}-${torrent.instanceId}`}
                        className="flex items-center gap-2 py-0.5 text-xs min-w-0"
                      >
                        <span className="truncate min-w-0 flex-1">
                          {incognitoMode? getLinuxIsoName(torrent.hash): torrent.name}
                        </span>
                        {trackerDomain && (
                          <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                            {trackerDomain}
                          </span>
                        )}
                      </div>
                    )
                  })}
                  {torrents.length > 8 && (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <p className="text-[10px] text-muted-foreground pt-0.5 cursor-help hover:text-foreground transition-colors w-fit">
                          + {torrents.length - 8} {tr("crossSeedWarning.list.more")}
                        </p>
                      </TooltipTrigger>
                      <TooltipContent side="bottom" align="start" className="max-w-sm">
                        <div className="space-y-0.5">
                          {torrents.slice(8).map((t) => (
                            <p key={`${t.hash}-${t.instanceId}`} className="truncate text-xs">
                              {incognitoMode ? getLinuxIsoName(t.hash) : t.name}
                            </p>
                          ))}
                        </div>
                      </TooltipContent>
                    </Tooltip>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
