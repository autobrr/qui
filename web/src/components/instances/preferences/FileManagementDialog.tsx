/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { FileManagementForm } from "./FileManagementForm"
import { Folder } from "lucide-react"

interface FileManagementDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function FileManagementDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: FileManagementDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Folder className="h-5 w-5" />
            File Management - {instanceName}
          </DialogTitle>
          <DialogDescription>
            Configure file paths and torrent management settings
          </DialogDescription>
        </DialogHeader>
        
        <FileManagementForm 
          instanceId={instanceId} 
          onSuccess={() => onOpenChange(false)} 
        />
      </DialogContent>
    </Dialog>
  )
}