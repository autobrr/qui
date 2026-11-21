/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChecklistPicker } from "@/components/search/ChecklistPicker"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { getSearchTypeLabel, getSearchTypeOptions, inferSearchTypeFromCategories, type SearchType } from "@/lib/search-derived-params"
import { activeAdvancedFields, advancedFieldsFor, NUMERIC_FIELDS, type AdvancedField, type AdvancedParams } from "@/lib/search-results"
import { cn } from "@/lib/utils"
import type { TorznabIndexer, TorznabRecentSearch } from "@/types"
import { History, Loader2, Search as SearchIcon, Server, SlidersHorizontal, X } from "lucide-react"
import { Children, type ReactNode, useId, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

const SUGGESTION_LIMIT = 6

const FIELD_KEYS: Record<AdvancedField, { label: string; placeholder: string }> = {
  imdbId: { label: "advanced.imdbId.label", placeholder: "advanced.imdbId.placeholder" },
  tvdbId: { label: "advanced.tvdbId.label", placeholder: "advanced.tvdbId.placeholder" },
  season: { label: "advanced.season.label", placeholder: "advanced.season.placeholder" },
  episode: { label: "advanced.episode.label", placeholder: "advanced.episode.placeholder" },
  year: { label: "advanced.year.label", placeholder: "advanced.year.placeholder" },
  artist: { label: "advanced.artist.label", placeholder: "advanced.artist.placeholder" },
  album: { label: "advanced.album.label", placeholder: "advanced.album.placeholder" },
  limit: { label: "advanced.limit.label", placeholder: "advanced.limit.placeholder" },
  offset: { label: "advanced.offset.label", placeholder: "advanced.offset.placeholder" },
}

const PLACEHOLDER_KEYS: Record<SearchType, string> = {
  movies: "searchTypes.movies.placeholder",
  tv: "searchTypes.tv.placeholder",
  music: "searchTypes.music.placeholder",
  books: "searchTypes.books.placeholder",
  apps: "searchTypes.apps.placeholder",
  xxx: "searchTypes.xxx.placeholder",
}

const BACKEND_KEYS: Record<TorznabIndexer["backend"], string> = {
  jackett: "indexerSheet.backend.jackett",
  prowlarr: "indexerSheet.backend.prowlarr",
  native: "indexerSheet.backend.native",
}

interface SearchFormProps {
  query: string
  onQueryChange: (query: string) => void
  type: SearchType
  onTypeChange: (type: SearchType) => void
  typeReady: boolean
  params: AdvancedParams
  onParamsChange: (params: AdvancedParams) => void
  indexers: TorznabIndexer[]
  selectedIndexerIds: ReadonlySet<number>
  onIndexersChange: (ids: Set<number>) => void
  recent: TorznabRecentSearch[]
  pending: boolean
  canSubmit: boolean
  onSubmit: () => void
  onRecent: (search: TorznabRecentSearch) => void
  children?: ReactNode
}

export function SearchForm(props: SearchFormProps) {
  const { type, onTypeChange, typeReady, params, onParamsChange, indexers, selectedIndexerIds, onIndexersChange, pending, canSubmit, onSubmit } = props
  const { t } = useTranslation("search")
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const active = activeAdvancedFields(type, params)
  const allIndexers = selectedIndexerIds.size === indexers.length
  const mixedBackends = new Set(indexers.map(indexer => indexer.backend)).size > 1

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (!canSubmit) return
    inputRef.current?.blur()
    onSubmit()
  }

  const setParam = (field: AdvancedField, value: string) => onParamsChange({ ...params, [field]: value })

  return (
    <Card className="gap-0 py-0">
      <form onSubmit={submit} className="flex flex-wrap items-center gap-2 px-4 py-3 sm:px-6 sm:py-4" role="search">
        <div className="flex min-w-0 flex-1 gap-2 max-sm:order-1 max-sm:basis-full sm:order-2">
          <QueryInput {...props} inputRef={inputRef} />
          <Button type="submit" disabled={!canSubmit} className="h-10 shrink-0 max-sm:w-10 max-sm:px-0" aria-label={t("searchForm.search")}>
            {pending ? <Loader2 className="size-4 animate-spin" /> : <SearchIcon className="size-4" />}
            <span className="max-sm:sr-only">{pending ? t("searchForm.searching") : t("searchForm.search")}</span>
          </Button>
        </div>

        <div className="max-sm:order-2 sm:order-1">
          <Label htmlFor="search-type" className="sr-only">{t("searchForm.searchType")}</Label>
          <Select value={type} onValueChange={value => onTypeChange(value as SearchType)} disabled={!typeReady}>
            <SelectTrigger id="search-type" className="h-8 w-auto gap-1.5 sm:h-10 sm:w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {getSearchTypeOptions(t).map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>

        <div className="hidden h-0 basis-full sm:order-3 sm:block" />

        <div className="flex min-w-0 flex-wrap items-center gap-2 max-sm:order-3 sm:order-4">
          <ChecklistPicker
            title={t("picker.indexersTitle")}
            options={indexers.map(indexer => ({ value: String(indexer.id), label: indexer.name, detail: mixedBackends ? t(BACKEND_KEYS[indexer.backend]) : undefined }))}
            selected={new Set(Array.from(selectedIndexerIds, String))}
            onChange={ids => onIndexersChange(new Set(Array.from(ids, Number)))}
            trigger={
              <Button type="button" variant="outline" size="sm" className={cn(selectedIndexerIds.size === 0 && "border-destructive/60 text-destructive")}>
                <Server className="size-3.5 max-sm:hidden" />
                {t("form.indexers")}
                <span className="rounded bg-muted px-1.5 text-xs tabular-nums text-muted-foreground">
                  {allIndexers ? indexers.length : t("form.indexerCount", { selected: selectedIndexerIds.size, total: indexers.length })}
                </span>
              </Button>
            }
          />
          <Button type="button" variant="outline" size="sm" aria-expanded={advancedOpen} aria-controls="search-advanced" onClick={() => setAdvancedOpen(open => !open)} className={cn(advancedOpen && "bg-accent")}>
            <SlidersHorizontal className="size-3.5 max-sm:hidden" />
            {t("searchForm.advanced")}
            {active.length > 0 && <span className="rounded bg-primary px-1.5 text-xs tabular-nums text-primary-foreground">{active.length}</span>}
          </Button>
          {!advancedOpen && active.map(field => (
            <span key={field} className="inline-flex h-8 items-center gap-1 rounded-md border bg-muted/40 pr-1 pl-2.5 text-xs">
              <span className="text-muted-foreground">{t(FIELD_KEYS[field].label)}</span>
              <span className="max-w-32 truncate font-medium">{params[field]}</span>
              <button type="button" className="grid size-6 place-items-center rounded hover:bg-accent" aria-label={t("form.removeParam", { label: t(FIELD_KEYS[field].label) })} onClick={() => setParam(field, "")}>
                <X className="size-3.5" />
              </button>
            </span>
          ))}
        </div>

        {advancedOpen && (
          <div id="search-advanced" className="order-5 basis-full space-y-3 rounded-lg border bg-muted/30 p-3 sm:p-4">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
              {advancedFieldsFor(type).map(field => (
                <div key={field} className="space-y-1.5">
                  <Label htmlFor={`advanced-${field}`} className="text-xs text-muted-foreground">{t(FIELD_KEYS[field].label)}</Label>
                  <Input
                    id={`advanced-${field}`}
                    inputMode={NUMERIC_FIELDS.has(field) ? "numeric" : undefined}
                    autoComplete="off"
                    placeholder={t(FIELD_KEYS[field].placeholder)}
                    value={params[field] ?? ""}
                    onChange={event => setParam(field, event.target.value)}
                  />
                </div>
              ))}
            </div>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="text-xs text-muted-foreground">{t("searchForm.advancedNote")}</p>
              {active.length > 0 && (
                <Button type="button" variant="ghost" size="sm" className="-mx-2" onClick={() => onParamsChange({})}>
                  {t("form.clearFields")}
                </Button>
              )}
            </div>
          </div>
        )}
      </form>
      {Children.toArray(props.children).length > 0 && <div className="overflow-clip rounded-b-xl border-t">{props.children}</div>}
    </Card>
  )
}

