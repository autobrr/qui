/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery } from "@tanstack/react-query"
import { useBlocker } from "@tanstack/react-router"
import { useEffect, useState } from "react"

import { api, clearQuiServiceWorker, setSSORecoveryPaused } from "@/lib/api"
import { getAppVersion } from "@/lib/build-info"
import { withBasePath } from "@/lib/base-url"
import type { SelfUpdateResult } from "@/types"

export type RestartOverlay = "hidden" | "restarting" | "slow" | "notApplied" | "installed"

const POLL_INTERVAL_MS = 1000
const SLOW_AFTER_MS = 60_000

const withoutV = (version: string) => version.trim().replace(/^v/, "")

interface ActionTarget {
  startedAt: string
  update?: SelfUpdateResult
}

export type SelfUpdate = ReturnType<typeof useSelfUpdate>

type SystemAction = { kind: "restart" } | { kind: "update"; tag: string } | { kind: "shrink" }

export function useVersionInfo() {
  return useQuery({
    queryKey: ["version"],
    queryFn: () => api.getVersion(),
    staleTime: 5 * 60 * 1000,
  })
}

export function useSelfUpdate() {
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [overlay, setOverlay] = useState<RestartOverlay>("hidden")

  const versionQuery = useVersionInfo()

  const actionMutation = useMutation({
    mutationFn: async (action: SystemAction): Promise<ActionTarget | null> => {
      const { startedAt } = await api.getApplicationInfo()
      setSSORecoveryPaused(true)
      switch (action.kind) {
        case "restart":
          await api.restartQui()
          return { startedAt }
        case "shrink": {
          const { restarting } = await api.requestDatabaseShrink()
          if (!restarting) {
            setSSORecoveryPaused(false)
            return null
          }
          return { startedAt }
        }
        case "update":
          return { startedAt, update: await api.selfUpdateQui(action.tag) }
      }
    },
    onError: () => setSSORecoveryPaused(false),
    onSuccess: (target) => {
      setConfirmOpen(false)
      if (target) {
        setOverlay("restarting")
      }
    },
  })

  const overlayActive = overlay !== "hidden"
  useBlocker({
    shouldBlockFn: () => true,
    disabled: !overlayActive && !actionMutation.isPending,
    enableBeforeUnload: false,
  })
  const target = actionMutation.data ?? undefined
  useEffect(() => {
    if (!overlayActive || !target) {
      return
    }
    const controller = new AbortController()
    let pollTimer = 0
    const poll = async () => {
      try {
        const response = await fetch(withBasePath("/api/application/info"), {
          cache: "no-store",
          credentials: "include",
          signal: controller.signal,
        })
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
            await clearQuiServiceWorker()
            if (target.update.backupError) {
              setOverlay("installed")
              return
            }
            window.location.reload()
            return
          }
        }
      } catch {
        if (controller.signal.aborted) return
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
    currentVersion: withoutV(versionQuery.data?.version ?? getAppVersion()),
    selfUpdateUnavailableReason: versionQuery.data?.selfUpdateUnavailableReason,
    supportChecking: versionQuery.isPending || versionQuery.isFetching,
    supportCheckFailed: versionQuery.isError && !versionQuery.isFetching,
    retrySupportCheck: () => void versionQuery.refetch(),
    confirmOpen,
    setConfirmOpen: (open: boolean) => {
      if (overlayActive || (!open && actionMutation.isPending)) {
        return
      }
      setConfirmOpen(open)
      if (!open) {
        actionMutation.reset()
      }
    },
    restart: () => actionMutation.mutate({ kind: "restart" }),
    install: (tag: string) => actionMutation.mutate({ kind: "update", tag }),
    shrink: async () => (await actionMutation.mutateAsync({ kind: "shrink" })) !== null,
    pending: actionMutation.isPending,
    error: actionMutation.error,
    overlay,
    update: target?.update,
  }
}
