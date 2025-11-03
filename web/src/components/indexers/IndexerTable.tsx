/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Edit2, Trash2, TestTube, Check, X } from 'lucide-react'
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
}

export function IndexerTable({
  indexers,
  loading,
  onEdit,
  onDelete,
  onTest,
}: IndexerTableProps) {
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
          <TableHead>URL</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Priority</TableHead>
          <TableHead>Timeout</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {indexers?.map((indexer) => (
          <TableRow key={indexer.id}>
            <TableCell className="font-medium">{indexer.name}</TableCell>
            <TableCell className="text-muted-foreground">
              {indexer.baseUrl}
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
            <TableCell>{indexer.priority}</TableCell>
            <TableCell>{indexer.timeoutSeconds}s</TableCell>
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
        ))}
      </TableBody>
    </Table>
  )
}
