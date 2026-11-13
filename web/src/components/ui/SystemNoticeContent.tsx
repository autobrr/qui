/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { systemNoticeStyles } from "@/lib/system-notice"
import { cn } from "@/lib/utils"
import type { SystemNotice } from "@/types"
import { OctagonAlert } from "lucide-react"
import { useTranslation } from "react-i18next"

export function SystemNoticeContent({ notice }: { notice: SystemNotice }) {
  const { t } = useTranslation("common")
  const styles = systemNoticeStyles[notice.severity]

  return (
    <div className="flex items-start gap-2 min-w-0">
      <OctagonAlert className={cn("h-4 w-4 mt-0.5 flex-shrink-0", styles.icon)} />
      <div className="flex-1 min-w-0">
        <p className={cn("text-sm font-medium", styles.strong)}>
          {t([`systemNotice.kinds.${notice.kind}.title`, "systemNotice.unknown.title"])}
        </p>
        <p className={cn("text-xs mt-1", styles.body)}>
          {t([`systemNotice.kinds.${notice.kind}.fix`, "systemNotice.unknown.fix"])}
        </p>
        <p className={cn("text-xs mt-1 break-words opacity-80", styles.body)}>
          {t("systemNotice.cause", { cause: notice.cause })}
        </p>
      </div>
    </div>
  )
}
