/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Progress } from "@/components/ui/progress"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { DiscScans } from "@/hooks/useDiscScans"
import { cn, copyTextToClipboard, formatBytes } from "@/lib/utils"
import type { DiscScanRun, DiscScanStatus } from "@/types"
import { Copy, Disc3, Loader2, RotateCw, X } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// DiscReportButton is the BDInfo action on a Disc node in the Content tab. Its
// icon tells the state: no report yet, a scan queued or running, a report
// cached, or a failed scan. A canceled run looks like "no report", and a click
// on it starts a new scan.
export function DiscReportButton({
  status,
  disabled,
  onClick,
  className,
}: {
  status?: DiscScanStatus
  disabled?: boolean
  onClick: () => void
  className?: string
}) {
  const { t } = useTranslation("torrents")
  const active = status === "pending" || status === "scanning"
  const label = active
    ? t("discScan.actionActive")
    : status === "completed" ? t("discScan.actionReady") : status === "failed" ? t("discScan.actionFailed") : t("discScan.action")
  return (
    <button
      type="button"
      className={cn(
        "p-0.5 rounded transition-colors",
        status === "completed" ? "text-green-500" : status === "failed" ? "text-destructive" : "text-muted-foreground",
        disabled ? "opacity-50 cursor-not-allowed" : "hover:bg-muted/80 hover:text-foreground",
        className
      )}
      onClick={(e) => {
        e.stopPropagation()
        if (!disabled) onClick()
      }}
      disabled={disabled}
      aria-label={label}
      title={label}
    >
      {active ? <Loader2 className="h-3 w-3 animate-spin" /> : <Disc3 className="h-3 w-3" />}
    </button>
  )
}

interface TorrentDiscReportDialogProps {
  discPath: string | null
  onClose: () => void
  scans: DiscScans
}

// TorrentDiscReportDialog shows one Disc scan: its place in the queue, its
// progress, its error, or the cached Disc report with a Quick Summary tab and a
// Forum tab. The run comes from the scans of the torrent, so activity refetches
// move the dialog from state to state without a query of its own.
export function TorrentDiscReportDialog({ discPath, onClose, scans }: TorrentDiscReportDialogProps) {
  const { t } = useTranslation("torrents")
  const [tab, setTab] = useState<"summary" | "forum">("summary")
  const run = discPath !== null ? scans.runFor(discPath) : undefined
  const discName = discPath === null || discPath === "." ? "" : discPath.slice(discPath.lastIndexOf("/") + 1)

  const rescan = (force: boolean) => {
    if (discPath !== null) scans.start.mutate({ discPath, force })
  }

  const body = () => {
    if (scans.start.isError) {
      return (
        <StatusBlock text={scans.start.error.message}>
          <Button variant="outline" size="sm" onClick={() => rescan(false)}>
            <RotateCw className="h-4 w-4 mr-2" />
            {t("discScan.retry")}
          </Button>
        </StatusBlock>
      )
    }
    if (!run) {
      return (
        <div className="flex items-center justify-center gap-2 py-16 text-sm text-muted-foreground">
          <Loader2 className="h-5 w-5 animate-spin" />
          {t("discScan.starting")}
        </div>
      )
    }
    switch (run.status) {
      case "pending":
      case "scanning":
        return <ActiveBlock run={run} canceling={scans.cancel.isPending} onCancel={() => scans.cancel.mutate(run.id)} />
      case "failed":
      case "canceled":
        return (
          <StatusBlock text={run.status === "failed" ? run.errorMessage || t("discScan.failed") : t("discScan.canceled")} error={run.status === "failed"}>
            <Button variant="outline" size="sm" onClick={() => rescan(true)}>
              <RotateCw className="h-4 w-4 mr-2" />
              {t("discScan.rescan")}
            </Button>
          </StatusBlock>
        )
      default:
        return <ReportBlock run={run} tab={tab} onTabChange={setTab} onRescan={() => rescan(true)} />
    }
  }

  return (
    <Dialog open={discPath !== null} onOpenChange={(open) => { if (!open) { setTab("summary"); onClose() } }}>
      <DialogContent className="sm:max-w-lg md:max-w-4xl max-h-[85vh] overflow-hidden">
        <DialogHeader>
          <DialogTitle>{t("discScan.action")}</DialogTitle>
          {discName && <DialogDescription className="font-mono truncate">{discName}</DialogDescription>}
        </DialogHeader>
        {body()}
      </DialogContent>
    </Dialog>
  )
}

