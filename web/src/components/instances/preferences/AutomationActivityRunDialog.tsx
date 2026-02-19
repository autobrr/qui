/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { pickTrackerIconDomain } from "@/lib/tracker-icons"
import { copyTextToClipboard, formatBytes } from "@/lib/utils"
import type { AutomationActivity, AutomationActivityRunItem } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Copy, Loader2 } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

const PAGE_SIZE = 200

function isNotFoundError(error: unknown): boolean {
  if (!(error instanceof Error)) return false
  const message = error.message.toLowerCase()
  return ["not available", "status: 404", "http error! status: 404"].some((fragment) =>
    message.includes(fragment)
  )
}

const actionLabels: Record<AutomationActivity["action"], string> = {
  deleted_ratio: "Deleted (ratio)",
  deleted_seeding: "Deleted (seeding)",
  deleted_unregistered: "Deleted (unregistered)",
  deleted_condition: "Deleted (condition)",
  delete_failed: "Delete failed",
  limit_failed: "Limit failed",
  tags_changed: "Tags changed",
  category_changed: "Category changed",
  speed_limits_changed: "Speed limits changed",
  share_limits_changed: "Share limits changed",
  paused: "Paused",
  resumed: "Resumed",
  moved: "Moved",
  external_program: "External program",
  dry_run_no_match: "Dry-run (no match)",
}

interface AutomationActivityRunDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  activity: AutomationActivity
}

