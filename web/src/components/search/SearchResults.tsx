/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { SelectionBar } from "@/components/kit/SelectionBar"
import { ChecklistPicker } from "@/components/search/ChecklistPicker"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { SearchInput } from "@/components/ui/SearchInput"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SortIcon } from "@/components/ui/sort-icon"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TorrentName } from "@/components/ui/torrent-name"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { formatNarrowAge, formatRelativeTime } from "@/lib/dateTimeUtils"
import { FACETS, facetOptions, filterResults, resultKey, sortResults, tickResult, type CategoryName, type FacetKey, type FacetSelection, type ResultSort, type SortKey } from "@/lib/search-results"
import { cn, copyTextToClipboard, formatBytes } from "@/lib/utils"
import type { InstanceResponse, TorznabSearchCacheMetadata, TorznabSearchResult } from "@/types"
import type { TFunction } from "i18next"
import { AlertTriangle, ArrowDown, ArrowUp, ChevronDown, Copy, Database, Download, ExternalLink, MoreHorizontal, Plus, RefreshCw, X, Zap } from "lucide-react"
import { memo, useCallback, useDeferredValue, useId, useMemo, useRef, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const PAGE_SIZE = 50
const FIRST_ROWS = 25
const CHECKBOX_TAP = "relative pointer-coarse:after:absolute pointer-coarse:after:-inset-3"

export interface ResultActions {
  instances: InstanceResponse[]
  onAdd: (results: TorznabSearchResult[], instanceId: number) => void
  onDownload: (result: TorznabSearchResult) => void
  onOpenInfo: (result: TorznabSearchResult) => void
}

interface SearchResultsProps extends ResultActions {
  results: TorznabSearchResult[]
  cache?: TorznabSearchCacheMetadata | null
  partial: boolean
  categoryName: CategoryName
  sort: ResultSort
  onSortChange: (sort: ResultSort) => void
  onRefresh: () => void
  refreshReady: boolean
}

const SORT_OPTIONS: { value: string; sort: ResultSort; labelKey: string }[] = [
  { value: "seeders:desc", sort: { key: "seeders", desc: true }, labelKey: "sort.seeders" },
  { value: "published:desc", sort: { key: "published", desc: true }, labelKey: "sort.newest" },
  { value: "published:asc", sort: { key: "published", desc: false }, labelKey: "sort.oldest" },
  { value: "size:desc", sort: { key: "size", desc: true }, labelKey: "sort.largest" },
  { value: "size:asc", sort: { key: "size", desc: false }, labelKey: "sort.smallest" },
  { value: "title:asc", sort: { key: "title", desc: false }, labelKey: "sort.title" },
  { value: "indexer:asc", sort: { key: "indexer", desc: false }, labelKey: "sort.indexer" },
]

const FACET_LABEL_KEYS: Record<FacetKey, string> = {
  indexer: "table.indexer",
  category: "table.category",
  source: "table.source",
  collection: "facets.collection",
  group: "table.group",
  freeleech: "table.freeleech",
}

function discountLabel(t: TFunction, factor: number): string | undefined {
  if (factor <= 0) return t("results.free")
  if (factor < 1) return t("row.discount", { percent: Math.round(factor * 100) })
  return undefined
}

function facetLabel(t: TFunction, facet: FacetKey, value: string): string {
  if (facet !== "freeleech") return value
  return discountLabel(t, Number(value)) ?? t("facets.normal")
}

export function SearchResults({ results, cache, partial, categoryName, sort, onSortChange, onRefresh, refreshReady, ...actions }: SearchResultsProps) {
  const { t } = useTranslation("search")
  const { formatDate } = useDateTimeFormatters()
  const [text, setText] = useState("")
  const [facets, setFacets] = useState<FacetSelection>({})
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [picked, setPicked] = useState<ReadonlySet<string>>(() => new Set())
  const lastTicked = useRef<string | null>(null)

  const facetChoices = useMemo(
    () => FACETS.map(facet => ({ facet, options: facetOptions(results, facet, categoryName) })),
    [results, categoryName]
  )
  const filterText = useDeferredValue(text)
  const filterFacets = useDeferredValue(facets)
  const shown = useMemo(() => sortResults(filterResults(results, filterText, filterFacets, categoryName), sort), [results, filterText, filterFacets, categoryName, sort])
  const rows = useMemo(() => shown.slice(0, limit), [shown, limit])
  const mountedRows = useDeferredValue(rows, rows.slice(0, FIRST_ROWS))
  const filtering = text.trim() !== "" || Object.values(facets).some(values => values.length > 0)
  const selectable = actions.instances.length > 0
  const linkInRow = !useIsMobile()
  const pickedResults = useMemo(() => shown.filter(result => picked.has(resultKey(result))), [shown, picked])
  const pickableKeys = useMemo(() => rows.filter(row => row.downloadUrl).map(resultKey), [rows])

  const tick = useCallback((key: string, range: boolean) => {
    const anchor = range ? lastTicked.current : null
    setPicked(current => tickResult(current, pickableKeys, key, anchor))
    lastTicked.current = key
  }, [pickableKeys])
  const pageTick = pickedResults.length === 0 ? false : pickableKeys.every(key => picked.has(key)) ? true : "indeterminate"
  const tickPage = () => {
    lastTicked.current = null
    setPicked(pageTick === true ? new Set() : new Set(pickableKeys))
  }
  const clearPicked = () => {
    lastTicked.current = null
    setPicked(new Set())
  }
  const clearFilters = () => {
    setText("")
    setFacets({})
    clearPicked()
  }

  return (
    <Card className="@container gap-0 overflow-clip py-0">
      <div className="space-y-3 border-b px-4 py-3 sm:px-6 sm:py-4">
        <div className="flex min-h-8 items-center gap-2">
          {filtering ? (
            <button
              type="button"
              onClick={clearFilters}
              title={t("results.clearFilters")}
              className="group relative inline-flex shrink-0 items-center gap-1 rounded-full border border-primary/40 bg-primary/10 py-0.5 pr-1.5 pl-2.5 text-sm tabular-nums transition-colors hover:bg-primary/20 pointer-coarse:after:absolute pointer-coarse:after:-inset-2"
            >
              {t("results.filteredCount", { shown: shown.length, count: results.length })}
              <X className="size-3.5 text-muted-foreground group-hover:text-foreground" aria-hidden="true" />
              <span className="sr-only">{t("results.clearFilters")}</span>
            </button>
          ) : (
            <p className="text-sm tabular-nums">{t("results.count", { count: results.length })}</p>
          )}
          {cache && <CacheStatus cache={cache} />}
          {partial && <PartialStatus />}
          <RefreshButton ready={refreshReady} onRefresh={onRefresh} className="ml-auto" />
        </div>
        <div className="flex flex-col gap-2 @2xl:flex-row @2xl:items-center">
          <div className="@2xl:w-64 @2xl:shrink-0">
            <SearchInput
              value={text}
              onChange={event => setText(event.target.value)}
              onClear={() => setText("")}
              placeholder={t("results.filterPlaceholder")}
              aria-label={t("results.filterPlaceholder")}
              className="h-9"
            />
          </div>
          <div className="-mx-4 flex items-center gap-2 overflow-x-auto px-4 [scrollbar-width:none] sm:-mx-6 sm:px-6 @2xl:mx-0 @2xl:flex-wrap @2xl:overflow-visible @2xl:px-0">
            <Select
              value={SORT_OPTIONS.find(option => option.sort.key === sort.key && option.sort.desc === sort.desc)?.value}
              onValueChange={value => onSortChange(SORT_OPTIONS.find(option => option.value === value)!.sort)}
            >
              <SelectTrigger size="sm" className="shrink-0 @3xl:hidden" aria-label={t("sort.label")}>
                <SelectValue placeholder={t("sort.label")} />
              </SelectTrigger>
              <SelectContent>
                {SORT_OPTIONS.map(option => <SelectItem key={option.value} value={option.value}>{t(option.labelKey)}</SelectItem>)}
              </SelectContent>
            </Select>
            {facetChoices.map(({ facet, options }) => {
              const selected = facets[facet] ?? []
              if (options.length < 2 && selected.length === 0) return null
              return (
                <ChecklistPicker
                  key={facet}
                  title={t(FACET_LABEL_KEYS[facet])}
                  options={options.map(option => ({ value: option.value, label: facetLabel(t, facet, option.value), count: option.count }))}
                  selected={new Set(selected)}
                  onChange={values => setFacets(current => ({ ...current, [facet]: Array.from(values) }))}
                  trigger={
                    <Button type="button" variant="outline" size="sm" className={cn("shrink-0", selected.length > 0 && "border-primary/60 bg-primary/10")}>
                      {t(FACET_LABEL_KEYS[facet])}
                      {selected.length > 0 && <span className="rounded bg-primary px-1.5 text-xs tabular-nums text-primary-foreground">{selected.length}</span>}
                    </Button>
                  }
                />
              )
            })}
          </div>
        </div>
      </div>

      {shown.length === 0 ? (
        <div className="flex flex-col items-center gap-3 px-4 py-12 text-center">
          <p className="text-sm text-muted-foreground">{t("results.noMatches")}</p>
          <Button type="button" variant="outline" size="sm" onClick={clearFilters}>{t("results.clearFilters")}</Button>
        </div>
      ) : (
        <Table>
          <TableHeader className={selectable ? undefined : "hidden @xl:table-header-group"}>
            <TableRow className="hover:bg-transparent">
              {selectable && (
                <TableHead className="w-px pr-0 pl-4 sm:pl-6">
                  <Checkbox
                    checked={pageTick}
                    disabled={pickableKeys.length === 0}
                    onCheckedChange={tickPage}
                    aria-label={t("bulk.selectShown")}
                    className={cn("align-middle", CHECKBOX_TAP)}
                  />
                </TableHead>
              )}
              <SortHeader sortKey="title" label={t("table.title")} sort={sort} onSortChange={onSortChange} className={cn("w-full", !selectable && "pl-4 sm:pl-6")} />
              <SortHeader sortKey="indexer" label={t("table.indexer")} sort={sort} onSortChange={onSortChange} className="hidden @4xl:table-cell" />
              <SortHeader sortKey="size" label={t("table.size")} sort={sort} onSortChange={onSortChange} className="hidden text-right @xl:table-cell" end />
              <SortHeader sortKey="seeders" label={t("table.seeders")} sort={sort} onSortChange={onSortChange} className="hidden text-right @xl:table-cell" end />
              <TableHead className="hidden text-right text-xs font-medium tracking-wide text-muted-foreground uppercase @3xl:table-cell">{t("table.leechers")}</TableHead>
              <SortHeader sortKey="published" label={t("columns.age")} sort={sort} onSortChange={onSortChange} className="hidden text-right @3xl:table-cell" end />
              <TableHead className="pr-3 sm:pr-5"><span className="sr-only">{t("table.actions")}</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {mountedRows.map(result => {
              const key = resultKey(result)
              return <ResultRow key={key} rowKey={key} result={result} selectable={selectable} picked={picked.has(key)} onTick={tick} linkInRow={linkInRow} t={t} formatDate={formatDate} {...actions} />
            })}
          </TableBody>
        </Table>
      )}

      {pickedResults.length > 0 && (
        <SelectionBar label={t("bulk.label")} summary={t("bulk.selected", { count: pickedResults.length })} onClear={clearPicked}>
          <AddMenu t={t} instances={actions.instances} onAdd={instanceId => actions.onAdd(pickedResults, instanceId)} side="top">
            {({ name, onClick }) => (
              <Button type="button" size="sm" onClick={onClick} title={name ? t("card.addToNamed", { name }) : undefined}>
                <Plus className="size-4" />
                {t("row.add")}
                {name === null && <ChevronDown className="size-3.5 opacity-70" />}
              </Button>
            )}
          </AddMenu>
        </SelectionBar>
      )}

      {shown.length > limit && (
        <div className="flex flex-col items-center gap-1 border-t px-4 py-4">
          <Button type="button" variant="outline" onClick={() => setLimit(current => current + PAGE_SIZE)}>
            {t("results.showMore")}
          </Button>
          <p className="text-xs text-muted-foreground">{t("results.shownOf", { shown: limit, total: shown.length })}</p>
        </div>
      )}
    </Card>
  )
}

const TEXT_SORTS: ReadonlySet<SortKey> = new Set(["title", "indexer"])

export function RefreshButton({ ready, onRefresh, className }: { ready: boolean; onRefresh: () => void; className?: string }) {
  const { t } = useTranslation("search")
  return (
    <Tooltip>
      <TooltipTrigger asChild openOnTap={!ready}>
        <span className={cn("inline-flex", className)}>
          <Button type="button" variant="ghost" size="sm" disabled={!ready} onClick={onRefresh} className="text-muted-foreground">
            <RefreshCw className="size-3.5" />
            <span className="max-sm:sr-only">{t("results.refresh")}</span>
          </Button>
        </span>
      </TooltipTrigger>
      <TooltipContent>{ready ? t("results.refreshFromIndexers") : t("results.refreshCooldown")}</TooltipContent>
    </Tooltip>
  )
}

function SortHeader({ sortKey, label, sort, onSortChange, className, end = false }: { sortKey: SortKey; label: string; sort: ResultSort; onSortChange: (sort: ResultSort) => void; className?: string; end?: boolean }) {
  const active = sort.key === sortKey
  return (
    <TableHead className={className} aria-sort={active ? (sort.desc ? "descending" : "ascending") : undefined}>
      <button
        type="button"
        className={cn("-mx-1 inline-flex items-center gap-1 rounded px-1 py-1 text-xs font-medium tracking-wide text-muted-foreground uppercase hover:text-foreground", end && "flex-row-reverse", active && "text-foreground")}
        onClick={() => onSortChange(active ? { key: sortKey, desc: !sort.desc } : { key: sortKey, desc: !TEXT_SORTS.has(sortKey) })}
      >
        {label}
        {active && <SortIcon sorted={sort.desc ? "desc" : "asc"} />}
      </button>
    </TableHead>
  )
}

function PartialStatus() {
  const { t } = useTranslation("search")
  return (
    <Tooltip>
      <TooltipTrigger asChild openOnTap>
        <span tabIndex={0} className="inline-flex min-w-0 items-center gap-1 rounded-full border border-yellow-500/40 px-2 py-0.5 text-xs text-yellow-600 dark:text-yellow-500">
          <AlertTriangle className="size-3 shrink-0" />
          <span className="truncate">{t("results.partial")}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>{t("results.partialHint")}</TooltipContent>
    </Tooltip>
  )
}

function CacheStatus({ cache }: { cache: TorznabSearchCacheMetadata }) {
  const { t } = useTranslation("search")
  const { formatDate } = useDateTimeFormatters()
  const label = cache.source === "hybrid" ? t("cache.cacheAndLive") : cache.hit ? t("results.cachedAgo", { age: formatRelativeTime(cache.cachedAt) }) : t("results.live")
  const Icon = cache.hit || cache.source === "hybrid" ? Database : Zap
  return (
    <Tooltip>
      <TooltipTrigger asChild openOnTap>
        <span tabIndex={0} className="inline-flex min-w-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs text-muted-foreground">
          <Icon className="size-3 shrink-0" />
          <span className="truncate">{label}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>
        {t("results.cachedSummary", { cached: formatDate(new Date(cache.cachedAt)), expires: formatDate(new Date(cache.expiresAt)) })}
      </TooltipContent>
    </Tooltip>
  )
}

interface ResultRowProps extends ResultActions {
  rowKey: string
  result: TorznabSearchResult
  selectable: boolean
  picked: boolean
  onTick: (key: string, range: boolean) => void
  linkInRow: boolean
  t: TFunction
  formatDate: (date: Date) => string
}

const ResultRow = memo(function ResultRow({ rowKey, result, selectable, picked, onTick, linkInRow, t, formatDate, ...actions }: ResultRowProps) {
  const titleId = useId()
  const published = Date.parse(result.publishDate)
  const age = Number.isNaN(published) ? "–" : formatNarrowAge(Date.now() - published)
  const publishedTitle = Number.isNaN(published) ? undefined : formatDate(new Date(published))
  const marks = <ResultMarks t={t} result={result} />
  const pickable = selectable && Boolean(result.downloadUrl)

  return (
    <TableRow
      data-state={picked ? "selected" : undefined}
      className={cn(pickable && "cursor-pointer")}
      onMouseDown={pickable ? event => {
        if (event.shiftKey) event.preventDefault()
      } : undefined}
      onClick={pickable ? event => {
        if (window.getSelection()?.type === "Range") return
        onTick(rowKey, event.shiftKey)
      } : undefined}
    >
      {selectable && (
        <TableCell className="pr-0 pl-4 align-top sm:pl-6">
          {pickable && (
            <Checkbox
              checked={picked}
              onClick={event => {
                event.stopPropagation()
                onTick(rowKey, event.shiftKey)
              }}
              aria-labelledby={titleId}
              className={cn("mt-0.5", CHECKBOX_TAP)}
            />
          )}
        </TableCell>
      )}
      <TableCell className={cn("w-full max-w-0 py-2.5 align-top whitespace-normal", !selectable && "pl-4 sm:pl-6")}>
        <div className="flex min-w-0 items-start gap-2">
          <div id={titleId} className="min-w-0 font-medium pointer-fine:line-clamp-2" title={result.title}>
            <TorrentName name={result.title} />
          </div>
          <span className="hidden shrink-0 items-center gap-1 pt-px @4xl:flex">{marks}</span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground @4xl:hidden">
          <span className="truncate">{result.indexer}</span>
          <span className="@xl:hidden" aria-hidden="true">·</span>
          <span className="tabular-nums @xl:hidden">{formatBytes(result.size)}</span>
          <span className="inline-flex items-center gap-1.5 tabular-nums @3xl:hidden">
            <span className="inline-flex items-center gap-0.5 @xl:hidden"><ArrowUp className="size-3" />{result.seeders}</span>
            <span className="inline-flex items-center gap-0.5"><ArrowDown className="size-3" />{result.leechers}</span>
          </span>
          <span className="@3xl:hidden" aria-hidden="true">·</span>
          <span className="tabular-nums @3xl:hidden" title={publishedTitle}>{age}</span>
          {marks}
        </div>
      </TableCell>
      <TableCell className="hidden max-w-48 truncate py-2.5 align-top text-muted-foreground @4xl:table-cell">{result.indexer}</TableCell>
      <TableCell className="hidden py-2.5 text-right align-top tabular-nums @xl:table-cell">{formatBytes(result.size)}</TableCell>
      <TableCell className={cn("hidden py-2.5 text-right align-top tabular-nums @xl:table-cell", result.seeders === 0 ? "text-muted-foreground" : "font-medium")}>{result.seeders}</TableCell>
      <TableCell className="hidden py-2.5 text-right align-top tabular-nums text-muted-foreground @3xl:table-cell">{result.leechers}</TableCell>
      <TableCell className="hidden py-2.5 text-right align-top tabular-nums text-muted-foreground @3xl:table-cell" title={publishedTitle}>{age}</TableCell>
      <TableCell className="cursor-default py-1 pr-3 align-top sm:pr-5" onClick={event => event.stopPropagation()}>
        <RowActions t={t} result={result} linkInRow={linkInRow} {...actions} />
      </TableCell>
    </TableRow>
  )
})

function ResultMarks({ t, result }: { t: TFunction; result: TorznabSearchResult }) {
  const discount = discountLabel(t, result.downloadVolumeFactor)
  return (
    <>
      {discount && <Badge variant={result.downloadVolumeFactor <= 0 ? "default" : "secondary"} className="px-1.5 py-0 text-[11px]">{discount}</Badge>}
      {result.uploadVolumeFactor > 1 && <Badge variant="outline" className="px-1.5 py-0 text-[11px]">{t("row.upload", { factor: result.uploadVolumeFactor })}</Badge>}
    </>
  )
}

function RowActions({ t, result, linkInRow, instances, onAdd, onDownload, onOpenInfo }: ResultActions & { t: TFunction; result: TorznabSearchResult; linkInRow: boolean }) {
  const copyTitle = async () => {
    try {
      await copyTextToClipboard(result.title)
      toast.success(t("common:feedback.copied", { label: t("table.title") }))
    } catch {
      toast.error(t("common:feedback.copyFailed"))
    }
  }

  return (
    <div className="flex items-center justify-end gap-1">
      {result.downloadUrl && instances.length > 0 && (
        <AddMenu t={t} instances={instances} onAdd={instanceId => onAdd([result], instanceId)}>
          {({ name, onClick }) => (
            <Button type="button" variant="outline" size="sm" className="@max-2xl:size-9 @max-2xl:px-0" onClick={onClick} aria-label={name ? t("card.addToNamed", { name }) : t("row.add")} title={name ? t("card.addToNamed", { name }) : undefined}>
              <Plus className="size-4" />
              <span className="hidden @2xl:inline">{t("row.add")}</span>
            </Button>
          )}
        </AddMenu>
      )}
      {result.downloadUrl && instances.length === 0 && (
        <Button type="button" variant="outline" size="sm" className="@max-2xl:size-9 @max-2xl:px-0" aria-label={t("table.downloadTorrent")} onClick={() => onDownload(result)}>
          <Download className="size-4" />
          <span className="hidden @2xl:inline">{t("results.download")}</span>
        </Button>
      )}
      {linkInRow && (result.infoUrl ? (
        <Button type="button" variant="ghost" size="icon" className="size-8 text-muted-foreground hover:text-foreground" aria-label={t("row.openIndexer")} title={t("row.openIndexer")} onClick={() => onOpenInfo(result)}>
          <ExternalLink className="size-4" />
        </Button>
      ) : (
        <span className="size-8" aria-hidden="true" />
      ))}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button type="button" variant="ghost" size="icon" className="size-9 @2xl:size-8" aria-label={t("row.more")}>
            <MoreHorizontal className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-52">
          {result.downloadUrl && instances.length > 0 && (
            <DropdownMenuItem onSelect={() => onDownload(result)}>
              <Download className="size-4" />
              {t("table.downloadTorrent")}
            </DropdownMenuItem>
          )}
          {result.infoUrl && !linkInRow && (
            <DropdownMenuItem onSelect={() => onOpenInfo(result)}>
              <ExternalLink className="size-4" />
              {t("row.openIndexer")}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={copyTitle}>
            <Copy className="size-4" />
            {t("row.copyTitle")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

interface AddTrigger {
  name: string | null
  onClick?: () => void
}

function AddMenu({ t, instances, onAdd, side, children }: { t: TFunction; instances: InstanceResponse[]; onAdd: (instanceId: number) => void; side?: "top"; children: (trigger: AddTrigger) => ReactNode }) {
  if (instances.length === 1) {
    const [only] = instances
    return children({ name: only.name, onClick: () => onAdd(only.id) })
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children({ name: null })}</DropdownMenuTrigger>
      <DropdownMenuContent align="end" side={side} className="w-60">
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{t("row.addTo")}</DropdownMenuLabel>
        {instances.map(instance => (
          <DropdownMenuItem key={instance.id} onSelect={() => onAdd(instance.id)}>
            <InstanceMarkIcon instanceId={instance.id} problem={!instance.connected} />
            <span className="min-w-0 flex-1 truncate">{instance.name}</span>
            {!instance.connected && <span className="text-xs text-muted-foreground">{t("instanceSelector.offline")}</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
