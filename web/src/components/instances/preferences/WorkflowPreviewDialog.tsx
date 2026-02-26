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
  AlertDialogTitle,
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"

type TranslateFn = (key: string, options?: Record<string, unknown>) => string

const STATE_LABEL_KEYS: Record<string, string> = {
  downloading: "workflowDialog.preview.stateValues.downloading",
  uploading: "workflowDialog.preview.stateValues.uploading",
  completed: "workflowDialog.preview.stateValues.completed",
  stopped: "workflowDialog.preview.stateValues.stopped",
  active: "workflowDialog.preview.stateValues.active",
  inactive: "workflowDialog.preview.stateValues.inactive",
  running: "workflowDialog.preview.stateValues.running",
  stalled: "workflowDialog.preview.stateValues.stalled",
  stalled_uploading: "workflowDialog.preview.stateValues.stalledUploading",
  stalled_downloading: "workflowDialog.preview.stateValues.stalledDownloading",
  errored: "workflowDialog.preview.stateValues.errored",
  tracker_down: "workflowDialog.preview.stateValues.trackerDown",
  checking: "workflowDialog.preview.stateValues.checking",
  checkingResumeData: "workflowDialog.preview.stateValues.checkingResumeData",
  moving: "workflowDialog.preview.stateValues.moving",
  missingFiles: "workflowDialog.preview.stateValues.missingFiles",
}

const HARDLINK_SCOPE_LABEL_KEYS: Record<string, string> = {
  none: "workflowDialog.preview.hardlinkValues.none",
  torrents_only: "workflowDialog.preview.hardlinkValues.torrentsOnly",
  outside_qbittorrent: "workflowDialog.preview.hardlinkValues.outsideQbittorrent",
}

function getLabelFromValues(
  values: Array<{ value: string; label: string }>,
  value: string,
  tr: TranslateFn,
  labelKeys?: Record<string, string>,
): string {
  const labelKey = labelKeys?.[value]
  if (labelKey) return tr(labelKey)

  const found = values.find(v => v.value === value)
  if (found) return found.label

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
  headerKey: string
  align: "left" | "right" | "center"
  triggerFields: string[]
  render: (torrent: AutomationPreviewTorrent, tr: TranslateFn) => React.ReactNode
}