export function AutomationActivityRunDialog({
  open,
  onOpenChange,
  instanceId,
  activity,
}: AutomationActivityRunDialogProps) {
  const [offset, setOffset] = useState(0)
  const [items, setItems] = useState<AutomationActivityRunItem[]>([])
  const [total, setTotal] = useState(0)
  const { formatAddedOn, formatISOTimestamp } = useDateTimeFormatters()
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()

  const domainToCustomization = useMemo(() => {
    const map = new Map<string, { displayName: string; domains: string[] }>()
    for (const custom of trackerCustomizations ?? []) {
      for (const domain of custom.domains) {
        map.set(domain.toLowerCase(), {
          displayName: custom.displayName,
          domains: custom.domains,
        })
      }
    }
    return map
  }, [trackerCustomizations])

  const getTrackerDisplay = useCallback((domain: string): { displayName: string; iconDomain: string; isCustomized: boolean } => {
    const customization = domainToCustomization.get(domain.toLowerCase())
    if (customization) {
      return {
        displayName: customization.displayName,
        iconDomain: pickTrackerIconDomain(trackerIcons, customization.domains, domain),
        isCustomized: true,
      }
    }
    return {
      displayName: domain,
      iconDomain: domain,
      isCustomized: false,
    }
  }, [domainToCustomization, trackerIcons])

  const runQuery = useQuery({
    queryKey: ["automation-activity-run", instanceId, activity.id, offset],
    queryFn: () => api.getAutomationActivityRun(instanceId, activity.id, {
      limit: PAGE_SIZE,
      offset,
    }),
    enabled: open,
    retry: (failureCount, error) => !isNotFoundError(error) && failureCount < 2,
  })

  useEffect(() => {
    setOffset(0)
    setItems([])
    setTotal(0)
  }, [open, activity.id])

  useEffect(() => {
    const page = runQuery.data?.items
    if (!page) return

    setTotal(runQuery.data?.total ?? 0)
    setItems((prev) => {
      const seen = new Set(prev.map((item) => item.hash))
      const next = [...prev]
      for (const item of page) {
        if (!seen.has(item.hash)) {
          next.push(item)
        }
      }
      return next
    })
  }, [runQuery.data])

  const notAvailable = runQuery.isError && isNotFoundError(runQuery.error)
  const hasMore = items.length < total && !notAvailable
  const title = actionLabels[activity.action] ?? "Automation run"
  const displayTitle = activity.outcome === "dry-run" ? `${title} (dry run)` : title

  const handleLoadMore = () => {
    setOffset((prev) => prev + PAGE_SIZE)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-5xl max-h-[85dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{displayTitle} run</DialogTitle>
          <DialogDescription>
            {formatISOTimestamp(activity.createdAt)} - {total} torrent{total === 1 ? "" : "s"} - stored temporarily in memory
            {activity.outcome === "dry-run" && (
              <span className="block text-xs text-muted-foreground mt-1">Dry run: no changes were applied.</span>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="border rounded-md overflow-hidden flex-1 min-h-0 flex flex-col">
          <div className="overflow-auto">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-muted">
                <tr className="border-b">
                  <th className="text-left p-2 font-medium">Name</th>
                  <th className="text-left p-2 font-medium">Hash</th>
                  <th className="text-left p-2 font-medium">Tracker</th>
                  <th className="text-left p-2 font-medium">Tags added</th>
                  <th className="text-left p-2 font-medium">Tags removed</th>
                  <th className="text-right p-2 font-medium">Size</th>
                  <th className="text-right p-2 font-medium">Ratio</th>
                  <th className="text-right p-2 font-medium">Added</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.hash} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="p-2 max-w-[320px]">
                      <TruncatedText className="block max-w-[320px]">
                        {item.name || item.hash}
                      </TruncatedText>
                    </td>
                    <td className="p-2">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-xs">{item.hash?.substring(0, 7)}</span>
                        <button
                          type="button"
                          className="text-muted-foreground hover:text-foreground transition-colors"
                          onClick={() => {
                            copyTextToClipboard(item.hash)
                            toast.success("Hash copied")
                          }}
                          title="Copy hash"
                        >
                          <Copy className="h-3 w-3" />
                        </button>
                      </div>
                    </td>
                    <td className="p-2 text-xs text-muted-foreground">
                      {item.trackerDomain ? (() => {
                        const tracker = getTrackerDisplay(item.trackerDomain)
                        return (
                          <div className="flex items-center gap-1">
                            <TrackerIconImage tracker={tracker.iconDomain} trackerIcons={trackerIcons} />
                            {tracker.isCustomized ? (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="text-xs font-medium cursor-default">{tracker.displayName}</span>
                                </TooltipTrigger>
                                <TooltipContent>
                                  <p className="text-xs">Original: {item.trackerDomain}</p>
                                </TooltipContent>
                              </Tooltip>
                            ) : (
                              <span className="text-xs font-medium">{tracker.displayName}</span>
                            )}
                          </div>
                        )
                      })() : "-"}
                    </td>
                    <td className="p-2">
                      {item.tagsAdded && item.tagsAdded.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {item.tagsAdded.map((tag) => (
                            <Badge key={`add-${item.hash}-${tag}`} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-emerald-500/10 text-emerald-500 border-emerald-500/20">
                              +{tag}
                            </Badge>
                          ))}
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">-</span>
                      )}
                    </td>
                    <td className="p-2">
                      {item.tagsRemoved && item.tagsRemoved.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {item.tagsRemoved.map((tag) => (
                            <Badge key={`rm-${item.hash}-${tag}`} variant="outline" className="text-[10px] px-1.5 py-0 h-5 bg-red-500/10 text-red-500 border-red-500/20">
                              -{tag}
                            </Badge>
                          ))}
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">-</span>
                      )}
                    </td>
                    <td className="p-2 text-right text-xs text-muted-foreground whitespace-nowrap">
                      {typeof item.size === "number" ? formatBytes(item.size) : "-"}
                    </td>
                    <td className="p-2 text-right text-xs text-muted-foreground whitespace-nowrap">
                      {typeof item.ratio === "number" ? item.ratio.toFixed(2) : "-"}
                    </td>
                    <td className="p-2 text-right text-xs text-muted-foreground whitespace-nowrap">
                      {typeof item.addedOn === "number" && item.addedOn > 0 ? formatAddedOn(item.addedOn) : "-"}
                    </td>
                  </tr>
                ))}

                {runQuery.isLoading && items.length === 0 && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      <Loader2 className="h-4 w-4 animate-spin inline-block mr-2" />
                      Loading...
                    </td>
                  </tr>
                )}

                {notAvailable && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      Run details not available (in-memory only).
                    </td>
                  </tr>
                )}

                {!runQuery.isLoading && !notAvailable && runQuery.isError && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      Failed to load run details.
                    </td>
                  </tr>
                )}

                {!runQuery.isLoading && !notAvailable && !runQuery.isError && items.length === 0 && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      No torrents recorded for this run.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {hasMore && (
            <div className="flex items-center justify-between gap-3 p-2 text-xs text-muted-foreground border-t bg-muted/30">
              <span>Showing {items.length} of {total}</span>
              <Button
                size="sm"
                variant="secondary"
                onClick={handleLoadMore}
                disabled={runQuery.isFetching}
              >
                {runQuery.isFetching && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                Load more
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
