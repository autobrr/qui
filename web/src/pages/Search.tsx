/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SearchResultCard } from "@/components/search/SearchResultCard"
import { AddTorrentDialog, type AddTorrentDropPayload } from "@/components/torrents/AddTorrentDialog"
import { ColumnFilterPopover } from "@/components/torrents/ColumnFilterPopover"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import type { ColumnFilter } from "@/lib/column-filter-utils"
import { filterSearchResult } from "@/lib/column-filter-utils"
import { getCategoriesForSearchType, getSearchTypeLabel, getSearchTypeOptions, inferSearchTypeFromCategories, type SearchType } from "@/lib/search-derived-params"
import { extractImdbId, extractTvdbId } from "@/lib/search-id-parsing"
import { cn, formatBytes } from "@/lib/utils"
import type { TorznabIndexer, TorznabRecentSearch, TorznabSearchRequest, TorznabSearchResponse, TorznabSearchResult } from "@/types"
import { Link } from "@tanstack/react-router"
import { useVirtualizer } from "@tanstack/react-virtual"
import { Check, ChevronDown, ChevronUp, Download, ExternalLink, Plus, RefreshCw, Search as SearchIcon, SlidersHorizontal, X } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

type AdvancedParamsState = {
  imdbId: string
  tvdbId: string
  year: string
  season: string
  episode: string
  artist: string
  album: string
  limit: string
  offset: string
}

type AdvancedParamConfig = {
  key: keyof AdvancedParamsState
  labelKey: string
  placeholderKey?: string
  type: "text" | "number"
  min?: number
}

const ADVANCED_PARAM_DEFAULTS: AdvancedParamsState = {
  imdbId: "",
  tvdbId: "",
  year: "",
  season: "",
  episode: "",
  artist: "",
  album: "",
  limit: "",
  offset: "",
}

const SEARCH_PLACEHOLDER_KEYS: Record<SearchType, string> = {
  auto: "searchTypes.auto.placeholder",
  movies: "searchTypes.movies.placeholder",
  tv: "searchTypes.tv.placeholder",
  music: "searchTypes.music.placeholder",
  books: "searchTypes.books.placeholder",
  apps: "searchTypes.apps.placeholder",
  xxx: "searchTypes.xxx.placeholder",
}

const ADVANCED_PARAM_CONFIG: AdvancedParamConfig[] = [
  { key: "imdbId", labelKey: "advanced.imdbId.label", placeholderKey: "advanced.imdbId.placeholder", type: "text" },
  { key: "tvdbId", labelKey: "advanced.tvdbId.label", placeholderKey: "advanced.tvdbId.placeholder", type: "text" },
  { key: "year", labelKey: "advanced.year.label", placeholderKey: "advanced.year.placeholder", type: "number", min: 0 },
  { key: "season", labelKey: "advanced.season.label", placeholderKey: "advanced.season.placeholder", type: "number", min: 0 },
  { key: "episode", labelKey: "advanced.episode.label", placeholderKey: "advanced.episode.placeholder", type: "number", min: 0 },
  { key: "artist", labelKey: "advanced.artist.label", placeholderKey: "advanced.artist.placeholder", type: "text" },
  { key: "album", labelKey: "advanced.album.label", placeholderKey: "advanced.album.placeholder", type: "text" },
  { key: "limit", labelKey: "advanced.limit.label", placeholderKey: "advanced.limit.placeholder", type: "number", min: 1 },
  { key: "offset", labelKey: "advanced.offset.label", placeholderKey: "advanced.offset.placeholder", type: "number", min: 0 },
]

const LAST_USED_INSTANCE_KEY = "qui:search:lastInstanceId"

const SEARCH_TABLE_GRID_TEMPLATE =
  "minmax(240px, 1fr) 140px 100px 100px 140px 110px 140px 110px 100px 150px 80px"
const SEARCH_TABLE_MIN_WIDTH = 1410
const SEARCH_ROW_HEIGHT = 40
const SEARCH_CARD_HEIGHT = 158

const getSearchResultKey = (result: TorznabSearchResult) => {
  const resultId = result.guid.trim() === "" ? result.downloadUrl : result.guid
  return `${result.indexerId}-${resultId}`
}

type SearchTableRowProps = {
  result: TorznabSearchResult
  isSelected: boolean
  isEven: boolean
  height: number
  translateY: number
  categoryLabel: string
  publishedLabel: string
  onToggleSelection: (result: TorznabSearchResult) => void
  onDownload: (result: TorznabSearchResult) => void
  onViewDetails: (result: TorznabSearchResult) => void
}

const SearchTableRow = memo(function SearchTableRow({
  result,
  isSelected,
  isEven,
  height,
  translateY,
  categoryLabel,
  publishedLabel,
  onToggleSelection,
  onDownload,
  onViewDetails,
}: SearchTableRowProps) {
  const { t } = useTranslation("search")
  return (
    <div
      role="button"
      tabIndex={0}
      aria-selected={isSelected}
      className={cn(
        "grid cursor-pointer select-none transition-colors border-b",
        isSelected? "bg-accent text-accent-foreground hover:bg-accent/90": isEven? "hover:bg-muted/60 bg-card/90 dark:bg-card/80": "hover:bg-muted/60 bg-background/70 dark:bg-background/30"
      )}
      style={{
        gridTemplateColumns: SEARCH_TABLE_GRID_TEMPLATE,
        position: "absolute",
        top: 0,
        left: 0,
        width: "100%",
        height: `${height}px`,
        transform: `translateY(${translateY}px)`,
        contain: "layout style paint",
      }}
      onClick={() => onToggleSelection(result)}
      onKeyDown={(event) => {
        if (event.currentTarget !== event.target) {
          return
        }

        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault()
          onToggleSelection(result)
        }
      }}
    >
      <div className={cn("px-2 flex items-center min-w-0 text-sm font-medium", isSelected && "text-accent-foreground")}>
        <div className="truncate" title={result.title}>
          {result.title}
        </div>
      </div>
      <div className={cn("px-2 flex items-center min-w-0 text-sm truncate", isSelected && "text-accent-foreground")}>{result.indexer}</div>
      <div className={cn("px-2 flex items-center text-sm", isSelected && "text-accent-foreground")}>{formatBytes(result.size)}</div>
      <div className={cn("px-2 flex items-center", isSelected && "text-accent-foreground")}>
        <Badge variant={result.seeders > 0 ? "default" : "secondary"}>
          {result.seeders}
        </Badge>
      </div>
      <div className={cn("px-2 flex items-center min-w-0 text-sm text-muted-foreground truncate", isSelected && "text-accent-foreground")}>
        {categoryLabel}
      </div>
      <div className={cn("px-2 flex items-center min-w-0 text-sm", isSelected && "text-accent-foreground")}>
        {result.source ? (
          <Badge variant="outline">{result.source}</Badge>
        ) : (
          <span className="text-muted-foreground">-</span>
        )}
      </div>
      <div className={cn("px-2 flex items-center min-w-0 text-sm", isSelected && "text-accent-foreground")}>
        {result.collection ? (
          <Badge variant="outline" className="truncate max-w-full">{result.collection}</Badge>
        ) : (
          <span className="text-muted-foreground">-</span>
        )}
      </div>
      <div className={cn("px-2 flex items-center min-w-0 text-sm", isSelected && "text-accent-foreground")}>
        {result.group ? (
          <Badge variant="outline" className="truncate max-w-full">{result.group}</Badge>
        ) : (
          <span className="text-muted-foreground">-</span>
        )}
      </div>
      <div className={cn("px-2 flex items-center", isSelected && "text-accent-foreground")}>
        {result.downloadVolumeFactor === 0 && (
          <Badge variant="default">{t("results.free")}</Badge>
        )}
        {result.downloadVolumeFactor > 0 && result.downloadVolumeFactor < 1 && (
          <Badge variant="secondary">{result.downloadVolumeFactor * 100}%</Badge>
        )}
      </div>
      <div className={cn("px-2 flex items-center text-sm text-muted-foreground", isSelected && "text-accent-foreground")}>
        {publishedLabel}
      </div>
      <div className="px-2 flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => onDownload(result)}
              disabled={!result.downloadUrl}
            >
              <Download className="h-3.5 w-3.5" />
              <span className="sr-only">{t("card.download")}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("table.downloadTorrent")}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => onViewDetails(result)}
              disabled={!result.infoUrl}
            >
              <ExternalLink className="h-3.5 w-3.5" />
              <span className="sr-only">{t("card.viewDetails")}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent>{result.infoUrl ? t("table.viewDetails") : t("table.noInfoUrl")}</TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
})

