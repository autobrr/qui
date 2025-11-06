/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Edit2, Trash2, TestTube, Check, X, RefreshCw } from 'lucide-react'
import { useState } from 'react'
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
import type { TorznabIndexer } from '@/types'

interface IndexerTableProps {
  indexers: TorznabIndexer[]
  loading: boolean
  onEdit: (indexer: TorznabIndexer) => void
  onDelete: (id: number) => void
  onTest: (id: number) => void
  onSyncCaps: (id: number) => void
}

export function IndexerTable({
  indexers,
  loading,
  onEdit,
  onDelete,
  onTest,
  onSyncCaps,
}: IndexerTableProps) {
  const [allCapabilitiesExpanded, setAllCapabilitiesExpanded] = useState(false)

  const toggleAllCapabilities = () => {
    setAllCapabilitiesExpanded(prev => !prev)
  }

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
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Backend</TableHead>
          <TableHead>URL</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Test Status</TableHead>
          <TableHead>Capabilities</TableHead>
          <TableHead>Priority</TableHead>
          <TableHead>Timeout</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {indexers?.map((indexer) => (
          <>
            <TableRow key={indexer.id}>
              <TableCell className="font-medium">{indexer.name}</TableCell>
              <TableCell>
                <Badge variant="outline">
                  {indexer.backend === 'jackett' && 'Jackett'}
                  {indexer.backend === 'prowlarr' && 'Prowlarr'}
                  {indexer.backend === 'native' && 'Native'}
                </Badge>
              </TableCell>
              <TableCell className="text-muted-foreground">
                {indexer.base_url}
              </TableCell>
              <TableCell>
                {indexer.enabled ? (
                  <Badge variant="default" className="gap-1">
                    <Check className="h-3 w-3" />
                    Enabled
                  </Badge>
                ) : (
                  <Badge variant="secondary" className="gap-1">
                    <X className="h-3 w-3" />
                    Disabled
                  </Badge>
                )}
              </TableCell>
              <TableCell>
                {indexer.last_test_status === 'ok' ? (
                  <Badge variant="default" className="gap-1">
                    <Check className="h-3 w-3" />
                    Working
                  </Badge>
                ) : indexer.last_test_status === 'error' ? (
                  <Badge variant="destructive" className="gap-1" title={indexer.last_test_error || 'Unknown error'}>
                    <X className="h-3 w-3" />
                    Failed
                  </Badge>
                ) : (
                  <Badge variant="secondary" className="gap-1">
                    Untested
                  </Badge>
                )}
              </TableCell>
              <TableCell>
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
              <TableCell>{indexer.priority}</TableCell>
              <TableCell>{indexer.timeout_seconds}s</TableCell>
              <TableCell className="text-right">
                <div className="flex justify-end gap-2">
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => onTest(indexer.id)}
                    title="Test connection"
                  >
                    <TestTube className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => onSyncCaps(indexer.id)}
                    title="Sync capabilities"
                  >
                    <RefreshCw className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => onEdit(indexer)}
                    title="Edit"
                  >
                    <Edit2 className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => onDelete(indexer.id)}
                    title="Delete"
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </>
        ))}
      </TableBody>
    </Table>
  )
}
