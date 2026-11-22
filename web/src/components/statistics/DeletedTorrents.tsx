/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { PAGE_ROWS, Pager } from "@/components/statistics/Pager"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TorrentName } from "@/components/ui/torrent-name"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { useTrackerGroupIcon } from "@/hooks/useTrackerGroupName"
import { api } from "@/lib/api"
import { HISTORY_WRITE_MS } from "@/lib/statistics"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"
import { cn, formatBytes, formatDuration } from "@/lib/utils"
import type { TransferHistoryCauseFilter, TransferHistoryCauseKind, TransferHistoryDeleteEvent, TransferHistoryDeletesAnswer, TransferHistoryInstance } from "@/types"
import { useQueries, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { ArchiveRestore, ChevronDown, ChevronRight, KeyRound, Loader2, Unplug, User, Zap } from "lucide-react"
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { flushSync } from "react-dom"
import { useTranslation } from "react-i18next"

export const DELETES_PAGE = 50

// The number of rows that a phone shows at first, and adds for each Show more.
const PHONE_ROWS = 5
const PHONE_MORE = 10

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

function deletesQuery(instanceId: number, period: { from?: Date; to: Date }, cause: TransferHistoryCauseFilter, page: number) {
  return {
    queryKey: ["transfer-history", "deletes", instanceId, cause, period.from?.toISOString(), period.to.toISOString(), page],
    queryFn: () => api.getTransferHistoryDeletes(instanceId, { from: period.from, to: period.to, cause, limit: DELETES_PAGE, offset: page * DELETES_PAGE }),
  }
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
    queries: instances.flatMap(instance => Array.from({ length: pages }, (_, page) => deletesQuery(instance.instanceId, period, cause, page))),
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

/**
 * The deleted torrents of the period with the newest first, with who deleted each one and the numbers it ended with.
 * A desktop shows the rows in pages. A phone shows the first rows, and Show more adds rows.
 */
export function DeletedTorrents({ scope, instances, period, displayGroup }: DeletedTorrentsProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { formatISOTimestamp } = useDateTimeFormatters()
  const [incognitoMode] = useIncognitoMode()
  const groupIcon = useTrackerGroupIcon()
  const narrow = useIsPhone()
  const [cause, setCause] = useState<TransferHistoryCauseFilter>("")
  // The phone rows that the user opened with a tap, to read the whole name and cause, as a touch screen cannot show a title.
  const [fullNames, setFullNames] = useState<ReadonlySet<string>>(new Set())
  // The phone rows whose name or cause the row cuts. Only such a row has more to show when it opens.
  const [cutRows, setCutRows] = useState<ReadonlySet<string>>(new Set())
  const [paging, setPaging] = useState({ scope, cause, pages: 1 })
  const pages = paging.scope === scope && paging.cause === cause ? paging.pages : 1
  const loaded = useDeletesPages(instances, period, cause, pages)
  const [kept, setKept] = useState<{ scope: string; cause: TransferHistoryCauseFilter; pages: DeletesPages }>()
  if (loaded.deletes && (kept?.pages !== loaded || kept.cause !== cause || kept.scope !== scope)) {
    setKept({ scope, cause, pages: loaded })
  }
  // A preset period moves forward each 5 minutes, and another filter can still load. The rows of the same scope stay
  // until the new rows load, dimmed when they are of another filter.
  const keep = !loaded.deletes && !loaded.error && kept?.scope === scope
  const shownCause = keep ? kept.cause : cause
  const shownView = `${scope}:${shownCause}`
  const stale = shownCause !== cause
  const { deletes, counts, error, loadingMore, retry } = keep ? kept.pages : loaded
  const instanceNames = instances.length > 1 ? new Map(instances.map(instance => [instance.instanceId, instance.name])) : undefined
  const formatCount = useMemo(() => new Intl.NumberFormat(locale), [locale])

  const countOf = (filter: TransferHistoryCauseFilter) => counts && (filter === "" ? counts.automation + counts.user + counts.other : counts[filter])
  const total = countOf(shownCause)
  const [phoneRows, setPhoneRows] = useState({ view: shownView, count: PHONE_ROWS })
  if (phoneRows.view !== shownView) {
    setPhoneRows({ view: shownView, count: PHONE_ROWS })
  }
  const [paged, setPaged] = useState({ view: shownView, page: 0 })
  if (paged.view !== shownView) {
    setPaged({ view: shownView, page: 0 })
  }
  const wantedPage = paged.view === shownView ? paged.page : 0
  // A desktop stays on its page until the rows of the next page load.
  const page = Math.min(wantedPage, Math.max(0, Math.ceil((deletes?.length ?? 0) / PAGE_ROWS) - 1))
  const rows = narrow
    ? deletes?.slice(0, phoneRows.view === shownView ? phoneRows.count : PHONE_ROWS)
    : deletes?.slice(page * PAGE_ROWS, (page + 1) * PAGE_ROWS)
  const footer = useRef<HTMLDivElement>(null)
  const next = (rows?.length ?? 0) + PHONE_MORE
  // A background reload keeps the loaded rows, so only a step that needs a new page of the server waits for it.
  const waiting = loadingMore && (narrow ? next > (deletes?.length ?? 0) : wantedPage > page)
  // Loads the rows up to the count when the server holds more than the loaded rows.
  const loadTo = (count: number) => {
    const loaded = deletes?.length ?? 0
    // The rows that are loaded show first, also when a later page failed.
    if (count <= loaded) {
      return
    }
    if (error) {
      retry()
    } else if (total !== undefined && loaded < total) {
      setPaging({ scope, cause, pages: pages + 1 })
    }
  }
  const showMore = () => {
    setPhoneRows({ view: shownView, count: next })
    loadTo(next)
  }

  // The first page of the other filters loads after the first filter, so that a switch shows its rows at once.
  const queryClient = useQueryClient()
  const preloaded = useRef<string>(undefined)
  const firstLoaded = loaded.deletes !== undefined
  useEffect(() => {
    if (!firstLoaded || preloaded.current === scope) {
      return
    }
    preloaded.current = scope
    for (const filter of FILTERS) {
      for (const instance of filter.cause === cause ? [] : instances) {
        void queryClient.prefetchQuery({ ...deletesQuery(instance.instanceId, period, filter.cause, 0), staleTime: HISTORY_WRITE_MS })
      }
    }
  }, [firstLoaded, scope, cause, instances, period, queryClient])

  // Deleted torrents ends the page. A shorter list after a filter switch or a page switch keeps the height of the last
  // list, so that the page does not end higher and move the tabs away from the pointer. The space goes when it is below the screen, or when
  // the rows of the new filter show and fill it.
  const body = useRef<HTMLDivElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const [held, setHeld] = useState<{ scope: string; height: number }>()
  const minHeight = held?.scope === scope ? held.height : undefined
  const holdHeight = () => setHeld(body.current ? { scope, height: body.current.offsetHeight } : undefined)
  useEffect(() => {
    // The rows of the last filter or of the last page still show, and they fill the space by themselves.
    if (minHeight === undefined || stale || waiting) {
      return
    }
    const release = () => {
      const node = content.current
      if (!node || node.getBoundingClientRect().bottom >= window.innerHeight || node.offsetHeight >= minHeight) {
        setHeld(undefined)
      }
    }
    release()
    window.addEventListener("scroll", release, { capture: true, passive: true })
    window.addEventListener("resize", release)
    return () => {
      window.removeEventListener("scroll", release, { capture: true })
      window.removeEventListener("resize", release)
    }
  }, [minHeight, stale, waiting])

  const table = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const node = table.current
    if (!narrow || !node) {
      return
    }
    const measure = () => {
      const cut = new Set<string>()
      for (const row of node.querySelectorAll<HTMLElement>("tr[data-delete]")) {
        if ([...row.querySelectorAll("span[data-slot=truncated-text]")].some(span => span.scrollHeight > span.clientHeight + 1 || span.scrollWidth > span.clientWidth + 1)) {
          cut.add(row.dataset.delete!)
        }
      }
      setCutRows(last => last.size === cut.size && [...cut].every(key => last.has(key)) ? last : cut)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [narrow, rows, fullNames])

  return (
    <>
      <div className="overflow-x-auto px-4 pb-4 sm:px-6">
        <Tabs
          value={cause}
          onValueChange={value => {
            holdHeight()
            setCause(value as TransferHistoryCauseFilter)
          }}
        >
          <TabsList className="max-sm:w-max max-sm:min-w-full">
            {FILTERS.map(filter => (
              <TabsTrigger key={filter.cause} value={filter.cause} className="gap-1.5 px-3 max-sm:gap-1 max-sm:px-1.5 max-sm:text-xs">
                {t(filter.labelKey)}
                {counts && <span className="tabular-nums text-muted-foreground">{formatCount.format(countOf(filter.cause)!)}</span>}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      <div ref={body} style={{ minHeight }}>
        <div ref={content} aria-busy={stale} className={cn("transition-opacity", stale && "pointer-events-none opacity-60")}>
          {error && !rows ? (
            <div className="flex h-24 items-center justify-center border-t px-4 text-sm text-destructive sm:px-6">{t("errors.load", { message: error.message })}</div>
          ) : !rows ? (
            <div className="flex h-24 items-center justify-center border-t"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
          ) : rows.length === 0 ? (
            <div className="flex h-24 items-center justify-center border-t px-4 text-sm text-muted-foreground sm:px-6">{t("deletes.empty")}</div>
          ) : (
            <div ref={table} className="@container border-t">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="hidden pl-6 @xl:table-cell">{t("deletes.columns.time")}</TableHead>
                    <TableHead className="w-full pl-4 sm:pl-6 @xl:pl-2">{t("topTorrents.columns.torrent")}</TableHead>
                    <TableHead className="hidden @4xl:table-cell">{t("table.columns.tracker")}</TableHead>
                    <TableHead className="hidden @2xl:table-cell">{t("deletes.columns.cause")}</TableHead>
                    <TableHead className="pr-4 text-right sm:pr-6 @4xl:pr-2">{t("deletes.columns.uploaded")}</TableHead>
                    <TableHead className="hidden text-right @4xl:table-cell">{t("deletes.columns.ratio")}</TableHead>
                    <TableHead className="hidden pr-6 text-right @4xl:table-cell">{t("deletes.columns.seeded")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map(event => {
                    const name = incognitoMode ? getLinuxIsoName(event.hash) : event.name
                    const key = `${event.instanceId}:${event.hash}:${event.time}`
                    const full = fullNames.has(key)
                    const cause = <Cause kind={event.cause.kind} name={event.cause.name} wrap={full} openOnTap={!narrow} />
                    const ratio = event.ratio === undefined ? "–" : event.ratio.toFixed(2)
                    const seeded = event.seedingSeconds === undefined ? "–" : formatDuration(event.seedingSeconds)
                    const time = formatISOTimestamp(event.time)
                    const tracker = `${displayGroup(event.trackerGroup)}${instanceNames ? ` · ${instanceNames.get(event.instanceId)}` : ""}`
                    const icon = groupIcon(event.trackerGroup)
                    const trackerIcon = icon ? <TrackerIconImage {...icon} /> : <span className="size-4 shrink-0" />
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
                    // An open row stays open until a tap closes it. A closed row opens only when it cuts its name or cause.
                    const opens = narrow && (full || cutRows.has(key))
                    return (
                      <TableRow key={key} data-delete={key} className={cn(opens && "cursor-pointer", full && "bg-muted hover:bg-muted")} onClick={opens ? toggle : undefined}>
                        <TableCell className="hidden whitespace-nowrap pl-6 align-top text-muted-foreground @xl:table-cell">{time}</TableCell>
                        <TableCell className="w-full max-w-0 pl-4 font-medium sm:pl-6 @xl:pl-2">
                          {opens ? (
                            <button type="button" aria-expanded={full} className="flex w-full min-w-0 items-start gap-2 text-left">
                              {full ? <ChevronDown className="mt-1 size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="mt-1 size-3.5 shrink-0 text-muted-foreground" />}
                              <TorrentName name={name} openOnTap={false} className={cn("min-w-0", !full && "line-clamp-2")} />
                            </button>
                          ) : narrow ? (
                            <div className="flex w-full min-w-0 items-start gap-2">
                              {/* The space of the arrow keeps the name in line with the names of the rows that open. */}
                              <span className="mt-1 size-3.5 shrink-0" aria-hidden />
                              <TorrentName name={name} className="min-w-0 line-clamp-2" />
                            </div>
                          ) : <TorrentName name={name} className="line-clamp-2 @4xl:line-clamp-1" />}
                          <div className={cn("flex min-w-0 items-center gap-1.5 text-xs font-normal text-muted-foreground @4xl:hidden", line)}>{trackerIcon}<span className="truncate">{tracker}</span></div>
                          <div className={cn("text-xs font-normal @2xl:hidden", line)}><Cause kind={event.cause.kind} name={event.cause.name} wrap={full} openOnTap={!narrow} /></div>
                          <div className={cn("text-xs font-normal text-muted-foreground tabular-nums @4xl:hidden", line)}>{t("deletes.final", { ratio, seeded })}</div>
                        </TableCell>
                        <TableCell className="hidden max-w-64 align-top text-muted-foreground @4xl:table-cell"><span className="flex items-center gap-1.5">{trackerIcon}<TruncatedText>{tracker}</TruncatedText></span></TableCell>
                        <TableCell className="hidden max-w-64 align-top @2xl:table-cell">{cause}</TableCell>
                        <TableCell className="pr-4 text-right align-top tabular-nums sm:pr-6 @4xl:pr-2">
                          {formatBytes(event.uploaded)}
                          <div className="ml-auto max-w-24 whitespace-normal text-xs text-muted-foreground @xl:hidden">{time}</div>
                        </TableCell>
                        <TableCell className="hidden text-right align-top tabular-nums @4xl:table-cell">{ratio}</TableCell>
                        <TableCell className="hidden pr-6 text-right align-top tabular-nums @4xl:table-cell">{seeded}</TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
              {narrow ? (
                <div ref={footer} className="flex flex-wrap items-center justify-between gap-2 border-t px-4 pt-3 text-xs text-muted-foreground sm:px-6">
                  <span>{t("deletes.shown", { shown: formatCount.format(rows.length), total: formatCount.format(total ?? rows.length) })}</span>
                  {error && <span className="text-destructive">{t("errors.load", { message: error.message })}</span>}
                  <div className="flex gap-2">
                    {rows.length > PHONE_ROWS && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          flushSync(() => setPhoneRows({ view: shownView, count: PHONE_ROWS }))
                          // Safari keeps the scroll offset when the list above gets shorter, so the button moves out of view.
                          footer.current?.scrollIntoView({ block: "nearest" })
                        }}
                      >
                        {t("table.showFewer")}
                      </Button>
                    )}
                    {total !== undefined && rows.length < total && (
                      <Button variant="outline" size="sm" disabled={waiting} onClick={showMore}>
                        {waiting && <Loader2 className="size-3.5 animate-spin" />}
                        {t("deletes.more")}
                      </Button>
                    )}
                  </div>
                </div>
              ) : (
                <Pager
                  page={page}
                  total={total ?? rows.length}
                  waiting={waiting}
                  onPageChange={target => {
                    holdHeight()
                    setPaged({ view: shownView, page: target })
                    loadTo((target + 1) * PAGE_ROWS)
                  }}
                >
                  {error && <span className="text-destructive">{t("errors.load", { message: error.message })}</span>}
                </Pager>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  )
}

// An open phone row wraps the cause, as a tap on a cut name opens the row and not its tooltip.
function Cause({ kind, name, wrap = false, openOnTap }: { kind: TransferHistoryCauseKind; name?: string; wrap?: boolean; openOnTap: boolean }) {
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
      {name && <TruncatedText openOnTap={openOnTap} className="min-w-0 text-muted-foreground">· {name}</TruncatedText>}
    </span>
  )
}
