/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from 'react'
import { toast } from 'sonner'
import { Search as SearchIcon, Download, ExternalLink, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import type { TorznabSearchResult, TorznabIndexer } from '@/types'
import { api } from '@/lib/api'
import { AddTorrentDialog, type AddTorrentDropPayload } from '@/components/torrents/AddTorrentDialog'
import { useInstances } from '@/hooks/useInstances'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

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
                value={selectedInstanceId ? String(selectedInstanceId) : undefined}
                onValueChange={(value) => setSelectedInstanceId(Number(value))}
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
                <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-2 p-4 border rounded-md max-h-32 overflow-y-auto">
                  {indexers.map((indexer) => (
                    <div key={indexer.id} className="flex items-center space-x-2">
                      <Checkbox
                        id={`indexer-${indexer.id}`}
                        checked={selectedIndexers.has(indexer.id)}
                        onCheckedChange={() => toggleIndexer(indexer.id)}
                      />
                      <label
                        htmlFor={`indexer-${indexer.id}`}
                        className="text-sm cursor-pointer"
                      >
                        {indexer.name} ({formatBackend(indexer.backend)})
                      </label>
                    </div>
                  ))}
                </div>
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
              <div className="mb-4 text-sm text-muted-foreground">
                Showing {results.length} of {total} results
              </div>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Title</TableHead>
                    <TableHead>Indexer</TableHead>
                    <TableHead>Size</TableHead>
                    <TableHead>Seeders</TableHead>
                    <TableHead>Category</TableHead>
                    <TableHead>Freeleech</TableHead>
                    <TableHead>Published</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {results.map((result) => (
                    <TableRow key={result.guid}>
                      <TableCell className="font-medium max-w-md">
                        <div className="truncate" title={result.title}>
                          {result.title}
                        </div>
                      </TableCell>
                      <TableCell>{result.indexer}</TableCell>
                      <TableCell>{formatSize(result.size)}</TableCell>
                      <TableCell>
                        <Badge variant={result.seeders > 0 ? 'default' : 'secondary'}>
                          {result.seeders}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {result.category_name}
                      </TableCell>
                      <TableCell>
                        {result.download_volume_factor === 0 && (
                          <Badge variant="default">Free</Badge>
                        )}
                        {result.download_volume_factor > 0 && result.download_volume_factor < 1 && (
                          <Badge variant="secondary">{result.download_volume_factor * 100}%</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDate(result.publish_date)}
                      </TableCell>
                      <TableCell className="text-right">
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
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
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
