/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery } from "@tanstack/react-query"
import { useBlocker } from "@tanstack/react-router"
import { useEffect, useState } from "react"

import { api, clearQuiServiceWorker, setSSORecoveryPaused } from "@/lib/api"
import { withBasePath } from "@/lib/base-url"
import type { SelfUpdateResult } from "@/types"

export type RestartOverlay = "hidden" | "restarting" | "slow" | "notApplied"

const POLL_INTERVAL_MS = 1000
const SLOW_AFTER_MS = 60_000

// /api/version reports "1.31.0", a release tag_name is "v1.31.0".
const withoutV = (version: string) => version.trim().replace(/^v/, "")

interface ActionTarget {
  // startedAt of the process that got the request. The old process can still
  // answer while its graceful shutdown waits, so only a new one counts.
  startedAt: string
  // Set after a Self-update.
  update?: SelfUpdateResult
}

export type SelfUpdate = ReturnType<typeof useSelfUpdate>

// Holds the Restart and Self-update actions: the confirmation dialog, the
// request, and the overlay that waits for qui to answer again.
export function useSelfUpdate() {
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [overlay, setOverlay] = useState<RestartOverlay>("hidden")

  const versionQuery = useQuery({
    queryKey: ["version"],
    queryFn: () => api.getVersion(),
    staleTime: 5 * 60 * 1000,
  })

  // updateTag is null for a plain Restart.
  const actionMutation = useMutation({
    mutationFn: async (updateTag: string | null): Promise<ActionTarget> => {
      const { startedAt } = await api.getApplicationInfo()
      // qui can close its listener before the response arrives, so pause
      // before the request, not when the overlay shows.
      setSSORecoveryPaused(true)
      if (updateTag === null) {
        await api.restartQui()
        return { startedAt }
      }
      return { startedAt, update: await api.selfUpdateQui(updateTag) }
    },
    onError: () => setSSORecoveryPaused(false),
    onSuccess: () => {
      setConfirmOpen(false)
      setOverlay("restarting")
    },
  })

  const overlayActive = overlay !== "hidden"
  // Browser Back can unmount a Settings tab that owns this hook, and the poll
  // with it. No beforeunload prompt: the poll reloads the page itself.
  useBlocker({
    shouldBlockFn: () => true,
    disabled: !overlayActive && !actionMutation.isPending,
    enableBeforeUnload: false,
  })
  const target = actionMutation.data
  useEffect(() => {
    if (!overlayActive || !target) {
      return
    }
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
        // A 404 after a base URL change must not reload: the overlay then
        // points the user to the new address.
        if (response.ok) {
          const info = await response.json().catch(() => null) as { startedAt?: string; version?: string } | null
          if (info?.startedAt && info.startedAt !== target.startedAt) {
            if (!target.update) {
              window.location.reload()
              return
            }
            if (withoutV(info.version ?? "") !== withoutV(target.update.version)) {
              setOverlay("notApplied")
              return
            }
            // The service worker serves the old frontend from its precache and
            // holds the new one behind the "Update available" toast.
            await clearQuiServiceWorker()
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
    const slowTimer = window.setTimeout(() => setOverlay((current) => current === "restarting" ? "slow" : current), SLOW_AFTER_MS)

    return () => {
      controller.abort()
      window.clearTimeout(pollTimer)
      window.clearTimeout(slowTimer)
      setSSORecoveryPaused(false)
    }
  }, [overlayActive, target])

  return {
    restartAvailable: versionQuery.data?.restart === true,
    selfUpdateAvailable: versionQuery.data?.selfUpdate === true,
    currentVersion: versionQuery.data?.version,
    confirmOpen,
    setConfirmOpen: (open: boolean) => {
      // Escape still closes the dialog while Cancel is disabled, and focus
      // returns to the button under the overlay. A reset then drops the
      // startedAt that the overlay compares against.
      if (overlayActive || (!open && actionMutation.isPending)) {
        return
      }
      setConfirmOpen(open)
      if (!open) {
        actionMutation.reset()
      }
    },
    restart: () => actionMutation.mutate(null),
    install: (tag: string) => actionMutation.mutate(tag),
    pending: actionMutation.isPending,
    error: actionMutation.error,
    overlay,
    update: target?.update,
  }
}
