/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getLinuxIsoName, getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import type { PendingDeleteTorrent } from "@/types"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

const PAGE_SIZE = 200

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
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [incognitoMode] = useIncognitoMode()
  const displayNames = useMemo(() => {
    const byDomain = new Map<string, string>()
    for (const customization of trackerCustomizations ?? []) {
      for (const domain of customization.domains) {
        byDomain.set(domain.toLowerCase(), customization.displayName)
      }
    }
    return byDomain
  }, [trackerCustomizations])
  const shown = torrents.slice(0, limit)

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
                  {shown.map((torrent) => {
                    const tracker = incognitoMode && torrent.tracker ? getLinuxTrackerDomain(torrent.tracker) : torrent.tracker
                    const name = incognitoMode && torrent.name ? getLinuxIsoName(torrent.hash) : torrent.name
                    const displayName = incognitoMode ? undefined : displayNames.get(tracker.toLowerCase())
                    return (
                      <tr key={torrent.hash} className="border-b last:border-0 hover:bg-muted/30">
                        <td className="p-2">
                          {tracker ? (
                            <div className="flex items-center gap-1.5">
                              <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />
                              <TruncatedText className="max-w-[120px]" tooltipContent={displayName ? tracker : undefined}>
                                {displayName ?? tracker}
                              </TruncatedText>
                            </div>
                          ) : <span className="text-muted-foreground">-</span>}
                        </td>
                        <td className="p-2 max-w-[280px]">
                          {name ? (
                            <TruncatedText className="block">{name}</TruncatedText>
                          ) : (
                            <TruncatedText className="block font-mono text-xs text-muted-foreground">{torrent.hash}</TruncatedText>
                          )}
                        </td>
                        <td className="p-2 max-w-[280px]">
                          <TruncatedText className="block text-muted-foreground">{incognitoMode && torrent.message ? t("torrents:detailsPanel.trackerMessageHidden") : torrent.message || "-"}</TruncatedText>
                        </td>
                        <td className="p-2 text-muted-foreground whitespace-nowrap">{formatTimestamp(torrent.unregisteredSince)}</td>
                        <td className="p-2"><PendingDeleteWhen torrent={torrent} /></td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            {torrents.length > shown.length && (
              <div className="flex items-center justify-between gap-3 p-2 text-xs text-muted-foreground border-t bg-muted/30">
                <span>{t("preferences.workflowPreview.andMoreTorrents", { count: torrents.length - shown.length })}</span>
                <Button size="sm" variant="secondary" onClick={() => setLimit(limit + PAGE_SIZE)}>
                  {t("preferences.workflowPreview.loadMore")}
                </Button>
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