function StatusBlock({ text, error, children }: { text: string; error?: boolean; children: React.ReactNode }) {
  return (
    <div className="flex flex-col items-start gap-3 py-8">
      <p className={cn("text-sm break-words", error ? "text-destructive" : "text-muted-foreground")}>{text}</p>
      {children}
    </div>
  )
}

function ActiveBlock({ run, canceling, onCancel }: { run: DiscScanRun; canceling: boolean; onCancel: () => void }) {
  const { t } = useTranslation("torrents")
  const percent = run.totalBytes > 0 ? (run.processedBytes / run.totalBytes) * 100 : 0
  return (
    <div className="flex flex-col gap-4 py-8">
      {run.status === "pending" ? (
        <p className="text-sm text-muted-foreground">{t("discScan.queued", { position: run.queuePosition ?? 1 })}</p>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">
            {run.totalBytes > 0
              ? t("discScan.scanning", { processed: formatBytes(run.processedBytes), total: formatBytes(run.totalBytes) })
              : t("discScan.scanningUnknown")}
          </p>
          <div className="flex items-center gap-3">
            <Progress value={percent} className="h-2 flex-1" />
            <span className="text-xs tabular-nums text-muted-foreground w-10 text-right">{Math.floor(percent)}%</span>
          </div>
        </>
      )}
      <Button variant="outline" size="sm" className="self-start" onClick={onCancel} disabled={canceling}>
        <X className="h-4 w-4 mr-2" />
        {t("discScan.cancel")}
      </Button>
    </div>
  )
}

function ReportBlock({
  run,
  tab,
  onTabChange,
  onRescan,
}: {
  run: DiscScanRun
  tab: "summary" | "forum"
  onTabChange: (tab: "summary" | "forum") => void
  onRescan: () => void
}) {
  const { t } = useTranslation("torrents")
  const copyLabel = tab === "summary" ? t("discScan.copyQuickSummary") : t("discScan.copyForum")
  const copyText = (tab === "summary" ? run.quickSummary : run.forumsBlock) ?? ""

  const copy = async () => {
    try {
      await copyTextToClipboard(copyText)
      toast.success(t("discScan.toast.copied", { label: copyLabel }))
    } catch {
      toast.error(t("discScan.toast.copyFailed"))
    }
  }

  return (
    <Tabs value={tab} onValueChange={(value) => onTabChange(value as typeof tab)} className="w-full min-w-0">
      <div className="flex flex-wrap items-center justify-between gap-2 mb-4">
        <TabsList className="min-w-0">
          <TabsTrigger value="summary">{t("discScan.quickSummary")}</TabsTrigger>
          <TabsTrigger value="forum">{t("discScan.forum")}</TabsTrigger>
        </TabsList>
        <div className="flex shrink-0 gap-2">
          <Button variant="outline" size="sm" onClick={onRescan}>
            <RotateCw className="h-4 w-4 mr-2" />
            {t("discScan.rescan")}
          </Button>
          <Button variant="outline" size="sm" onClick={copy} disabled={!copyText}>
            <Copy className="h-4 w-4 mr-2" />
            {copyLabel}
          </Button>
        </div>
      </div>
      <TabsContent value={tab} className="m-0">
        <div className="max-h-[65vh] overflow-y-auto pr-4">
          <pre
            className={cn(
              "rounded-md border bg-muted/30 p-3 text-xs font-mono",
              tab === "summary" ? "whitespace-pre-wrap break-all" : "whitespace-pre overflow-x-auto"
            )}
          >
            {copyText}
          </pre>
        </div>
      </TabsContent>
    </Tabs>
  )
}
