/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HARDLINK_SCOPE_VALUES, TORRENT_STATES } from "@/components/query-builder/constants"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { PathCell } from "@/components/ui/path-cell"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { formatBytes, formatDurationCompact, getRatioColor } from "@/lib/utils"
import type { AutomationPreviewResult, AutomationPreviewTorrent, PreviewView, RuleCondition } from "@/types"
import { Download, Loader2 } from "lucide-react"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"
import { AnimatedLogo } from "@/components/ui/AnimatedLogo"

// Tabs component for needed/eligible toggle
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"

// Helper to get human-readable label from value/label arrays
function getLabelFromValues(values: Array<{ value: string; label: string }>, value: string): string {
  const found = values.find(v => v.value === value)
  if (found) return found.label
  // Fallback: capitalize and humanize
  return value.charAt(0).toUpperCase() + value.slice(1).replace(/_/g, " ")
}

interface WorkflowPreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: React.ReactNode
  preview: AutomationPreviewResult | null
  /** Condition used to filter - used to show relevant columns */
  condition?: RuleCondition | null
  onConfirm: () => void
  confirmLabel: string
  isConfirming: boolean
  onLoadMore?: () => void
  isLoadingMore?: boolean
  /** Use destructive styling (red button) */
  destructive?: boolean
  /** Use warning styling (amber button) for category changes */
  warning?: boolean
  /** Current preview view mode (only shown for delete rules with FREE_SPACE) */
  previewView?: PreviewView
  /** Callback when user switches preview view */
  onPreviewViewChange?: (view: PreviewView) => void
  /** Whether to show the preview view toggle (only for FREE_SPACE delete rules) */
  showPreviewViewToggle?: boolean
  /** Whether the preview is currently loading (e.g., when switching views) */
  isLoadingPreview?: boolean
  /** Callback to export all preview data to CSV */
  onExport?: () => void
  /** Whether export is in progress */
  isExporting?: boolean
  /** Whether the initial preview is loading (dialog just opened, waiting for first results) */
  isInitialLoading?: boolean
  /** Show score column for score-based sorting previews */
  showScore?: boolean
}

// Extract all field names from a condition tree
function extractConditionFields(cond: RuleCondition | null | undefined): Set<string> {
  const fields = new Set<string>()
  if (!cond) return fields

  if (cond.field) {
    fields.add(cond.field)
  }

  if (cond.conditions) {
    for (const child of cond.conditions) {
      for (const f of extractConditionFields(child)) {
        fields.add(f)
      }
    }
  }

  return fields
}

// Column definitions for dynamic columns
type ColumnDef = {
  key: string
  header: string
  align: "left" | "right" | "center"
  // Fields that trigger this column to appear
  triggerFields: string[]
  render: (t: AutomationPreviewTorrent) => React.ReactNode
}

function createDynamicColumns(
  t: ReturnType<typeof useTranslation>["t"],
  translateTorrentState: (value: string) => string,
  translateHardlinkScope: (value: string) => string
): ColumnDef[] {
  return [
    {
      key: "numComplete",
      header: t("preferences.workflowPreview.seeders"),
      align: "right",
      triggerFields: ["NUM_COMPLETE", "NUM_SEEDS"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground">
          {torrent.numComplete}
          {torrent.numSeeds > 0 && <span className="text-xs ml-1">({torrent.numSeeds})</span>}
        </span>
      ),
    },
    {
      key: "numIncomplete",
      header: t("preferences.workflowPreview.leechers"),
      align: "right",
      triggerFields: ["NUM_INCOMPLETE", "NUM_LEECHS"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground">
          {torrent.numIncomplete}
          {torrent.numLeechs > 0 && <span className="text-xs ml-1">({torrent.numLeechs})</span>}
        </span>
      ),
    },
    {
      key: "progress",
      header: t("preferences.workflowPreview.progress"),
      align: "right",
      triggerFields: ["PROGRESS"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground">
          {(torrent.progress * 100).toFixed(1)}%
        </span>
      ),
    },
    {
      key: "availability",
      header: t("preferences.workflowPreview.availability"),
      align: "right",
      triggerFields: ["AVAILABILITY"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground">
          {torrent.availability.toFixed(2)}
        </span>
      ),
    },
    {
      key: "addedAge",
      header: t("preferences.workflowPreview.added"),
      align: "right",
      triggerFields: ["ADDED_ON", "ADDED_ON_AGE"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground whitespace-nowrap">
          {formatDurationCompact(Math.floor(Date.now() / 1000) - torrent.addedOn)}
        </span>
      ),
    },
    {
      key: "completedAge",
      header: t("preferences.workflowPreview.completed"),
      align: "right",
      triggerFields: ["COMPLETION_ON", "COMPLETION_ON_AGE"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground whitespace-nowrap">
          {torrent.completionOn > 0 ? formatDurationCompact(Math.floor(Date.now() / 1000) - torrent.completionOn) : "-"}
        </span>
      ),
    },
    {
      key: "lastActivityAge",
      header: t("preferences.workflowPreview.inactive"),
      align: "right",
      triggerFields: ["LAST_ACTIVITY", "LAST_ACTIVITY_AGE"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground whitespace-nowrap">
          {torrent.lastActivity > 0 ? formatDurationCompact(Math.floor(Date.now() / 1000) - torrent.lastActivity) : "-"}
        </span>
      ),
    },
    {
      key: "timeActive",
      header: t("preferences.workflowPreview.active"),
      align: "right",
      triggerFields: ["TIME_ACTIVE"],
      render: (torrent) => (
        <span className="font-mono text-muted-foreground whitespace-nowrap">
          {formatDurationCompact(torrent.timeActive)}
        </span>
      ),
    },
    {
      key: "state",
      header: t("preferences.workflowPreview.state"),
      align: "center",
      triggerFields: ["STATE"],
      render: (torrent) => (
        <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
          {translateTorrentState(torrent.state)}
        </span>
      ),
    },
    {
      key: "hardlinkScope",
      header: t("preferences.workflowPreview.hardlinks"),
      align: "center",
      triggerFields: ["HARDLINK_SCOPE"],
      render: (torrent) => (
        torrent.hardlinkScope ? (
          <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
            {translateHardlinkScope(torrent.hardlinkScope)}
          </span>
        ) : null
      ),
    },
    {
      key: "hardlinkCrossScope",
      header: t("preferences.workflowPreview.hardlinksCross"),
      align: "center",
      triggerFields: ["HARDLINK_SCOPE_CROSS"],
      render: (torrent) => (
        torrent.hardlinkCrossScope ? (
          <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
            {translateHardlinkScope(torrent.hardlinkCrossScope)}
          </span>
        ) : null
      ),
    },
    {
      key: "status",
      header: t("preferences.workflowPreview.status"),
      align: "center",
      triggerFields: ["IS_UNREGISTERED"],
      render: (torrent) => (
        torrent.isUnregistered ? (
          <span className="text-xs px-1.5 py-0.5 rounded bg-destructive/10 text-destructive">
            {t("preferences.workflowPreview.unregistered")}
          </span>
        ) : null
      ),
    },
  ]
}

