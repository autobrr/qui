/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery } from "@tanstack/react-query"
import { useEffect, useState } from "react"

import { api, setSSORecoveryPaused } from "@/lib/api"
import { withBasePath } from "@/lib/base-url"

export type RestartOverlay = "hidden" | "restarting" | "slow"

const POLL_INTERVAL_MS = 1000
const SLOW_AFTER_MS = 60_000

// Holds the Restart action: the confirmation dialog, the request, and the
// overlay that waits for qui to answer again.
export function useSelfUpdate() {
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [overlay, setOverlay] = useState<RestartOverlay>("hidden")

  const versionQuery = useQuery({
    queryKey: ["version"],
    queryFn: () => api.getVersion(),
    staleTime: 5 * 60 * 1000,
  })

  const restartMutation = useMutation({
    mutationFn: async () => {
      const { startedAt } = await api.getApplicationInfo()
      await api.restartQui()
      return startedAt
    },
    onSuccess: () => {
      setConfirmOpen(false)
      setOverlay("restarting")
    },
  })

  const overlayActive = overlay !== "hidden"
  // startedAt of the process that got the Restart. The old process can still
  // answer while its graceful shutdown waits, so only a new one reloads.
  const startedAt = restartMutation.data
  useEffect(() => {
    if (!overlayActive) {
      return
    }
    setSSORecoveryPaused(true)
    // Plain fetch, not the API client: its SSO and 401 handling would navigate
    // away while qui is down.
    const controller = new AbortController()
    let pollTimer = 0
    const poll = async () => {
      try {
        const response = await fetch(withBasePath("/api/application/info"), {
          cache: "no-store",
          credentials: "include",
          signal: controller.signal,
        })
        // A reverse proxy answers 5xx while qui is down. Any other answer that
        // is not the old process comes from the new qui or the login in front of it.
        if (response.status < 500) {
          const info = await response.json().catch(() => null) as { startedAt?: string } | null
          if (info?.startedAt !== startedAt) {
            window.location.reload()
            return
          }
        }
      } catch {
        // qui is down.
      }
      if (!controller.signal.aborted) {
        pollTimer = window.setTimeout(poll, POLL_INTERVAL_MS)
      }
    }
    pollTimer = window.setTimeout(poll, POLL_INTERVAL_MS)
    const slowTimer = window.setTimeout(() => setOverlay("slow"), SLOW_AFTER_MS)

    return () => {
      controller.abort()
      window.clearTimeout(pollTimer)
      window.clearTimeout(slowTimer)
      setSSORecoveryPaused(false)
    }
  }, [overlayActive, startedAt])

  return {
    restartAvailable: versionQuery.data?.restart === true,
    confirmOpen,
    setConfirmOpen: (open: boolean) => {
      setConfirmOpen(open)
      if (!open) {
        restartMutation.reset()
      }
    },
    restart: () => restartMutation.mutate(),
    restartPending: restartMutation.isPending,
    restartError: restartMutation.error,
    overlay,
  }
}
