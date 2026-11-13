/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { useTorrentSelectionContext, useTorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { usePersistedCrossSeedBlocklist } from "@/hooks/usePersistedCrossSeedBlocklist"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { usePersistedDeleteFiles } from "@/hooks/usePersistedDeleteFiles"
import { actionRefetchDelay, failedActionCount, getActionErrorMessage, invalidateInstanceCaches, removeDeletedTorrents, scheduleTorrentListRefetches, showSuccessToast, showTagUpdateError, updateTags } from "./bulk-action-requests"
import { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { api } from "@/lib/api"
import { useIncognitoMode } from "@/lib/incognito"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import type { TagUpdatePlan } from "@/lib/tag-editor"
import { buildTorrentActionTargets, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import { actionTargetInstanceIds, actionTargetRequest, EMPTY_ACTION_TARGET } from "@/lib/torrent-selection"
import { anyTorrentHasTag, getCommonCategory, getCommonSavePath, getTorrentHashesWithTag } from "@/lib/torrent-utils"
import { formatBytes } from "@/lib/utils"
import type { CrossInstanceTorrent } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useLayoutEffect, useMemo, useReducer, useRef, useState, type MouseEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { DeleteTorrentDialog } from "../DeleteTorrentDialog"
import {
  LocationWarningDialog,
  RenameTorrentDialog,
  SetCategoryDialog,
  SetCommentDialog,
  SetLocationDialog,
  ShareLimitDialog,
  SpeedLimitsDialog,
  TagEditorDialog,
  TmmConfirmDialog
} from "../TorrentDialogs"
import { BulkActionCategoriesContext, BulkActionsContext } from "./bulk-actions-context"
import { bulkDialogReducer, bulkRequest, CLOSED_DIALOG, deleteRequests, deleteSize, dialogLists, dialogPick, isConfirmationDialog, requestInstanceId, resolvePick, type BulkActionPick, type BulkRequestValues, type DeleteRequest } from "./bulk-action-state"

type BulkRequest = ReturnType<typeof bulkRequest>

interface TagRequest {
  scope: number
  count: number
  plan: TagUpdatePlan
  request: Parameters<typeof updateTags>[2]
}

interface RenameRequest {
  instanceId: number
  hash: string
  name: string
}

interface DeletePlan {
  instanceId: number
  block?: { hashes: string[]; targets: TorrentActionTarget[] }
  requests: DeleteRequest[]
}

/**
 * The bulk action module (ADR 0049). It acts on the Action target that the
 * Layout publishes, opens the dialog that a pick needs, sends the requests,
 * and exports. An open dialog follows the live selection, and it closes when
 * the selection becomes empty or the Layout publishes nothing. A failed
 * request leaves it open, except the tag editor, which closes after a
 * failure, and the share limits, which close on submit. A delete clears the
 * selection when the Action target is deleted.
 */
export function BulkActionProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()
  const { dispatch: dispatchSelection } = useTorrentSelectionContext()
  const published = useTorrentSelectionSummary()
  const [dialog, dispatch] = useReducer(bulkDialogReducer, CLOSED_DIALOG)

  // A pick reads the target at click time, so a new target does not change the value of the context.
  const publishedRef = useRef(published)
  useLayoutEffect(() => {
    publishedRef.current = published
  }, [published])

  if (!published?.actionTarget.count && dialog !== CLOSED_DIALOG) {
    dispatch({ type: "close" })
  }

  // The closed dialog keeps its direction while it animates out.
  const tmmEnable = useRef(false)
  if (dialog.name === "toggleAutoTMM") {
    tmmEnable.current = dialog.enable
  }

  const instanceId = published?.instanceId ?? -1
  const actionTarget = published?.actionTarget ?? EMPTY_ACTION_TARGET
  const listFacts = published?.listFacts
  const { data: metadata, isLoading: isMetadataLoading } = useInstanceMetadata(Math.max(instanceId, 0), {
    fallbackDelayMs: 1500,
  })
  const lists = useMemo(() => dialogLists(listFacts, metadata, isMetadataLoading), [listFacts, metadata, isMetadataLoading])
  const allowSubcategories = listFacts?.allowSubcategories ?? false
  const categoriesValue = useMemo(
    () => ({ categories: lists.categories, useSubcategories: allowSubcategories }),
    [lists.categories, allowSubcategories]
  )

  const [incognitoMode] = useIncognitoMode()
  const { exportTorrents, isExporting } = useTorrentExporter({ instanceId, incognitoMode })
  const exportRef = useRef(exportTorrents)
  useLayoutEffect(() => {
    exportRef.current = exportTorrents
  }, [exportTorrents])
  const {
    deleteFiles,
    setDeleteFiles,
    isLocked: isDeleteFilesLocked,
    toggleLock: toggleDeleteFilesLock,
  } = usePersistedDeleteFiles(false)
  const { blockCrossSeeds, setBlockCrossSeeds } = usePersistedCrossSeedBlocklist(instanceId, false)
  const { blockCrossSeedHashes } = useCrossSeedBlocklistActions(instanceId)
  const crossSeedWarning = useCrossSeedWarning({
    instanceId,
    torrents: actionTarget.previewTorrents,
    selectAll: actionTarget.kind === "selectAll" ? actionTarget.selectAll : undefined,
  })
  const hasCrossSeedTag = useMemo(
    () => anyTorrentHasTag(actionTarget.previewTorrents, "cross-seed") || anyTorrentHasTag(crossSeedWarning.affectedTorrents, "cross-seed"),
    [actionTarget.previewTorrents, crossSeedWarning.affectedTorrents]
  )

  // A close of the delete dialog, for any reason, stops the cross-seed check and clears its result.
  const isDeleteOpen = dialog.name === "delete"
  const resetCrossSeedWarning = crossSeedWarning.reset
  useEffect(() => {
    if (!isDeleteOpen) resetCrossSeedWarning()
  }, [isDeleteOpen, resetCrossSeedWarning])

  const { mutate, mutateAsync, isPending: isRequestPending } = useMutation({
    mutationFn: ({ scope, body }: BulkRequest) => api.bulkAction(scope, body),
    onSuccess: (_, { body, count }) => {
      scheduleTorrentListRefetches(queryClient, actionRefetchDelay(body.action))
      showSuccessToast(t, body.action, Math.max(1, count), undefined, "enable" in body ? body.enable : undefined)
      dispatch({ type: "requestSucceeded", action: body.action })
    },
    onError: (error: Error, { body }) => {
      toast.error(getActionErrorMessage(t, body.action, failedActionCount(body)), {
        description: error.message || t("actionToasts.unexpectedError"),
      })
    },
  })

  const { mutate: mutateDelete, isPending: isDeletePending } = useMutation({
    mutationFn: async ({ instanceId, block, requests }: DeletePlan) => {
      if (block) {
        await blockCrossSeedHashes(block.hashes, block.targets)
      }
      for (const [index, { scope, body, clientHashes, count }] of requests.entries()) {
        try {
          await api.bulkAction(scope, body)
        } catch (error) {
          toast.error(getActionErrorMessage(t, "delete", failedActionCount(body)), {
            description: (error instanceof Error && error.message) || t("actionToasts.unexpectedError"),
          })
          // The server deletes on one instance after another, so a failure can follow deletes on other instances.
          if (scope === ALL_INSTANCES_ID) {
            scheduleTorrentListRefetches(queryClient, body.deleteFiles ? 5000 : 2000)
            // A retry would resolve the filter again and delete torrents that joined it since.
            if (body.selectAll) {
              dispatch({ type: "close" })
              dispatchSelection({ type: "clear" })
            }
          }
          throw error
        }
        removeDeletedTorrents(queryClient, instanceId, { ...body, clientHashes, clientCount: count })
        scheduleTorrentListRefetches(queryClient, body.deleteFiles ? 5000 : 2000)
        showSuccessToast(t, "delete", Math.max(1, count), body.deleteFiles)
        // The first request deletes the Action target, so a failed cross-seed request leaves nothing to retry.
        if (index === 0) {
          dispatch({ type: "requestSucceeded", action: "delete" })
          dispatchSelection({ type: "clear" })
        }
      }
    },
  })

  // The tag editor closes after a failure, and the toast names the step that failed.
  const { mutate: mutateTags, isPending: isTagsPending } = useMutation({
    mutationFn: async ({ scope, plan, request }: TagRequest) => {
      try {
        await updateTags(scope, plan, request)
      } catch (error) {
        await invalidateInstanceCaches(queryClient, scope)
        throw error
      }
    },
    onSuccess: (_, { count }) => {
      scheduleTorrentListRefetches(queryClient, 1000)
      dispatch({ type: "requestSucceeded", action: "setTags" })
      toast.success(t("actionToasts.updatedTags", { count: Math.max(1, count) }))
    },
    onError: (error: Error, { count }) => {
      dispatch({ type: "close" })
      showTagUpdateError(t, error, count)
    },
  })

  const { mutate: mutateRename, isPending: isRenamePending } = useMutation({
    mutationFn: ({ instanceId, hash, name }: RenameRequest) => api.renameTorrent(instanceId, hash, name),
    onSuccess: (_, { name }) => {
      dispatch({ type: "requestSucceeded", action: "renameTorrent" })
      scheduleTorrentListRefetches(queryClient, 750)
      toast.success(t("actionToasts.renameTorrentSuccess", { name }))
    },
    onError: (error: Error) => {
      toast.error(t("actionToasts.renameTorrentFailed", { error: error.message }))
    },
  })

  // One mutation reports only its latest call, so the two speed-limit requests need their own flag.
  const [isSpeedLimitsPending, setIsSpeedLimitsPending] = useState(false)
  const isPending = isRequestPending || isDeletePending || isTagsPending || isRenamePending || isSpeedLimitsPending

  const send = useCallback((values: BulkRequestValues) => {
    const target = publishedRef.current
    if (target) {
      mutate(bulkRequest(values, target))
    }
  }, [mutate])

  const pick = useCallback((pick: BulkActionPick) => {
    const target = publishedRef.current
    if (!target) return
    const outcome = resolvePick(pick, target.actionTarget.count)
    if (outcome.kind === "open") {
      dispatch({ type: "open", dialog: outcome.dialog })
    } else if (outcome.kind === "export") {
      void exportRef.current(target.actionTarget, target.listFacts.sortField, target.listFacts.sortOrder)
    } else {
      send(outcome.pick)
    }
  }, [send])

  const value = useMemo(() => ({ pick, isPending, isExporting }), [pick, isPending, isExporting])
  const count = actionTarget.count
  const { previewTorrents } = actionTarget
  // Stable, because the tag editor runs its reset effect again when onOpenChange changes.
  const close = useCallback((open: boolean) => {
    if (!open) dispatch({ type: "close" })
  }, [])
  const confirm = () => {
    if (isConfirmationDialog(dialog)) send(dialogPick(dialog))
  }

  const confirmTags = (plan: TagUpdatePlan) => {
    if (!published) return
    if (plan.add.length === 0 && plan.remove.length === 0) {
      dispatch({ type: "close" })
      return
    }
    const request = { instanceIds: published.instanceIds, ...actionTargetRequest(published.actionTarget) }
    mutateTags({ scope: requestInstanceId(published), count: published.actionTarget.count, plan, request })
  }

  const confirmSpeedLimits = async (uploadLimit: number, downloadLimit: number) => {
    if (!published) return
    const requests: BulkRequestValues[] = []
    if (uploadLimit >= 0) requests.push({ action: "setUploadLimit", uploadLimit })
    if (downloadLimit >= 0) requests.push({ action: "setDownloadLimit", downloadLimit })
    setIsSpeedLimitsPending(true)
    const results = await Promise.allSettled(requests.map(values => mutateAsync(bulkRequest(values, published))))
    setIsSpeedLimitsPending(false)
    if (results.every(result => result.status === "fulfilled")) {
      dispatch({ type: "requestSucceeded", action: "setSpeedLimits" })
    }
  }

  // Rename acts on the first torrent of the Action target, on the instance of the page.
  const confirmRename = (name: string) => {
    const hash = previewTorrents[0]?.hash
    if (!hash) return
    const trimmed = name.trim()
    if (!trimmed) {
      toast.error(t("actionToasts.renameTorrentEmpty"))
      return
    }
    mutateRename({ instanceId, hash, name: trimmed })
  }

  // The dialog stays open until the Action target is deleted.
  const confirmDelete = (event: MouseEvent) => {
    event.preventDefault()
    if (!published || dialog.name !== "delete") return
    const { deleteCrossSeeds } = dialog
    const crossSeedTargets = deleteCrossSeeds ? buildTorrentActionTargets(crossSeedWarning.affectedTorrents, instanceId) : []
    const block = hasCrossSeedTag && blockCrossSeeds ? {
      hashes: [...getTorrentHashesWithTag(actionTarget.previewTorrents, "cross-seed"), ...(deleteCrossSeeds ? getTorrentHashesWithTag(crossSeedWarning.affectedTorrents, "cross-seed") : [])],
      targets: [...buildTorrentActionTargets(actionTarget.previewTorrents, instanceId), ...crossSeedTargets],
    } : undefined
    mutateDelete({ instanceId, block, requests: deleteRequests(published, crossSeedTargets, deleteFiles) })
  }

  const totalDeleteSize = useMemo(() => published ? deleteSize(published) : 0, [published])
  const tagSelectionRequest = useMemo(() => ({ instanceId, ...actionTargetRequest(actionTarget) }), [instanceId, actionTarget])
  const limitInstanceIds = useMemo(() => actionTargetInstanceIds(actionTarget, instanceId), [actionTarget, instanceId])
  const singleTorrent = previewTorrents.length === 1 ? previewTorrents[0] : undefined

  return (
    <BulkActionsContext.Provider value={value}>
      <BulkActionCategoriesContext.Provider value={categoriesValue}>
        {children}
      </BulkActionCategoriesContext.Provider>

      <Dialog open={dialog.name === "recheck"} onOpenChange={close}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("recheckDialog.title", { count })}</DialogTitle>
            <DialogDescription>
              {t("recheckDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => close(false)}>
              {t("recheckDialog.cancel")}
            </Button>
            <Button onClick={confirm} disabled={isPending}>
              {t("recheckDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={dialog.name === "reannounce"} onOpenChange={close}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("reannounceDialog.title", { count })}</DialogTitle>
            <DialogDescription>
              {t("reannounceDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => close(false)}>
              {t("reannounceDialog.cancel")}
            </Button>
            <Button onClick={confirm} disabled={isPending}>
              {t("reannounceDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <TmmConfirmDialog
        open={dialog.name === "toggleAutoTMM"}
        onOpenChange={close}
        count={count}
        enable={tmmEnable.current}
        onConfirm={confirm}
        isPending={isPending}
      />

      <DeleteTorrentDialog
        open={isDeleteOpen}
        onOpenChange={close}
        count={count}
        totalSize={totalDeleteSize}
        formattedSize={formatBytes(totalDeleteSize)}
        deleteFiles={deleteFiles}
        onDeleteFilesChange={setDeleteFiles}
        isDeleteFilesLocked={isDeleteFilesLocked}
        onToggleDeleteFilesLock={toggleDeleteFilesLock}
        deleteCrossSeeds={isDeleteOpen && dialog.deleteCrossSeeds}
        onDeleteCrossSeedsChange={(checked) => dispatch({ type: "deleteCrossSeeds", checked })}
        showBlockCrossSeeds={hasCrossSeedTag}
        blockCrossSeeds={blockCrossSeeds}
        onBlockCrossSeedsChange={setBlockCrossSeeds}
        crossSeedWarning={crossSeedWarning}
        onConfirm={confirmDelete}
        isPending={isPending}
        isAllSelected={actionTarget.kind === "selectAll"}
        previewTorrents={actionTarget.previewTorrents}
      />

      <TagEditorDialog
        open={dialog.name === "setTags"}
        onOpenChange={close}
        availableTags={lists.tags}
        selectedTorrents={previewTorrents}
        hashCount={count}
        selectionRequest={tagSelectionRequest}
        onConfirm={confirmTags}
        isPending={isPending}
        isLoadingTags={lists.isLoadingTags}
      />

      <SetCategoryDialog
        open={dialog.name === "setCategory"}
        onOpenChange={close}
        availableCategories={lists.categories}
        hashCount={count}
        onConfirm={(category) => send({ action: "setCategory", category })}
        isPending={isPending}
        initialCategory={getCommonCategory(previewTorrents)}
        isLoadingCategories={lists.isLoadingCategories}
        useSubcategories={allowSubcategories}
      />

      <LocationWarningDialog
        open={dialog.name === "locationWarning"}
        onOpenChange={close}
        count={count}
        onConfirm={() => dispatch({ type: "locationWarningConfirmed" })}
        isPending={isPending}
      />

      <SetLocationDialog
        open={dialog.name === "setLocation"}
        onOpenChange={close}
        hashCount={count}
        onConfirm={(location) => send({ action: "setLocation", location })}
        isPending={isPending}
        initialLocation={getCommonSavePath(previewTorrents)}
        instanceId={instanceId}
        capabilities={listFacts?.capabilities}
      />

      <ShareLimitDialog
        open={dialog.name === "setShareLimit"}
        onOpenChange={close}
        hashCount={count}
        torrents={previewTorrents}
        onConfirm={(ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, shareLimitAction, shareLimitsMode) => send({ action: "setShareLimit", ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, shareLimitAction, shareLimitsMode })}
        isPending={isPending}
        instanceIds={limitInstanceIds}
      />

      <SpeedLimitsDialog
        open={dialog.name === "setSpeedLimits"}
        onOpenChange={close}
        hashCount={count}
        torrents={previewTorrents}
        onConfirm={(uploadLimit, downloadLimit) => void confirmSpeedLimits(uploadLimit, downloadLimit)}
        isPending={isPending}
      />

      <SetCommentDialog
        open={dialog.name === "setComment"}
        onOpenChange={close}
        hashCount={count}
        instanceId={(singleTorrent as CrossInstanceTorrent | undefined)?.instanceId ?? instanceId}
        torrentHash={singleTorrent?.hash}
        onConfirm={(comment) => send({ action: "setComment", comment })}
        isPending={isPending}
      />

      <RenameTorrentDialog
        open={dialog.name === "renameTorrent"}
        onOpenChange={close}
        currentName={previewTorrents[0]?.name}
        onConfirm={confirmRename}
        isPending={isPending}
      />
    </BulkActionsContext.Provider>
  )
}
