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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>Configure Reannounce</DialogTitle>
          <DialogDescription>{instanceName ?? "Instance"}</DialogDescription>
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
            {isUpdating ? "Saving..." : "Save Changes"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
