/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
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
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { crossSeedRoutesKey, useCrossSeedRoutes } from "@/hooks/useCrossSeedRoutes"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import type { CrossSeedRouteStatus } from "@/types"
import { useQueries } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"

/** The routed torrents under the rows, or null when one count is unknown (the Cross-seed instance is unavailable). */
export function countRoutedTorrents(rows: CrossSeedRouteStatus["rows"]): number | null {
  let total = 0
  for (const row of rows) {
    if (row.routedTorrents === null) return null
    total += row.routedTorrents
  }
  return total
}

interface RouteProtectionNoticeProps {
  sourceName: string
  crossSeedName: string
  /** Undefined while the count loads, null when it is unknown. */
  routedTorrents: number | null | undefined
  /** A route clear: the Cross-seed instance keeps the routed torrents. */
  clear?: boolean
}

/** Says how many routed torrents lose the Source instance's orphan scan protection (#3269). */
export function RouteProtectionNotice({ sourceName, crossSeedName, routedTorrents, clear }: RouteProtectionNoticeProps) {
  const { t } = useTranslation("crossseed")
  const names = { source: sourceName, crossSeed: crossSeedName }
  return (
    <div className="space-y-2 text-sm text-muted-foreground">
      <p>
        {routedTorrents === undefined? t("routeConfirm.counting"): routedTorrents === null? t("routeConfirm.countUnknown", names): t("routeConfirm.count", { ...names, count: routedTorrents })}
      </p>
      <p>{t("routeConfirm.orphanScan", names)}</p>
      {clear && <p>{t("routeConfirm.clear", names)}</p>}
    </div>
  )
}

/** Confirm before a change that stops the Source instance's orphan scan from protecting routed files. */
export function RouteProtectionConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  ...notice
}: RouteProtectionNoticeProps & { open: boolean; onOpenChange: (open: boolean) => void; onConfirm: () => void }) {
  const { t } = useTranslation("crossseed")
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("routeConfirm.title")}</AlertDialogTitle>
        </AlertDialogHeader>
        <AlertDialogDescription asChild>
          <div><RouteProtectionNotice {...notice} /></div>
        </AlertDialogDescription>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("routeConfirm.cancel")}</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm}>{t("routeConfirm.confirm")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

/**
 * For an instance delete dialog: names each Source instance that routes to the
 * instance and will add cross-seeds to itself again, with its routed torrents.
 * Renders nothing when the instance is not a Cross-seed instance.
 */
export function CrossSeedDeleteNotice({ instanceId }: { instanceId: number }) {
  const { t } = useTranslation("crossseed")
  const { instances } = useInstances()
  const { data: routes } = useCrossSeedRoutes()
  const crossSeedName = instances?.find(instance => instance.id === instanceId)?.name ?? ""
  const sources = (routes ?? [])
    .filter(route => route.crossSeedInstanceId === instanceId)
    .map(route => instances?.find(instance => instance.id === route.sourceInstanceId))
    .filter(instance => instance !== undefined)
  const reads = useQueries({
    queries: sources.map(source => ({
      queryKey: [...crossSeedRoutesKey, source.id, source.hardlinkBaseDir],
      queryFn: () => api.getCrossSeedRoute(source.id),
    })),
  })

  if (!sources.length) return null
  return (
    <div className="space-y-3">
      {sources.map((source, i) => {
        const read = reads[i]
        const routedTorrents = read?.isError? null: read?.data === undefined? undefined: read.data === null ? 0 : countRoutedTorrents(read.data.rows)
        return (
          <div key={source.id} className="space-y-2 rounded-md border border-border/70 p-3">
            <p className="text-sm font-medium">{t("routeConfirm.deleteSource", { source: source.name })}</p>
            <RouteProtectionNotice sourceName={source.name} crossSeedName={crossSeedName} routedTorrents={routedTorrents} />
          </div>
        )
      })}
    </div>
  )
}