const DYNAMIC_COLUMNS: ColumnDef[] = [
  {
    key: "numComplete",
    headerKey: "workflowDialog.preview.columns.numComplete",
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
    headerKey: "workflowDialog.preview.columns.numIncomplete",
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
    headerKey: "workflowDialog.preview.columns.progress",
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
    headerKey: "workflowDialog.preview.columns.availability",
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
    headerKey: "workflowDialog.preview.columns.addedAge",
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
    headerKey: "workflowDialog.preview.columns.completedAge",
    align: "right",
    triggerFields: ["COMPLETION_ON", "COMPLETION_ON_AGE"],
    render: (torrent, tr) => (
      <span className="font-mono text-muted-foreground whitespace-nowrap">
        {torrent.completionOn > 0
          ? formatDurationCompact(Math.floor(Date.now() / 1000) - torrent.completionOn)
          : tr("workflowDialog.activityRun.values.none")}
      </span>
    ),
  },
  {
    key: "lastActivityAge",
    headerKey: "workflowDialog.preview.columns.lastActivityAge",
    align: "right",
    triggerFields: ["LAST_ACTIVITY", "LAST_ACTIVITY_AGE"],
    render: (torrent, tr) => (
      <span className="font-mono text-muted-foreground whitespace-nowrap">
        {torrent.lastActivity > 0
          ? formatDurationCompact(Math.floor(Date.now() / 1000) - torrent.lastActivity)
          : tr("workflowDialog.activityRun.values.none")}
      </span>
    ),
  },
  {
    key: "timeActive",
    headerKey: "workflowDialog.preview.columns.timeActive",
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
    headerKey: "workflowDialog.preview.columns.state",
    align: "center",
    triggerFields: ["STATE"],
    render: (torrent, tr) => (
      <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
        {getLabelFromValues(TORRENT_STATES, torrent.state, tr, STATE_LABEL_KEYS)}
      </span>
    ),
  },
  {
    key: "hardlinkScope",
    headerKey: "workflowDialog.preview.columns.hardlinkScope",
    align: "center",
    triggerFields: ["HARDLINK_SCOPE"],
    render: (torrent, tr) => (
      torrent.hardlinkScope ? (
        <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
          {getLabelFromValues(HARDLINK_SCOPE_VALUES, torrent.hardlinkScope, tr, HARDLINK_SCOPE_LABEL_KEYS)}
        </span>
      ) : null
    ),
  },
  {
    key: "status",
    headerKey: "workflowDialog.preview.columns.status",
    align: "center",
    triggerFields: ["IS_UNREGISTERED"],
    render: (torrent, tr) => (
      torrent.isUnregistered ? (
        <span className="text-xs px-1.5 py-0.5 rounded bg-destructive/10 text-destructive">
          {tr("workflowDialog.preview.badges.unregistered")}
        </span>
      ) : null
    ),
  },
]

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
}: WorkflowPreviewDialogProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const hasMore = !!preview && preview.examples.length < preview.totalMatches

  const visibleDynamicColumns = useMemo(() => {
    const fields = extractConditionFields(condition)
    return DYNAMIC_COLUMNS.filter((col) =>
      col.triggerFields.some(f => fields.has(f))
    )
  }, [condition])

  if (isInitialLoading) {
    return (
      <AlertDialog open={open} onOpenChange={onOpenChange}>
        <AlertDialogContent className="sm:max-w-md">
          <div className="flex flex-col items-center justify-center py-12 gap-4">
            <AnimatedLogo className="h-16 w-16" />
            <p className="text-sm text-muted-foreground">{tr("workflowDialog.preview.loadingInitial")}</p>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>{tr("workflowDialog.actions.cancel")}</AlertDialogCancel>
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
                        {tr("workflowDialog.preview.tabs.needed")}
                      </TabsTrigger>
                      <TabsTrigger value="eligible" disabled={isLoadingPreview}>
                        {tr("workflowDialog.preview.tabs.eligible")}
                      </TabsTrigger>
                    </TabsList>
                  </Tabs>
                  <p className="text-xs text-muted-foreground">
                    {previewView === "needed"
                      ? tr("workflowDialog.preview.tabDescriptions.needed")
                      : tr("workflowDialog.preview.tabDescriptions.eligible")}
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
                    <th className="text-left p-2 font-medium bg-muted">{tr("workflowDialog.preview.table.tracker")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{tr("workflowDialog.preview.table.name")}</th>
                    <th className="text-right p-2 font-medium bg-muted">{tr("workflowDialog.preview.table.size")}</th>
                    <th className="text-right p-2 font-medium bg-muted">{tr("workflowDialog.preview.table.ratio")}</th>
                    <th className="text-right p-2 font-medium bg-muted">{tr("workflowDialog.preview.table.seedTime")}</th>
                    {visibleDynamicColumns.map(col => (
                      <th
                        key={col.key}
                        className={`p-2 font-medium bg-muted text-${col.align}`}
                      >
                        {tr(col.headerKey)}
                      </th>
                    ))}
                    <th className="text-left p-2 font-medium bg-muted">{tr("workflowDialog.preview.table.category")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{tr("workflowDialog.preview.table.path")}</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.examples.map((t) => {
                    const trackerCustom = trackerCustomizations?.find(c =>
                      c.domains.some(d => d.toLowerCase() === t.tracker.toLowerCase())
                    )
                    return (
                      <tr key={t.hash} className="border-b last:border-0 hover:bg-muted/30">
                        <td className="p-2">
                          <div className="flex items-center gap-1.5">
                            <TrackerIconImage
                              tracker={t.tracker}
                              trackerIcons={trackerIcons}
                            />
                            <span className="truncate max-w-[100px]" title={t.tracker}>
                              {trackerCustom?.displayName ?? t.tracker}
                            </span>
                          </div>
                        </td>
                        <td className="p-2 max-w-[280px]">
                          <div className="flex items-center gap-1.5">
                            <TruncatedText className="block flex-1 min-w-0">
                              {t.name}
                            </TruncatedText>
                            {(t.isCrossSeed || t.isHardlinkCopy) && (
                              <span className={`shrink-0 text-[10px] px-1.5 py-0.5 rounded ${
                                t.isHardlinkCopy
                                  ? "bg-violet-500/10 text-violet-600"
                                  : "bg-blue-500/10 text-blue-600"
                              }`}>
                                {t.isHardlinkCopy
                                  ? tr("workflowDialog.preview.badges.crossSeedHardlinked")
                                  : tr("workflowDialog.preview.badges.crossSeedSameFiles")}
                              </span>
                            )}
                          </div>
                        </td>
                        <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                          {formatBytes(t.size)}
                        </td>
                        <td
                          className="p-2 text-right font-mono whitespace-nowrap font-medium"
                          style={{ color: getRatioColor(t.ratio) }}
                        >
                          {t.ratio === -1 ? tr("workflowDialog.values.infinity") : t.ratio.toFixed(2)}
                        </td>
                        <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                          {formatDurationCompact(t.seedingTime)}
                        </td>
                        {visibleDynamicColumns.map(col => (
                          <td key={col.key} className={`p-2 text-${col.align}`}>
                            {col.render(t, tr)}
                          </td>
                        ))}
                        <td className="p-2">
                          <TruncatedText className="block max-w-[80px] text-muted-foreground">
                            {t.category || tr("workflowDialog.activityRun.values.none")}
                          </TruncatedText>
                        </td>
                        <td className="p-2 max-w-[200px]">
                          <PathCell path={t.contentPath} />
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            {hasMore && (
              <div className="flex items-center justify-between gap-3 p-2 text-xs text-muted-foreground border-t bg-muted/30">
                <span>{tr("workflowDialog.preview.moreTorrents", { count: preview.totalMatches - preview.examples.length })}</span>
                {onLoadMore && (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={onLoadMore}
                    disabled={isLoadingMore}
                  >
                    {isLoadingMore && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                    {tr("workflowDialog.preview.loadMore")}
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
                {tr("workflowDialog.preview.exportCsv")}
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <AlertDialogCancel>{tr("workflowDialog.actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={onConfirm}
              disabled={isConfirming}
              className={
                destructive
                  ? "bg-destructive text-destructive-foreground hover:bg-destructive/90"
                  : warning
                    ? "bg-amber-600 text-white hover:bg-amber-700"
                    : ""
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
