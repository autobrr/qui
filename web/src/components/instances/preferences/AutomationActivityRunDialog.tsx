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
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const PAGE_SIZE = 200

function isNotFoundError(error: unknown): boolean {
  if (!(error instanceof Error)) return false
  const message = error.message.toLowerCase()
  return ["not available", "status: 404", "http error! status: 404"].some((fragment) =>
    message.includes(fragment)
  )
}

const actionLabelKeys: Record<AutomationActivity["action"], string> = {
  deleted_ratio: "workflowDialog.dryRun.actions.deletedRatio",
  deleted_seeding: "workflowDialog.dryRun.actions.deletedSeeding",
  deleted_unregistered: "workflowDialog.dryRun.actions.deletedUnregistered",
  deleted_condition: "workflowDialog.dryRun.actions.deletedCondition",
  delete_failed: "workflowDialog.dryRun.actions.deleteFailed",
  limit_failed: "workflowDialog.dryRun.actions.limitFailed",
  tags_changed: "workflowDialog.dryRun.actions.tagsChanged",
  category_changed: "workflowDialog.dryRun.actions.categoryChanged",
  speed_limits_changed: "workflowDialog.dryRun.actions.speedLimitsChanged",
  share_limits_changed: "workflowDialog.dryRun.actions.shareLimitsChanged",
  paused: "workflowDialog.dryRun.actions.paused",
  resumed: "workflowDialog.dryRun.actions.resumed",
  rechecked: "workflowDialog.dryRun.actions.rechecked",
  reannounced: "workflowDialog.dryRun.actions.reannounced",
  moved: "workflowDialog.dryRun.actions.moved",
  external_program: "workflowDialog.dryRun.actions.externalProgram",
  dry_run_no_match: "workflowDialog.dryRun.actions.noMatches",
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
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
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
  const actionTitle = tr(actionLabelKeys[activity.action] ?? "workflowDialog.activityRun.automationRun")
  const displayTitle = activity.outcome === "dry-run"
    ? tr("workflowDialog.activityRun.titleDryRun", { action: actionTitle })
    : tr("workflowDialog.activityRun.title", { action: actionTitle })

  const handleLoadMore = () => {
    setOffset((prev) => prev + PAGE_SIZE)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-5xl max-h-[85dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{displayTitle}</DialogTitle>
          <DialogDescription>
            {tr("workflowDialog.activityRun.description", {
              timestamp: formatISOTimestamp(activity.createdAt),
              total,
            })}
            {activity.outcome === "dry-run" && (
              <span className="block text-xs text-muted-foreground mt-1">{tr("workflowDialog.activityRun.dryRunNote")}</span>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="border rounded-md overflow-hidden flex-1 min-h-0 flex flex-col">
          <div className="overflow-auto">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-muted">
                <tr className="border-b">
                  <th className="text-left p-2 font-medium">{tr("workflowDialog.activityRun.table.name")}</th>
                  <th className="text-left p-2 font-medium">{tr("workflowDialog.activityRun.table.hash")}</th>
                  <th className="text-left p-2 font-medium">{tr("workflowDialog.activityRun.table.tracker")}</th>
                  <th className="text-left p-2 font-medium">{tr("workflowDialog.activityRun.table.tagsAdded")}</th>
                  <th className="text-left p-2 font-medium">{tr("workflowDialog.activityRun.table.tagsRemoved")}</th>
                  <th className="text-right p-2 font-medium">{tr("workflowDialog.activityRun.table.size")}</th>
                  <th className="text-right p-2 font-medium">{tr("workflowDialog.activityRun.table.ratio")}</th>
                  <th className="text-right p-2 font-medium">{tr("workflowDialog.activityRun.table.added")}</th>
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
                            toast.success(tr("workflowDialog.activityRun.toasts.hashCopied"))
                          }}
                          title={tr("workflowDialog.activityRun.copyHash")}
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
                                  <p className="text-xs">{tr("workflowDialog.activityRun.originalTracker", { tracker: item.trackerDomain })}</p>
                                </TooltipContent>
                              </Tooltip>
                            ) : (
                              <span className="text-xs font-medium">{tracker.displayName}</span>
                            )}
                          </div>
                        )
                      })() : tr("workflowDialog.activityRun.values.none")}
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
                        <span className="text-xs text-muted-foreground">{tr("workflowDialog.activityRun.values.none")}</span>
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
                        <span className="text-xs text-muted-foreground">{tr("workflowDialog.activityRun.values.none")}</span>
                      )}
                    </td>
                    <td className="p-2 text-right text-xs text-muted-foreground whitespace-nowrap">
                      {typeof item.size === "number" ? formatBytes(item.size) : tr("workflowDialog.activityRun.values.none")}
                    </td>
                    <td className="p-2 text-right text-xs text-muted-foreground whitespace-nowrap">
                      {typeof item.ratio === "number" ? item.ratio.toFixed(2) : tr("workflowDialog.activityRun.values.none")}
                    </td>
                    <td className="p-2 text-right text-xs text-muted-foreground whitespace-nowrap">
                      {typeof item.addedOn === "number" && item.addedOn > 0 ? formatAddedOn(item.addedOn) : tr("workflowDialog.activityRun.values.none")}
                    </td>
                  </tr>
                ))}

                {runQuery.isLoading && items.length === 0 && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      <Loader2 className="h-4 w-4 animate-spin inline-block mr-2" />
                      {tr("workflowDialog.activityRun.loading")}
                    </td>
                  </tr>
                )}

                {notAvailable && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      {tr("workflowDialog.activityRun.notAvailable")}
                    </td>
                  </tr>
                )}

                {!runQuery.isLoading && !notAvailable && runQuery.isError && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      {tr("workflowDialog.activityRun.failedLoad")}
                    </td>
                  </tr>
                )}

                {!runQuery.isLoading && !notAvailable && !runQuery.isError && items.length === 0 && (
                  <tr>
                    <td colSpan={8} className="p-6 text-center text-muted-foreground">
                      {tr("workflowDialog.activityRun.noTorrents")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {hasMore && (
            <div className="flex items-center justify-between gap-3 p-2 text-xs text-muted-foreground border-t bg-muted/30">
              <span>{tr("workflowDialog.activityRun.showing", { shown: items.length, total })}</span>
              <Button
                size="sm"
                variant="secondary"
                onClick={handleLoadMore}
                disabled={runQuery.isFetching}
              >
                {runQuery.isFetching && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {tr("workflowDialog.activityRun.loadMore")}
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
