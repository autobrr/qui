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
import { usePersistedDeleteFiles } from "@/hooks/usePersistedDeleteFiles"
import { actionRefetchDelay, failedActionCount, getActionErrorMessage, removeDeletedTorrents, scheduleTorrentListRefetches, showSuccessToast } from "@/hooks/useTorrentActions"
import { api } from "@/lib/api"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import { buildTorrentActionTargets, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import { EMPTY_ACTION_TARGET } from "@/lib/torrent-selection"
import { anyTorrentHasTag, getTorrentHashesWithTag } from "@/lib/torrent-utils"
import { formatBytes } from "@/lib/utils"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useLayoutEffect, useMemo, useReducer, useRef, type MouseEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { DeleteTorrentDialog } from "../DeleteTorrentDialog"
import { TmmConfirmDialog } from "../TorrentDialogs"
import { BulkActionsContext } from "./bulk-actions-context"
import { bulkDialogReducer, bulkRequest, CLOSED_DIALOG, deleteRequests, deleteSize, dialogPick, resolvePick, type BulkActionPick, type DeleteRequest } from "./bulk-action-state"

type BulkRequest = ReturnType<typeof bulkRequest>

interface DeletePlan {
  instanceId: number
  block?: { hashes: string[]; targets: TorrentActionTarget[] }
  requests: DeleteRequest[]
}

/**
 * The bulk action module (ADR 0049). It acts on the Action target that the
 * Layout publishes, opens the dialog that a pick needs, and sends the
 * requests. An open dialog follows the live selection, and it closes when the
 * selection becomes empty or the Layout publishes nothing. A failed request
 * leaves it open. A delete clears the selection when the Action target is deleted.
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

  const { mutate, isPending: isRequestPending } = useMutation({
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
          // The server deletes on one instance after another, so a failure can follow deletes on other
          // instances. A retry would resolve the filter again and delete torrents that joined it since.
          if (scope === ALL_INSTANCES_ID && body.selectAll) {
            dispatch({ type: "close" })
            dispatchSelection({ type: "clear" })
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

  const isPending = isRequestPending || isDeletePending

  const send = useCallback((pick: BulkActionPick) => {
    const target = publishedRef.current
    if (target) {
      mutate(bulkRequest(pick, target))
    }
  }, [mutate])

  const pick = useCallback((pick: BulkActionPick) => {
    const target = publishedRef.current
    if (!target) return
    const outcome = resolvePick(pick, target.actionTarget.count)
    if (outcome.kind === "open") {
      dispatch({ type: "open", dialog: outcome.dialog })
    } else {
      send(outcome.pick)
    }
  }, [send])

  const value = useMemo(() => ({ pick, isPending }), [pick, isPending])
  const count = actionTarget.count
  const close = (open: boolean) => {
    if (!open) dispatch({ type: "close" })
  }
  const confirm = () => {
    if (dialog.name !== "closed" && dialog.name !== "delete") send(dialogPick(dialog))
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

  return (
    <BulkActionsContext.Provider value={value}>
      {children}

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
    </BulkActionsContext.Provider>
  )
}
