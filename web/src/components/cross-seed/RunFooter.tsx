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
import { CardFooter } from "@/components/ui/card"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { RunState } from "@/lib/run-state"
import { cn } from "@/lib/utils"
import { Loader2, XCircle } from "lucide-react"
import { useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

interface RunFooterProps {
  state: RunState
  runLabel: string
  runIcon: ReactNode
  onRun: () => void
  /** Called after the user confirms the cancel dialog. */
  onCancel: () => void
  cancelDialog: { title: string; description: string }
  /** Run options such as Dry run, shown after the run button. */
  options?: ReactNode
  /** The Save button, shown at the right. */
  children: ReactNode
}

export function RunFooter({ state, runLabel, runIcon, onRun, onCancel, cancelDialog, options, children }: RunFooterProps) {
  const { t } = useTranslation("crossseed")
  const [confirmOpen, setConfirmOpen] = useState(false)
  const busy = state.kind === "running" || state.kind === "cancelling"
  const blocked = state.kind === "blocked"

  return (
    <CardFooter className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-wrap items-center gap-3">
        {busy ? (
          <Button
            variant="outline"
            className="min-h-11 md:min-h-9"
            onClick={() => setConfirmOpen(true)}
            disabled={state.kind === "cancelling"}
          >
            {state.kind === "cancelling" ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <XCircle className="mr-2 h-4 w-4" />}
            {t(state.kind === "cancelling" ? "automation.stopping" : "automation.cancel")}
          </Button>
        ) : (
          // A disabled button lets taps and hover through to the wrapper, which shows why the run is blocked.
          <Tooltip>
            <TooltipTrigger asChild>
              <span className={cn("inline-flex", blocked && "cursor-not-allowed")}>
                <Button
                  variant="outline"
                  className="min-h-11 md:min-h-9"
                  onClick={onRun}
                  disabled={blocked}
                >
                  {runIcon}
                  {runLabel}
                </Button>
              </span>
            </TooltipTrigger>
            {blocked && (
              <TooltipContent align="start" className="max-w-xs text-xs">
                {state.reason}
              </TooltipContent>
            )}
          </Tooltip>
        )}
        {state.kind === "running" && state.note && <span className="text-sm text-muted-foreground">{state.note}</span>}
        {options}
      </div>
      {children}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{cancelDialog.title}</AlertDialogTitle>
            <AlertDialogDescription>{cancelDialog.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("cancelDialog.keepRunning")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={onCancel}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("cancelDialog.cancelRun")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </CardFooter>
  )
}
