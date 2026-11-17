/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { listShares, removeShare, sharesQueryKey, shareTorrentUrl, stopShare, unlockShare, type Share } from "@/lib/shares-api"
import { formatBytes } from "@/lib/utils"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Download, Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// A running Share changes as its recipient announces, every few minutes at most.
const REFRESH_MS = 15_000

/** The Shares tab of the Transfers page: one row for each Share, the newest first. */
export function SharesTab() {
  const { t } = useTranslation("shares")
  const query = useQuery({ queryKey: sharesQueryKey, queryFn: listShares, refetchInterval: REFRESH_MS })
  const { instances } = useInstances()
  const nameOf = (id: number) => instances?.find((instance) => instance.id === id)?.name ?? t("tab.unknownInstance", { id })

  if (query.isPending) {
    return <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
  }
  if (query.isError) {
    return <p className="text-sm text-destructive">{t("tab.loadFailed")}</p>
  }
  if (query.data.length === 0) {
    return <p className="py-8 text-center text-sm text-muted-foreground">{t("tab.empty")}</p>
  }
  return (
    <div className="space-y-3">
      <ul className="space-y-3">
        {query.data.map((share) => <ShareRow key={share.id} share={share} instanceName={nameOf(share.instanceId)} />)}
      </ul>
      {query.data.some((share) => share.state === "ended") && <p className="text-sm text-muted-foreground">{t("tab.endedStay")}</p>}
    </div>
  )
}

function ShareRow({ share, instanceName }: { share: Share; instanceName: string }) {
  const { t, i18n } = useTranslation("shares")
  const { formatDate } = useDateTimeFormatters()
  const queryClient = useQueryClient()
  const action = useMutation({
    mutationFn: (run: (id: number) => Promise<void>) => run(share.id),
    onSettled: () => queryClient.invalidateQueries({ queryKey: sharesQueryKey }),
    onError: (error) => toast.error(error.message),
  })
  const percent = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language, { style: "percent", maximumFractionDigits: 0 })
  const outside = share.tracker === "outside"
  const end = share.endedAt
    ? t("tab.endedAt", { date: formatDate(new Date(share.endedAt)) })
    : t("tab.endsAt", { date: formatDate(new Date(share.expiresAt)) })

  return (
    <li className="space-y-3 rounded-lg border p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 space-y-0.5">
          <p className="font-medium break-all">{share.name}</p>
          <p className="text-sm text-muted-foreground">{instanceName} · {end}</p>
        </div>
        <Badge variant={share.state === "active" ? "default" : "secondary"}>
          {t([`tab.state.${share.state}`, "tab.state.unknown"])}
        </Badge>
      </div>

      {share.state !== "active" && share.endReason && <p className="text-sm text-muted-foreground">{share.endReason}</p>}

      <div className="space-y-1">
        <div className="flex justify-between gap-2 text-sm">
          <span>{t("tab.progress", { percent: percent.format(share.progress) })}</span>
          <span className="tabular-nums">{t("tab.sent", { size: formatBytes(share.uploaded) })}</span>
        </div>
        <Progress value={share.progress * 100} aria-label={t("tab.progressLabel")} />
      </div>

      <div className="space-y-1 text-sm">
        <p className="font-medium">{outside ? t("tab.connectedPeers") : t("tab.announced")}</p>
        {share.peers.length === 0 ? (
          <p className="text-muted-foreground">{t("tab.noPeers")}</p>
        ) : (
          <ul className="space-y-0.5">
            {share.peers.map((peer) => (
              <li key={peer.address} className="flex justify-between gap-2 font-mono text-xs">
                <span className="break-all">{peer.address}</span>
                <span className="tabular-nums">{percent.format(peer.progress)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="flex flex-wrap gap-2">
        <Button asChild variant="outline" size="sm">
          <a href={shareTorrentUrl(share.id)} download>
            <Download className="mr-1.5 h-4 w-4" />
            {t("dialog.result.torrent")}
          </a>
        </Button>
        {share.state === "active" && share.firstDownloaderOnly && !outside && (
          <Button variant="outline" size="sm" disabled={action.isPending} onClick={() => action.mutate(unlockShare)}>
            {t("tab.unlock")}
          </Button>
        )}
        {share.state === "active" && (
          <Button variant="outline" size="sm" disabled={action.isPending} onClick={() => action.mutate(stopShare)}>
            {t("tab.stop")}
          </Button>
        )}
        {share.state === "ended" && (
          <Button variant="outline" size="sm" disabled={action.isPending} onClick={() => action.mutate(removeShare)}>
            {t("tab.remove")}
          </Button>
        )}
      </div>
    </li>
  )
}
