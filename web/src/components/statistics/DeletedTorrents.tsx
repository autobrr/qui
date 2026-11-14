/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"
import { formatBytes, formatDuration } from "@/lib/utils"
import type { TransferHistoryCauseFilter, TransferHistoryCauseKind, TransferHistoryDeleteEvent, TransferHistoryDeletesAnswer, TransferHistoryInstance } from "@/types"
import { useQueries, type UseQueryResult } from "@tanstack/react-query"
import { ArchiveRestore, KeyRound, Loader2, Unplug, User, Zap } from "lucide-react"
import { useCallback, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

export const DELETES_PAGE = 50

const FILTERS = [
  { cause: "", labelKey: "deletes.filters.all" },
  { cause: "automation", labelKey: "deletes.filters.automation" },
  { cause: "user", labelKey: "deletes.filters.user" },
  { cause: "other", labelKey: "deletes.filters.other" },
] as const satisfies readonly { cause: TransferHistoryCauseFilter; labelKey: string }[]

const CAUSES = {
  automation: { icon: Zap, labelKey: "deletes.causes.automation" },
  user: { icon: User, labelKey: "deletes.causes.user" },
  backupRestore: { icon: ArchiveRestore, labelKey: "deletes.causes.backupRestore" },
  clientProxy: { icon: KeyRound, labelKey: "deletes.causes.clientProxy" },
  outside: { icon: Unplug, labelKey: "deletes.causes.outside" },
} as const satisfies Record<TransferHistoryCauseKind, { icon: typeof Zap; labelKey: string }>

interface InstanceDelete extends TransferHistoryDeleteEvent {
  instanceId: number
  timeMs: number
}

interface DeletesPages {
  // The newest deletes of all instances, of each page that each instance loaded. Undefined until the first page loaded.
  deletes?: InstanceDelete[]
  counts?: TransferHistoryDeletesAnswer["counts"]
  error?: Error
  loadingMore: boolean
  // Loads each page again that failed.
  retry: () => void
}

/**
 * Reads the first pages of deletes of each instance. The newest pages * DELETES_PAGE deletes of all instances are among
 * the first pages of each instance, so the merge of those pages holds them in full.
 */
function useDeletesPages(instances: TransferHistoryInstance[], period: { from?: Date; to: Date }, cause: TransferHistoryCauseFilter, pages: number): DeletesPages {
  const combine = useCallback((results: UseQueryResult<TransferHistoryDeletesAnswer>[]): DeletesPages => {
    const answer = (instance: number, page: number) => results[instance * pages + page]?.data
    let loaded = 0
    while (loaded < pages && instances.every((_, index) => answer(index, loaded))) {
      loaded++
    }
    // A delete that lands between two page loads moves the offsets, so a row can come back on the next page.
    const unique = new Map(instances.flatMap((instance, index) => Array.from({ length: loaded }, (_, page) => answer(index, page)!.deletes.map(event => [
      `${instance.instanceId}:${event.hash}:${event.time}`,
      { ...event, instanceId: instance.instanceId, timeMs: Date.parse(event.time) },
    ] as const)).flat()))
    const deletes = [...unique.values()].sort((a, b) => b.timeMs - a.timeMs)
    return {
      deletes: loaded > 0 ? deletes.slice(0, loaded * DELETES_PAGE) : undefined,
      counts: loaded > 0 ? instances.reduce((sum, _, index) => {
        const counts = answer(index, 0)!.counts
        return { automation: sum.automation + counts.automation, user: sum.user + counts.user, other: sum.other + counts.other }
      }, { automation: 0, user: 0, other: 0 }) : undefined,
      error: results.find(result => result.error)?.error ?? undefined,
      loadingMore: loaded > 0 && results.some(result => result.isFetching),
      retry: () => results.filter(result => result.error).forEach(result => void result.refetch()),
    }
  }, [instances, pages])
  return useQueries({
    queries: instances.flatMap(instance => Array.from({ length: pages }, (_, page) => ({
      queryKey: ["transfer-history", "deletes", instance.instanceId, cause, period.from?.toISOString(), period.to.toISOString(), page],
      queryFn: () => api.getTransferHistoryDeletes(instance.instanceId, { from: period.from, to: period.to, cause, limit: DELETES_PAGE, offset: page * DELETES_PAGE }),
    }))),
    combine,
  })
}

interface DeletedTorrentsProps {
  // A new scope, another period choice or instance selection, starts again at the first page.
  scope: string
  instances: TransferHistoryInstance[]
  period: { from?: Date; to: Date }
  displayGroup: (group: string) => string
}

/** The deleted torrents of the period with the newest first, with who deleted each one and the numbers it ended with. */
export function DeletedTorrents({ scope, instances, period, displayGroup }: DeletedTorrentsProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { formatISOTimestamp } = useDateTimeFormatters()
  const [incognitoMode] = useIncognitoMode()
  const [cause, setCause] = useState<TransferHistoryCauseFilter>("")
  const [paging, setPaging] = useState({ scope, cause, pages: 1 })
  const pages = paging.scope === scope && paging.cause === cause ? paging.pages : 1
  const loaded = useDeletesPages(instances, period, cause, pages)
  // A preset period moves forward each 5 minutes. The rows of the same view stay until the rows of the new period load.
  const view = `${scope}:${cause}`
  const [kept, setKept] = useState<{ view: string; pages: DeletesPages }>()
  if (loaded.deletes && kept?.pages !== loaded) {
    setKept({ view, pages: loaded })
  }
  const { deletes, counts, error, loadingMore, retry } = !loaded.deletes && !loaded.error && kept?.view === view ? kept.pages : loaded
  const instanceNames = instances.length > 1 ? new Map(instances.map(instance => [instance.instanceId, instance.name])) : undefined
  const formatCount = useMemo(() => new Intl.NumberFormat(locale), [locale])

  const countOf = (filter: TransferHistoryCauseFilter) => counts && (filter === "" ? counts.automation + counts.user + counts.other : counts[filter])
  const total = countOf(cause)

  return (
    <>
      <div className="overflow-x-auto px-6 pb-4">
        <Tabs value={cause} onValueChange={value => setCause(value as TransferHistoryCauseFilter)}>
          <TabsList>
            {FILTERS.map(filter => (
              <TabsTrigger key={filter.cause} value={filter.cause} className="gap-1.5 px-3">
                {t(filter.labelKey)}
                {counts && <span className="tabular-nums text-muted-foreground">{formatCount.format(countOf(filter.cause)!)}</span>}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      {error && !deletes ? (
        <div className="flex h-24 items-center justify-center border-t px-6 text-sm text-destructive">{t("errors.load", { message: error.message })}</div>
      ) : !deletes ? (
        <div className="flex h-24 items-center justify-center border-t"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
      ) : deletes.length === 0 ? (
        <div className="flex h-24 items-center justify-center border-t px-6 text-sm text-muted-foreground">{t("deletes.empty")}</div>
      ) : (
        <div className="@container border-t">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="hidden pl-6 @xl:table-cell">{t("deletes.columns.time")}</TableHead>
                <TableHead className="w-full pl-6 @xl:pl-2">{t("topTorrents.columns.torrent")}</TableHead>
                <TableHead className="hidden @2xl:table-cell">{t("table.columns.tracker")}</TableHead>
                <TableHead className="hidden @xl:table-cell">{t("deletes.columns.cause")}</TableHead>
                <TableHead className="text-right">{t("deletes.columns.uploaded")}</TableHead>
                <TableHead className="hidden text-right @2xl:table-cell">{t("deletes.columns.ratio")}</TableHead>
                <TableHead className="hidden pr-6 text-right @2xl:table-cell">{t("deletes.columns.seeded")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deletes.map(event => {
                const name = incognitoMode ? getLinuxIsoName(event.hash) : event.name
                const cause = <Cause kind={event.cause.kind} name={event.cause.name} />
                const ratio = event.ratio === undefined ? "–" : event.ratio.toFixed(2)
                const seeded = event.seedingSeconds === undefined ? "–" : formatDuration(event.seedingSeconds)
                const time = formatISOTimestamp(event.time)
                return (
                  <TableRow key={`${event.instanceId}:${event.hash}:${event.time}`}>
                    <TableCell className="hidden whitespace-nowrap pl-6 align-top text-muted-foreground @xl:table-cell">{time}</TableCell>
                    <TableCell className="w-full max-w-0 pl-6 font-medium @xl:pl-2">
                      <span className="line-clamp-2 break-all whitespace-normal @2xl:line-clamp-1" title={name}>{name}</span>
                      <div className="truncate text-xs font-normal text-muted-foreground @2xl:hidden">
                        {displayGroup(event.trackerGroup)}{instanceNames && ` · ${instanceNames.get(event.instanceId)}`}
                      </div>
                      <div className="text-xs font-normal @xl:hidden">{cause}</div>
                      <div className="truncate text-xs font-normal text-muted-foreground tabular-nums @xl:hidden">{time}</div>
                      <div className="truncate text-xs font-normal text-muted-foreground tabular-nums @2xl:hidden">{t("deletes.final", { ratio, seeded })}</div>
                      {instanceNames && <div className="hidden truncate text-xs font-normal text-muted-foreground @2xl:block">{instanceNames.get(event.instanceId)}</div>}
                    </TableCell>
                    <TableCell className="hidden max-w-48 truncate align-top text-muted-foreground @2xl:table-cell">{displayGroup(event.trackerGroup)}</TableCell>
                    <TableCell className="hidden max-w-64 align-top @xl:table-cell">{cause}</TableCell>
                    <TableCell className="text-right align-top tabular-nums">{formatBytes(event.uploaded)}</TableCell>
                    <TableCell className="hidden text-right align-top tabular-nums @2xl:table-cell">{ratio}</TableCell>
                    <TableCell className="hidden pr-6 text-right align-top tabular-nums @2xl:table-cell">{seeded}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t px-6 pt-3 text-xs text-muted-foreground">
            <span>{t("deletes.shown", { shown: formatCount.format(deletes.length), total: formatCount.format(total ?? deletes.length) })}</span>
            {error && <span className="text-destructive">{t("errors.load", { message: error.message })}</span>}
            {total !== undefined && deletes.length < total && (
              <Button variant="outline" size="sm" disabled={loadingMore} onClick={() => error ? retry() : setPaging({ scope, cause, pages: pages + 1 })}>
                {loadingMore && <Loader2 className="size-3.5 animate-spin" />}
                {t("deletes.more")}
              </Button>
            )}
          </div>
        </div>
      )}
    </>
  )
}

function Cause({ kind, name }: { kind: TransferHistoryCauseKind; name?: string }) {
  const { t } = useTranslation("statistics")
  const { icon: Icon, labelKey } = CAUSES[kind]
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="shrink-0 whitespace-nowrap">{t(labelKey)}</span>
      {name && <span className="min-w-0 truncate text-muted-foreground" title={name}>· {name}</span>}
    </span>
  )
}
