/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstances } from "@/hooks/useInstances"
import { useSpreadsheetClassic } from "@/lib/spreadsheet-disguise"
import { cn } from "@/lib/utils"
import { Link, useLocation } from "@tanstack/react-router"

// Disguise vocabulary, not UI copy: deliberately English and not translated,
// like the incognito ISO names. Each tab is a real navigation target. The
// classic variant renders the same strip as a 2003-era menu bar, where the
// entry marked `instance` points at the first active instance (the "sheet").
const RIBBON_ARIA = "Ribbon"
const FILE_TAB = "File"

const MODERN_TABS = [
  { tab: "Home", instance: true },
  { tab: "Insert", to: "/search" },
  { tab: "Data", to: "/rss" },
  { tab: "Formulas", to: "/automations" },
  { tab: "Review", to: "/cross-seed" },
  { tab: "View", to: "/dashboard" },
] as const

const CLASSIC_TABS = [
  { tab: "Edit", to: "/search" },
  { tab: "View", to: "/dashboard" },
  { tab: "Insert", to: "/rss" },
  { tab: "Format", to: "/automations" },
  { tab: "Tools", to: "/cross-seed" },
  { tab: "Data", instance: true },
] as const

export function SpreadsheetRibbonTabs() {
  const location = useLocation()
  const classic = useSpreadsheetClassic()
  const { instances } = useInstances()
  const firstActiveInstanceId = instances?.find((instance) => instance.isActive)?.id
  const instancesActive = location.pathname.startsWith("/instances")
  const tabs = classic ? CLASSIC_TABS : MODERN_TABS

  return (
    <nav className="ss-ribbon-tabs hidden md:flex" aria-label={RIBBON_ARIA}>
      <Link to="/settings" className="ss-ribbon-tab ss-ribbon-tab-file">{FILE_TAB}</Link>
      {tabs.map((entry) => "instance" in entry? (
        firstActiveInstanceId !== undefined && (
          <Link
            key={entry.tab}
            to="/instances/$instanceId"
            params={{ instanceId: String(firstActiveInstanceId) }}
            className={cn("ss-ribbon-tab", instancesActive && "ss-ribbon-tab-active")}
          >
            {entry.tab}
          </Link>
        )
      ): (
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
