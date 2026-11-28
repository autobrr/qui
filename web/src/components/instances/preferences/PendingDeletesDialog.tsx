/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import type { PendingDeleteTorrent } from "@/types"
import { useTranslation } from "react-i18next"

interface PendingDeletesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  ruleName: string
  torrents: PendingDeleteTorrent[]
}

function PendingDeleteWhen({ torrent }: { torrent: PendingDeleteTorrent }) {
  const { t } = useTranslation("instances")
  const { formatTimestamp } = useDateTimeFormatters()

  if (torrent.awaitsTracker) {
    return <span className="text-xs text-muted-foreground whitespace-nowrap">{t("preferences.workflowPreview.waitsForTracker")}</span>
  }
  if (torrent.deletesAt * 1000 > Date.now()) {
    return <span className="text-xs text-muted-foreground whitespace-nowrap">{t("preferences.workflowPreview.waitsUntil", { time: formatTimestamp(torrent.deletesAt) })}</span>
  }
  return <span className="text-xs text-destructive whitespace-nowrap">{t("preferences.pendingDeletes.nextRun")}</span>
}

export function PendingDeletesDialog({ open, onOpenChange, ruleName, torrents }: PendingDeletesDialogProps) {
  const { t } = useTranslation("instances")
  const { formatTimestamp } = useDateTimeFormatters()
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-5xl max-h-[85dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("preferences.pendingDeletes.title", { name: ruleName })}</DialogTitle>
          <DialogDescription>{t("preferences.pendingDeletes.description")}</DialogDescription>
        </DialogHeader>

        {torrents.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("preferences.pendingDeletes.empty")}</p>
        ) : (
          <div className="flex-1 min-h-0 overflow-hidden border rounded-lg">
            <div className="overflow-auto max-h-[60vh]">
              <table className="w-full text-sm">
                <thead className="sticky top-0">
                  <tr className="border-b">
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.workflowPreview.tracker")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.workflowPreview.name")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.pendingDeletes.message")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.pendingDeletes.unregisteredSince")}</th>
                    <th className="text-left p-2 font-medium bg-muted">{t("preferences.workflowPreview.deletes")}</th>
                  </tr>
                </thead>
                <tbody>
                  {torrents.map((torrent) => {
                    const trackerCustom = torrent.tracker ? trackerCustomizations?.find(c =>
                      c.domains.some(d => d.toLowerCase() === torrent.tracker.toLowerCase())
                    ) : undefined
                    return (
                      <tr key={torrent.hash} className="border-b last:border-0 hover:bg-muted/30">
                        <td className="p-2">
                          {torrent.tracker ? (
                            <div className="flex items-center gap-1.5">
                              <TrackerIconImage tracker={torrent.tracker} trackerIcons={trackerIcons} />
                              <TruncatedText className="max-w-[120px]" tooltipContent={trackerCustom?.displayName ? torrent.tracker : undefined}>
                                {trackerCustom?.displayName ?? torrent.tracker}
                              </TruncatedText>
                            </div>
                          ) : <span className="text-muted-foreground">-</span>}
                        </td>
                        <td className="p-2 max-w-[280px]">
                          {torrent.name ? (
                            <TruncatedText className="block">{torrent.name}</TruncatedText>
                          ) : (
                            <TruncatedText className="block font-mono text-xs text-muted-foreground">{torrent.hash}</TruncatedText>
                          )}
                        </td>
                        <td className="p-2 max-w-[280px]">
                          <TruncatedText className="block text-muted-foreground">{torrent.message || "-"}</TruncatedText>
                        </td>
                        <td className="p-2 text-muted-foreground whitespace-nowrap">{formatTimestamp(torrent.unregisteredSince)}</td>
                        <td className="p-2"><PendingDeleteWhen torrent={torrent} /></td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
