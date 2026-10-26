/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { z } from "zod"

// i18n-namespace: automations

/** Nav sections in display order. */
export const AUTOMATIONS_NAV_GROUPS = [
  {
    tabs: [
      { tab: "workflows", labelKey: "tabs.workflows" },
      { tab: "reannounce", labelKey: "tabs.reannounce" },
      { tab: "orphan-scan", labelKey: "tabs.orphanScan" },
    ],
  },
] as const

export type AutomationsTab = (typeof AUTOMATIONS_NAV_GROUPS)[number]["tabs"][number]["tab"]

/** Tab values as they appear in the URL search parameter. The cast gives z.enum the tuple it needs. */
export const AUTOMATIONS_TABS = AUTOMATIONS_NAV_GROUPS.flatMap(group => group.tabs.map(entry => entry.tab)) as [AutomationsTab, ...AutomationsTab[]]

export const DEFAULT_AUTOMATIONS_TAB: AutomationsTab = "workflows"

/** An unknown tab value drops to undefined, and the route then shows the default tab. */
export const automationsSearchSchema = z.object({
  tab: z.enum(AUTOMATIONS_TABS).optional().catch(undefined),
})
