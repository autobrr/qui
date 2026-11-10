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
import { useTorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import { actionRefetchDelay, failedActionCount, getActionErrorMessage, scheduleTorrentListRefetches, showSuccessToast } from "@/hooks/useTorrentActions"
import { api } from "@/lib/api"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useCallback, useLayoutEffect, useMemo, useReducer, useRef, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { TmmConfirmDialog } from "../TorrentDialogs"
import { BulkActionsContext } from "./bulk-actions-context"
import { bulkDialogReducer, bulkRequest, CLOSED_DIALOG, dialogPick, resolvePick, type BulkActionPick } from "./bulk-action-state"

type BulkRequest = ReturnType<typeof bulkRequest>

/**
 * The bulk action module (ADR 0049). It acts on the Action target that the
 * Layout publishes, opens the confirmation that a pick needs, and sends the
 * request. An open confirmation follows the live selection, and it closes when
 * the selection becomes empty or the Layout publishes nothing. A failed request
 * leaves it open.
 */
export function BulkActionProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()
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

  const { mutate, isPending } = useMutation({
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
  const count = published?.actionTarget.count ?? 0
  const close = (open: boolean) => {
    if (!open) dispatch({ type: "close" })
  }
  const confirm = () => {
    if (dialog.name !== "closed") send(dialogPick(dialog))
  }

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
    </BulkActionsContext.Provider>
  )
}
