/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from 'react'
import { toast } from 'sonner'
import { Search as SearchIcon, Download, ExternalLink } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import type { TorznabSearchResult } from '@/types'
import { api } from '@/lib/api'

export function Search() {
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [results, setResults] = useState<TorznabSearchResult[]>([])
  const [total, setTotal] = useState(0)

  const handleSearch = async (e: React.FormEvent) => {
    e.preventDefault()
    
    if (!query.trim()) {
      toast.error('Please enter a search query')
      return
    }

    setLoading(true)
    try {
      const response = await api.searchTorznab({ query })
      setResults(response.results)
      setTotal(response.total)
      
      if (response.results.length === 0) {
        toast.info('No results found')
      } else {
        toast.success(`Found ${response.total} results`)
      }
    } catch (error) {
      toast.error('Search failed')
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
    // TODO: Open AddTorrent dialog with the download URL
    // For now, just open the URL
    window.open(result.download_url, '_blank')
  }

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
              <Button type="submit" disabled={loading || !query.trim()}>
                <SearchIcon className="mr-2 h-4 w-4" />
                {loading ? 'Searching...' : 'Search'}
              </Button>
            </div>
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
    </div>
  )
}
