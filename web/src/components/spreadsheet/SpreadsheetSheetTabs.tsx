/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstances } from "@/hooks/useInstances"
import { cn } from "@/lib/utils"
import { Link } from "@tanstack/react-router"

// Disguise vocabulary, not UI copy: deliberately English and not translated.
const ADD_SHEET_ARIA = "Add workbook"
const ADD_SHEET_MARK = "+"

// Sheet tabs = instances. The "+" adds a workbook, i.e. links to instance
// management in settings.
export function SpreadsheetSheetTabs({ currentInstanceId }: { currentInstanceId: number }) {
  const { instances } = useInstances()
  const activeInstances = instances?.filter((instance) => instance.isActive) ?? []

  if (activeInstances.length === 0) {
    return null
  }

  return (
    <div className="ss-sheet-tabs hidden md:flex">
      {activeInstances.map((instance) => (
        <Link
          key={instance.id}
          to="/instances/$instanceId"
          params={{ instanceId: String(instance.id) }}
          className={cn("ss-sheet-tab", instance.id === currentInstanceId && "ss-sheet-tab-active")}
          title={instance.name}
        >
          {instance.name}
        </Link>
      ))}
      <Link
        to="/settings"
        search={{ tab: "instances" }}
        className="ss-sheet-tab-add"
        aria-label={ADD_SHEET_ARIA}
      >
        {ADD_SHEET_MARK}
      </Link>
    </div>
  )
}
