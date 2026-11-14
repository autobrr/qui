/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { OctagonAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { useDatabaseShrink } from "@/hooks/useDatabaseShrink"
import { systemNoticesQueryKey } from "@/hooks/useSystemNotices"
import { api } from "@/lib/api"
import { systemNoticeStyles } from "@/lib/system-notice"
import { cn } from "@/lib/utils"
import type { SystemNotice } from "@/types"

export function SystemNoticeContent({ notice }: { notice: SystemNotice }) {
  const { t } = useTranslation("common")
  const styles = systemNoticeStyles[notice.severity]
  const fixKeys = [`systemNotice.kinds.${notice.kind}.fix`, "systemNotice.unknown.fix"]
  if (notice.reason) {
    fixKeys.unshift(`systemNotice.kinds.${notice.kind}.fixes.${notice.reason}`)
  }

  return (
    <div className="flex items-start gap-2 min-w-0">
      <OctagonAlert className={cn("h-4 w-4 mt-0.5 flex-shrink-0", styles.icon)} />
      <div className="flex-1 min-w-0">
        <p className={cn("text-sm font-medium", styles.strong)}>
          {t([`systemNotice.kinds.${notice.kind}.title`, "systemNotice.unknown.title"])}
        </p>
        <p className={cn("text-xs mt-1", styles.body)}>
          {t(fixKeys)}
        </p>
        <p className={cn("text-xs mt-1 break-words opacity-80", styles.body)}>
          {t("systemNotice.cause", { cause: notice.cause })}
        </p>
        {notice.kind === "shrink_failed" && <ShrinkFailedActions notice={notice} />}
      </div>
    </div>
  )
}

function ShrinkFailedActions({ notice }: { notice: SystemNotice }) {
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
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <Button size="sm" variant="outline" className="h-7 px-2 text-xs" disabled={pending} onClick={() => void requestShrink()}>
        {t("systemNotice.actions.tryAgain")}
      </Button>
      <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" disabled={dismiss.isPending} onClick={() => dismiss.mutate()}>
        {t("systemNotice.actions.dismiss")}
      </Button>
      {!knownCause && (
        <Link to="/settings" search={{ tab: "logs" }} className="text-xs underline underline-offset-2">
          {t("systemNotice.actions.openLog")}
        </Link>
      )}
    </div>
  )
}