function QueryInput({ query, onQueryChange, type, recent, onRecent, inputRef }: SearchFormProps & { inputRef: React.RefObject<HTMLInputElement | null> }) {
  const { t } = useTranslation("search")
  const listId = useId()
  const [open, setOpen] = useState(false)
  const [highlight, setHighlight] = useState(-1)
  const needle = query.trim().toLowerCase()
  const suggestions = recent.filter(search => needle === "" || (search.query.toLowerCase().includes(needle) && search.query.toLowerCase() !== needle)).slice(0, SUGGESTION_LIMIT)
  const showList = open && suggestions.length > 0

  const close = () => {
    setOpen(false)
    setHighlight(-1)
  }

  const pick = (search: TorznabRecentSearch) => {
    close()
    inputRef.current?.blur()
    onRecent(search)
  }

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return
    if (event.key === "Escape") {
      close()
      return
    }
    if (!showList) {
      if (event.key === "ArrowDown") setOpen(true)
      return
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault()
      const step = event.key === "ArrowDown" ? 1 : -1
      setHighlight(current => {
        const next = current + step
        if (next >= suggestions.length) return -1
        return next < -1 ? suggestions.length - 1 : next
      })
      return
    }
    if (event.key === "Enter" && highlight >= 0 && highlight < suggestions.length) {
      event.preventDefault()
      pick(suggestions[highlight])
    }
  }

  return (
    <div className="relative min-w-0 flex-1">
      <Label htmlFor="search-query" className="sr-only">{t("searchForm.searchQuery")}</Label>
      <SearchIcon className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        ref={inputRef}
        id="search-query"
        role="combobox"
        aria-expanded={showList}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={showList && highlight >= 0 && highlight < suggestions.length ? `${listId}-${highlight}` : undefined}
        enterKeyHint="search"
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        value={query}
        placeholder={t(PLACEHOLDER_KEYS[type])}
        onChange={event => {
          onQueryChange(event.target.value)
          setOpen(true)
          setHighlight(-1)
        }}
        onFocus={() => setOpen(true)}
        onBlur={close}
        onKeyDown={onKeyDown}
        className="h-10 pr-9 pl-9"
      />
      {query && (
        <button
          type="button"
          aria-label={t("form.clearQuery")}
          className="absolute top-1/2 right-1 grid size-8 -translate-y-1/2 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
          onMouseDown={event => event.preventDefault()}
          onClick={() => {
            onQueryChange("")
            setHighlight(-1)
            inputRef.current?.focus()
          }}
        >
          <X className="size-4" />
        </button>
      )}
      {showList && (
        <ul id={listId} role="listbox" aria-label={t("form.recentSearches")} className="absolute inset-x-0 top-full z-50 mt-1 overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
          <li role="presentation" className="px-2 pt-1 pb-1.5 text-xs font-medium text-muted-foreground">{t("form.recentSearches")}</li>
          {suggestions.map((search, index) => (
            <li
              key={search.cacheKey}
              id={`${listId}-${index}`}
              role="option"
              aria-selected={index === highlight}
              className={cn("flex cursor-pointer items-center gap-2.5 rounded-sm px-2 py-2 text-sm", index === highlight ? "bg-accent text-accent-foreground" : "hover:bg-accent/60")}
              onMouseDown={event => event.preventDefault()}
              onMouseEnter={() => setHighlight(index)}
              onClick={() => pick(search)}
            >
              <History className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{search.query}</span>
                <span className="block truncate text-xs text-muted-foreground">
                  <RecentMeta search={search} />
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function RecentMeta({ search }: { search: TorznabRecentSearch }) {
  const { t } = useTranslation("search")
  return t("historyMeta", {
    type: getSearchTypeLabel(inferSearchTypeFromCategories(search.categories) ?? "movies", t),
    count: search.totalResults,
    timestamp: formatRelativeTime(search.lastUsedAt ?? search.cachedAt),
  })
}
