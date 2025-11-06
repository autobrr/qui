/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Plus, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { AutodiscoveryDialog } from "./AutodiscoveryDialog"
import { IndexerDialog } from "./IndexerDialog"
import { IndexerTable } from "./IndexerTable"
import type { TorznabIndexer } from "@/types"
import { api } from "@/lib/api"

interface IndexersPageProps {
  withContainer?: boolean
}

export function IndexersPage({ withContainer = true }: IndexersPageProps) {
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
      toast.error("Failed to load indexers")
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
    if (!confirm("Are you sure you want to delete this indexer?")) return

    try {
      await api.deleteTorznabIndexer(id)
      toast.success("Indexer deleted successfully")
      loadIndexers()
    } catch (error) {
      toast.error("Failed to delete indexer")
    }
  }

  const handleTest = async (id: number) => {
    try {
      await api.testTorznabIndexer(id)
      toast.success("Connection test successful")
    } catch (error) {
      toast.error("Connection test failed")
    }
  }

  const handleTestAll = async () => {
    if (indexers.length === 0) {
      toast.info("No indexers to test")
      return
    }

    let successCount = 0
    let failCount = 0
    const results: { name: string; success: boolean; error?: string }[] = []

    toast.info(`Testing ${indexers.length} indexers...`)

    for (const indexer of indexers) {
      try {
        await api.testTorznabIndexer(indexer.id)
        successCount++
        results.push({ name: indexer.name, success: true })
      } catch (error) {
        failCount++
        const errorMsg = error instanceof Error ? error.message : String(error)
        results.push({ name: indexer.name, success: false, error: errorMsg })
        console.error(`Failed to test ${indexer.name}:`, error)
      }
    }

    await loadIndexers()

    if (failCount === 0) {
      toast.success(`All ${successCount} indexers tested successfully`)
    } else {
      toast.warning(`${successCount} passed, ${failCount} failed`)
      const failedNames = results.filter((result) => !result.success).map((result) => result.name).join(", ")
      toast.error(`Failed indexers: ${failedNames}`)
    }
  }

  const handleDialogClose = () => {
    setAddDialogOpen(false)
    setEditDialogOpen(false)
    setEditingIndexer(null)
    loadIndexers()
  }

  const content = (
    <>
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4">
            <div>
              <CardTitle>Torznab Indexers</CardTitle>
              <CardDescription>
                Manage Torznab indexers powered by Jackett, Prowlarr, or native tracker endpoints
                {indexers.length > 0 && (
                  <span className="block mt-1">
                    {indexers.filter(idx => idx.enabled).length} enabled, {' '}
                    {indexers.filter(idx => idx.capabilities && idx.capabilities.length > 0).length} with capabilities
                  </span>
                )}
              </CardDescription>
            </div>
            <div className="flex flex-wrap gap-2 justify-end">
              <Button
                variant="outline"
                onClick={handleTestAll}
                disabled={loading || indexers.length === 0}
              >
                <RefreshCw className="mr-2 h-4 w-4" />
                Test All
              </Button>
              <Button
                variant="outline"
                onClick={() => setAutodiscoveryOpen(true)}
              >
                <RefreshCw className="mr-2 h-4 w-4" />
                Discover Indexers
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
            onSyncCaps={async (id) => {
              try {
                const updated = await api.syncTorznabCaps(id)
                toast.success("Capabilities synced from backend")
                setIndexers((prev) => prev.map((idx) => (idx.id === updated.id ? updated : idx)))
              } catch (error) {
                const message = error instanceof Error ? error.message : "Failed to sync caps"
                toast.error(message)
              }
            }}
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

    </>
  )

  if (withContainer) {
    return (
      <div className="container mx-auto space-y-4 p-6">
        {content}
      </div>
    )
  }

  return <div className="space-y-4">{content}</div>
}
