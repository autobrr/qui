/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AddTorrentDialog, type AddTorrentDropPayload } from '@/components/torrents/AddTorrentDialog'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useDateTimeFormatters } from '@/hooks/useDateTimeFormatters'
import { useInstances } from '@/hooks/useInstances'
import { api } from '@/lib/api'
import type { TorznabIndexer, TorznabRecentSearch, TorznabSearchRequest, TorznabSearchResponse, TorznabSearchResult } from '@/types'
import { ArrowDown, ArrowUp, ArrowUpDown, Download, ExternalLink, Plus, RefreshCw, Search as SearchIcon } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

export function Search() {
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [results, setResults] = useState<TorznabSearchResult[]>([])
  const [total, setTotal] = useState(0)
  const [indexers, setIndexers] = useState<TorznabIndexer[]>([])
  const [selectedIndexers, setSelectedIndexers] = useState<Set<number>>(new Set())
  const [loadingIndexers, setLoadingIndexers] = useState(true)
  const { instances, isLoading: loadingInstances } = useInstances()
  const [selectedInstanceId, setSelectedInstanceId] = useState<number | null>(null)
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [addDialogPayload, setAddDialogPayload] = useState<AddTorrentDropPayload | null>(null)
  const [resultsFilter, setResultsFilter] = useState('')
  const [sortColumn, setSortColumn] = useState<'title' | 'indexer' | 'size' | 'seeders' | 'category' | 'published' | 'source' | 'collection' | 'group' | null>('seeders')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')
  const [cacheMetadata, setCacheMetadata] = useState<TorznabSearchResponse["cache"] | null>(null)
  const [refreshConfirmOpen, setRefreshConfirmOpen] = useState(false)
  const [refreshCooldownUntil, setRefreshCooldownUntil] = useState(0)
  const [, forceRefreshTick] = useState(0)
  const [recentSearches, setRecentSearches] = useState<TorznabRecentSearch[] | null>(null)
  const [queryFocused, setQueryFocused] = useState(false)
  const queryInputRef = useRef<HTMLInputElement | null>(null)
  const blurTimeoutRef = useRef<number | null>(null)
  const rafIdRef = useRef<number | null>(null)
  const { formatDate } = useDateTimeFormatters()

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

  const formatBackend = (backend: TorznabIndexer['backend']) => {
    switch (backend) {
      case 'prowlarr':
        return 'Prowlarr'
      case 'native':
        return 'Native'
      default:
        return 'Jackett'
    }
  }

  const validateSearchInputs = useCallback((overrideQuery?: string) => {
    const normalizedQuery = (overrideQuery ?? query).trim()
    if (!normalizedQuery) {
      toast.error('Please enter a search query')
      return false
    }

    if (selectedIndexers.size === 0) {
      toast.error('Please select at least one indexer')
      return false
    }

    if (indexers.length === 0) {
      toast.error('No enabled indexers available. Please add and enable indexers first.')
      return false
    }

    return true
  }, [indexers.length, query, selectedIndexers])

  const refreshRecentSearches = useCallback(async () => {
    try {
      const data = await api.getRecentTorznabSearches(20, "general")
      setRecentSearches(Array.isArray(data) ? data : [])
    } catch (error) {
      console.error("Load recent searches error:", error)
      setRecentSearches([])
    }
  }, [api])

  const runSearch = useCallback(
    async ({ bypassCache = false, queryOverride }: { bypassCache?: boolean; queryOverride?: string } = {}) => {
      const searchQuery = (queryOverride ?? query).trim()
      setLoading(true)
      setCacheMetadata(null)

      try {
        const payload: Omit<TorznabSearchRequest, "categories"> = {
          query: searchQuery,
          indexer_ids: Array.from(selectedIndexers),
        }

        if (bypassCache) {
          payload.cache_mode = "bypass"
        }

        const response = await api.searchTorznab(payload)
        setResults(response.results)
        setTotal(response.total)
        setCacheMetadata(response.cache ?? null)

        if (response.results.length === 0) {
          toast.info('No results found')
        } else {
          const cacheSuffix = response.cache?.hit ? ' (cached)' : ''
          toast.success(`Found ${response.total} results${cacheSuffix}`)
        }
        void refreshRecentSearches()
      } catch (error) {
        const errorMsg = error instanceof Error ? error.message : 'Unknown error'
        toast.error(`Search failed: ${errorMsg}`)
        console.error('Search error:', error)
      } finally {
        setLoading(false)
      }
    },
    [api, query, selectedIndexers, refreshRecentSearches]
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

  // Group indexers by backend
  const indexersByBackend = useMemo(() => {
    const groups: Record<string, TorznabIndexer[]> = {
      prowlarr: [],
      jackett: [],
      native: []
    }

    indexers.forEach(indexer => {
      const backend = indexer.backend || 'jackett'
      if (groups[backend]) {
        groups[backend].push(indexer)
      }
    })

    return groups
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
        toast.error('Failed to load indexers')
        console.error('Load indexers error:', error)
      } finally {
        setLoadingIndexers(false)
      }
    }
    loadIndexers()
  }, [])

  useEffect(() => {
    refreshRecentSearches()
  }, [refreshRecentSearches])

  useEffect(() => {
    if (!instances || instances.length === 0) {
      setSelectedInstanceId(null)
      return
    }

    setSelectedInstanceId((prev) => {
      if (prev && instances.some(instance => instance.id === prev)) {
        return prev
      }

      const preferred = instances.find(instance => instance.connected)?.id ?? instances[0]?.id ?? null
      return preferred
    })
  }, [instances])

  const toggleIndexer = (id: number) => {
    const newSelected = new Set(selectedIndexers)
    if (newSelected.has(id)) {
      newSelected.delete(id)
    } else {
      newSelected.add(id)
    }
    setSelectedIndexers(newSelected)
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
    await runSearch()
  }

  const formatSize = (bytes: number): string => {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
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
      if (sortOrder === 'desc') {
        setSortOrder('asc')
      } else {
        // Reset sorting on third click
        setSortColumn(null)
        setSortOrder('desc')
      }
    } else {
      setSortColumn(column)
      setSortOrder('desc')
    }
  }

  const getSortIcon = (column: Exclude<typeof sortColumn, null>) => {
    if (sortColumn !== column) {
      return <ArrowUpDown className="h-4 w-4" />
    }
    return sortOrder === 'asc' ? <ArrowUp className="h-4 w-4" /> : <ArrowDown className="h-4 w-4" />
  }

  // Filter and sort results
  const filteredAndSortedResults = useMemo(() => {
    let filtered = results

    // Apply filter
    if (resultsFilter.trim()) {
      const filter = resultsFilter.toLowerCase()
      filtered = results.filter(result =>
        result.title.toLowerCase().includes(filter) ||
        result.indexer.toLowerCase().includes(filter) ||
        (categoryMap.get(result.category_id) || result.category_name || '').toLowerCase().includes(filter) ||
        (result.source || '').toLowerCase().includes(filter) ||
        (result.collection || '').toLowerCase().includes(filter) ||
        (result.group || '').toLowerCase().includes(filter)
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
        case 'title':
          aVal = a.title.toLowerCase()
          bVal = b.title.toLowerCase()
          break
        case 'indexer':
          aVal = a.indexer.toLowerCase()
          bVal = b.indexer.toLowerCase()
          break
        case 'size':
          aVal = a.size
          bVal = b.size
          break
        case 'seeders':
          aVal = a.seeders
          bVal = b.seeders
          break
        case 'category':
          aVal = (categoryMap.get(a.category_id) || a.category_name || '').toLowerCase()
          bVal = (categoryMap.get(b.category_id) || b.category_name || '').toLowerCase()
          break
        case 'published':
          aVal = new Date(a.publish_date).getTime()
          bVal = new Date(b.publish_date).getTime()
          break
        case 'source':
          aVal = (a.source || '').toLowerCase()
          bVal = (b.source || '').toLowerCase()
          break
        case 'collection':
          aVal = (a.collection || '').toLowerCase()
          bVal = (b.collection || '').toLowerCase()
          break
        case 'group':
          aVal = (a.group || '').toLowerCase()
          bVal = (b.group || '').toLowerCase()
          break
        default:
          return 0
      }

      if (aVal < bVal) return sortOrder === 'asc' ? -1 : 1
      if (aVal > bVal) return sortOrder === 'asc' ? 1 : -1
      return 0
    })

    return sorted
  }, [results, resultsFilter, sortColumn, sortOrder, categoryMap])

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

  const handleSuggestionClick = useCallback((value: string) => {
    setQuery(value)
    const rafId = requestAnimationFrame(() => {
      queryInputRef.current?.focus()
    })
    rafIdRef.current = rafId
    const normalized = value.trim()
    if (!validateSearchInputs(normalized)) {
      cancelAnimationFrame(rafId)
      rafIdRef.current = null
      return
    }
    void runSearch({ queryOverride: normalized })
  }, [runSearch, validateSearchInputs])

  const handleDownload = (result: TorznabSearchResult) => {
    window.open(result.download_url, '_blank')
  }

  const handleAddTorrent = (result: TorznabSearchResult) => {
    if (!selectedInstanceId) {
      toast.error('Select an instance to add torrents')
      return
    }

    if (!result.download_url) {
      toast.error('No download URL available for this result')
      return
    }

    setAddDialogPayload({ type: 'url', urls: [result.download_url] })
    setAddDialogOpen(true)
  }

  const handleDialogOpenChange = (open: boolean) => {
    setAddDialogOpen(open)
    if (!open) {
      setAddDialogPayload(null)
    }
  }

  const canAddTorrent = !!selectedInstanceId

  return (
    <div className="container mx-auto p-6">
      <Card>
        <CardHeader>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex-1">
              <CardTitle>Search Indexers</CardTitle>
              <CardDescription>
                Search across all enabled indexers. Categories are automatically detected based on your query.
              </CardDescription>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Badge
                      variant={cacheMetadata?.hit ? 'secondary' : 'outline'}
                      className={!cacheMetadata ? 'invisible' : ''}
                    >
                      {cacheMetadata?.hit ? 'Cache hit' : 'Live fetch'}
                    </Badge>
                  </TooltipTrigger>
                  {cacheMetadata && (
                    <TooltipContent>
                      <p className="text-xs">
                        Cached {formatCacheTimestamp(cacheMetadata.cachedAt)} · Expires {formatCacheTimestamp(cacheMetadata.expiresAt)}
                      </p>
                    </TooltipContent>
                  )}
                </Tooltip>
              </TooltipProvider>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className={`h-7 w-7 opacity-40 transition-opacity hover:opacity-100 ${!showRefreshButton ? 'invisible' : ''}`}
                onClick={() => setRefreshConfirmOpen(true)}
                disabled={!canForceRefresh}
                title={refreshCooldownRemaining > 0 ? `Ready in ${Math.ceil(refreshCooldownRemaining / 1000)}s` : 'Refresh from indexers'}
              >
                <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSearch} className="space-y-4">
            <div className="flex gap-2">
              <div className="flex-1 relative">
                <Label htmlFor="query" className="sr-only">Search Query</Label>
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
                    blurTimeoutRef.current = window.setTimeout(() => {
                      setQueryFocused(false)
                      blurTimeoutRef.current = null
                    }, 100)
                  }}
                  placeholder="Enter search query (e.g., 'Ubuntu', 'Breaking Bad S01E01', 'Interstellar 2014')"
                  disabled={loading}
                />
                {shouldShowSuggestions && (
                  <div className="absolute left-0 right-0 z-20 mt-1 rounded-md border bg-popover shadow-lg">
                    {suggestionMatches.map((search) => (
                      <button
                        type="button"
                        key={search.cacheKey}
                        className="w-full px-3 py-2 text-left text-sm hover:bg-muted focus-visible:outline-none"
                        onMouseDown={(event) => event.preventDefault()}
                        onClick={() => handleSuggestionClick(search.query)}
                      >
                        <div className="font-medium text-foreground">
                          {search.query}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {search.totalResults} results · {formatCacheTimestamp(search.lastUsedAt ?? search.cachedAt)}
                        </div>
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <Button type="submit" disabled={loading || !query.trim() || selectedIndexers.size === 0}>
                <SearchIcon className="mr-2 h-4 w-4" />
                {loading ? 'Searching...' : 'Search'}
              </Button>
            </div>

            <div className="space-y-2">
              <Label htmlFor="instance-select" className="text-sm font-medium">Add torrents to</Label>
              <Select
                value={selectedInstanceId !== null && selectedInstanceId !== undefined ? String(selectedInstanceId) : ""}
                onValueChange={(value) => {
                  const parsed = parseInt(value, 10)
                  setSelectedInstanceId(Number.isNaN(parsed) ? null : parsed)
                }}
                disabled={loadingInstances || !instances || instances.length === 0}
              >
                <SelectTrigger id="instance-select" className="w-full md:w-80">
                  <SelectValue placeholder={loadingInstances ? 'Loading instances...' : 'No instances available'} />
                </SelectTrigger>
                <SelectContent>
                  {instances?.map((instance) => (
                    <SelectItem key={instance.id} value={String(instance.id)}>
                      {instance.name}{instance.connected ? '' : ' (offline)'}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {!loadingInstances && (!instances || instances.length === 0) && (
                <p className="text-xs text-muted-foreground">Add a download instance under Settings -&gt; Instances to enable quick adding.</p>
              )}
            </div>

            {/* Indexer Selection */}
            {!loadingIndexers && indexers.length > 0 && (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label className="text-sm font-medium">Indexers ({selectedIndexers.size} of {indexers.length} selected)</Label>
                  <div className="flex gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={handleSelectAll}
                    >
                      Select All
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={handleDeselectAll}
                    >
                      Deselect All
                    </Button>
                  </div>
                </div>
                <Accordion type="multiple" className="border rounded-lg">
                  {(['prowlarr', 'jackett', 'native'] as const).map((backend) => {
                    const backendIndexers = indexersByBackend[backend] || []
                    if (backendIndexers.length === 0) return null

                    const selectedCount = backendIndexers.filter(idx => selectedIndexers.has(idx.id)).length

                    return (
                      <AccordionItem key={backend} value={backend} className="border-0 last:border-b-0">
                        <AccordionTrigger className="hover:no-underline py-3 px-4 bg-muted/50 hover:bg-muted">
                          <div className="flex items-center gap-2 flex-1">
                            <span className="text-sm font-medium">{formatBackend(backend)}</span>
                            <Badge variant="secondary" className="text-[10px] font-normal">
                              {selectedCount}/{backendIndexers.length}
                            </Badge>
                          </div>
                        </AccordionTrigger>
                        <AccordionContent className="px-4 pb-3 pt-1">
                          <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                            {backendIndexers.map((indexer) => {
                              // Only show parent categories (category_id is multiple of 1000 in Torznab spec)
                              const parentCategories = indexer.categories
                                ?.filter(cat => cat.category_id % 1000 === 0)
                                .map(cat => cat.category_name) || []

                              const hasCategories = parentCategories.length > 0
                              const isSelected = selectedIndexers.has(indexer.id)

                              return (
                                <label
                                  key={indexer.id}
                                  htmlFor={`indexer-${indexer.id}`}
                                  className={`flex items-start gap-3 rounded-md border p-3 transition-colors cursor-pointer ${
                                    isSelected
                                      ? 'bg-muted/40 border-muted-foreground/20'
                                      : 'hover:bg-muted/20'
                                  }`}
                                >
                                  <Checkbox
                                    id={`indexer-${indexer.id}`}
                                    checked={isSelected}
                                    onCheckedChange={() => toggleIndexer(indexer.id)}
                                    className="mt-0.5 shrink-0"
                                  />
                                  <div className="flex-1 min-w-0 space-y-1.5">
                                    <div className="text-sm font-medium leading-none">
                                      {indexer.name}
                                    </div>
                                    {hasCategories ? (
                                      <div className="flex flex-wrap gap-1">
                                        {parentCategories.map((catName, idx) => (
                                          <Badge key={idx} variant="secondary" className="text-[10px] font-normal">
                                            {catName}
                                          </Badge>
                                        ))}
                                      </div>
                                    ) : (
                                      <p className="text-xs text-muted-foreground">No categories</p>
                                    )}
                                  </div>
                                </label>
                              )
                            })}
                          </div>
                        </AccordionContent>
                      </AccordionItem>
                    )
                  })}
                </Accordion>
              </div>
            )}

            {!loadingIndexers && indexers.length === 0 && (
              <div className="text-sm text-muted-foreground">
                No enabled indexers available. Please add and enable indexers in the <a href="/settings?tab=indexers" className="text-primary hover:underline">Indexers page</a>.
              </div>
            )}

            {(recentSearches?.length ?? 0) > 0 && (
              <div className="space-y-2">
                <Label className="text-sm font-medium">Recent searches</Label>
                <Accordion type="single" collapsible className="border rounded-lg">
                  <AccordionItem value="recent-searches" className="border-0">
                    <AccordionTrigger className="hover:no-underline py-3 px-4 bg-muted/50 hover:bg-muted">
                      <div className="flex items-center gap-2 flex-1">
                        <span className="text-sm font-medium">History</span>
                        <Badge variant="secondary" className="text-[10px] font-normal">
                          {recentSearches?.length ?? 0}
                        </Badge>
                      </div>
                    </AccordionTrigger>
                    <AccordionContent className="px-4 pb-3 pt-1">
                      <div className="flex flex-col gap-2">
                        {(recentSearches ?? []).map(search => {
                          const indexerSummary = search.indexerIds.length > 0 ? `${search.indexerIds.length} indexers` : "All indexers"
                          return (
                            <button
                              type="button"
                              key={`${search.cacheKey}-${search.cachedAt}`}
                              className="rounded-md border px-3 py-2 text-left transition hover:border-primary hover:bg-muted/40 focus-visible:outline-none"
                              onClick={() => handleSuggestionClick(search.query)}
                            >
                              <div className="flex items-center justify-between gap-2">
                                <p className="font-medium text-foreground text-sm">{search.query || "Untitled search"}</p>
                                <span className="text-xs text-muted-foreground shrink-0">{formatCacheTimestamp(search.lastUsedAt ?? search.cachedAt)}</span>
                              </div>
                              <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                                <Badge variant="outline" className="text-[10px] uppercase tracking-wide">
                                  {search.scope === "cross_seed" ? "Cross-seed" : "General"}
                                </Badge>
                                <span>{indexerSummary}</span>
                                <span>{search.totalResults} results</span>
                                {search.hitCount > 0 && <span>{search.hitCount} hits</span>}
                              </div>
                            </button>
                          )
                        })}
                      </div>
                    </AccordionContent>
                  </AccordionItem>
                </Accordion>
              </div>
            )}
          </form>

          {results.length > 0 && (
            <div className="mt-6">
              <div className="mb-4 flex items-center gap-4">
                <div className="text-sm text-muted-foreground">
                  Showing {filteredAndSortedResults.length} of {total} results
                </div>
                <div className="flex-1 max-w-sm">
                  <Input
                    type="text"
                    placeholder="Filter results..."
                    value={resultsFilter}
                    onChange={(e) => setResultsFilter(e.target.value)}
                    className="h-9"
                  />
                </div>
              </div>
              <div className="max-h-[600px] overflow-auto border rounded-md">
                <table className="w-full caption-bottom text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('title')}>
                        <div className="flex items-center gap-1">
                          Title
                          {getSortIcon('title')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('indexer')}>
                        <div className="flex items-center gap-1">
                          Indexer
                          {getSortIcon('indexer')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('size')}>
                        <div className="flex items-center gap-1">
                          Size
                          {getSortIcon('size')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('seeders')}>
                        <div className="flex items-center gap-1">
                          Seeders
                          {getSortIcon('seeders')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('category')}>
                        <div className="flex items-center gap-1">
                          Category
                          {getSortIcon('category')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('source')}>
                        <div className="flex items-center gap-1">
                          Source
                          {getSortIcon('source')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('collection')}>
                        <div className="flex items-center gap-1">
                          Collection
                          {getSortIcon('collection')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('group')}>
                        <div className="flex items-center gap-1">
                          Group
                          {getSortIcon('group')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap sticky top-0 z-10 bg-card">Freeleech</th>
                      <th className="text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap cursor-pointer select-none sticky top-0 z-10 bg-card" onClick={() => handleSort('published')}>
                        <div className="flex items-center gap-1">
                          Published
                          {getSortIcon('published')}
                        </div>
                      </th>
                      <th className="text-foreground h-10 px-2 text-center align-middle font-medium whitespace-nowrap sticky top-0 z-10 bg-card">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredAndSortedResults.map((result) => (
                    <tr key={result.guid} className="hover:bg-muted/50 border-b transition-colors">
                      <td className="p-2 align-middle whitespace-nowrap font-medium max-w-md">
                        <div className="truncate" title={result.title}>
                          {result.title}
                        </div>
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap">{result.indexer}</td>
                      <td className="p-2 align-middle whitespace-nowrap">{formatSize(result.size)}</td>
                      <td className="p-2 align-middle whitespace-nowrap">
                        <Badge variant={result.seeders > 0 ? 'default' : 'secondary'}>
                          {result.seeders}
                        </Badge>
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-sm text-muted-foreground">
                        {categoryMap.get(result.category_id) || result.category_name || `Category ${result.category_id}`}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-sm">
                        {result.source ? (
                          <Badge variant="outline">{result.source}</Badge>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-sm">
                        {result.collection ? (
                          <Badge variant="outline">{result.collection}</Badge>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-sm">
                        {result.group ? (
                          <Badge variant="outline">{result.group}</Badge>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap">
                        {result.download_volume_factor === 0 && (
                          <Badge variant="default">Free</Badge>
                        )}
                        {result.download_volume_factor > 0 && result.download_volume_factor < 1 && (
                          <Badge variant="secondary">{result.download_volume_factor * 100}%</Badge>
                        )}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-sm text-muted-foreground">
                        {formatCacheTimestamp(result.publish_date)}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-right">
                        <div className="flex justify-end gap-2">
                          {result.info_url && (
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => window.open(result.info_url, '_blank')}
                            title="View details"
                            aria-label="View details"
                          >
                            <ExternalLink className="h-4 w-4" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleAddTorrent(result)}
                          title={canAddTorrent ? 'Add to instance' : 'Select an instance to add torrents'}
                          aria-label={canAddTorrent ? "Add to instance" : "Select an instance to add torrents"}
                          disabled={!canAddTorrent}
                        >
                          <Plus className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleDownload(result)}
                          title="Download"
                          aria-label="Download"
                          >
                            <Download className="h-4 w-4" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {!loading && results.length === 0 && total === 0 && query && (
            <div className="mt-6 text-center text-muted-foreground">
              No results found for "{query}"
            </div>
          )}

          {!loading && !query && (
            <div className="mt-6 text-center text-muted-foreground">
              Enter a search query to get started
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
            <AlertDialogTitle>Bypass the cache?</AlertDialogTitle>
            <AlertDialogDescription>
              This will send the request directly to every selected indexer. Use sparingly to avoid rate limits. You can refresh again after a short cooldown.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={loading}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleForceRefreshConfirm}
              disabled={!canForceRefresh || loading}
            >
              Refresh now
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