export function WorkflowPreviewDialog({
  open,
  onOpenChange,
  title,
  description,
  preview,
  condition,
  onConfirm,
  confirmLabel,
  isConfirming,
  onLoadMore,
  isLoadingMore = false,
  destructive = true,
  warning = false,
  previewView = "needed",
  onPreviewViewChange,
  showPreviewViewToggle = false,
  isLoadingPreview = false,
  onExport,
  isExporting = false,
  isInitialLoading = false,
  showScore = false,
}: WorkflowPreviewDialogProps) {
  const { t, i18n } = useTranslation(["instances", "automations"])
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const hasMore = !!preview && preview.examples.length < preview.totalMatches
  const showScoreColumn = showScore && !!preview?.examples.some(t => t.score !== undefined && t.score !== null)
  const dynamicColumns = useMemo(
    () => createDynamicColumns(
      t,
      (value: string) => i18n.t(`queryBuilder.torrentStates.${value}`, {
        ns: "automations",
        defaultValue: getLabelFromValues(TORRENT_STATES, value),
      }),
      (value: string) => i18n.t(`queryBuilder.hardlinkScopes.${value}`, {
        ns: "automations",
        defaultValue: getLabelFromValues(HARDLINK_SCOPE_VALUES, value),
      })
    ),
    [i18n, t]
  )

  // Determine which dynamic columns to show based on condition fields
  const visibleDynamicColumns = useMemo(() => {
    const fields = extractConditionFields(condition)
    return dynamicColumns.filter(col =>
      col.triggerFields.some(f => fields.has(f))
    )
  }, [condition, dynamicColumns])

  // Show loading state when initial preview is being fetched
  if (isInitialLoading) {
    return (
      <AlertDialog open={open} onOpenChange={onOpenChange}>
        <AlertDialogContent className="sm:max-w-md">
          <div className="flex flex-col items-center justify-center py-12 gap-4">
            <AnimatedLogo className="h-16 w-16" />
            <p className="text-sm text-muted-foreground">{t("preferences.workflowPreview.loadingPreview")}</p>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.workflowPreview.cancel")}</AlertDialogCancel>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    )
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="sm:max-w-5xl max-h-[85dvh] flex flex-col">
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-3">
              {description}
              {showPreviewViewToggle && (
                <div className="space-y-2 pt-1">
                  <Tabs
                    value={previewView}
                    onValueChange={(v) => onPreviewViewChange?.(v as PreviewView)}
                    className="w-full"
                  >
                    <TabsList className="grid w-full grid-cols-2">
                      <TabsTrigger value="needed" disabled={isLoadingPreview}>
                        {t("preferences.workflowPreview.neededToReachTarget")}
                      </TabsTrigger>
                      <TabsTrigger value="eligible" disabled={isLoadingPreview}>
                        {t("preferences.workflowPreview.allEligible")}
                      </TabsTrigger>
                    </TabsList>
                  </Tabs>
                  <p className="text-xs text-muted-foreground">
                    {previewView === "needed"? t("preferences.workflowPreview.neededDescription"): t("preferences.workflowPreview.eligibleDescription")}
                  </p>
                </div>
              )}
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>

        {preview && preview.examples.length > 0 && (
          <div className="flex-1 min-h-0 overflow-hidden border rounded-lg relative">
            {isLoadingPreview && (
              <div className="absolute inset-0 bg-background/80 flex items-center justify-center z-10">
                <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
              </div>
            )}
            <div className="overflow-auto max-h-[50vh]">
              <table className="w-full text-sm">
                <thead className="sticky top-0">
                  <tr className="border-b">
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.workflowPreview.tracker")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.workflowPreview.name")}</th>
                    <th className="text-right p-2 font-medium bg-muted">{t("preferences.workflowPreview.size")}</th>
                    <th className="text-right p-2 font-medium bg-muted">{t("preferences.workflowPreview.ratio")}</th>
                    <th className="text-right p-2 font-medium bg-muted">{t("preferences.workflowPreview.seedTime")}</th>
                    {showScoreColumn && <th className="text-right p-2 font-medium bg-muted">{t("preferences.workflowPreview.score")}</th>}
                    {visibleDynamicColumns.map(col => (
                      <th
                        key={col.key}
                        className={`p-2 font-medium bg-muted text-${col.align}`}
                      >
                        {col.header}
                      </th>
                    ))}
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.workflowPreview.category")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.workflowPreview.path")}</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.examples.map((torrent) => {
                    const trackerCustom = trackerCustomizations?.find(c =>
                      c.domains.some(d => d.toLowerCase() === torrent.tracker.toLowerCase())
                    )
                    return (
                      <tr key={torrent.hash} className="border-b last:border-0 hover:bg-muted/30">
                        <td className="p-2">
                          <div className="flex items-center gap-1.5">
                            <TrackerIconImage
                              tracker={torrent.tracker}
                              trackerIcons={trackerIcons}
                            />
                            <span className="truncate max-w-[100px]" title={torrent.tracker}>
                              {trackerCustom?.displayName ?? torrent.tracker}
                            </span>
                          </div>
                        </td>
                        <td className="p-2 max-w-[280px]">
                          <div className="flex items-center gap-1.5">
                            <TruncatedText className="block flex-1 min-w-0">
                              {torrent.name}
                            </TruncatedText>
                            {/* Single cross-seed badge with appropriate variant based on expansion type */}
                            {(torrent.isCrossSeed || torrent.isHardlinkCopy) && (
                              <span className={`shrink-0 text-[10px] px-1.5 py-0.5 rounded ${
                                torrent.isHardlinkCopy? "bg-violet-500/10 text-violet-600": "bg-blue-500/10 text-blue-600"
                              }`}>
                                {torrent.isHardlinkCopy ? t("preferences.workflowPreview.crossSeedHardlinked") : t("preferences.workflowPreview.crossSeedSameFiles")}
                              </span>
                            )}
                          </div>
                        </td>
                        <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                          {formatBytes(torrent.size)}
                        </td>
                        <td
                          className="p-2 text-right font-mono whitespace-nowrap font-medium"
                          style={{ color: getRatioColor(torrent.ratio) }}
                        >
                          {torrent.ratio === -1 ? "∞" : torrent.ratio.toFixed(2)}
                        </td>
                        <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                          {formatDurationCompact(torrent.seedingTime)}
                        </td>
                        {showScoreColumn && (
                          <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                            {torrent.score !== undefined && torrent.score !== null ? torrent.score.toFixed(2) : "-"}
                          </td>
                        )}
                        {visibleDynamicColumns.map(col => (
                          <td key={col.key} className={`p-2 text-${col.align}`}>
                            {col.render(torrent)}
                          </td>
                        ))}
                        <td className="p-2">
                          <TruncatedText className="block max-w-[80px] text-muted-foreground">
                            {torrent.category || "-"}
                          </TruncatedText>
                        </td>
                        <td className="p-2 max-w-[200px]">
                          <PathCell path={torrent.contentPath} />
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            {hasMore && (
              <div className="flex items-center justify-between gap-3 p-2 text-xs text-muted-foreground border-t bg-muted/30">
                <span>{t("preferences.workflowPreview.andMoreTorrents", { count: preview.totalMatches - preview.examples.length })}</span>
                {onLoadMore && (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={onLoadMore}
                    disabled={isLoadingMore}
                  >
                    {isLoadingMore && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                    {t("preferences.workflowPreview.loadMore")}
                  </Button>
                )}
              </div>
            )}
          </div>
        )}

        <AlertDialogFooter className="mt-4 sm:justify-between">
          <div>
            {onExport && preview && preview.totalMatches > 0 && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onExport}
                disabled={isExporting}
              >
                {isExporting ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : (
                  <Download className="h-4 w-4 mr-2" />
                )}
                {t("preferences.workflowPreview.exportCSV")}
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <AlertDialogCancel>{t("preferences.workflowPreview.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={onConfirm}
              disabled={isConfirming}
              className={
                destructive? "bg-destructive text-destructive-foreground hover:bg-destructive/90": warning? "bg-amber-600 text-white hover:bg-amber-700": ""
              }
            >
              {isConfirming && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {confirmLabel}
            </AlertDialogAction>
          </div>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
