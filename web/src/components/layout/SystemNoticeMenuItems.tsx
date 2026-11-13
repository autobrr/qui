/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { SystemNoticeContent } from "@/components/ui/SystemNoticeContent"
import type { SystemNotice } from "@/types"

export function SystemNoticeMenuItems({ notices }: { notices: SystemNotice[] }) {
  if (notices.length === 0) {
    return null
  }
  return (
    <>
      {notices.map((notice) => (
        <div key={notice.kind} role="alert" className="px-2 py-1.5">
          <SystemNoticeContent notice={notice} />
        </div>
      ))}
      <DropdownMenuSeparator />
    </>
  )
}
