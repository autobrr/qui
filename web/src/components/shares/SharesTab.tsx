/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { EmptyTile } from "@/components/kit/EmptyTile"
import { Section } from "@/components/kit/Section"
import { RowActions, type RowAction } from "@/components/transfers/RowActions"
import { useIncognitoDisplay } from "@/components/transfers/use-incognito-display"
import { Progress } from "@/components/ui/progress"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { TorrentName } from "@/components/ui/torrent-name"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { listShares, removeShare, sharesQueryKey, shareTorrentUrl, stopShare, unlockShare, type Share, type ShareState } from "@/lib/shares-api"
import { cn, formatBytes } from "@/lib/utils"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CircleSlash, CircleStop, Download, Loader2, LockOpen, Radio, Trash2, type LucideIcon } from "lucide-react"
import { useId } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const REFRESH_MS = 15_000

const stateIcons: Record<ShareState, { icon: LucideIcon; className: string }> = {
  active: { icon: Radio, className: "text-emerald-500" },
  ending: { icon: Loader2, className: "motion-safe:animate-spin" },
  ended: { icon: CircleSlash, className: "text-muted-foreground" },
}

const translatedEndReasons = new Set(["downloaded", "expired", "stopped", "torrent_deleted", "torrent_moved", "files_renamed", "share_torrent_gone"])

export function SharesTab() {
  const { t } = useTranslation("shares")
  const query = useQuery({ queryKey: sharesQueryKey, queryFn: listShares, refetchInterval: REFRESH_MS })
  const { instances } = useInstances()
  const nameOf = (id: number) => instances?.find((instance) => instance.id === id)?.name ?? t("tab.unknownInstance", { id })
  const shares = query.data ?? []

  return (
    <Section
      title={t("transfers:tabs.shares")}
      description={t("tab.description")}
      footer={shares.some((share) => share.state === "ended") ? t("tab.endedStay") : undefined}
      queries={[query]}
    >
      {shares.length === 0 ? (
        <EmptyTile title={t("tab.empty")} />
      ) : (
        <div className="@container -mx-6 border-t max-sm:-mx-4">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-full pl-6 max-sm:pl-4">{t("transfers:columns.torrent")}</TableHead>
                <TableHead className="hidden @lg:table-cell">{t("transfers:columns.state")}</TableHead>
                <TableHead className="pr-6 max-sm:pr-4"><span className="sr-only">{t("transfers:columns.actions")}</span></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {shares.map((share) => <ShareRow key={share.id} share={share} instanceName={nameOf(share.instanceId)} />)}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}

function ShareRow({ share, instanceName }: { share: Share; instanceName: string }) {
  const { t, i18n } = useTranslation("shares")
  const { formatDate } = useDateTimeFormatters()
  const queryClient = useQueryClient()
  const display = useIncognitoDisplay()
  const nameId = useId()
  const action = useMutation({
    mutationFn: (run: (id: number) => Promise<void>) => run(share.id),
    onSettled: () => queryClient.invalidateQueries({ queryKey: sharesQueryKey }),
    onError: (error) => toast.error(display.text(error.message)),
  })
  const percent = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language, { style: "percent", maximumFractionDigits: 0 })
  const outside = share.tracker === "outside"
  const active = share.state === "active"
  const end = share.endedAt
    ? t("tab.endedAt", { date: formatDate(new Date(share.endedAt)) })
    : t("tab.endsAt", { date: formatDate(new Date(share.expiresAt)) })

  const step = (label: string, icon: LucideIcon, run: (id: number) => Promise<void>, destructive = false): RowAction => ({
    label,
    icon,
    onSelect: () => action.mutate(run),
    destructive,
    pending: action.isPending && action.variables === run,
    disabled: action.isPending,
  })
  const actions: RowAction[] = [
    { label: t("dialog.result.torrent"), icon: Download, href: shareTorrentUrl(share.id) },
    ...(active && share.firstDownloaderOnly && !outside ? [step(t("tab.unlock"), LockOpen, unlockShare)] : []),
    ...(active ? [step(t("tab.stop"), CircleStop, stopShare, true)] : []),
    ...(share.state === "ended" ? [step(t("tab.remove"), Trash2, removeShare, true)] : []),
  ]
  const progress = (
    <div className="mt-1.5 w-full max-w-48 space-y-1">
      <Progress value={share.progress * 100} aria-label={t("tab.progressLabel")} className="h-1.5" />
      <div className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">
        {t("tab.progress", { percent: percent.format(share.progress) })}
        <span aria-hidden="true"> · </span>
        {t("tab.sent", { size: formatBytes(share.uploaded) })}
      </div>
    </div>
  )

  return (
    <TableRow data-share-id={share.id}>
      <TableCell className="w-full max-w-0 pl-6 align-top whitespace-normal max-sm:pl-4">
        <div id={nameId} className="font-medium"><TorrentName name={display.name(share)} className="pointer-fine:line-clamp-2" /></div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
          <span>{instanceName}</span>
          <span aria-hidden="true">·</span>
          <span>{end}</span>
        </div>
        <div className="mt-1 text-xs @lg:hidden">
          <StateLabel state={share.state} />
          {progress}
        </div>
        {!active && share.endReason && (
          <p className="mt-1 text-xs text-muted-foreground [overflow-wrap:anywhere]">
            {translatedEndReasons.has(share.endReasonCode)
              ? t(`tab.endReason.${share.endReasonCode}`)
              : display.text(share.endReason)}
          </p>
        )}
        <div className="mt-2 text-xs">
          <div className="text-muted-foreground">{outside ? t("tab.connectedPeers") : t("tab.announced")}</div>
          {share.peers.length === 0 ? (
            <div className="text-muted-foreground">{t("tab.noPeers")}</div>
          ) : (
            <ul className="mt-0.5 space-y-0.5">
              {share.peers.map((peer) => (
                <li key={peer.address} className="flex gap-3 font-mono">
                  <span className="[overflow-wrap:anywhere]">{display.peerAddress(peer.address)}</span>
                  <span className="text-muted-foreground tabular-nums">{percent.format(peer.progress)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </TableCell>
      <TableCell className="hidden align-top @lg:table-cell">
        <StateLabel state={share.state} className="text-sm" />
        {progress}
      </TableCell>
      <TableCell className="pr-6 max-sm:pr-4">
        <RowActions actions={actions} describedBy={nameId} />
      </TableCell>
    </TableRow>
  )
}

function StateLabel({ state, className }: { state: ShareState; className?: string }) {
  const { t } = useTranslation("shares")
  const { icon: Icon, className: iconClass } = stateIcons[state] ?? stateIcons.ended
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-foreground", className)}>
      <Icon aria-hidden="true" className={cn("size-3.5 shrink-0", iconClass)} />
      {t([`tab.state.${state}`, "tab.state.unknown"])}
    </span>
  )
}
