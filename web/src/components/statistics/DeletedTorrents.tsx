/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TorrentName } from "@/components/statistics/TopTorrents"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { api } from "@/lib/api"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"
import { cn, formatBytes, formatDuration } from "@/lib/utils"
import type { TransferHistoryCauseFilter, TransferHistoryCauseKind, TransferHistoryDeleteEvent, TransferHistoryDeletesAnswer, TransferHistoryInstance } from "@/types"
import { useQueries, type UseQueryResult } from "@tanstack/react-query"
import { ArchiveRestore, ChevronDown, ChevronRight, KeyRound, Loader2, Unplug, User, Zap } from "lucide-react"
import { useCallback, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

export const DELETES_PAGE = 50

// The number of rows that a phone shows at first, and adds for each Show more.
const PHONE_ROWS = 10

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
  const narrow = useIsPhone()
  const [cause, setCause] = useState<TransferHistoryCauseFilter>("")
  // The phone rows that the user opened with a tap, to read the whole name and cause, as a touch screen cannot show a title.
  const [fullNames, setFullNames] = useState<ReadonlySet<string>>(new Set())
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
  const [phoneRows, setPhoneRows] = useState({ view, count: PHONE_ROWS })
  const limit = narrow ? phoneRows.view === view ? phoneRows.count : PHONE_ROWS : Infinity
  const rows = deletes?.slice(0, limit)
  const showMore = () => {
    const loaded = deletes?.length ?? 0
    const next = (rows?.length ?? 0) + (narrow ? PHONE_ROWS : DELETES_PAGE)
    if (narrow) {
      setPhoneRows({ view, count: next })
    }
    // A phone first shows the rows that it loaded, also when a later page failed.
    if (next <= loaded) {
      return
    }
    if (error) {
      retry()
    } else if (total !== undefined && loaded < total) {
      setPaging({ scope, cause, pages: pages + 1 })
    }
  }

  return (
    <>
      <div className="overflow-x-auto px-4 pb-4 sm:px-6">
        <Tabs value={cause} onValueChange={value => setCause(value as TransferHistoryCauseFilter)}>
          <TabsList className="max-sm:w-full">
            {FILTERS.map(filter => (
              <TabsTrigger key={filter.cause} value={filter.cause} className="gap-1.5 px-3 max-sm:gap-1 max-sm:px-1.5 max-sm:text-xs">
                {t(filter.labelKey)}
                {counts && <span className="tabular-nums text-muted-foreground">{formatCount.format(countOf(filter.cause)!)}</span>}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      {error && !rows ? (
        <div className="flex h-24 items-center justify-center border-t px-4 text-sm text-destructive sm:px-6">{t("errors.load", { message: error.message })}</div>
      ) : !rows ? (
        <div className="flex h-24 items-center justify-center border-t"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
      ) : rows.length === 0 ? (
        <div className="flex h-24 items-center justify-center border-t px-4 text-sm text-muted-foreground sm:px-6">{t("deletes.empty")}</div>
      ) : (
        <div className="@container border-t">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="hidden pl-6 @xl:table-cell">{t("deletes.columns.time")}</TableHead>
                <TableHead className="w-full pl-4 sm:pl-6 @xl:pl-2">{t("topTorrents.columns.torrent")}</TableHead>
                <TableHead className="hidden @2xl:table-cell">{t("table.columns.tracker")}</TableHead>
                <TableHead className="hidden @xl:table-cell">{t("deletes.columns.cause")}</TableHead>
                <TableHead className="pr-4 text-right sm:pr-6 @2xl:pr-2">{t("deletes.columns.uploaded")}</TableHead>
                <TableHead className="hidden text-right @2xl:table-cell">{t("deletes.columns.ratio")}</TableHead>
                <TableHead className="hidden pr-6 text-right @2xl:table-cell">{t("deletes.columns.seeded")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map(event => {
                const name = incognitoMode ? getLinuxIsoName(event.hash) : event.name
                const key = `${event.instanceId}:${event.hash}:${event.time}`
                const full = fullNames.has(key)
                const cause = <Cause kind={event.cause.kind} name={event.cause.name} />
                const ratio = event.ratio === undefined ? "–" : event.ratio.toFixed(2)
                const seeded = event.seedingSeconds === undefined ? "–" : formatDuration(event.seedingSeconds)
                const time = formatISOTimestamp(event.time)
                const toggle = () => setFullNames(names => {
                  const next = new Set(names)
                  if (full) {
                    next.delete(key)
                  } else {
                    next.add(key)
                  }
                  return next
                })
                const line = narrow ? "pl-5.5" : undefined
                return (
                  <TableRow key={key} className={cn(narrow && "cursor-pointer", full && "bg-muted hover:bg-muted")} onClick={narrow ? toggle : undefined}>
                    <TableCell className="hidden whitespace-nowrap pl-6 align-top text-muted-foreground @xl:table-cell">{time}</TableCell>
                    <TableCell className="w-full max-w-0 pl-4 font-medium sm:pl-6 @xl:pl-2">
                      {narrow ? (
                        <button type="button" aria-expanded={full} className="flex w-full min-w-0 items-start gap-2 text-left">
                          {full ? <ChevronDown className="mt-1 size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="mt-1 size-3.5 shrink-0 text-muted-foreground" />}
                          <TorrentName name={name} className={cn("min-w-0", !full && "line-clamp-2")} />
                        </button>
                      ) : <TorrentName name={name} className="line-clamp-2 @2xl:line-clamp-1" />}
                      <div className={cn("truncate text-xs font-normal text-muted-foreground @2xl:hidden", line)}>
                        {displayGroup(event.trackerGroup)}{instanceNames && ` · ${instanceNames.get(event.instanceId)}`}
                      </div>
                      <div className={cn("text-xs font-normal @xl:hidden", line)}><Cause kind={event.cause.kind} name={event.cause.name} wrap={full} /></div>
                      <div className={cn("text-xs font-normal text-muted-foreground tabular-nums @2xl:hidden", line)}>{t("deletes.final", { ratio, seeded })}</div>
                      {instanceNames && <div className="hidden truncate text-xs font-normal text-muted-foreground @2xl:block">{instanceNames.get(event.instanceId)}</div>}
                    </TableCell>
                    <TableCell className="hidden max-w-48 truncate align-top text-muted-foreground @2xl:table-cell">{displayGroup(event.trackerGroup)}</TableCell>
                    <TableCell className="hidden max-w-64 align-top @xl:table-cell">{cause}</TableCell>
                    <TableCell className="pr-4 text-right align-top tabular-nums sm:pr-6 @2xl:pr-2">
                      {formatBytes(event.uploaded)}
                      <div className="ml-auto max-w-24 whitespace-normal text-xs text-muted-foreground @xl:hidden">{time}</div>
                    </TableCell>
                    <TableCell className="hidden text-right align-top tabular-nums @2xl:table-cell">{ratio}</TableCell>
                    <TableCell className="hidden pr-6 text-right align-top tabular-nums @2xl:table-cell">{seeded}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t px-4 pt-3 text-xs text-muted-foreground sm:px-6">
            <span>{t("deletes.shown", { shown: formatCount.format(rows.length), total: formatCount.format(total ?? rows.length) })}</span>
            {error && <span className="text-destructive">{t("errors.load", { message: error.message })}</span>}
            {total !== undefined && rows.length < total && (
              <Button variant="outline" size="sm" disabled={loadingMore} onClick={showMore}>
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

// An open phone row wraps the cause, as a touch screen cannot show the title of a cut name.
function Cause({ kind, name, wrap = false }: { kind: TransferHistoryCauseKind; name?: string; wrap?: boolean }) {
  const { t } = useTranslation("statistics")
  const { icon: Icon, labelKey } = CAUSES[kind]
  if (wrap) {
    return (
      <span className="block whitespace-normal [overflow-wrap:anywhere]">
        <Icon className="mr-1.5 inline size-3.5 align-[-2px] text-muted-foreground" />
        {t(labelKey)}
        {name && <span className="text-muted-foreground"> · {name}</span>}
      </span>
    )
  }
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="shrink-0 whitespace-nowrap">{t(labelKey)}</span>
      {name && <span className="min-w-0 truncate text-muted-foreground" title={name}>· {name}</span>}
    </span>
  )
}
