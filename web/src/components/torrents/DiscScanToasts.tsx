/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStreamManager } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"
import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// DiscScanToasts shows a toast when a Disc scan finishes, wherever the user is
// in qui. The discscan.run event carries the run id only, so each event reads
// the run. A progress event's read can land after the run completed and so
// can the completion event's read; the toasted set keeps that to one toast.
export function DiscScanToasts(): null {
  const { subscribeActivity } = useSyncStreamManager()
  const { t } = useTranslation("torrents")
  const toasted = useRef(new Set<number>())

  useEffect(() => subscribeActivity(event => {
    if (event.kind !== "discscan.run" || !event.instanceId || !event.resourceId) return
    const runId = Number(event.resourceId)
    api.getDiscScan(event.instanceId, runId).then(run => {
      if ((run.status !== "completed" && run.status !== "failed") || toasted.current.has(runId)) return
      toasted.current.add(runId)
      if (run.status === "completed") toast.success(t("discScan.toast.completed"))
      else toast.error(t("discScan.toast.failed", { error: run.errorMessage ?? "" }))
    }).catch(() => {})
  }), [subscribeActivity, t])

  return null
}
