/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { AlertTriangle, ChevronDown, ChevronRight, GitBranch, Info, Loader2 } from "lucide-react"

import { cn } from "@/lib/utils"
import type { CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"

interface CrossSeedWarningProps {
  affectedTorrents: CrossSeedTorrent[]
  isLoading: boolean
  hasWarning: boolean
  deleteFiles: boolean
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
  isLoading,
  hasWarning,
  deleteFiles,
  className,
}: CrossSeedWarningProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [incognitoMode] = useIncognitoMode()

  // Loading state - subtle inline indicator
  if (isLoading) {
    return (
      <div className={cn("flex items-center gap-2 py-2 text-xs text-muted-foreground", className)}>
        <Loader2 className="h-3 w-3 animate-spin" />
        <span>Checking for cross-seeded torrents...</span>
      </div>
    )
  }

  // No cross-seeds found
  if (!hasWarning) {
    return null
  }

  // Group by instance
  const byInstance = affectedTorrents.reduce<Record<string, CrossSeedTorrent[]>>(
    (acc, torrent) => {
      const key = torrent.instanceName || `Instance ${torrent.instanceId}`
      if (!acc[key]) {
        acc[key] = []
      }
      acc[key].push(torrent)
      return acc
    },
    {}
  )

  const instanceCount = Object.keys(byInstance).length
  const isDestructive = deleteFiles

  // Collect unique trackers for summary
  const uniqueTrackers = new Set<string>()
  affectedTorrents.forEach((t) => {
    const domain = getTrackerDomain(t.tracker)
    if (domain) uniqueTrackers.add(domain)
  })

  return (
    <div
      className={cn(
        "rounded-lg border py-3 px-4 overflow-hidden",
        isDestructive
          ? "border-destructive/40 bg-destructive/5"
          : "border-blue-500/30 bg-blue-500/5",
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
            {isDestructive
              ? "This will break cross-seeded torrents"
              : "Cross-seeded torrents detected (safe to remove)"}
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {affectedTorrents.length} {affectedTorrents.length === 1 ? "torrent shares" : "torrents share"} {isDestructive ? "" : "these "}files
            {instanceCount > 1 && ` across ${instanceCount} instances`}
            {uniqueTrackers.size > 0 && (
              <span className="ml-1">
                on {uniqueTrackers.size === 1
                  ? Array.from(uniqueTrackers)[0]
                  : `${uniqueTrackers.size} trackers`}
              </span>
            )}
            {!isDestructive && " — data will be preserved"}
          </p>
        </div>
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
          <span>{isExpanded ? "Hide" : "Show"} affected torrents</span>
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
                          {incognitoMode
                            ? getLinuxIsoName(torrent.hash)
                            : torrent.name}
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
                    <p className="text-[10px] text-muted-foreground pt-0.5">
                      + {torrents.length - 8} more
                    </p>
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
