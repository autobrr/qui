/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { PathCell } from "@/components/ui/path-cell"
import { useConfirmOrphanScanDeletion, useOrphanScanRun } from "@/hooks/useOrphanScan"
import { api } from "@/lib/api"
import { type CsvColumn, downloadBlob, toCsv } from "@/lib/csv-export"
import { formatBytes } from "@/lib/utils"
import type { OrphanScanFile } from "@/types"
import { Download, Loader2, Trash2 } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface OrphanScanPreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  runId: number
}

const PAGE_SIZE = 200

export function OrphanScanPreviewDialog({
  open,
  onOpenChange,
  instanceId,
  runId,
}: OrphanScanPreviewDialogProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const [offset, setOffset] = useState(0)
  const [files, setFiles] = useState<OrphanScanFile[]>([])
  const [isExporting, setIsExporting] = useState(false)

  const runQuery = useOrphanScanRun(instanceId, runId, {
    limit: PAGE_SIZE,
    offset,
    enabled: open,
  })

  const confirmMutation = useConfirmOrphanScanDeletion(instanceId)

  useEffect(() => {
    if (!open) {
      setOffset(0)
      setFiles([])
    }
  }, [open])

  useEffect(() => {
    const page = runQuery.data?.files
    if (!page) return

    setFiles((prev) => {
      const seen = new Set(prev.map((f) => f.id))
      const next = [...prev]
      for (const item of page) {
        if (!seen.has(item.id)) next.push(item)
      }
      return next
    })
  }, [runQuery.data])

  const run = runQuery.data
  const totalFiles = run?.filesFound ?? 0
  const hasMore = files.length < totalFiles

  const totalSize = useMemo(() => {
    if (!run) return 0
    return run.bytesReclaimed || 0
  }, [run])

  const handleLoadMore = () => {
    setOffset((prev) => prev + PAGE_SIZE)
  }

  const handleConfirm = () => {
    confirmMutation.mutate(runId, {
      onSuccess: () => {
        toast.success(tr("orphanScanPreviewDialog.toasts.deletionStarted.title"), {
          description: tr("orphanScanPreviewDialog.toasts.deletionStarted.description"),
        })
        onOpenChange(false)
      },
      onError: (error) => {
        toast.error(tr("orphanScanPreviewDialog.toasts.failedStartDeletion.title"), {
          description: error instanceof Error ? error.message : tr("orphanScanPreviewDialog.toasts.unknownError"),
        })
      },
    })
  }

  // CSV columns for orphan files export
  const csvColumns: CsvColumn<OrphanScanFile>[] = [
    { header: tr("orphanScanPreviewDialog.csv.path"), accessor: f => f.filePath },
    { header: tr("orphanScanPreviewDialog.csv.size"), accessor: f => formatBytes(f.fileSize) },
    { header: tr("orphanScanPreviewDialog.csv.sizeBytes"), accessor: f => f.fileSize },
    { header: tr("orphanScanPreviewDialog.csv.modified"), accessor: f => f.modifiedAt ?? "" },
  ]

  const handleExport = async () => {
    if (!run || totalFiles === 0) return

    setIsExporting(true)
    try {
      const pageSize = 500
      const allItems: OrphanScanFile[] = []
      let exportOffset = 0

      while (allItems.length < totalFiles) {
        const result = await api.getOrphanScanRun(instanceId, runId, {
          limit: pageSize,
          offset: exportOffset,
        })
        allItems.push(...result.files)
        exportOffset += pageSize
        if (result.files.length === 0) break
      }

      const csv = toCsv(allItems, csvColumns)
      downloadBlob(csv, `orphan_files_${runId}.csv`)
      toast.success(tr("orphanScanPreviewDialog.toasts.exportedCsv", { count: allItems.length }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : tr("orphanScanPreviewDialog.toasts.failedExport"))
    } finally {
      setIsExporting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[85dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{tr("orphanScanPreviewDialog.title")}</DialogTitle>
          <DialogDescription>
            {tr("orphanScanPreviewDialog.description")}
          </DialogDescription>
        </DialogHeader>

        {run && (
          <div className="text-sm text-muted-foreground">
            {tr("orphanScanPreviewDialog.summary.filesAndSize", { count: run.filesFound, size: formatBytes(totalSize) })}
            {run.truncated && ` ${tr("orphanScanPreviewDialog.summary.truncated")}`}
          </div>
        )}

        <div className="flex-1 min-h-0 overflow-hidden border rounded-lg">
          <div className="overflow-auto max-h-[50vh]">
            <table className="w-full text-sm">
              <thead className="sticky top-0">
                <tr className="border-b">
                  <th className="text-left p-2 font-medium bg-muted">{tr("orphanScanPreviewDialog.table.path")}</th>
                  <th className="text-right p-2 font-medium bg-muted">{tr("orphanScanPreviewDialog.table.size")}</th>
                  <th className="text-right p-2 font-medium bg-muted">{tr("orphanScanPreviewDialog.table.modified")}</th>
                  <th className="text-left p-2 font-medium bg-muted">{tr("orphanScanPreviewDialog.table.status")}</th>
                </tr>
              </thead>
              <tbody>
                {files.map((f) => (
                  <tr key={f.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="p-2 max-w-[520px]">
                      <PathCell path={f.filePath} />
                    </td>
                    <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                      {formatBytes(f.fileSize)}
                    </td>
                    <td className="p-2 text-right font-mono text-muted-foreground whitespace-nowrap">
                      {f.modifiedAt ? new Date(f.modifiedAt).toLocaleString() : "-"}
                    </td>
                    <td className="p-2">
                      <div className="text-xs font-mono text-muted-foreground">
                        {f.status}
                        {f.errorMessage ? (
                          <div className="mt-1 text-[11px] text-muted-foreground/80 whitespace-pre-wrap break-all">
                            {f.errorMessage}
                          </div>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                ))}
                {runQuery.isLoading && files.length === 0 && (
                  <tr>
                    <td colSpan={4} className="p-6 text-center text-muted-foreground">
                      <Loader2 className="h-4 w-4 animate-spin inline-block mr-2" />
                      {tr("orphanScanPreviewDialog.states.loading")}
                    </td>
                  </tr>
                )}
                {!runQuery.isLoading && files.length === 0 && (
                  <tr>
                    <td colSpan={4} className="p-6 text-center text-muted-foreground">
                      {tr("orphanScanPreviewDialog.states.noFiles")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {hasMore && (
            <div className="flex items-center justify-between gap-3 p-2 text-xs text-muted-foreground border-t bg-muted/30">
              <span>{tr("orphanScanPreviewDialog.pagination.showing", { shown: files.length, total: totalFiles })}</span>
              <Button
                size="sm"
                variant="secondary"
                onClick={handleLoadMore}
                disabled={runQuery.isFetching}
              >
                {runQuery.isFetching && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {tr("orphanScanPreviewDialog.actions.loadMore")}
              </Button>
            </div>
          )}
        </div>

        <DialogFooter className="mt-4 sm:justify-between">
          <div>
            {totalFiles > 0 && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleExport}
                disabled={isExporting}
              >
                {isExporting ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : (
                  <Download className="h-4 w-4 mr-2" />
                )}
                {tr("orphanScanPreviewDialog.actions.exportCsv")}
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {tr("orphanScanPreviewDialog.actions.close")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleConfirm}
              disabled={confirmMutation.isPending || !run || run.status !== "preview_ready"}
            >
              {confirmMutation.isPending ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <Trash2 className="h-4 w-4 mr-2" />
              )}
              {tr("orphanScanPreviewDialog.actions.deleteFiles")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