export function Search() {
  const { t } = useTranslation("search")
  const SUGGESTION_BLUR_DELAY_MS = 100
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [results, setResults] = useState<TorznabSearchResult[]>([])
  const [total, setTotal] = useState(0)
  const [indexers, setIndexers] = useState<TorznabIndexer[]>([])
  const [selectedIndexers, setSelectedIndexers] = useState<Set<number>>(new Set())
  const [indexerSheetOpen, setIndexerSheetOpen] = useState(false)
  const [searchType, setSearchType] = useState<SearchType>("auto")
  const [loadingIndexers, setLoadingIndexers] = useState(true)
  const { instances, isLoading: loadingInstances } = useInstances()
  const [selectedInstanceId, setSelectedInstanceId] = useState<number | null>(null)
  const [instanceMenuOpen, setInstanceMenuOpen] = useState(false)
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [addDialogPayload, setAddDialogPayload] = useState<AddTorrentDropPayload | null>(null)
  const [resultsFilter, setResultsFilter] = useState("")
  const [columnFilters, setColumnFilters] = useState<Record<string, ColumnFilter>>({})
  const [sortColumn, setSortColumn] = useState<"title" | "indexer" | "size" | "seeders" | "category" | "published" | "source" | "collection" | "group" | null>("seeders")
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("desc")
  const [cacheMetadata, setCacheMetadata] = useState<TorznabSearchResponse["cache"] | null>(null)
  const [refreshConfirmOpen, setRefreshConfirmOpen] = useState(false)
  const [refreshCooldownUntil, setRefreshCooldownUntil] = useState(0)
  const [, forceRefreshTick] = useState(0)
  const [recentSearches, setRecentSearches] = useState<TorznabRecentSearch[] | null>(null)
  const [queryFocused, setQueryFocused] = useState(false)
  const [showAdvancedParams, setShowAdvancedParams] = useState(false)
  const [advancedParams, setAdvancedParams] = useState<AdvancedParamsState>(() => ({ ...ADVANCED_PARAM_DEFAULTS }))
  const [selectedResultKey, setSelectedResultKey] = useState<string | null>(null)
  const searchTypeOptions = useMemo(() => getSearchTypeOptions(t), [t])
  const searchPlaceholder = useMemo(() => t(SEARCH_PLACEHOLDER_KEYS[searchType]), [searchType, t])
  const advancedParamConfig = useMemo(
    () => ADVANCED_PARAM_CONFIG.map((config) => ({
      ...config,
      label: t(config.labelKey),
      placeholder: config.placeholderKey ? t(config.placeholderKey) : undefined,
    })),
    [t]
  )
  const hasAdvancedParams = useMemo(() => Object.values(advancedParams).some(value => value.trim() !== ""), [advancedParams])
  const queryInputRef = useRef<HTMLInputElement | null>(null)
  const blurTimeoutRef = useRef<number | null>(null)
  const rafIdRef = useRef<number | null>(null)
  const tableScrollRef = useRef<HTMLDivElement | null>(null)
  const cardsScrollRef = useRef<HTMLDivElement | null>(null)
  const { formatDate } = useDateTimeFormatters()
  const closeSuggestions = useCallback(() => {
    if (blurTimeoutRef.current !== null) {
      window.clearTimeout(blurTimeoutRef.current)
      blurTimeoutRef.current = null
    }
    setQueryFocused(false)
    queryInputRef.current?.blur()
  }, [])
  const persistSelectedInstanceId = useCallback((instanceId: number | null) => {
    setSelectedInstanceId(instanceId)
    if (typeof window === "undefined") {
      return
    }
    try {
      if (instanceId === null) {
        window.sessionStorage.removeItem(LAST_USED_INSTANCE_KEY)
      } else {
        window.sessionStorage.setItem(LAST_USED_INSTANCE_KEY, String(instanceId))
      }
    } catch (error) {
      console.error("Failed to persist instance selection", error)
    }
  }, [])

  const handleAdvancedParamChange = useCallback((key: keyof AdvancedParamsState, value: string) => {
    setAdvancedParams(prev => ({ ...prev, [key]: value }))
  }, [])

  const handleResetAdvancedParams = useCallback(() => {
    setAdvancedParams({ ...ADVANCED_PARAM_DEFAULTS })
  }, [])

  // Cleanup timeouts and RAF on unmount
  useEffect(() => {
    return () => {
      if (blurTimeoutRef.current !== null) {
        window.clearTimeout(blurTimeoutRef.current)
      }
      if (rafIdRef.current !== null) {
        cancelAnimationFrame(rafIdRef.current)
      }
    }
  }, [])

  const formatCacheTimestamp = useCallback((value?: string | null) => {
    if (!value) {
      return "—"
    }
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) {
      return "—"
    }
    return formatDate(parsed)
  }, [formatDate])
  const hasInstances = (instances?.length ?? 0) > 0
  const targetInstance = useMemo(() => {
    if (!instances || selectedInstanceId === null) {
      return null
    }
    return instances.find(instance => instance.id === selectedInstanceId) ?? null
  }, [instances, selectedInstanceId])
  const totalIndexers = indexers.length
  let indexerSummaryText = t("indexerSummary.noEnabled")
  if (totalIndexers > 0 && selectedIndexers.size === totalIndexers) {
    indexerSummaryText = t("indexerSummary.allEnabled", { count: totalIndexers })
  } else if (totalIndexers > 0) {
    indexerSummaryText = t("indexerSummary.selected", { selected: selectedIndexers.size, total: totalIndexers })
  }

  const REFRESH_COOLDOWN_MS = 30_000
  const refreshCooldownRemaining = Math.max(0, refreshCooldownUntil - Date.now())
  const canForceRefresh = !loading && refreshCooldownRemaining <= 0 && (results.length > 0 || cacheMetadata)
  const showRefreshButton = results.length > 0 || cacheMetadata

  useEffect(() => {
    if (!refreshCooldownUntil) {
      return
    }

    const id = window.setInterval(() => {
      if (Date.now() >= refreshCooldownUntil) {
        setRefreshCooldownUntil(0)
        forceRefreshTick(tick => tick + 1)
        window.clearInterval(id)
      } else {
        forceRefreshTick(tick => tick + 1)
      }
    }, 1_000)

    return () => window.clearInterval(id)
  }, [refreshCooldownUntil, forceRefreshTick])

  const formatBackend = (backend: TorznabIndexer["backend"]) => {
    switch (backend) {
      case "prowlarr":
        return t("indexerSheet.backend.prowlarr")
      case "native":
        return t("indexerSheet.backend.native")
      default:
        return t("indexerSheet.backend.jackett")
    }
  }

  const validateSearchInputs = useCallback((overrideQuery?: string) => {
    const normalizedQuery = (overrideQuery ?? query).trim()

    // Allow search with either query or advanced parameters
    if (!normalizedQuery && !hasAdvancedParams) {
      toast.error(t("toast.enterSearchQuery"))
      return false
    }

    if (selectedIndexers.size === 0) {
      toast.error(t("toast.selectIndexer"))
      return false
    }

    if (indexers.length === 0) {
      toast.error(t("toast.noEnabledIndexers"))
      return false
    }

    return true
  }, [t, indexers.length, query, selectedIndexers, hasAdvancedParams])

  const refreshRecentSearches = useCallback(async () => {
    try {
      const data = await api.getRecentTorznabSearches(20, "general")
      setRecentSearches(Array.isArray(data) ? data : [])
    } catch (error) {
      console.error("Load recent searches error:", error)
      setRecentSearches([])
    }
  }, [])

  const latestReqIdRef = useRef(0)
  const runSearch = useCallback(
    async ({
      bypassCache = false,
      queryOverride,
      searchTypeOverride,
    }: { bypassCache?: boolean; queryOverride?: string; searchTypeOverride?: SearchType } = {}) => {
      const reqId = ++latestReqIdRef.current
      const searchQuery = (queryOverride ?? query).trim()
      const targetSearchType = searchTypeOverride ?? searchType
      const detectedImdbId = extractImdbId(searchQuery)
      const detectedTvdbId = extractTvdbId(searchQuery)
      setLoading(true)
      setCacheMetadata(null)
      setSelectedResultKey(null)
      setResults([])
      setTotal(0)

      try {
        const payload: TorznabSearchRequest = {
          query: searchQuery,
          indexer_ids: Array.from(selectedIndexers),
        }

        const derivedCategories = getCategoriesForSearchType(targetSearchType)
        if (derivedCategories && derivedCategories.length > 0) {
          payload.categories = derivedCategories
        }

        const parseNumberParam = (value: string) => {
          const trimmed = value.trim()
          if (!trimmed) {
            return null
          }
          const parsed = Number(trimmed)
          return Number.isNaN(parsed) ? null : parsed
        }

        const manualImdbId = advancedParams.imdbId.trim()
        const imdbIdToUse = manualImdbId || detectedImdbId || ""
        if (imdbIdToUse) {
          payload.imdb_id = imdbIdToUse
        }

        const manualTvdbId = advancedParams.tvdbId.trim()
        const tvdbIdToUse = manualTvdbId || detectedTvdbId || ""
        if (tvdbIdToUse) {
          payload.tvdb_id = tvdbIdToUse
        }

        const artist = advancedParams.artist.trim()
        if (artist) {
          payload.artist = artist
        }

        const album = advancedParams.album.trim()
        if (album) {
          payload.album = album
        }

        const yearValue = parseNumberParam(advancedParams.year)
        if (yearValue !== null) {
          payload.year = yearValue
        }

        const seasonValue = parseNumberParam(advancedParams.season)
        if (seasonValue !== null) {
          payload.season = seasonValue
        }

        const episodeValue = parseNumberParam(advancedParams.episode)
        if (episodeValue !== null) {
          payload.episode = episodeValue
        }

        const limitValue = parseNumberParam(advancedParams.limit)
        if (limitValue !== null && limitValue > 0) {
          payload.limit = limitValue
        }

        const offsetValue = parseNumberParam(advancedParams.offset)
        if (offsetValue !== null && offsetValue >= 0) {
          payload.offset = offsetValue
        }

        if (bypassCache) {
          payload.cache_mode = "bypass"
        }

        const response = await api.searchTorznab(payload)
        if (reqId !== latestReqIdRef.current) return
        setResults(response.results)
        setTotal(response.total)
        setCacheMetadata(response.cache ?? null)

        if (response.results.length === 0) {
          toast.info(t("toast.noResults"))
        } else {
          const cacheSuffix = response.cache?.hit ? t("toast.cached") : ""
          toast.success(t("toast.foundResults", { total: response.total, suffix: cacheSuffix }))
        }
        void refreshRecentSearches()
      } catch (error) {
        const errorMsg = error instanceof Error ? error.message : "Unknown error"
        toast.error(t("toast.searchFailed", { error: errorMsg }))
        console.error("Search error:", error)
      } finally {
        if (reqId === latestReqIdRef.current) setLoading(false)
      }
    },
    [advancedParams, query, selectedIndexers, refreshRecentSearches, searchType, t]
  )

  // Build a category ID to name map from all indexers
  // Only use parent categories (multiples of 1000) for cleaner display
  const categoryMap = useMemo(() => {
    const map = new Map<number, string>()
    indexers.forEach(indexer => {
      indexer.categories?.forEach(cat => {
        // Store parent categories directly
        if (cat.category_id % 1000 === 0) {
          map.set(cat.category_id, cat.category_name)
        } else {
          // For subcategories, map them to their parent category
          const parentCategoryId = Math.floor(cat.category_id / 1000) * 1000
          // Find parent category name
          const parentCat = indexer.categories?.find(c => c.category_id === parentCategoryId)
          if (parentCat && !map.has(cat.category_id)) {
            map.set(cat.category_id, parentCat.category_name)
          }
        }
      })
    })
    return map
  }, [indexers])

  const indexerOptions = useMemo(() => {
    const uniqueIndexers = Array.from(new Set(indexers.map(i => i.name))).sort()
    return uniqueIndexers.map(i => ({ value: i, label: i }))
  }, [indexers])

  const categoryOptions = useMemo(() => {
    const uniqueCategories = Array.from(new Set(results.map(r => categoryMap.get(r.categoryId) || r.categoryName || String(r.categoryId)))).sort()
    return uniqueCategories.map(c => ({ value: c, label: c }))
  }, [results, categoryMap])

  const sourceOptions = useMemo(() => {
    const uniqueSources = Array.from(new Set(results.map(r => r.source).filter(Boolean))).sort()
    return uniqueSources.map(s => ({ value: s!, label: s! }))
  }, [results])

  const freeleechOptions = [
    { value: "true", label: t("results.free") },
    { value: "0.25", label: "25%" },
    { value: "0.5", label: "50%" },
    { value: "0.75", label: "75%" },
    { value: "false", label: t("results.freeNeutral") },
  ]

  useEffect(() => {
    setSelectedIndexers(prev => {
      if (indexers.length === 0) {
        return new Set()
      }
      const validIds = new Set(indexers.map(idx => idx.id))
      let changed = false
      const next = new Set<number>()
      prev.forEach(id => {
        if (validIds.has(id)) {
          next.add(id)
        } else {
          changed = true
        }
      })
      return changed ? next : prev
    })
  }, [indexers])

  useEffect(() => {
    const loadIndexers = async () => {
      try {
        const data = await api.listTorznabIndexers()
        const enabledIndexers = data.filter(idx => idx.enabled)
        setIndexers(enabledIndexers)
        // Select all enabled indexers by default
        setSelectedIndexers(new Set(enabledIndexers.map(idx => idx.id)))
      } catch (error) {
        toast.error(t("toast.failedToLoadIndexers"))
        console.error("Load indexers error:", error)
      } finally {
        setLoadingIndexers(false)
      }
    }
    loadIndexers()
  }, [t])

  useEffect(() => {
    refreshRecentSearches()
  }, [refreshRecentSearches])

  useEffect(() => {
    if (loadingInstances) {
      return
    }

    const availableInstances = instances ?? []

    if (availableInstances.length === 0) {
      if (selectedInstanceId !== null) {
        persistSelectedInstanceId(null)
      }
      return
    }

    if (selectedInstanceId !== null && availableInstances.some(instance => instance.id === selectedInstanceId)) {
      return
    }

    let nextInstanceId: number | null = null

    if (availableInstances.length === 1) {
      nextInstanceId = availableInstances[0].id
    } else if (typeof window !== "undefined") {
      try {
        const storedValue = window.sessionStorage.getItem(LAST_USED_INSTANCE_KEY)
        if (storedValue) {
          const parsed = parseInt(storedValue, 10)
          if (!Number.isNaN(parsed) && availableInstances.some(instance => instance.id === parsed)) {
            nextInstanceId = parsed
          }
        }
      } catch (error) {
        console.error("Failed to load instance selection", error)
      }
    }

    if (nextInstanceId !== null) {
      persistSelectedInstanceId(nextInstanceId)
    } else if (selectedInstanceId !== null) {
      persistSelectedInstanceId(null)
    }
  }, [instances, loadingInstances, persistSelectedInstanceId, selectedInstanceId])

  const handleInstanceSelection = useCallback((instanceId: number | null) => {
    persistSelectedInstanceId(instanceId)
    setInstanceMenuOpen(false)
  }, [persistSelectedInstanceId, setInstanceMenuOpen])

  const applyIndexerSelectionFromSuggestion = useCallback((indexerIds: number[]) => {
    if (!indexerIds || indexerIds.length === 0 || indexers.length === 0) {
      return
    }

    const enabled = new Set(indexers.map(idx => idx.id))
    const filtered = indexerIds.filter(id => enabled.has(id))
    if (filtered.length === 0) {
      return
    }
    setSelectedIndexers(new Set(filtered))
  }, [indexers])

  const toggleIndexer = (id: number) => {
    setSelectedIndexers(prev => {
      const newSelected = new Set(prev)
      if (newSelected.has(id)) {
        newSelected.delete(id)
      } else {
        newSelected.add(id)
      }
      return newSelected
    })
  }

  const handleSelectAll = () => {
    setSelectedIndexers(new Set(indexers.map(idx => idx.id)))
  }

  const handleDeselectAll = () => {
    setSelectedIndexers(new Set())
  }

  const handleSearch = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!validateSearchInputs()) {
      return
    }
    closeSuggestions()
    await runSearch()
  }

  const handleForceRefreshConfirm = async () => {
    if (!validateSearchInputs()) {
      setRefreshConfirmOpen(false)
      return
    }

    setRefreshConfirmOpen(false)
    setRefreshCooldownUntil(Date.now() + REFRESH_COOLDOWN_MS)
    await runSearch({ bypassCache: true })
  }

  const handleSort = (column: Exclude<typeof sortColumn, null>) => {
    if (sortColumn === column) {
      if (sortOrder === "desc") {
        setSortOrder("asc")
      } else {
        // Reset sorting on third click
        setSortColumn(null)
        setSortOrder("desc")
      }
    } else {
      setSortColumn(column)
      setSortOrder("desc")
    }
  }

  const getSortIcon = (column: Exclude<typeof sortColumn, null>) => {
    if (sortColumn !== column) return null

    return sortOrder === "asc" ? <ChevronUp className="h-4 w-4 shrink-0" /> : <ChevronDown className="h-4 w-4 shrink-0" />
  }

  // Filter and sort results
  const filteredAndSortedResults = useMemo(() => {
    let filtered = results

    // Apply filter
    const activeFilters = Object.values(columnFilters)
    if (activeFilters.length > 0) {
      filtered = results.filter(result => {
        return activeFilters.every(filter => filterSearchResult(result, filter, categoryMap))
      })
    }

    // Apply search filter
    if (resultsFilter.trim()) {
      const filter = resultsFilter.toLowerCase()
      filtered = filtered.filter(result =>
        result.title.toLowerCase().includes(filter) ||
        result.indexer.toLowerCase().includes(filter) ||
        (categoryMap.get(result.categoryId) || result.categoryName || "").toLowerCase().includes(filter) ||
        (result.source || "").toLowerCase().includes(filter) ||
        (result.collection || "").toLowerCase().includes(filter) ||
        (result.group || "").toLowerCase().includes(filter)
      )
    }

    // Apply sorting
    if (!sortColumn) {
      return filtered
    }

    const sorted = [...filtered].sort((a, b) => {
      let aVal: string | number
      let bVal: string | number

      switch (sortColumn) {
        case "title":
          aVal = a.title.toLowerCase()
          bVal = b.title.toLowerCase()
          break
        case "indexer":
          aVal = a.indexer.toLowerCase()
          bVal = b.indexer.toLowerCase()
          break
        case "size":
          aVal = a.size
          bVal = b.size
          break
        case "seeders":
          aVal = a.seeders
          bVal = b.seeders
          break
        case "category":
          aVal = (categoryMap.get(a.categoryId) || a.categoryName || "").toLowerCase()
          bVal = (categoryMap.get(b.categoryId) || b.categoryName || "").toLowerCase()
          break
        case "published":
          aVal = new Date(a.publishDate).getTime()
          bVal = new Date(b.publishDate).getTime()
          break
        case "source":
          aVal = (a.source || "").toLowerCase()
          bVal = (b.source || "").toLowerCase()
          break
        case "collection":
          aVal = (a.collection || "").toLowerCase()
          bVal = (b.collection || "").toLowerCase()
          break
        case "group":
          aVal = (a.group || "").toLowerCase()
          bVal = (b.group || "").toLowerCase()
          break
        default:
          return 0
      }

      if (aVal < bVal) return sortOrder === "asc" ? -1 : 1
      if (aVal > bVal) return sortOrder === "asc" ? 1 : -1
      return 0
    })

    return sorted
  }, [results, resultsFilter, columnFilters, sortColumn, sortOrder, categoryMap])

  const tableVirtualizer = useVirtualizer({
    count: filteredAndSortedResults.length,
    getScrollElement: () => tableScrollRef.current,
    estimateSize: () => SEARCH_ROW_HEIGHT,
    overscan: filteredAndSortedResults.length > 50000 ? 3 : filteredAndSortedResults.length > 10000 ? 5 : filteredAndSortedResults.length > 1000 ? 15 : 30,
    getItemKey: useCallback(
      (index: number) => getSearchResultKey(filteredAndSortedResults[index]),
      [filteredAndSortedResults]
    ),
  })

  const cardsVirtualizer = useVirtualizer({
    count: filteredAndSortedResults.length,
    getScrollElement: () => cardsScrollRef.current,
    estimateSize: () => SEARCH_CARD_HEIGHT,
    overscan: filteredAndSortedResults.length > 1000 ? 5 : 10,
    getItemKey: useCallback(
      (index: number) => getSearchResultKey(filteredAndSortedResults[index]),
      [filteredAndSortedResults]
    ),
  })

  useEffect(() => {
    tableVirtualizer.measure()
    cardsVirtualizer.measure()
  }, [filteredAndSortedResults.length, tableVirtualizer, cardsVirtualizer])

  const selectedResult = useMemo(() => {
    if (!selectedResultKey) {
      return null
    }
    return results.find(result => getSearchResultKey(result) === selectedResultKey) ?? null
  }, [results, selectedResultKey])

  useEffect(() => {
    if (!selectedResultKey) {
      return
    }
    const stillVisible = filteredAndSortedResults.some(result => getSearchResultKey(result) === selectedResultKey)
    if (!stillVisible) {
      setSelectedResultKey(null)
    }
  }, [filteredAndSortedResults, selectedResultKey])

  const suggestionMatches = useMemo(() => {
    const searches = recentSearches ?? []
    if (searches.length === 0) {
      return []
    }

    const normalizedQuery = query.trim().toLowerCase()
    if (!normalizedQuery) {
      return searches.slice(0, 5)
    }

    const matches = searches.filter(search => search.query.toLowerCase().includes(normalizedQuery))
    return matches.slice(0, 5)
  }, [recentSearches, query])

  const shouldShowSuggestions = queryFocused && suggestionMatches.length > 0

  const cacheBadge = useMemo(() => {
    if (!cacheMetadata) {
      return { label: "", variant: "outline" as const }
    }
    if (cacheMetadata.source === "hybrid") {
      return { label: t("cache.cacheAndLive"), variant: "secondary" as const }
    }
    if (cacheMetadata.hit) {
      return { label: t("cache.cacheHit"), variant: "secondary" as const }
    }
    return { label: t("cache.liveFetch"), variant: "outline" as const }
  }, [cacheMetadata, t])

  const handleSuggestionClick = useCallback((search: TorznabRecentSearch) => {
    setQuery(search.query)
    const derivedType = inferSearchTypeFromCategories(search.categories) ?? "auto"
    setSearchType(derivedType)
    applyIndexerSelectionFromSuggestion(search.indexerIds)
    const normalized = search.query.trim()
    if (!validateSearchInputs(normalized)) {
      return
    }
    closeSuggestions()
    void runSearch({ queryOverride: normalized, searchTypeOverride: derivedType })
  }, [applyIndexerSelectionFromSuggestion, closeSuggestions, runSearch, validateSearchInputs])

  const handleDownload = useCallback((result: TorznabSearchResult) => {
    window.open(result.downloadUrl, "_blank", "noopener,noreferrer")
  }, [])

  const handleAddTorrent = useCallback((result: TorznabSearchResult, overrideInstanceId?: number) => {
    const targetId = overrideInstanceId ?? selectedInstanceId

    if (!targetId) {
      if (!hasInstances) {
        toast.error(t("toast.addInstanceFirst"))
      } else {
        toast.error(t("toast.chooseInstance"))
        setInstanceMenuOpen(true)
      }
      return
    }

    if (!result.downloadUrl) {
      toast.error(t("toast.noDownloadUrl"))
      return
    }

    persistSelectedInstanceId(targetId)
    setAddDialogPayload({ type: "url", urls: [result.downloadUrl], indexerId: result.indexerId })
    setAddDialogOpen(true)
  }, [hasInstances, persistSelectedInstanceId, selectedInstanceId, setInstanceMenuOpen, t])

  const handleViewDetails = useCallback((result: TorznabSearchResult) => {
    if (!result.infoUrl) {
      toast.error(t("toast.noInfoUrl"))
      return
    }
    try {
      const url = new URL(result.infoUrl)
      if (!["http:", "https:"].includes(url.protocol)) {
        toast.error(t("toast.invalidProtocol"))
        return
      }
    } catch {
      toast.error(t("toast.invalidUrl"))
      return
    }

    window.open(result.infoUrl, "_blank", "noopener,noreferrer")
  }, [t])

  const handleToggleResultSelection = useCallback((result: TorznabSearchResult) => {
    const resultKey = getSearchResultKey(result)
    setSelectedResultKey(prev => prev === resultKey ? null : resultKey)
  }, [])

  const handleClearSelection = () => {
    setSelectedResultKey(null)
  }

  const handleDialogOpenChange = (open: boolean) => {
    setAddDialogOpen(open)
    if (!open) {
      setAddDialogPayload(null)
    }
  }

  let addButtonTitle = t("toast.addInstanceFirst")
  let primaryAddButtonLabel = t("card.addToInstance")
  if (targetInstance) {
    addButtonTitle = t("card.addToNamed", { name: targetInstance.name })
    primaryAddButtonLabel = t("card.addToNamed", { name: targetInstance.name })
  } else if (hasInstances) {
    addButtonTitle = t("toast.chooseInstance")
  }
  const instancesAvailable = hasInstances
  let instanceSelectorLabel = t("instanceSelector.noInstances")
  if (targetInstance?.connected) {
    instanceSelectorLabel = t("instanceSelector.targetNamed", { name: targetInstance.name })
  } else if (targetInstance) {
    instanceSelectorLabel = t("instanceSelector.targetNamedOffline", { name: targetInstance.name })
  } else if (instancesAvailable) {
    instanceSelectorLabel = t("instanceSelector.chooseTarget")
  }
  const getInstanceActionLabel = (instance: { name: string; connected: boolean }) => {
    if (instance.connected) {
      return t("card.addToNamed", { name: instance.name })
    }
    return t("card.addToNamedOffline", { name: instance.name })
  }

  let refreshButtonTitle = t("results.refreshFromIndexers")
  if (refreshCooldownRemaining > 0) {
    refreshButtonTitle = t("results.refreshReadyIn", { seconds: Math.ceil(refreshCooldownRemaining / 1000) })
  }

  return (
    <TooltipProvider>
      <div className="space-y-6 p-4 lg:p-6">
        <div className="flex-1 space-y-2">
          <h1 className="text-2xl font-semibold">{t("pageTitle")}</h1>
          <p className="text-sm text-muted-foreground">
            {t("pageDescription")}
          </p>
        </div>

        <div className="rounded-lg border bg-muted/40 px-4 py-2">
          <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
            <Sheet open={indexerSheetOpen} onOpenChange={setIndexerSheetOpen}>
              <SheetTrigger asChild>
                <Button type="button" variant="outline" size="sm" className="flex w-full items-center justify-center gap-2 sm:w-auto sm:justify-start">
                  <SlidersHorizontal className="h-3.5 w-3.5" />
                  <span className="text-sm">{t("indexerSummary.label")}: {indexerSummaryText}</span>
                </Button>
              </SheetTrigger>

              <SheetContent side="right" className="flex h-full max-h-dvh max-w-xl flex-col overflow-hidden p-0">
                <SheetHeader>
                  <SheetTitle>{t("indexerSheet.title")}</SheetTitle>
                  <SheetDescription>{t("indexerSheet.description")}</SheetDescription>
                </SheetHeader>

                <div className="flex flex-1 min-h-0 flex-col gap-4 overflow-hidden px-4 pb-4">
                  <div className="flex flex-wrap gap-2">
                    <Button type="button" variant="outline" size="sm" onClick={handleSelectAll}>
                      {t("indexerSheet.selectAll")}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" onClick={handleDeselectAll}>
                      {t("indexerSheet.clearSelection")}
                    </Button>
                  </div>

                  <div className="flex-1 min-h-0">
                    <ScrollArea className="h-full rounded-lg border">
                      <div className="space-y-2 p-3">
                        {indexers.map(indexer => {
                          const parentCategories = indexer.categories
                            ?.filter(cat => cat.category_id % 1000 === 0)
                            .map(cat => cat.category_name) || []
                          const hasCategories = parentCategories.length > 0
                          const isSelected = selectedIndexers.has(indexer.id)

                          return (
                            <label
                              key={indexer.id}
                              htmlFor={`indexer-${indexer.id}`}
                              className={`flex w-full items-start gap-3 rounded-md border p-3 transition-colors cursor-pointer ${isSelected? "bg-muted/40 border-muted-foreground/20": "hover:bg-muted/20"
                              }`}
                            >
                              <Checkbox
                                id={`indexer-${indexer.id}`}
                                checked={isSelected}
                                onCheckedChange={() => toggleIndexer(indexer.id)}
                                className="mt-0.5 shrink-0"
                              />
                              <div className="min-w-0 flex-1 space-y-1.5">
                                <div className="flex items-center gap-2 text-sm font-medium leading-none">
                                  <span className="truncate">{indexer.name}</span>
                                  <Badge variant="secondary" className="text-[10px] font-normal capitalize">
                                    {formatBackend(indexer.backend)}
                                  </Badge>
                                </div>
                                {hasCategories ? (
                                  <div className="flex flex-wrap gap-1">
                                    {parentCategories.map((catName, idx) => (
                                      <Badge key={idx} variant="outline" className="text-[10px] font-normal">
                                        {catName}
                                      </Badge>
                                    ))}
                                  </div>
                                ) : (
                                  <p className="text-xs text-muted-foreground">{t("indexerSheet.noCategories")}</p>
                                )}
                              </div>
                            </label>
                          )
                        })}
                      </div>
                    </ScrollArea>
                  </div>
                </div>

                <SheetFooter className="border-t bg-muted/30 p-4 sm:flex-row sm:items-center sm:justify-between">
                  <p className="text-sm text-muted-foreground">
                    {t("indexerSheet.footer", { selected: selectedIndexers.size, total: indexers.length })}
                  </p>
                  <SheetClose asChild>
                    <Button type="button" size="sm">
                      {t("indexerSheet.done")}
                    </Button>
                  </SheetClose>
                </SheetFooter>
              </SheetContent>
            </Sheet>

            <div className="flex flex-wrap items-center gap-2 sm:ml-auto">
              <DropdownMenu open={instanceMenuOpen} onOpenChange={setInstanceMenuOpen}>
                <DropdownMenuTrigger asChild>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={loadingInstances || !instancesAvailable}
                    className="flex w-full items-center justify-center gap-2 sm:w-auto sm:justify-start"
                  >
                    <span className="text-sm">{instanceSelectorLabel}</span>
                    <ChevronDown className="h-3 w-3" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-64">
                  {instancesAvailable ? (
                    <>
                      {instances?.map(instance => (
                        <DropdownMenuItem
                          key={instance.id}
                          onSelect={(event) => {
                            event.preventDefault()
                            handleInstanceSelection(instance.id)
                          }}
                        >
                          <Check
                            className={`h-4 w-4 text-muted-foreground ${targetInstance?.id === instance.id ? "opacity-100" : "opacity-0"}`}
                          />
                          <div className="flex flex-col">
                            <span className="font-medium">{instance.name}</span>
                            {!instance.connected && (
                              <span className="text-xs text-muted-foreground">{t("instanceSelector.offline")}</span>
                            )}
                          </div>
                        </DropdownMenuItem>
                      ))}
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        onSelect={(event) => {
                          event.preventDefault()
                          handleInstanceSelection(null)
                        }}
                        disabled={!targetInstance}
                      >
                        {t("instanceSelector.clearSelection")}
                      </DropdownMenuItem>
                    </>
                  ) : (
                    <DropdownMenuItem disabled>{t("instanceSelector.noInstancesConfigured")}</DropdownMenuItem>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
              {!instancesAvailable && !loadingInstances && (
                <p className="text-xs text-muted-foreground">
                  {t("instanceSelector.addInstanceNote")}
                </p>
              )}
            </div>
          </div>
        </div>

        <Card>
          <CardContent>
            <form onSubmit={handleSearch} className="space-y-4">
              <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:gap-2">
                <div className="flex items-center gap-2">
                  <div className="shrink-0 min-w-30 max-w-45">
                    <Label htmlFor="search-type" className="sr-only">{t("searchForm.searchType")}</Label>
                    <Select value={searchType} onValueChange={(value) => setSearchType(value as SearchType)}>
                      <SelectTrigger id="search-type" className="w-full">
                        <SelectValue placeholder={t("searchTypes.auto.label")} />
                      </SelectTrigger>
                      <SelectContent>
                        {searchTypeOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button
                      type="button"
                      variant={showAdvancedParams ? "default" : "outline"}
                      size="default"
                      className={cn(
                        "border! px-4! py-2.5! h-9",
                        showAdvancedParams? "border-primary bg-primary text-primary-foreground shadow-xs hover:bg-primary/90": "border-input dark:border-input"
                      )}
                      onClick={() => setShowAdvancedParams(prev => !prev)}
                    >
                      <SlidersHorizontal className="mr-2 h-4 w-4" />
                      {t("searchForm.advanced")}
                    </Button>
                    {hasAdvancedParams && (
                      <>
                        <Badge variant="secondary" className="text-[10px] uppercase tracking-wide">{t("searchForm.active")}</Badge>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="text-muted-foreground"
                          onClick={handleResetAdvancedParams}
                        >
                          {t("searchForm.clear")}
                        </Button>
                      </>
                    )}
                  </div>
                </div>
                <div className="flex flex-1 items-center gap-2 min-w-0">
                  <div className="flex-1 relative min-w-0">
                    <Label htmlFor="query" className="sr-only">{t("searchForm.searchQuery")}</Label>
                    <Input
                      ref={queryInputRef}
                      id="query"
                      type="text"
                      autoComplete="off"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                      onFocus={() => {
                        // Clear any pending blur timeout
                        if (blurTimeoutRef.current !== null) {
                          window.clearTimeout(blurTimeoutRef.current)
                          blurTimeoutRef.current = null
                        }
                        setQueryFocused(true)
                      }}
                      onBlur={() => {
                        // Clear any existing timeout
                        if (blurTimeoutRef.current !== null) {
                          window.clearTimeout(blurTimeoutRef.current)
                        }
                        // Delay blur to allow suggestion clicks before SUGGESTION_BLUR_DELAY_MS expires
                        blurTimeoutRef.current = window.setTimeout(() => {
                          setQueryFocused(false)
                          blurTimeoutRef.current = null
                        }, SUGGESTION_BLUR_DELAY_MS)
                      }}
                      placeholder={searchPlaceholder}
                      disabled={loading}
                    />
                    {shouldShowSuggestions && (
                      <div className="absolute left-0 right-0 z-50 mt-1 rounded-md border bg-popover shadow-lg">
                        {suggestionMatches.map((search) => {
                          const suggestionType = inferSearchTypeFromCategories(search.categories)
                          const suggestionTypeLabel = getSearchTypeLabel(suggestionType ?? "auto", t)
                          return (
                            <button
                              type="button"
                              key={search.cacheKey}
                              className="w-full px-3 py-2 text-left text-sm hover:bg-muted focus-visible:outline-none"
                              onMouseDown={(event) => event.preventDefault()}
                              onClick={() => handleSuggestionClick(search)}
                            >
                              <div className="font-medium text-foreground">
                                {search.query}
                              </div>
                              <div className="text-xs text-muted-foreground">
                                {t("historyMeta", {
                                  type: suggestionTypeLabel,
                                  count: search.totalResults,
                                  timestamp: formatCacheTimestamp(search.lastUsedAt ?? search.cachedAt),
                                })}
                              </div>
                            </button>
                          )
                        })}
                      </div>
                    )}
                  </div>
                  <Button
                    type="submit"
                    disabled={loading || (!query.trim() && !hasAdvancedParams) || selectedIndexers.size === 0}
                    className="shrink-0"
                  >
                    <SearchIcon className="mr-2 h-4 w-4" />
                    {loading ? t("searchForm.searching") : t("searchForm.search")}
                  </Button>
                </div>
              </div>

              {/* Advanced Search Parameters */}
              {showAdvancedParams && (
                <div className="rounded-lg border bg-muted/40 p-4 space-y-4">
                  <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                    {advancedParamConfig.map(({ key, label, placeholder, type, min }) => (
                      <div key={key} className="space-y-1.5">
                        <Label htmlFor={`advanced-${key}`} className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                          {label}
                        </Label>
                        <Input
                          id={`advanced-${key}`}
                          type={type}
                          inputMode={type === "number" ? "numeric" : undefined}
                          min={type === "number" && typeof min !== "undefined" ? min : undefined}
                          placeholder={placeholder}
                          value={advancedParams[key]}
                          onChange={(e) => handleAdvancedParamChange(key, e.target.value)}
                        />
                      </div>
                    ))}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {t("searchForm.advancedNote")}
                  </p>
                </div>
              )}
              {!loadingIndexers && indexers.length === 0 && (
                <div className="text-sm text-muted-foreground">
                  {t("searchForm.noIndexers")}{" "}
                  <Link to="/settings" search={{ tab: "indexers" }} className="font-medium text-primary underline-offset-4 hover:underline">
                    {t("searchForm.indexersPage")}
                  </Link>
                  .
                </div>
              )}

            </form>

            {results.length > 0 && (
              <div className="mt-6">
                <div className="mb-2 text-xs text-muted-foreground">
                  {t("results.showing", { filtered: filteredAndSortedResults.length, total })}
                </div>
                <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:gap-2">
                  <div className="w-full sm:min-w-50 sm:flex-1 min-w-0 relative">
                    <Input
                      type="text"
                      placeholder={t("results.filterPlaceholder")}
                      value={resultsFilter}
                      onChange={(e) => setResultsFilter(e.target.value)}
                      className="pr-8"
                    />

                    {resultsFilter && (
                      <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="p-1 hover:bg-muted rounded-sm transition-colors hidden sm:block"
                              onClick={() => {
                                setResultsFilter("")
                              }}
                            >
                              <X className="h-3.5 w-3.5 text-muted-foreground" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent>{t("results.clearSearch")}</TooltipContent>
                        </Tooltip>
                      </div>
                    )}
                  </div>
                  {Object.keys(columnFilters).length > 0 && (
                    <Tooltip>
                      <TooltipTrigger
                        asChild
                        onFocus={(e) => {
                          // Prevent tooltip from showing on focus - only show on hover
                          e.preventDefault()
                        }}
                      >
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setColumnFilters({})}
                          className="h-9"
                        >
                          <X className="h-4 w-4" />
                          <span className="sr-only">{t("results.clearAllFilters")}</span>
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>{t("results.clearAllFilters")}</TooltipContent>
                    </Tooltip>
                  )}
                  {selectedResult && (
                    <>
                      <div className="hidden sm:flex flex-wrap items-center gap-2">
                        <div className="inline-flex items-stretch rounded-md overflow-hidden">
                          <Button
                            type="button"
                            size="sm"
                            onClick={() => handleAddTorrent(selectedResult)}
                            disabled={!instancesAvailable}
                            title={addButtonTitle}
                            className="rounded-none border-none h-9"
                          >
                            <Plus className="h-4 w-4" />
                            <span className="hidden lg:inline ml-2">{primaryAddButtonLabel}</span>
                          </Button>
                          <div className="w-px bg-primary-foreground/20" />
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button
                                type="button"
                                size="sm"
                                className="rounded-none border-none h-9 px-2"
                                disabled={!instancesAvailable}
                              >
                                <ChevronDown className="h-4 w-4" />
                                <span className="sr-only">{t("results.pickInstance")}</span>
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" className="w-56">
                              {instances?.map(instance => (
                                <DropdownMenuItem
                                  key={instance.id}
                                  onSelect={(event) => {
                                    event.preventDefault()
                                    handleAddTorrent(selectedResult, instance.id)
                                  }}
                                >
                                  {getInstanceActionLabel(instance)}
                                </DropdownMenuItem>
                              ))}
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </div>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          onClick={handleClearSelection}
                        >
                          {t("results.clearSelectionButton")}
                        </Button>
                      </div>
                      <div className="sm:hidden">
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button type="button" size="sm" variant="outline" className="w-full">
                              {t("results.actions")}
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem
                              onSelect={(event) => {
                                event.preventDefault()
                                handleAddTorrent(selectedResult)
                              }}
                              disabled={!instancesAvailable}
                            >
                              <Plus className="mr-2 h-4 w-4" /> {primaryAddButtonLabel}
                            </DropdownMenuItem>
                            {instancesAvailable && (
                              <DropdownMenuSub>
                                <DropdownMenuSubTrigger>
                                  {t("results.quickAddTo")}
                                </DropdownMenuSubTrigger>
                                <DropdownMenuSubContent>
                                  {instances?.map(instance => (
                                    <DropdownMenuItem
                                      key={instance.id}
                                      onSelect={(event) => {
                                        event.preventDefault()
                                        handleAddTorrent(selectedResult, instance.id)
                                      }}
                                    >
                                      {getInstanceActionLabel(instance)}
                                    </DropdownMenuItem>
                                  ))}
                                </DropdownMenuSubContent>
                              </DropdownMenuSub>
                            )}
                            <DropdownMenuItem
                              onSelect={(event) => {
                                event.preventDefault()
                                if (selectedResult.downloadUrl) {
                                  handleDownload(selectedResult)
                                }
                              }}
                              disabled={!selectedResult.downloadUrl}
                            >
                              <Download className="mr-2 h-4 w-4" /> {t("results.download")}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onSelect={(event) => {
                                event.preventDefault()
                                if (selectedResult.infoUrl) {
                                  handleViewDetails(selectedResult)
                                }
                              }}
                              disabled={!selectedResult.infoUrl}
                            >
                              <ExternalLink className="mr-2 h-4 w-4" /> {t("results.viewDetails")}
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </>
                  )}
                  <div className="flex items-center gap-2 shrink-0 sm:ml-auto">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Badge
                          variant={cacheBadge.variant}
                          className={!cacheMetadata ? "invisible" : ""}
                        >
                          {cacheBadge.label}
                        </Badge>
                      </TooltipTrigger>
                      {cacheMetadata && (
                        <TooltipContent>
                          <p className="text-xs">
                            {t("results.cachedSummary", {
                              cached: formatCacheTimestamp(cacheMetadata.cachedAt),
                              expires: formatCacheTimestamp(cacheMetadata.expiresAt),
                            })}
                            <br />
                            {t("results.cacheSource", { source: cacheMetadata.source, scope: cacheMetadata.scope })}
                          </p>
                        </TooltipContent>
                      )}
                    </Tooltip>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className={`h-7 w-7 opacity-40 transition-opacity hover:opacity-100 ${!showRefreshButton ? "invisible" : ""}`}
                      onClick={() => setRefreshConfirmOpen(true)}
                      disabled={!canForceRefresh}
                      title={refreshButtonTitle}
                    >
                      <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
                    </Button>
                  </div>
                </div>
                {/* Mobile: Card-based view */}
                <div ref={cardsScrollRef} className="sm:hidden max-h-150 overflow-auto will-change-transform contain-paint">
                  <div style={{ height: `${cardsVirtualizer.getTotalSize()}px`, width: "100%", position: "relative" }}>
                    {cardsVirtualizer.getVirtualItems().map((virtualRow) => {
                      const result = filteredAndSortedResults[virtualRow.index]
                      return (
                        <div
                          key={virtualRow.key}
                          data-index={virtualRow.index}
                          ref={cardsVirtualizer.measureElement}
                          style={{
                            position: "absolute",
                            top: 0,
                            left: 0,
                            width: "100%",
                            transform: `translateY(${virtualRow.start}px)`,
                            paddingBottom: "8px",
                            contain: "layout style",
                          }}
                        >
                          <SearchResultCard
                            result={result}
                            isSelected={selectedResultKey === getSearchResultKey(result)}
                            onSelect={() => handleToggleResultSelection(result)}
                            onAddTorrent={(overrideInstanceId) => handleAddTorrent(result, overrideInstanceId)}
                            onDownload={() => handleDownload(result)}
                            onViewDetails={() => handleViewDetails(result)}
                            categoryName={categoryMap.get(result.categoryId) || result.categoryName || t("results.categoryFallback", { id: result.categoryId })}
                            formatSize={formatBytes}
                            formatDate={formatCacheTimestamp}
                            instances={instances}
                            hasInstances={hasInstances}
                            targetInstanceName={targetInstance?.name}
                          />
                        </div>
                      )
                    })}
                  </div>
                </div>

                {/* Desktop: Full table view */}
                <div ref={tableScrollRef} className="hidden sm:block max-h-150 overflow-auto border rounded-md will-change-transform contain-paint">
                  <div style={{ position: "relative", minWidth: `${SEARCH_TABLE_MIN_WIDTH}px` }}>
                    <div
                      className="sticky top-0 z-20 grid bg-card border-b"
                      style={{ gridTemplateColumns: SEARCH_TABLE_GRID_TEMPLATE }}
                    >
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("title")}>
                          <span className="select-none">{t("table.title")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("title")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="title"
                                columnName={t("table.title")}
                                columnType="string"
                                currentFilter={columnFilters.title}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.title = filter
                                    else delete next.title
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("indexer")}>
                          <span className="select-none">{t("table.indexer")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("indexer")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="indexer"
                                columnName={t("table.indexer")}
                                columnType="enum"
                                options={indexerOptions}
                                currentFilter={columnFilters.indexer}
                                multiSelect={true}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.indexer = filter
                                    else delete next.indexer
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("size")}>
                          <span className="select-none">{t("table.size")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("size")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="size"
                                columnName={t("table.size")}
                                columnType="size"
                                currentFilter={columnFilters.size}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.size = filter
                                    else delete next.size
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("seeders")}>
                          <span className="select-none">{t("table.seeders")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("seeders")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="seeders"
                                columnName={t("table.seeders")}
                                columnType="number"
                                currentFilter={columnFilters.seeders}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.seeders = filter
                                    else delete next.seeders
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("category")}>
                          <span className="select-none">{t("table.category")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("category")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="category"
                                columnName={t("table.category")}
                                columnType="enum"
                                options={categoryOptions}
                                currentFilter={columnFilters.category}
                                multiSelect={true}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.category = filter
                                    else delete next.category
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("source")}>
                          <span className="select-none">{t("table.source")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("source")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="source"
                                columnName={t("table.source")}
                                columnType="enum"
                                options={sourceOptions}
                                currentFilter={columnFilters.source}
                                multiSelect={true}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.source = filter
                                    else delete next.source
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("collection")}>
                          <span className="select-none">{t("table.collection")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("collection")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="collection"
                                columnName={t("table.collection")}
                                columnType="string"
                                currentFilter={columnFilters.collection}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.collection = filter
                                    else delete next.collection
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("group")}>
                          <span className="select-none">{t("table.group")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("group")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="group"
                                columnName={t("table.group")}
                                columnType="string"
                                currentFilter={columnFilters.group}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.group = filter
                                    else delete next.group
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 select-none">
                          <span>{t("table.freeleech")}</span>
                          <ColumnFilterPopover
                            columnId="freeleech"
                            columnName={t("table.freeleech")}
                            columnType="enum"
                            options={freeleechOptions}
                            currentFilter={columnFilters.freeleech}
                            multiSelect={true}
                            onApply={(filter) => {
                              setColumnFilters(prev => {
                                const next = { ...prev }
                                if (filter) next.freeleech = filter
                                else delete next.freeleech
                                return next
                              })
                            }}
                          />
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <div className="group flex w-full items-center justify-between gap-2 cursor-pointer select-none" onClick={() => handleSort("published")}>
                          <span className="select-none">{t("table.published")}</span>
                          <div className="flex items-center gap-1">
                            {getSortIcon("published")}
                            <div onClick={(e) => e.stopPropagation()}>
                              <ColumnFilterPopover
                                columnId="published"
                                columnName={t("table.published")}
                                columnType="date"
                                currentFilter={columnFilters.published}
                                onApply={(filter) => {
                                  setColumnFilters(prev => {
                                    const next = { ...prev }
                                    if (filter) next.published = filter
                                    else delete next.published
                                    return next
                                  })
                                }}
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div className="h-10 px-2 flex items-center text-sm font-medium text-muted-foreground">
                        <span className="sr-only">{t("table.actions")}</span>
                      </div>
                    </div>

                    <div style={{ height: `${tableVirtualizer.getTotalSize()}px`, width: "100%", position: "relative" }}>
                      {tableVirtualizer.getVirtualItems().map((virtualRow) => {
                        const result = filteredAndSortedResults[virtualRow.index]
                        return (
                          <SearchTableRow
                            key={virtualRow.key}
                            result={result}
                            isSelected={selectedResultKey === getSearchResultKey(result)}
                            isEven={virtualRow.index % 2 === 0}
                            height={virtualRow.size}
                            translateY={virtualRow.start}
                            categoryLabel={categoryMap.get(result.categoryId) || result.categoryName || t("results.categoryFallback", { id: result.categoryId })}
                            publishedLabel={formatCacheTimestamp(result.publishDate)}
                            onToggleSelection={handleToggleResultSelection}
                            onDownload={handleDownload}
                            onViewDetails={handleViewDetails}
                          />
                        )
                      })}
                    </div>
                  </div>
                </div>
              </div>
            )}

            {!loading && results.length === 0 && total === 0 && query && (
              <div className="mt-6 text-center text-muted-foreground">
                {t("results.noResultsFor", { query })}
              </div>
            )}

            {!loading && !query && results.length == 0 && (
              <div className="mt-6 text-center text-muted-foreground">
                {t("results.enterQuery")}
              </div>
            )}
          </CardContent>
        </Card>

        {selectedInstanceId && (
          <AddTorrentDialog
            instanceId={selectedInstanceId}
            open={addDialogOpen}
            onOpenChange={handleDialogOpenChange}
            dropPayload={addDialogPayload}
            onDropPayloadConsumed={() => setAddDialogPayload(null)}
          />
        )}

        <AlertDialog open={refreshConfirmOpen} onOpenChange={setRefreshConfirmOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t("refreshDialog.title")}</AlertDialogTitle>
              <AlertDialogDescription>
                {t("refreshDialog.description")}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={loading}>{t("refreshDialog.cancel")}</AlertDialogCancel>
              <AlertDialogAction
                onClick={handleForceRefreshConfirm}
                disabled={!canForceRefresh || loading}
              >
                {t("refreshDialog.refreshNow")}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </TooltipProvider>
  )
}
