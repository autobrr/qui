/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { Page } from "@/components/layout/Page"
import { RecentMeta, SearchForm } from "@/components/search/SearchForm"
import { RefreshButton, SearchResults } from "@/components/search/SearchResults"
import { AddTorrentDialog, type AddTorrentDropPayload } from "@/components/torrents/AddTorrentDialog"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { useInstances } from "@/hooks/useInstances"
import { useSearchType } from "@/hooks/useSearchPreferences"
import { api } from "@/lib/api"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { inferSearchTypeFromCategories, resolveSuggestionIndexerIds } from "@/lib/search-derived-params"
import { activeAdvancedFields, buildSearchRequest, DEFAULT_SORT, type AdvancedParams, type ResultSort, type SubmittedSearch } from "@/lib/search-results"
import type { TorznabIndexer, TorznabRecentSearch, TorznabSearchResult } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { AlertCircle, ChevronRight, History, Loader2, Search as SearchIcon, Server } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const REFRESH_COOLDOWN_MS = 30_000
const RECENT_LIMIT = 8

function buildCategoryNames(indexers: TorznabIndexer[]): Map<number, string> {
  const names = new Map<number, string>()
  for (const indexer of indexers) {
    const categories = indexer.categories ?? []
    for (const category of categories) {
      const parentId = Math.floor(category.category_id / 1000) * 1000
      const parent = category.category_id === parentId ? category : categories.find(candidate => candidate.category_id === parentId)
      if (parent && !names.has(category.category_id)) names.set(category.category_id, parent.category_name)
    }
  }
  return names
}

