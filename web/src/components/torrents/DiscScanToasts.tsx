/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStreamManager } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"
import { useEffect } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// DiscScanToasts shows a toast when a Disc scan finishes, wherever the user is
// in qui. The discscan.run event carries the run id only, so each event reads
// the run; the service fires exactly one event after a run completes or fails,
// and none when a start returns a cached report.
export function DiscScanToasts(): null {
  const { subscribeActivity } = useSyncStreamManager()
  const { t } = useTranslation("torrents")

  useEffect(() => subscribeActivity(event => {
    if (event.kind !== "discscan.run" || !event.instanceId || !event.resourceId) return
    api.getDiscScan(event.instanceId, Number(event.resourceId)).then(run => {
      if (run.status === "completed") toast.success(t("discScan.toast.completed"))
      if (run.status === "failed") toast.error(t("discScan.toast.failed", { error: run.errorMessage ?? "" }))
    }).catch(() => {})
  }), [subscribeActivity, t])

  return null
}
