/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from 'react'
import { toast } from 'sonner'
import { Plus, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { IndexerTable } from './IndexerTable'
import { IndexerDialog } from './IndexerDialog'
import { AutodiscoveryDialog } from './AutodiscoveryDialog'
import type { TorznabIndexer } from '@/types'
import { api } from '@/lib/api'

export function IndexersPage() {
  const [indexers, setIndexers] = useState<TorznabIndexer[]>([])
  const [loading, setLoading] = useState(true)
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [autodiscoveryOpen, setAutodiscoveryOpen] = useState(false)
  const [editingIndexer, setEditingIndexer] = useState<TorznabIndexer | null>(null)

  const loadIndexers = async () => {
    try {
      setLoading(true)
      const data = await api.listTorznabIndexers()
      setIndexers(data || [])
    } catch (error) {
      toast.error('Failed to load indexers')
      setIndexers([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadIndexers()
  }, [])

  const handleEdit = (indexer: TorznabIndexer) => {
    setEditingIndexer(indexer)
    setEditDialogOpen(true)
  }

  const handleDelete = async (id: number) => {
    if (!confirm('Are you sure you want to delete this indexer?')) return

    try {
      await api.deleteTorznabIndexer(id)
      toast.success('Indexer deleted successfully')
      loadIndexers()
    } catch (error) {
      toast.error('Failed to delete indexer')
    }
  }

  const handleTest = async (id: number) => {
    try {
      await api.testTorznabIndexer(id)
      toast.success('Connection test successful')
    } catch (error) {
      toast.error('Connection test failed')
    }
  }

  const handleDialogClose = () => {
    setAddDialogOpen(false)
    setEditDialogOpen(false)
    setEditingIndexer(null)
    loadIndexers()
  }

  return (
    <div className="container mx-auto p-6">
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Torznab Indexers</CardTitle>
              <CardDescription>
                Manage Torznab/Jackett indexers for cross-seed discovery
              </CardDescription>
            </div>
            <div className="flex gap-2">
              <Button
                variant="outline"
                onClick={() => setAutodiscoveryOpen(true)}
              >
                <RefreshCw className="mr-2 h-4 w-4" />
                Discover Jackett
              </Button>
              <Button onClick={() => setAddDialogOpen(true)}>
                <Plus className="mr-2 h-4 w-4" />
                Add Indexer
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <IndexerTable
            indexers={indexers}
            loading={loading}
            onEdit={handleEdit}
            onDelete={handleDelete}
            onTest={handleTest}
          />
        </CardContent>
      </Card>

      <IndexerDialog
        open={addDialogOpen}
        onClose={handleDialogClose}
        mode="create"
      />

      <IndexerDialog
        open={editDialogOpen}
        onClose={handleDialogClose}
        mode="edit"
        indexer={editingIndexer}
      />

      <AutodiscoveryDialog
        open={autodiscoveryOpen}
        onClose={() => {
          setAutodiscoveryOpen(false)
          loadIndexers()
        }}
      />
    </div>
  )
}