export function Search() {
  const { t } = useTranslation("search")
  const queryClient = useQueryClient()
  const settled = useClientSettingsSettled()
  const { instances: instanceList } = useInstances()
  const instances = useMemo(() => instanceList ?? [], [instanceList])
  const [type, setType] = useSearchType()

  const indexersQuery = useQuery({
    queryKey: ["torznab", "indexers"],
    queryFn: () => api.listTorznabIndexers(),
    staleTime: 5 * 60 * 1000,
  })
  const indexers = useMemo(() => (indexersQuery.data ?? []).filter(indexer => indexer.enabled), [indexersQuery.data])
  const recentQuery = useQuery({
    queryKey: ["torznab", "search", "recent", "general"],
    queryFn: () => api.getRecentTorznabSearches(20, "general"),
  })
  const recent = indexers.length > 0 ? recentQuery.data ?? [] : []

  const [query, setQuery] = useState("")
  const [params, setParams] = useState<AdvancedParams>({})
  const [pickedIndexers, setPickedIndexers] = useState<Set<number> | null>(null)
  const selectedIndexerIds = useMemo(
    () => new Set(indexers.filter(indexer => pickedIndexers === null || pickedIndexers.has(indexer.id)).map(indexer => indexer.id)),
    [indexers, pickedIndexers]
  )
  const [sort, setSort] = useState<ResultSort>(DEFAULT_SORT)
  const [addInstanceId, setAddInstanceId] = useState<number | null>(null)
  const [addPayload, setAddPayload] = useState<AddTorrentDropPayload | null>(null)
  const [refreshAsk, setRefreshAsk] = useState(false)
  const [coolingDown, setCoolingDown] = useState(false)

  useEffect(() => {
    if (!coolingDown) return
    const id = window.setTimeout(() => setCoolingDown(false), REFRESH_COOLDOWN_MS)
    return () => window.clearTimeout(id)
  }, [coolingDown])

  const search = useMutation({
    mutationFn: (submitted: SubmittedSearch) => api.searchTorznab(buildSearchRequest(submitted)),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ["torznab", "search", "recent"] })
    },
  })

  const categoryNames = useMemo(() => buildCategoryNames(indexers), [indexers])
  const categoryName = useCallback(
    (result: TorznabSearchResult) => categoryNames.get(result.categoryId) ?? (result.categoryName && Number.isNaN(Number(result.categoryName)) ? result.categoryName : t("results.categoryFallback", { id: result.categoryId })),
    [categoryNames, t]
  )

  const run = (submitted: SubmittedSearch) => search.mutate(submitted)

  const canSubmit = selectedIndexerIds.size > 0 && (query.trim() !== "" || activeAdvancedFields(type, params).length > 0)
  const submit = () => run({ query, type, indexerIds: Array.from(selectedIndexerIds), params })

  const runRecent = (recentSearch: TorznabRecentSearch) => {
    const recentType = inferSearchTypeFromCategories(recentSearch.categories) ?? "movies"
    const restored = resolveSuggestionIndexerIds(recentSearch.indexerIds, indexers.map(indexer => indexer.id))
    setQuery(recentSearch.query)
    setType(recentType)
    if (restored) setPickedIndexers(restored.size === indexers.length ? null : restored)
    const indexerIds = Array.from(restored ?? selectedIndexerIds)
    if (indexerIds.length > 0) run({ query: recentSearch.query, type: recentType, indexerIds, params })
  }

  const searchAllIndexers = () => {
    setPickedIndexers(null)
    if (search.variables) run({ ...search.variables, indexerIds: indexers.map(indexer => indexer.id), bypassCache: false })
  }

  const onAdd = useCallback((picked: TorznabSearchResult[], instanceId: number) => {
    const addable = picked.filter(result => result.downloadUrl)
    if (addable.length === 0) {
      toast.error(t("toast.noDownloadUrl"))
      return
    }
    setAddInstanceId(instanceId)
    setAddPayload({
      type: "url",
      urls: addable.map(result => result.downloadUrl),
      indexerByUrl: Object.fromEntries(addable.map(result => [result.downloadUrl, result.indexerId])),
    })
  }, [t])

  const onDownload = useCallback((result: TorznabSearchResult) => {
    window.open(result.downloadUrl, "_blank", "noopener,noreferrer")
  }, [])

  const onOpenInfo = useCallback((result: TorznabSearchResult) => {
    try {
      const url = new URL(result.infoUrl ?? "")
      if (url.protocol !== "http:" && url.protocol !== "https:") {
        toast.error(t("toast.invalidProtocol"))
        return
      }
      window.open(url.href, "_blank", "noopener,noreferrer")
    } catch {
      toast.error(t("toast.invalidUrl"))
    }
  }, [t])

  const actions = { instances, onAdd, onDownload, onOpenInfo }

  const noIndexers = indexersQuery.isSuccess && indexers.length === 0
  const onRefresh = () => setRefreshAsk(true)

  return (
    <Page title={t("pageTitle")} description={t("pageDescription")}>
      {indexersQuery.isLoadingError ? (
        <ErrorCard title={t("form.indexersFailed")} message={indexersQuery.error.message} retrying={indexersQuery.isFetching} onRetry={() => void indexersQuery.refetch()} />
      ) : noIndexers ? (
        <NoIndexers />
      ) : (
        <SearchForm
          query={query}
          onQueryChange={setQuery}
          type={type}
          onTypeChange={setType}
          typeReady={settled}
          params={params}
          onParamsChange={setParams}
          indexers={indexers}
          selectedIndexerIds={selectedIndexerIds}
          onIndexersChange={ids => setPickedIndexers(ids.size === indexers.length ? null : ids)}
          recent={recent}
          pending={search.isPending}
          canSubmit={canSubmit}
          onSubmit={submit}
          onRecent={runRecent}
        >
          {search.isIdle && indexers.length > 0 && <RecentSearches recent={recent.slice(0, RECENT_LIMIT)} onPick={runRecent} />}
          {search.isPending && <Searching count={search.variables?.indexerIds.length ?? 0} />}
          {search.isError && (
            <ErrorCard inset title={t("results.failed")} message={search.error.message} onRetry={() => search.variables && run({ ...search.variables, bypassCache: false })} />
          )}
          {search.isSuccess && search.data.results.length === 0 && (
            <div className="flex flex-col items-center gap-3 px-4 py-12 text-center">
              <SearchIcon className="size-8 text-muted-foreground/60" />
              <p className="font-medium">{search.variables.query.trim() ? t("results.noResultsFor", { query: search.variables.query.trim() }) : t("results.noResults")}</p>
              <p className="max-w-md text-sm text-muted-foreground">{t("results.noResultsHint")}</p>
              <div className="flex flex-wrap justify-center gap-2">
                {indexers.some(indexer => !search.variables.indexerIds.includes(indexer.id)) && (
                  <Button type="button" variant="outline" size="sm" onClick={searchAllIndexers}>
                    <Server className="size-3.5" />
                    {t("results.searchAllIndexers")}
                  </Button>
                )}
                {search.data.cache && <RefreshButton ready={!coolingDown} onRefresh={onRefresh} />}
              </div>
            </div>
          )}
          {search.isSuccess && search.data.results.length > 0 && (
            <SearchResults
              key={search.submittedAt}
              results={search.data.results}
              cache={search.data.cache}
              partial={search.data.partial === true}
              categoryName={categoryName}
              sort={sort}
              onSortChange={setSort}
              onRefresh={onRefresh}
              refreshReady={!coolingDown}
              {...actions}
            />
          )}
        </SearchForm>
      )}

      {addInstanceId !== null && (
        <AddTorrentDialog
          key={addInstanceId}
          instanceId={addInstanceId}
          open
          onOpenChange={open => {
            if (!open) {
              setAddInstanceId(null)
              setAddPayload(null)
            }
          }}
          dropPayload={addPayload}
          onDropPayloadConsumed={() => setAddPayload(null)}
        />
      )}

      <ConfirmDialog
        open={refreshAsk}
        onOpenChange={setRefreshAsk}
        title={t("refreshDialog.title")}
        description={t("refreshDialog.description")}
        confirmLabel={t("refreshDialog.refreshNow")}
        reversible
        onConfirm={() => {
          setRefreshAsk(false)
          setCoolingDown(true)
          if (search.variables) run({ ...search.variables, bypassCache: true })
        }}
      />
    </Page>
  )
}

