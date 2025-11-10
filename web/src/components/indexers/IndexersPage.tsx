/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { api } from "@/lib/api"
import type { TorznabIndexer } from "@/types"
import { ChevronDown, Plus, RefreshCw, Trash2 } from "lucide-react"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { AutodiscoveryDialog } from "./AutodiscoveryDialog"
import { IndexerDialog } from "./IndexerDialog"
import { IndexerTable } from "./IndexerTable"

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
  const [deleteIndexerId, setDeleteIndexerId] = useState<number | null>(null)
  const [showDeleteAllDialog, setShowDeleteAllDialog] = useState(false)

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

  const handleDelete = (id: number) => {
    setDeleteIndexerId(id)
  }

  const confirmDelete = async () => {
    if (deleteIndexerId === null) return

    try {
      await api.deleteTorznabIndexer(deleteIndexerId)
      toast.success("Indexer deleted successfully")
      setDeleteIndexerId(null)
      loadIndexers()
    } catch (error) {
      toast.error("Failed to delete indexer")
    }
  }

  const handleDeleteAll = async () => {
    if (indexers.length === 0) return

    const results = await Promise.allSettled(
      indexers.map(indexer =>
        api.deleteTorznabIndexer(indexer.id)
          .then(() => ({ id: indexer.id, name: indexer.name, success: true }))
          .catch(error => ({ id: indexer.id, name: indexer.name, success: false, error }))
      )
    )

    const successCount = results.filter(r => r.status === 'fulfilled' && r.value.success).length
    const failCount = indexers.length - successCount

    if (failCount === 0) {
      toast.success(`Deleted all ${indexers.length} indexers`)
    } else {
      const failedNames = results
        .filter(r => r.status === 'fulfilled' && !r.value.success)
        .map(r => r.status === 'fulfilled' ? r.value.name : '')
        .join(', ')
      toast.warning(`Deleted ${successCount} indexers, ${failCount} failed: ${failedNames}`)
    }

    setShowDeleteAllDialog(false)
    loadIndexers()
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
                onClick={() => setShowDeleteAllDialog(true)}
                disabled={loading || indexers.length === 0}
              >
                <Trash2 className="h-4 w-4" />
                Delete All
              </Button>
              <div className="flex">
                <Button
                  onClick={() => setAutodiscoveryOpen(true)}
                  className="rounded-r-none"
                >
                  <RefreshCw className="h-4 w-4" />
                  Discover
                </Button>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button className="rounded-l-none border-l px-2">
                      <ChevronDown className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => setAddDialogOpen(true)}>
                      <Plus className="h-4 w-4 mr-2" />
                      Add single
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
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
            onTestAll={handleTestAll}
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

      <AlertDialog open={!!deleteIndexerId} onOpenChange={() => setDeleteIndexerId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Indexer?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete the indexer.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={confirmDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={showDeleteAllDialog} onOpenChange={setShowDeleteAllDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete All Indexers?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete all {indexers.length} indexers.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteAll}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete All
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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
