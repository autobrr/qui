/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { AlertTriangle } from "lucide-react"

export function ReannounceEnableWarningAlert() {
  return (
    <Alert variant="warning" className="border-yellow-500/40 bg-yellow-500/10 text-yellow-950 dark:text-yellow-100">
      <AlertTriangle className="h-4 w-4 text-yellow-600 dark:text-yellow-400" />
      <AlertTitle>Leave this off unless you need it</AlertTitle>
      <AlertDescription className="space-y-1">
        <p>This only helps with a small subset of trackers that are slow to register new uploads.</p>
        <p>If you are not seeing stalled torrents, do not enable it.</p>
      </AlertDescription>
    </Alert>
  )
}

interface ReannounceEnableWarningDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  confirming?: boolean
}

export function ReannounceEnableWarningDialog({
  open,
  onOpenChange,
  onConfirm,
  confirming = false,
}: ReannounceEnableWarningDialogProps) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Enable automatic reannounce?</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-3">
              <p>Use this only if stalled torrents are a real problem on this instance.</p>
              <p>It helps with only a handful of trackers that are slow to register new uploads.</p>
              <p>If torrents are behaving normally, leave it off.</p>
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={confirming}>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} disabled={confirming}>
            {confirming ? "Enabling..." : "Enable"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