function ErrorCard({ title, message, retrying = false, inset = false, onRetry }: { title: string; message: string; retrying?: boolean; inset?: boolean; onRetry: () => void }) {
  const { t } = useTranslation("search")
  const content = (
    <>
      <AlertCircle className="mt-0.5 size-5 shrink-0 text-destructive" />
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-sm font-medium">{title}</p>
        <p className="text-sm break-words text-muted-foreground">{message}</p>
      </div>
      <Button type="button" variant="outline" size="sm" disabled={retrying} onClick={onRetry}>{t("results.retry")}</Button>
    </>
  )
  if (inset) return <div className="flex items-start gap-3 px-4 py-4 sm:px-6">{content}</div>
  return <Card className="flex-row items-start gap-3 border-destructive/40 px-4 py-4 sm:px-6">{content}</Card>
}

function Searching({ count }: { count: number }) {
  const { t } = useTranslation("search")
  return (
    <div aria-busy="true">
      <div className="flex items-center gap-2 border-b px-4 py-3 text-sm text-muted-foreground sm:px-6 sm:py-4">
        <Loader2 className="size-4 animate-spin" />
        {t("results.searching", { count })}
      </div>
      <div className="divide-y">
        {[0, 1, 2, 3, 4, 5].map(row => (
          <div key={row} className="flex items-center gap-4 px-4 py-3.5 sm:px-6">
            <div className="flex-1 space-y-2">
              <div className="h-3.5 animate-pulse rounded bg-muted" style={{ width: `${85 - row * 7}%` }} />
              <div className="h-2.5 w-1/3 animate-pulse rounded bg-muted/70" />
            </div>
            <div className="h-8 w-16 animate-pulse rounded-md bg-muted/70" />
          </div>
        ))}
      </div>
    </div>
  )
}

function RecentSearches({ recent, onPick }: { recent: TorznabRecentSearch[]; onPick: (search: TorznabRecentSearch) => void }) {
  const { t } = useTranslation("search")
  if (recent.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
        <SearchIcon className="size-8 text-muted-foreground/60" />
        <p className="font-medium">{t("results.idleTitle")}</p>
        <p className="max-w-md text-sm text-muted-foreground">{t("results.idleHint")}</p>
      </div>
    )
  }
  return (
    <>
      <p className="px-4 pt-3 pb-1 text-xs font-medium text-muted-foreground sm:px-6">{t("form.recentSearches")}</p>
      <ul>
        {recent.map(search => (
          <li key={search.cacheKey}>
            <button type="button" className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-muted/50 sm:px-6" onClick={() => onPick(search)}>
              <History className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{search.query}</span>
                <span className="block truncate text-xs text-muted-foreground"><RecentMeta search={search} /></span>
              </span>
              <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
            </button>
          </li>
        ))}
      </ul>
    </>
  )
}

function NoIndexers() {
  const { t } = useTranslation("search")
  return (
    <Card className="items-center gap-3 px-4 py-12 text-center">
      <Server className="size-8 text-muted-foreground/60" />
      <p className="font-medium">{t("form.noIndexersTitle")}</p>
      <p className="max-w-md text-sm text-muted-foreground">{t("form.noIndexersBody")}</p>
      <Button asChild size="sm">
        <Link to="/settings" search={{ tab: "indexers" }}>{t("form.noIndexersAction")}</Link>
      </Button>
    </Card>
  )
}
