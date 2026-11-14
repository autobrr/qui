/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { useDatabaseShrink } from "@/hooks/useDatabaseShrink"
import { systemNoticesQueryKey } from "@/hooks/useSystemNotices"
import { api } from "@/lib/api"
import type { SystemNotice } from "@/types"

/** Try again, Dismiss, and for an unknown cause a link to the log, for the notice "Shrink failed". */
export function ShrinkFailedActions({ notice, onNavigate }: { notice: SystemNotice; onNavigate?: () => void }) {
  const { t } = useTranslation("common")
  const queryClient = useQueryClient()
  const { requestShrink, pending } = useDatabaseShrink()
  const dismiss = useMutation({
    mutationFn: () => api.dismissSystemNotice(notice.kind),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: systemNoticesQueryKey }),
    onError: (error) => toast.error(error.message),
  })
  const knownCause = notice.reason === "disk_full" || notice.reason === "locked"

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button size="sm" className="h-7 px-2.5 text-xs" disabled={pending} onClick={() => void requestShrink()}>
        {t("systemNotice.actions.tryAgain")}
      </Button>
      <Button size="sm" variant="ghost" className="h-7 px-2.5 text-xs" disabled={dismiss.isPending} onClick={() => dismiss.mutate()}>
        {t("systemNotice.actions.dismiss")}
      </Button>
      {!knownCause && (
        <Link to="/settings" search={{ tab: "logs" }} onClick={onNavigate} className="text-xs underline underline-offset-2">
          {t("systemNotice.actions.openLog")}
        </Link>
      )}
    </div>
  )
}
