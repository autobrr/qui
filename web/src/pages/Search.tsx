/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AddTorrentDialog, type AddTorrentDropPayload } from '@/components/torrents/AddTorrentDialog'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useInstances } from '@/hooks/useInstances'
import { api } from '@/lib/api'
import type { TorznabIndexer, TorznabSearchResult } from '@/types'
import { ArrowDown, ArrowUp, ArrowUpDown, Download, ExternalLink, Plus, Search as SearchIcon } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
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
  const [sortColumn, setSortColumn] = useState<'title' | 'indexer' | 'size' | 'seeders' | 'category' | 'published' | null>('seeders')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')

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
    
    if (!query.trim()) {
      toast.error('Please enter a search query')
      return
    }

    if (selectedIndexers.size === 0) {
      toast.error('Please select at least one indexer')
      return
    }

    if (indexers.length === 0) {
      toast.error('No enabled indexers available. Please add and enable indexers first.')
      return
    }

    setLoading(true)
    try {
      const response = await api.searchTorznab({ 
        query,
        indexer_ids: Array.from(selectedIndexers)
      })
      setResults(response.results)
      setTotal(response.total)
      
      if (response.results.length === 0) {
        toast.info('No results found')
      } else {
        toast.success(`Found ${response.total} results`)
      }
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : 'Unknown error'
      toast.error(`Search failed: ${errorMsg}`)
      console.error('Search error:', error)
    } finally {
      setLoading(false)
    }
  }

  const formatSize = (bytes: number): string => {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
  }

  const formatDate = (dateStr: string): string => {
    const date = new Date(dateStr)
    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString()
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
        (categoryMap.get(result.category_id) || result.category_name || '').toLowerCase().includes(filter)
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
        default:
          return 0
      }

      if (aVal < bVal) return sortOrder === 'asc' ? -1 : 1
      if (aVal > bVal) return sortOrder === 'asc' ? 1 : -1
      return 0
    })

    return sorted
  }, [results, resultsFilter, sortColumn, sortOrder, categoryMap])

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
          <CardTitle>Search Indexers</CardTitle>
          <CardDescription>
            Search across all enabled indexers. Categories are automatically detected based on your query.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSearch} className="space-y-4">
            <div className="flex gap-2">
              <div className="flex-1">
                <Label htmlFor="query" className="sr-only">Search Query</Label>
                <Input
                  id="query"
                  type="text"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Enter search query (e.g., 'Ubuntu', 'Breaking Bad S01E01', 'Interstellar 2014')"
                  disabled={loading}
                />
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
              <div className="space-y-3">
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
                <Accordion type="multiple" defaultValue={['prowlarr', 'jackett', 'native']} className="border rounded-lg">
                  {(['prowlarr', 'jackett', 'native'] as const).map((backend) => {
                    const backendIndexers = indexersByBackend[backend] || []
                    if (backendIndexers.length === 0) return null

                    const selectedCount = backendIndexers.filter(idx => selectedIndexers.has(idx.id)).length

                    return (
                      <AccordionItem key={backend} value={backend} className="border-0 last:border-b-0">
                        <AccordionTrigger className="hover:no-underline py-3 px-4 hover:bg-muted/50">
                          <div className="flex items-center gap-2 flex-1">
                            <span className="text-sm font-medium">{formatBackend(backend)}</span>
                            <Badge variant="secondary" className="text-[10px] font-normal">
                              {selectedCount}/{backendIndexers.length}
                            </Badge>
                          </div>
                        </AccordionTrigger>
                        <AccordionContent>
                          <div className="grid grid-cols-1 lg:grid-cols-2 gap-2 px-4 pb-4 pt-2">
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
                                    className="mt-0.5"
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
                      <td className="p-2 align-middle whitespace-nowrap">
                        {result.download_volume_factor === 0 && (
                          <Badge variant="default">Free</Badge>
                        )}
                        {result.download_volume_factor > 0 && result.download_volume_factor < 1 && (
                          <Badge variant="secondary">{result.download_volume_factor * 100}%</Badge>
                        )}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-sm text-muted-foreground">
                        {formatDate(result.publish_date)}
                      </td>
                      <td className="p-2 align-middle whitespace-nowrap text-right">
                        <div className="flex justify-end gap-2">
                          {result.info_url && (
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => window.open(result.info_url, '_blank')}
                            title="View details"
                          >
                            <ExternalLink className="h-4 w-4" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleAddTorrent(result)}
                          title={canAddTorrent ? 'Add to instance' : 'Select an instance to add torrents'}
                          disabled={!canAddTorrent}
                        >
                          <Plus className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleDownload(result)}
                          title="Download"
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
    </div>
  )
}
