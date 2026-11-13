/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SystemNoticeContent } from "@/components/ui/SystemNoticeContent"
import { useSystemNotices } from "@/hooks/useSystemNotices"
import { systemNoticeStyles } from "@/lib/system-notice"
import { cn } from "@/lib/utils"

export function SystemNoticeList() {
  const notices = useSystemNotices()

  return notices.map((notice) => (
    <div key={notice.kind} role="alert" className={cn("mb-3 rounded-md border p-3", systemNoticeStyles[notice.severity].box)}>
      <SystemNoticeContent notice={notice} />
    </div>
  ))
}
