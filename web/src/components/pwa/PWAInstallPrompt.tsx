/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useRef } from "react"
import { Button } from "@/components/ui/button"
import { usePWAInstallPrompt } from "@/hooks/usePWAInstallPrompt"
import { toast } from "sonner"

export function PWAInstallPrompt() {
  const { promptAvailable, promptInstall, suppressFor, dismissForever } = usePWAInstallPrompt()
  const toastIdRef = useRef<string | number | null>(null)
  const isMountedRef = useRef(true)

  const clearToastRef = () => {
    toastIdRef.current = null
  }

  useEffect(() => {
    isMountedRef.current = true
    if (!promptAvailable || toastIdRef.current !== null) return

    const toastId = toast.custom((id) => (
      <div className="grid gap-3 rounded-lg border bg-popover p-4 text-popover-foreground shadow-lg">
        <div className="text-sm font-medium leading-tight">Install qui for faster access?</div>
        <div className="text-xs text-muted-foreground">Add qui to your home screen for faster launch and an app-like experience.</div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            onClick={async () => {
              const accepted = await promptInstall()
              if (!isMountedRef.current) return
              toast.dismiss(id)
              clearToastRef()
              if (!accepted) {
                suppressFor()
              }
            }}
          >
            Install app
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              if (!isMountedRef.current) return
              suppressFor()
              toast.dismiss(id)
              clearToastRef()
            }}
          >
            Not now
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              if (!isMountedRef.current) return
              dismissForever()
              toast.dismiss(id)
              clearToastRef()
            }}
          >
            Don&apos;t ask again
          </Button>
        </div>
      </div>
    ), {
      duration: Number.POSITIVE_INFINITY,
      closeButton: true,
      onDismiss: () => {
        if (isMountedRef.current) suppressFor()
        if (isMountedRef.current) clearToastRef()
      },
      onAutoClose: () => {
        if (isMountedRef.current) suppressFor()
        if (isMountedRef.current) clearToastRef()
      },
      classNames: {
        toast: "max-w-md",
      },
    })

    toastIdRef.current = toastId

    return () => {
      isMountedRef.current = false
      if (toastIdRef.current !== null) {
        toast.dismiss(toastIdRef.current)
        toastIdRef.current = null
      }
    }
  }, [promptAvailable, promptInstall, suppressFor, dismissForever])

  return null
}
