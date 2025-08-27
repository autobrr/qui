/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { SeedingLimitsForm } from "./SeedingLimitsForm"
import { Upload } from "lucide-react"

interface SeedingLimitsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function SeedingLimitsDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: SeedingLimitsDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Upload className="h-5 w-5" />
            Seeding Limits - {instanceName}
          </DialogTitle>
          <DialogDescription>
            Configure share ratio and seeding time limits
          </DialogDescription>
        </DialogHeader>
        
        <SeedingLimitsForm 
          instanceId={instanceId} 
          onSuccess={() => onOpenChange(false)} 
        />
      </DialogContent>
    </Dialog>
  )
}