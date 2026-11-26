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
import { crossSeedRouteKey, useCrossSeedRoutes } from "@/hooks/useCrossSeedRoutes"
import { api } from "@/lib/api"
import type { CrossSeedRouteStatus } from "@/types"
import { useQueries, useQuery } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"

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
  routedTorrents: number | null | undefined
  clear?: boolean
}

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
          <AlertDialogAction onClick={onConfirm} disabled={notice.routedTorrents === undefined}>{t("routeConfirm.confirm")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

export function useCrossSeedDeleteCheck(instanceId: number, enabled: boolean) {
  const instanceList = useQuery({ queryKey: ["instances"], queryFn: () => api.getInstances(), enabled, staleTime: 0 })
  const instances = instanceList.data
  const list = useCrossSeedRoutes(enabled, 0)
  const crossSeedName = instances?.find(instance => instance.id === instanceId)?.name ?? ""
  const sources = (list.data ?? [])
    .filter(route => route.crossSeedInstanceId === instanceId)
    .map(route => instances?.find(instance => instance.id === route.sourceInstanceId))
    .filter(instance => instance !== undefined)
  const reads = useQueries({
    queries: sources.map(source => ({
      queryKey: crossSeedRouteKey(source),
      queryFn: () => api.getCrossSeedRoute(source.id),
      enabled,
      staleTime: 0,
    })),
  })
  const routesUnknown = list.isError || instanceList.isError
  const pending = enabled && !routesUnknown && (list.data === undefined || list.isFetching || instanceList.isFetching || reads.some(read => read.isPending || read.isFetching))
  return { crossSeedName, sources, reads, routesUnknown, pending }
}

export function CrossSeedDeleteNotice({ check }: { check: ReturnType<typeof useCrossSeedDeleteCheck> }) {
  const { t } = useTranslation("crossseed")
  const { crossSeedName, sources, reads, routesUnknown } = check
  if (routesUnknown) return <p className="text-sm text-muted-foreground">{t("routeConfirm.routesUnknown")}</p>
  if (!sources.length) return null
  return (
    <div className="max-h-[40vh] space-y-3 overflow-y-auto">
      {sources.map((source, i) => {
        const read = reads[i]
        const routedTorrents = read?.isError? null: read?.data === undefined || read.isFetching? undefined: read.data === null ? 0 : countRoutedTorrents(read.data.rows)
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
