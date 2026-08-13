/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstances } from "@/hooks/useInstances"
import { cn } from "@/lib/utils"
import { Link, useLocation } from "@tanstack/react-router"

// Disguise vocabulary, not UI copy: deliberately English and not translated,
// like the incognito ISO names. Each tab is a real navigation target.
const RIBBON_ARIA = "Ribbon"
const FILE_TAB = "File"
const HOME_TAB = "Home"

const RIBBON_TABS = [
  { tab: "Insert", to: "/search" },
  { tab: "Data", to: "/rss" },
  { tab: "Formulas", to: "/automations" },
  { tab: "Review", to: "/cross-seed" },
  { tab: "View", to: "/dashboard" },
] as const

export function SpreadsheetRibbonTabs() {
  const location = useLocation()
  const { instances } = useInstances()
  const firstActiveInstanceId = instances?.find((instance) => instance.isActive)?.id
  const homeActive = location.pathname.startsWith("/instances")

  return (
    <nav className="ss-ribbon-tabs hidden md:flex" aria-label={RIBBON_ARIA}>
      <Link to="/settings" className="ss-ribbon-tab ss-ribbon-tab-file">{FILE_TAB}</Link>
      {firstActiveInstanceId !== undefined && (
        <Link
          to="/instances/$instanceId"
          params={{ instanceId: String(firstActiveInstanceId) }}
          className={cn("ss-ribbon-tab", homeActive && "ss-ribbon-tab-active")}
        >
          {HOME_TAB}
        </Link>
      )}
      {RIBBON_TABS.map((entry) => (
        <Link
          key={entry.tab}
          to={entry.to}
          className={cn("ss-ribbon-tab", location.pathname.startsWith(entry.to) && "ss-ribbon-tab-active")}
        >
          {entry.tab}
        </Link>
      ))}
    </nav>
  )
}
