/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getRatioColor } from "@/lib/utils"
import type { AutomationPreviewResult } from "@/types"
import { Loader2 } from "lucide-react"

interface AutomationPreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: React.ReactNode
  preview: AutomationPreviewResult | null
  onConfirm: () => void
  confirmLabel: string
  isConfirming: boolean
  onLoadMore?: () => void
  isLoadingMore?: boolean
  /** Use destructive styling (red button) */
  destructive?: boolean
}

export function AutomationPreviewDialog({
  open,
  onOpenChange,
  title,
  description,
  preview,
  onConfirm,
  confirmLabel,
  isConfirming,
  onLoadMore,
  isLoadingMore = false,
  destructive = true,
}: AutomationPreviewDialogProps) {
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const hasMore = !!preview && preview.examples.length < preview.totalMatches

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="sm:max-w-4xl max-h-[85dvh] flex flex-col">
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-3">
              {description}
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>

        {preview && preview.examples.length > 0 && (
          <div className="flex-1 min-h-0 overflow-hidden border rounded-lg">
            <div className="overflow-auto max-h-[50vh]">
              <table className="w-full text-sm">
                <thead className="sticky top-0">
                  <tr className="border-b">
                    <th className="text-left p-2 font-medium bg-muted">Tracker</th>
                    <th className="text-left p-2 font-medium bg-muted">Name</th>
                    <th className="text-right p-2 font-medium bg-muted">Size</th>
                    <th className="text-right p-2 font-medium bg-muted">Ratio</th>
                    <th className="text-right p-2 font-medium bg-muted">Seed Time</th>
                    <th className="text-left p-2 font-medium bg-muted">Category</th>
                    <th className="text-center p-2 font-medium bg-muted">Status</th>
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
                          <TruncatedText className="block">
                            {t.name}
                          </TruncatedText>
                        </td>
                        <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                          {formatBytes(t.size)}
                        </td>
                        <td
                          className="p-2 text-right font-mono whitespace-nowrap font-medium"
                          style={{ color: getRatioColor(t.ratio) }}
                        >
                          {t.ratio === -1 ? "∞" : t.ratio.toFixed(2)}
                        </td>
                        <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                          {formatDuration(t.seedingTime)}
                        </td>
                        <td className="p-2">
                          <TruncatedText className="block max-w-[80px] text-muted-foreground">
                            {t.category || "-"}
                          </TruncatedText>
                        </td>
                        <td className="p-2 text-center">
                          {t.isUnregistered && (
                            <span className="text-xs px-1.5 py-0.5 rounded bg-destructive/10 text-destructive">
                              Unregistered
                            </span>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            {hasMore && (
              <div className="flex items-center justify-between gap-3 p-2 text-xs text-muted-foreground border-t bg-muted/30">
                <span>... and {preview.totalMatches - preview.examples.length} more torrents</span>
                {onLoadMore && (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={onLoadMore}
                    disabled={isLoadingMore}
                  >
                    {isLoadingMore && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                    Load more
                  </Button>
                )}
              </div>
            )}
          </div>
        )}

        <AlertDialogFooter className="mt-4">
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            disabled={isConfirming}
            className={destructive ? "bg-destructive text-destructive-foreground hover:bg-destructive/90" : ""}
          >
            {isConfirming && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GiB`
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}
