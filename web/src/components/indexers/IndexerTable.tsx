/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Edit2, Trash2, TestTube, Check, X, RefreshCw, ArrowUpDown, Filter } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { TorznabIndexer } from '@/types'

type SortField = 'name' | 'backend' | 'priority' | 'status'
type SortDirection = 'asc' | 'desc'

interface IndexerTableProps {
  indexers: TorznabIndexer[]
  loading: boolean
  onEdit: (indexer: TorznabIndexer) => void
  onDelete: (id: number) => void
  onTest: (id: number) => void
  onSyncCaps: (id: number) => void
  onTestAll: () => void
}

export function IndexerTable({
  indexers,
  loading,
  onEdit,
  onDelete,
  onTest,
  onSyncCaps,
  onTestAll,
}: IndexerTableProps) {
  const [allCapabilitiesExpanded, setAllCapabilitiesExpanded] = useState(false)
  const [sortField, setSortField] = useState<SortField>('priority')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')
  const [filterStatus, setFilterStatus] = useState<'all' | 'enabled' | 'disabled'>('all')
  const [filterTestStatus, setFilterTestStatus] = useState<'all' | 'ok' | 'error' | 'untested'>('all')
  const [filterBackend, setFilterBackend] = useState<'all' | 'jackett' | 'prowlarr' | 'native'>('all')

  const toggleAllCapabilities = () => {
    setAllCapabilitiesExpanded(prev => !prev)
  }

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc')
    } else {
      setSortField(field)
      setSortDirection('asc')
    }
  }

  const filteredAndSortedIndexers = useMemo(() => {
    let filtered = [...indexers]

    // Apply filters
    if (filterStatus !== 'all') {
      filtered = filtered.filter(idx =>
        filterStatus === 'enabled' ? idx.enabled : !idx.enabled
      )
    }

    if (filterTestStatus !== 'all') {
      filtered = filtered.filter(idx => {
        if (filterTestStatus === 'ok') return idx.last_test_status === 'ok'
        if (filterTestStatus === 'error') return idx.last_test_status === 'error'
        return idx.last_test_status !== 'ok' && idx.last_test_status !== 'error'
      })
    }

    if (filterBackend !== 'all') {
      filtered = filtered.filter(idx => idx.backend === filterBackend)
    }

    // Apply sorting
    filtered.sort((a, b) => {
      let comparison = 0

      switch (sortField) {
        case 'name':
          comparison = a.name.localeCompare(b.name)
          break
        case 'backend':
          comparison = a.backend.localeCompare(b.backend)
          break
        case 'priority':
          comparison = a.priority - b.priority
          break
        case 'status':
          comparison = (a.enabled ? 1 : 0) - (b.enabled ? 1 : 0)
          break
      }

      return sortDirection === 'asc' ? comparison : -comparison
    })

    return filtered
  }, [indexers, sortField, sortDirection, filterStatus, filterTestStatus, filterBackend])

  const hasActiveFilters = filterStatus !== 'all' || filterTestStatus !== 'all' || filterBackend !== 'all'

  if (loading) {
    return <div className="text-center py-8 text-muted-foreground">Loading...</div>
  }

  if (!indexers || indexers.length === 0) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        No indexers configured. Add one to get started.
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Filter Controls */}
      <div className="flex flex-wrap items-center gap-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" className="h-8">
              <Filter className="mr-2 h-4 w-4" />
              Filters
              {hasActiveFilters && (
                <Badge variant="secondary" className="ml-2 h-5 px-1.5">
                  {[filterStatus !== 'all', filterTestStatus !== 'all', filterBackend !== 'all'].filter(Boolean).length}
                </Badge>
              )}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuLabel>Status</DropdownMenuLabel>
            <DropdownMenuCheckboxItem
              checked={filterStatus === 'all'}
              onCheckedChange={() => setFilterStatus('all')}
            >
              All
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterStatus === 'enabled'}
              onCheckedChange={() => setFilterStatus('enabled')}
            >
              Enabled only
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterStatus === 'disabled'}
              onCheckedChange={() => setFilterStatus('disabled')}
            >
              Disabled only
            </DropdownMenuCheckboxItem>

            <DropdownMenuSeparator />
            <DropdownMenuLabel>Test Status</DropdownMenuLabel>
            <DropdownMenuCheckboxItem
              checked={filterTestStatus === 'all'}
              onCheckedChange={() => setFilterTestStatus('all')}
            >
              All
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterTestStatus === 'ok'}
              onCheckedChange={() => setFilterTestStatus('ok')}
            >
              Working only
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterTestStatus === 'error'}
              onCheckedChange={() => setFilterTestStatus('error')}
            >
              Failed only
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterTestStatus === 'untested'}
              onCheckedChange={() => setFilterTestStatus('untested')}
            >
              Untested only
            </DropdownMenuCheckboxItem>

            <DropdownMenuSeparator />
            <DropdownMenuLabel>Backend</DropdownMenuLabel>
            <DropdownMenuCheckboxItem
              checked={filterBackend === 'all'}
              onCheckedChange={() => setFilterBackend('all')}
            >
              All
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterBackend === 'jackett'}
              onCheckedChange={() => setFilterBackend('jackett')}
            >
              Jackett
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterBackend === 'prowlarr'}
              onCheckedChange={() => setFilterBackend('prowlarr')}
            >
              Prowlarr
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={filterBackend === 'native'}
              onCheckedChange={() => setFilterBackend('native')}
            >
              Native
            </DropdownMenuCheckboxItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <Button
          variant="outline"
          size="sm"
          className="h-8"
          onClick={onTestAll}
          disabled={loading || indexers.length === 0}
        >
          <RefreshCw className="mr-2 h-4 w-4" />
          Test All
        </Button>

        {hasActiveFilters && (
          <Button
            variant="ghost"
            size="sm"
            className="h-8"
            onClick={() => {
              setFilterStatus('all')
              setFilterTestStatus('all')
              setFilterBackend('all')
            }}
          >
            Clear filters
          </Button>
        )}

        <div className="ml-auto text-sm text-muted-foreground">
          Showing {filteredAndSortedIndexers.length} of {indexers.length} indexers
        </div>
      </div>

      {/* Table */}
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>
                <Button
                  variant="ghost"
                  size="sm"
                  className="-ml-3 h-8 data-[state=open]:bg-accent"
                  onClick={() => handleSort('name')}
                >
                  Name
                  <ArrowUpDown className="ml-2 h-4 w-4" />
                </Button>
              </TableHead>
              <TableHead className="hidden md:table-cell">
                <Button
                  variant="ghost"
                  size="sm"
                  className="-ml-3 h-8 data-[state=open]:bg-accent"
                  onClick={() => handleSort('backend')}
                >
                  Backend
                  <ArrowUpDown className="ml-2 h-4 w-4" />
                </Button>
              </TableHead>
              <TableHead className="hidden lg:table-cell">URL</TableHead>
              <TableHead>
                <Button
                  variant="ghost"
                  size="sm"
                  className="-ml-3 h-8 data-[state=open]:bg-accent"
                  onClick={() => handleSort('status')}
                >
                  Status
                  <ArrowUpDown className="ml-2 h-4 w-4" />
                </Button>
              </TableHead>
              <TableHead>Test Status</TableHead>
              <TableHead className="hidden xl:table-cell">Capabilities</TableHead>
              <TableHead className="hidden sm:table-cell">
                <Button
                  variant="ghost"
                  size="sm"
                  className="-ml-3 h-8 data-[state=open]:bg-accent"
                  onClick={() => handleSort('priority')}
                >
                  Priority
                  <ArrowUpDown className="ml-2 h-4 w-4" />
                </Button>
              </TableHead>
              <TableHead className="hidden sm:table-cell">Timeout</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredAndSortedIndexers.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} className="text-center py-8 text-muted-foreground">
                  No indexers match the current filters
                </TableCell>
              </TableRow>
            ) : (
              filteredAndSortedIndexers.map((indexer) => (
                <TableRow key={indexer.id}>
                  <TableCell className="font-medium">
                    <div>
                      <div>{indexer.name}</div>
                      <div className="md:hidden text-xs text-muted-foreground mt-1">
                        {indexer.backend === 'jackett' && 'Jackett'}
                        {indexer.backend === 'prowlarr' && 'Prowlarr'}
                        {indexer.backend === 'native' && 'Native'}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    <Badge variant="outline" className="capitalize">
                      {indexer.backend}
                    </Badge>
                  </TableCell>
                  <TableCell className="hidden lg:table-cell text-muted-foreground text-sm">
                    {indexer.base_url}
                  </TableCell>
                  <TableCell>
                    {indexer.enabled ? (
                      <Badge variant="default" className="gap-1">
                        <Check className="h-3 w-3" />
                        <span className="hidden sm:inline">Enabled</span>
                      </Badge>
                    ) : (
                      <Badge variant="secondary" className="gap-1">
                        <X className="h-3 w-3" />
                        <span className="hidden sm:inline">Disabled</span>
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {indexer.last_test_status === 'ok' ? (
                      <Badge variant="default" className="gap-1">
                        <Check className="h-3 w-3" />
                        <span className="hidden sm:inline">Working</span>
                      </Badge>
                    ) : indexer.last_test_status === 'error' ? (
                      <Badge variant="destructive" className="gap-1" title={indexer.last_test_error || 'Unknown error'}>
                        <X className="h-3 w-3" />
                        <span className="hidden sm:inline">Failed</span>
                      </Badge>
                    ) : (
                      <Badge variant="secondary" className="gap-1">
                        <span className="hidden sm:inline">Untested</span>
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="hidden xl:table-cell">
                    {indexer.capabilities && indexer.capabilities.length > 0 ? (
                      <div className="max-w-xs">
                    {allCapabilitiesExpanded ? (
                      // Expanded view - show all capabilities
                      <div className="space-y-1">
                        <div className="flex flex-wrap gap-1">
                          {indexer.capabilities.map((cap) => (
                            <Badge 
                              key={cap} 
                              variant="secondary" 
                              className="text-xs"
                              title={`Capability: ${cap}`}
                            >
                              {cap}
                            </Badge>
                          ))}
                        </div>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-5 px-2 text-xs text-muted-foreground hover:text-foreground"
                          onClick={toggleAllCapabilities}
                          title="Collapse all capabilities"
                        >
                          Collapse
                        </Button>
                      </div>
                    ) : (
                      // Collapsed view - single line with first few caps and +X more
                      <div className="flex items-center gap-1 overflow-hidden" title={indexer.capabilities.join(', ')}>
                        {indexer.capabilities.slice(0, 2).map((cap) => (
                          <Badge key={cap} variant="secondary" className="text-xs flex-shrink-0">
                            {cap}
                          </Badge>
                        ))}
                        {indexer.capabilities.length > 2 && (
                          <Badge 
                            variant="outline" 
                            className="text-xs cursor-pointer hover:bg-accent flex-shrink-0"
                            onClick={toggleAllCapabilities}
                            title={`Click to show all ${indexer.capabilities.length} capabilities for all indexers`}
                          >
                            +{indexer.capabilities.length - 2}
                          </Badge>
                        )}
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">
                      No capabilities
                    </span>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 px-2 text-xs"
                      onClick={() => onSyncCaps(indexer.id)}
                      title="Sync capabilities from backend"
                    >
                      Sync
                    </Button>
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="hidden sm:table-cell">{indexer.priority}</TableCell>
                  <TableCell className="hidden sm:table-cell">{indexer.timeout_seconds}s</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        onClick={() => onTest(indexer.id)}
                        title="Test connection"
                      >
                        <TestTube className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 hidden sm:inline-flex"
                        onClick={() => onSyncCaps(indexer.id)}
                        title="Sync capabilities"
                      >
                        <RefreshCw className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        onClick={() => onEdit(indexer)}
                        title="Edit"
                      >
                        <Edit2 className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        onClick={() => onDelete(indexer.id)}
                        title="Delete"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
