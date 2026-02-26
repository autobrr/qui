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
import { useTranslation } from "react-i18next"
import { OrphanScanSettingsForm } from "./OrphanScanSettingsForm"

const FORM_ID = "orphan-scan-settings-dialog-form"

interface OrphanScanSettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName?: string
}

export function OrphanScanSettingsDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: OrphanScanSettingsDialogProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl max-h-[90dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{tr("orphanScanSettingsDialog.title")}</DialogTitle>
          <DialogDescription>{instanceName ?? tr("orphanScanSettingsDialog.instanceFallback")}</DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto pr-1">
          <OrphanScanSettingsForm
            instanceId={instanceId}
            formId={FORM_ID}
            onSuccess={() => onOpenChange(false)}
          />
        </div>

        <DialogFooter className="border-t pt-4">
          <Button type="submit" form={FORM_ID}>
            {tr("orphanScanSettingsDialog.actions.saveChanges")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
