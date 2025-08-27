/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { InstancePreferencesForm } from "@/components/settings/InstancePreferencesForm"

interface InstancePreferencesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function InstancePreferencesDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: InstancePreferencesDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Instance Preferences - {instanceName}</DialogTitle>
          <DialogDescription>
            Configure qBittorrent preferences for this instance
          </DialogDescription>
        </DialogHeader>
        
        <InstancePreferencesForm instanceId={instanceId} />
      </DialogContent>
    </Dialog>
  )
}