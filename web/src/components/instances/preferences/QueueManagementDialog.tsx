/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { QueueManagementForm } from "./QueueManagementForm"
import { Clock } from "lucide-react"

interface QueueManagementDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function QueueManagementDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: QueueManagementDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Clock className="h-5 w-5" />
            Queue Management - {instanceName}
          </DialogTitle>
          <DialogDescription>
            Configure torrent queue settings and active torrent limits
          </DialogDescription>
        </DialogHeader>
        
        <QueueManagementForm 
          instanceId={instanceId} 
          onSuccess={() => onOpenChange(false)} 
        />
      </DialogContent>
    </Dialog>
  )
}