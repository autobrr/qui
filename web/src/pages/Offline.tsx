/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { WifiOff } from "lucide-react"
import { toast } from "sonner"
import { router } from "@/router"
import { LAST_ONLINE_PATH_KEY } from "@/hooks/useOfflineRedirect"

export function Offline() {
  const retry = () => {
    if (typeof navigator !== "undefined" && navigator.onLine) {
      const target = (() => {
        try {
          return sessionStorage.getItem(LAST_ONLINE_PATH_KEY) || "/"
        } catch {
          return "/"
        }
      })()

      try {
        router.navigate({ to: target as any })
      } catch {
        router.navigate({ to: "/" as any })
      }
      return
    }

    toast.info("Still offline. Check your connection and try again.")
  }

  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-12">
      <div className="w-full max-w-lg space-y-6 text-center">
        <div className="flex justify-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-full bg-muted">
            <WifiOff className="h-7 w-7 text-destructive" />
          </div>
        </div>
        <div className="space-y-2">
          <h1 className="text-3xl font-bold tracking-tight">You&apos;re offline</h1>
        </div>
        <div className="flex items-center justify-center gap-3">
          <Button onClick={retry}>
            Retry connection
          </Button>
        </div>
      </div>
    </div>
  )
}
