/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { api } from "@/lib/api"
import type { TorznabIndexer } from "@/types"
import { ChevronDown, Database, Plus, RefreshCw, Trash2 } from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { AutodiscoveryDialog } from "./AutodiscoveryDialog"
import { IndexerActivityPanel } from "./IndexerActivityPanel"
import { IndexerDialog } from "./IndexerDialog"
import { SearchHistoryPanel } from "./SearchHistoryPanel"
import { IndexerTable } from "./IndexerTable"

interface IndexersPageProps {
  withContainer?: boolean
}

function useCommonTr() {
  const { t } = useTranslation("common")
  return (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
}

export function IndexersPage({ withContainer = true }: IndexersPageProps) {
  const tr = useCommonTr()
  const [indexers, setIndexers] = useState<TorznabIndexer[]>([])
  const [loading, setLoading] = useState(true)
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [autodiscoveryOpen, setAutodiscoveryOpen] = useState(false)
  const [editingIndexer, setEditingIndexer] = useState<TorznabIndexer | null>(null)
  const [deleteIndexerId, setDeleteIndexerId] = useState<number | null>(null)
  const [showDeleteAllDialog, setShowDeleteAllDialog] = useState(false)
  const [indexersOpen, setIndexersOpen] = useState(true)

  const loadIndexers = useCallback(async (signal?: AbortSignal) => {
    try {
      setLoading(true)
      const data = await api.listTorznabIndexers({ signal })
      if (signal?.aborted) {
        return
      }
      setIndexers(data || [])
    } catch (error) {
      if (signal?.aborted) {
        return
      }
      toast.error(tr("indexersPage.toasts.loadFailed"))
      setIndexers([])
    } finally {
      if (signal?.aborted) {
        return
      }
      setLoading(false)
    }
  }, [tr])

  useEffect(() => {
    const controller = new AbortController()
    loadIndexers(controller.signal)
    return () => controller.abort()
  }, [loadIndexers])

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
      toast.success(tr("indexersPage.toasts.deleted"))
      setDeleteIndexerId(null)
      loadIndexers()
    } catch (error) {
      toast.error(tr("indexersPage.toasts.deleteFailed"))
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
      toast.success(tr("indexersPage.toasts.deletedAll", { count: indexers.length }))
    } else {
      const failedNames = results
        .filter(r => r.status === 'fulfilled' && !r.value.success)
        .map(r => r.status === 'fulfilled' ? r.value.name : '')
        .join(', ')
      toast.warning(tr("indexersPage.toasts.deletedSome", {
        success: successCount,
        failed: failCount,
        failedNames,
      }))
    }

    setShowDeleteAllDialog(false)
    loadIndexers()
  }

  const handleTest = async (id: number) => {
    updateIndexerTestState(id, "testing", undefined)
    try {
      await api.testTorznabIndexer(id)
      updateIndexerTestState(id, "ok", undefined)
      toast.success(tr("indexersPage.toasts.connectionSuccess"))
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : tr("indexersPage.toasts.connectionFailed")
      updateIndexerTestState(id, "error", errorMsg)
      toast.error(errorMsg)
    }
  }

  const handleTestAll = async (indexersToTest: TorznabIndexer[]) => {
    if (indexersToTest.length === 0) {
      toast.info(tr("indexersPage.toasts.noIndexersToTest"))
      return
    }

    const toastId = toast.loading(tr("indexersPage.toasts.testingAll", { count: indexersToTest.length }))
    // mark all as in-flight immediately to avoid stale status while we fire requests
    indexersToTest.forEach(idx => updateIndexerTestState(idx.id, "testing", undefined))

    const results = await Promise.all(
      indexersToTest.map(async (indexer) => {
        try {
          await api.testTorznabIndexer(indexer.id)
          updateIndexerTestState(indexer.id, "ok", undefined)
          return { name: indexer.name, success: true as const }
        } catch (error) {
          const errorMsg = error instanceof Error ? error.message : String(error)
          updateIndexerTestState(indexer.id, "error", errorMsg)
          console.error(`Failed to test ${indexer.name}:`, error)
          return { name: indexer.name, success: false as const, error: errorMsg }
        }
      })
    )

    const successCount = results.filter(r => r.success).length
    const failCount = results.length - successCount

    if (failCount === 0) {
      toast.success(tr("indexersPage.toasts.testedAllSuccess", { count: successCount }), { id: toastId })
    } else {
      toast.warning(tr("indexersPage.toasts.testedMixed", { success: successCount, failed: failCount }), { id: toastId })
      const failedNames = results.filter((result) => !result.success).map((result) => result.name).join(", ")
      toast.error(tr("indexersPage.toasts.failedIndexers", { failedNames }))
    }
  }

  const updateIndexerTestState = (id: number, status: string, errorMsg?: string) => {
    const now = new Date().toISOString()
    setIndexers(prev =>
      prev.map(idx => {
        if (idx.id !== id) {
          return idx
        }
        return {
          ...idx,
          last_test_status: status,
          last_test_error: errorMsg,
          last_test_at: now
        }
      })
    )
  }

  const handleDialogClose = () => {
    setAddDialogOpen(false)
    setEditDialogOpen(false)
    setEditingIndexer(null)
    loadIndexers()
  }

  const enabledCount = indexers.filter(idx => idx.enabled).length
  const capsCount = indexers.filter(idx => idx.capabilities && idx.capabilities.length > 0).length

  const content = (
    <>
      {indexers.length > 0 && <SearchHistoryPanel />}
      {indexers.length > 0 && <IndexerActivityPanel />}

      <Collapsible open={indexersOpen} onOpenChange={setIndexersOpen}>
        <div className="rounded-xl border bg-card text-card-foreground shadow-sm">
          <CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-4 hover:cursor-pointer text-left hover:bg-muted/50 transition-colors rounded-xl">
            <div className="flex items-center gap-2">
              <Database className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm font-medium">{tr("indexersPage.title")}</span>
              {indexers.length > 0 && (
                <span className="text-xs text-muted-foreground">
                  {tr("indexersPage.summary", { enabled: enabledCount, capabilities: capsCount })}
                </span>
              )}
            </div>
            <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${indexersOpen ? "rotate-180" : ""}`} />
          </CollapsibleTrigger>

          <CollapsibleContent>
            <div className="px-4 pb-4 space-y-4">
              <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <p className="text-sm text-muted-foreground">
                  {tr("indexersPage.description")}
                </p>
                <div className="flex flex-wrap gap-2 shrink-0">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setShowDeleteAllDialog(true)}
                    disabled={loading || indexers.length === 0}
                  >
                    <Trash2 className="h-4 w-4" />
                    {tr("indexersPage.actions.deleteAll")}
                  </Button>
                  <div className="flex">
                    <Button
                      size="sm"
                      onClick={() => setAutodiscoveryOpen(true)}
                      className="rounded-r-none"
                    >
                      <RefreshCw className="h-4 w-4" />
                      {tr("indexersPage.actions.discover")}
                    </Button>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button size="sm" className="rounded-l-none border-l px-2">
                          <ChevronDown className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => setAddDialogOpen(true)}>
                          <Plus className="h-4 w-4 mr-2" />
                          {tr("indexersPage.actions.addSingle")}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </div>
              </div>
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
                    toast.success(tr("indexersPage.toasts.syncCapsSuccess"))
                    setIndexers((prev) => prev.map((idx) => (idx.id === updated.id ? updated : idx)))
                  } catch (error) {
                    const message = error instanceof Error ? error.message : tr("indexersPage.toasts.syncCapsFailed")
                    toast.error(message)
                  }
                }}
              />
            </div>
          </CollapsibleContent>
        </div>
      </Collapsible>

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
            <AlertDialogTitle>{tr("indexersPage.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {tr("indexersPage.deleteDialog.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tr("indexersPage.actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={confirmDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {tr("indexersPage.actions.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={showDeleteAllDialog} onOpenChange={setShowDeleteAllDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{tr("indexersPage.deleteAllDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {tr("indexersPage.deleteAllDialog.description", { count: indexers.length })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tr("indexersPage.actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteAll}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {tr("indexersPage.actions.deleteAll")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

    </>
  )

  if (withContainer) {
    return (
      <div className="container mx-auto space-y-4 p-4 lg:p-6">
        {content}
      </div>
    )
  }

  return <div className="space-y-4">{content}</div>
}
