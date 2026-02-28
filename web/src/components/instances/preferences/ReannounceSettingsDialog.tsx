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
  DialogTitle,
} from "@/components/ui/dialog"
import { useInstances } from "@/hooks/useInstances"
import { useTranslation } from "react-i18next"
import { TrackerReannounceForm } from "./TrackerReannounceForm"

const FORM_ID = "reannounce-settings-dialog-form"

interface ReannounceSettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName?: string
}

export function ReannounceSettingsDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: ReannounceSettingsDialogProps) {
  const { isUpdating } = useInstances()
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>{tr("reannounceSettingsDialog.title")}</DialogTitle>
          <DialogDescription>{instanceName ?? tr("reannounceSettingsDialog.instanceFallback")}</DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto min-h-0 pr-1">
          <TrackerReannounceForm
            instanceId={instanceId}
            variant="embedded"
            formId={FORM_ID}
            onSuccess={() => onOpenChange(false)}
          />
        </div>

        <DialogFooter className="flex-shrink-0 border-t pt-4">
          <Button type="submit" form={FORM_ID} disabled={isUpdating}>
            {isUpdating ? tr("reannounceSettingsDialog.actions.saving") : tr("reannounceSettingsDialog.actions.saveChanges")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
