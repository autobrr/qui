/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { SpeedLimitsForm } from "./SpeedLimitsForm"
import { Download } from "lucide-react"

interface SpeedLimitsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function SpeedLimitsDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: SpeedLimitsDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Download className="h-5 w-5" />
            Speed Limits - {instanceName}
          </DialogTitle>
          <DialogDescription>
            Configure download and upload speed limits for this qBittorrent instance
          </DialogDescription>
        </DialogHeader>
        
        <SpeedLimitsForm 
          instanceId={instanceId} 
          onSuccess={() => onOpenChange(false)} 
        />
      </DialogContent>
    </Dialog>
  )
}