/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { ChevronDown, Globe, Link2, Plus, RefreshCw, ScanSearch, Server, TestTube, Trash2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { AutodiscoveryDialog } from "@/components/indexers/AutodiscoveryDialog"
import { IndexerActivityPanel } from "@/components/indexers/IndexerActivityPanel"
import { IndexerDialog, type IndexerSaveRequest } from "@/components/indexers/IndexerDialog"
import { IndexerToolbar } from "@/components/indexers/IndexerToolbar"
import { activeFilterCount, DEFAULT_SORT, NO_FILTERS, viewIndexers } from "@/components/indexers/indexer-view"
import { SearchHistoryPanel } from "@/components/indexers/SearchHistoryPanel"
import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { ResourceList, type ResourceRow } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { usePendingIds } from "@/components/kit/use-pending-ids"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { TruncatedText } from "@/components/ui/truncated-text"
import { api } from "@/lib/api"
import type { TorznabIndexer } from "@/types"

// Shared with the cross-seed pages, so a change here also reaches them.
const INDEXERS_KEY = ["torznab", "indexers"]
const TOGGLE_KEY = ["torznab-indexers", "toggle"]
const TEST_KEY = ["torznab-indexers", "test"]
const SYNC_KEY = ["torznab-indexers", "sync"]

const errorMessage = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback

/** The Indexers tab: the Torznab indexers, the scheduler activity, and the search history. */
export function IndexersPage() {
  const indexersQuery = useQuery({
    queryKey: INDEXERS_KEY,
    queryFn: () => api.listTorznabIndexers(),
  })
  const hasIndexers = (indexersQuery.data?.length ?? 0) > 0

  return (
    <>
      <IndexersSection query={indexersQuery} />
      {hasIndexers && <IndexerActivityPanel />}
      {hasIndexers && <SearchHistoryPanel />}
    </>
  )
}

function IndexersSection({ query }: { query: UseQueryResult<TorznabIndexer[]> }) {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const indexers = query.data ?? []
  const [filters, setFilters] = useState(NO_FILTERS)
  const [sort, setSort] = useState(DEFAULT_SORT)
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<TorznabIndexer | null>(null)
  // A new key on each open gives the form fresh state.
  const [editorSession, setEditorSession] = useState(0)
  const [discoverOpen, setDiscoverOpen] = useState(false)
  const [deleteAllOpen, setDeleteAllOpen] = useState(false)

  const shown = viewIndexers(indexers, filters, sort)
  const filtered = activeFilterCount(filters) > 0
  const invalidate = () => queryClient.invalidateQueries({ queryKey: INDEXERS_KEY })
  const patchInCache = (id: number, patch: (indexer: TorznabIndexer) => TorznabIndexer) =>
    queryClient.setQueryData<TorznabIndexer[]>(INDEXERS_KEY, (old) => old?.map((indexer) => indexer.id === id ? patch(indexer) : indexer))

  const saveMutation = useMutation({
    mutationFn: ({ id, data }: IndexerSaveRequest) => id === null ? api.createTorznabIndexer(data) : api.updateTorznabIndexer(id, data),
    onSuccess: ({ warnings }, { id }) => {
      if (warnings?.length) {
        toast.warning(t(id === null ? "indexers.dialog.toast.createdWithWarnings" : "indexers.dialog.toast.updatedWithWarnings", { warnings: warnings.join(", ") }))
      } else {
        toast.success(t(id === null ? "indexers.dialog.toast.createdSuccess" : "indexers.dialog.toast.updatedSuccess"))
      }
      void invalidate()
      setEditorOpen(false)
    },
  })

  // The switch of an indexer takes no input while its save runs, so two saves of one indexer never overlap.
  const toggleMutation = useMutation({
    mutationKey: TOGGLE_KEY,
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api.updateTorznabIndexer(id, { enabled }),
    onMutate: async ({ id, enabled }) => {
      await queryClient.cancelQueries({ queryKey: INDEXERS_KEY })
      patchInCache(id, (indexer) => ({ ...indexer, enabled }))
    },
    onSuccess: ({ warnings }) => {
      if (warnings?.length) toast.warning(t("indexers.dialog.toast.updatedWithWarnings", { warnings: warnings.join(", ") }))
    },
    onError: (error, { id, enabled }) => {
      patchInCache(id, (indexer) => ({ ...indexer, enabled: !enabled }))
      toast.error(errorMessage(error, t("indexers.dialog.toast.failedEdit")))
    },
    onSettled: invalidate,
  })
  const toggling = usePendingIds(TOGGLE_KEY)

  const setTestResult = (id: number, status: "ok" | "error", error?: string) =>
    patchInCache(id, (indexer) => ({ ...indexer, last_test_status: status, last_test_error: error, last_test_at: new Date().toISOString() }))

  const testMutation = useMutation({
    mutationKey: TEST_KEY,
    mutationFn: (id: number) => api.testTorznabIndexer(id),
    onSuccess: (_result, id) => setTestResult(id, "ok"),
    onError: (error, id) => setTestResult(id, "error", errorMessage(error, t("indexers.toast.testFailed"))),
  })
  const testing = usePendingIds(TEST_KEY)

  const test = (indexer: TorznabIndexer) =>
    testMutation.mutateAsync(indexer.id).then(
      () => toast.success(t("indexers.toast.testSuccess")),
      (error: unknown) => toast.error(errorMessage(error, t("indexers.toast.testFailed")))
    )

  const testAll = async () => {
    if (shown.length === 0) {
      toast.info(t("indexers.toast.noIndexersToTest"))
      return
    }
    const toastId = toast.loading(t("indexers.toast.testing", { count: shown.length }))
    const results = await Promise.all(shown.map((indexer) =>
      testMutation.mutateAsync(indexer.id).then(() => ({ name: indexer.name, ok: true }), () => ({ name: indexer.name, ok: false }))
    ))
    const failed = results.filter((result) => !result.ok).map((result) => result.name)
    if (failed.length === 0) {
      toast.success(t("indexers.toast.testAllSuccess", { count: results.length }), { id: toastId })
    } else {
      toast.warning(t("indexers.toast.testAllPartial", { success: results.length - failed.length, failed: failed.length }), { id: toastId })
      toast.error(t("indexers.toast.failedIndexers", { names: failed.join(", ") }))
    }
  }

  const syncMutation = useMutation({
    mutationKey: SYNC_KEY,
    mutationFn: (id: number) => api.syncTorznabCaps(id),
    onSuccess: ({ id, capabilities, categories }) => {
      // The answer carries the whole row, and a switch save can land after the server read it.
      patchInCache(id, (indexer) => ({ ...indexer, capabilities, categories }))
      toast.success(t("indexers.toast.capsSynced"))
    },
    onError: (error) => toast.error(errorMessage(error, t("indexers.toast.failedToSyncCaps"))),
  })
  const syncing = usePendingIds(SYNC_KEY)

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteTorznabIndexer(id),
    onSuccess: () => {
      void invalidate()
      toast.success(t("indexers.toast.deletedSuccess"))
    },
    onError: () => toast.error(t("indexers.toast.deleteFailed")),
  })

  const deleteAll = async () => {
    const all = indexers
    const results = await Promise.all(all.map((indexer) => api.deleteTorznabIndexer(indexer.id).then(() => true, () => false)))
    const failed = all.filter((_indexer, index) => !results[index]).map((indexer) => indexer.name)
    if (failed.length === 0) {
      toast.success(t("indexers.toast.deleteAllSuccess", { count: all.length }))
    } else {
      toast.warning(t("indexers.toast.deleteAllPartial", { success: all.length - failed.length, failed: failed.length, names: failed.join(", ") }))
    }
    await invalidate()
  }

  const openEditor = (indexer: TorznabIndexer | null) => {
    saveMutation.reset()
    setEditing(indexer)
    setEditorSession((session) => session + 1)
    setEditorOpen(true)
  }

  const status = (indexer: TorznabIndexer): ResourceRow["status"] => {
    if (testing.has(indexer.id)) return { tone: "off", label: t("indexers.list.testing") }
    if (indexer.last_test_status === "ok") return { tone: "ok", label: t("indexers.table.working") }
    if (indexer.last_test_status === "error") return { tone: "error", label: t("indexers.table.failed") }
    return { tone: "off", label: t("indexers.table.untested") }
  }

  const summary = filtered
    ? t("indexers.table.showingCount", { shown: shown.length, total: indexers.length })
    : t("indexers.page.summary", {
      enabled: indexers.filter((indexer) => indexer.enabled).length,
      capabilities: indexers.filter((indexer) => indexer.capabilities?.length).length,
    })

  return (
    <>
      <Section
        title={t("indexers.title")}
        description={t("indexers.page.description")}
        actions={
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" disabled={!query.data}>
                <Plus />
                {t("indexers.dialog.addTitle")}
                <ChevronDown className="text-muted-foreground" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem className="pointer-coarse:py-2.5" onSelect={() => setDiscoverOpen(true)}>
                <ScanSearch />
                {t("indexers.page.addMenu.discover")}
              </DropdownMenuItem>
              <DropdownMenuItem className="pointer-coarse:py-2.5" onSelect={() => openEditor(null)}>
                <Link2 />
                {t("indexers.page.addMenu.manual")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        }
        queries={[query]}
      >
        {indexers.length > 0 && (
          <IndexerToolbar
            summary={summary}
            filters={filters}
            onFiltersChange={setFilters}
            sort={sort}
            onSortChange={setSort}
            onTestAll={() => void testAll()}
            testing={testing.size > 0}
            onDeleteAll={() => setDeleteAllOpen(true)}
          />
        )}
        <ResourceList
          label={t("indexers.title")}
          items={shown}
          itemKey={(indexer) => indexer.id}
          row={(indexer) => ({
            title: indexer.name,
            icon: indexer.backend === "native" ? Globe : Server,
            status: status(indexer),
            meta: [
              t(`indexers.dialog.backends.${indexer.backend}`),
              t("indexers.list.priority", { priority: indexer.priority }),
              indexer.capabilities?.length
                ? t("indexers.list.capabilities", { count: indexer.capabilities.length })
                : t("indexers.table.noCapabilities"),
            ],
            enabled: {
              checked: indexer.enabled,
              onCheckedChange: (enabled) => toggleMutation.mutate({ id: indexer.id, enabled }),
              disabled: toggling.has(indexer.id),
            },
            onOpen: () => openEditor(indexer),
            actions: [
              { label: t("indexers.table.tooltipTest"), icon: TestTube, pending: testing.has(indexer.id), onSelect: () => void test(indexer) },
              { label: t("indexers.table.tooltipSyncCaps"), icon: RefreshCw, pending: syncing.has(indexer.id), onSelect: () => syncMutation.mutate(indexer.id) },
              {
                label: t("indexers.table.tooltipDelete"),
                icon: Trash2,
                destructive: true,
                onSelect: () => deleteMutation.mutateAsync(indexer.id),
                confirm: {
                  title: t("indexers.deleteIndexer"),
                  description: t("indexers.deleteIndexerDescription", { name: indexer.name }),
                  confirmLabel: t("indexers.table.tooltipDelete"),
                },
              },
            ],
            detail: indexer.last_test_status === "error" && indexer.last_test_error && !testing.has(indexer.id)
              ? <TruncatedText wrap className="line-clamp-2 text-xs text-destructive [overflow-wrap:anywhere]">{indexer.last_test_error}</TruncatedText>
              : undefined,
          })}
          empty={filtered
            ? { title: t("indexers.table.noMatch") }
            : { title: t("indexers.list.emptyTitle"), description: t("indexers.list.emptyDescription"), onAdd: () => setDiscoverOpen(true) }}
        />
      </Section>
      {/* Outside the section, so a failed refetch of the list never takes an open form away. */}
      <IndexerDialog
        key={editorSession}
        open={editorOpen}
        onOpenChange={setEditorOpen}
        indexer={editing}
        pending={saveMutation.isPending}
        error={saveMutation.error
          ? errorMessage(saveMutation.error, t(editing ? "indexers.dialog.toast.failedEdit" : "indexers.dialog.toast.failedCreate"))
          : undefined}
        onSave={(request) => saveMutation.mutate(request)}
      />
      <AutodiscoveryDialog
        open={discoverOpen}
        indexers={indexers}
        onClose={() => {
          setDiscoverOpen(false)
          void invalidate()
        }}
      />
      <ConfirmDialog
        open={deleteAllOpen}
        onOpenChange={setDeleteAllOpen}
        title={t("indexers.deleteAllIndexers")}
        description={t("indexers.deleteAllDescription", { count: indexers.length })}
        confirmLabel={t("indexers.deleteAllIndexers")}
        onConfirm={deleteAll}
      />
    </>
  )
}
