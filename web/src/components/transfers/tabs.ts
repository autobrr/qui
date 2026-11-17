/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { z } from "zod"

// i18n-namespace: transfers

/** Nav sections in display order. */
export const TRANSFERS_NAV_GROUPS = [
  {
    tabs: [
      { tab: "transfers", labelKey: "tabs.transfers" },
      { tab: "shares", labelKey: "tabs.shares" },
    ],
  },
] as const

export type TransfersTab = (typeof TRANSFERS_NAV_GROUPS)[number]["tabs"][number]["tab"]

/** Tab values as they appear in the URL search parameter. The cast gives z.enum the tuple it needs. */
export const TRANSFERS_TABS = TRANSFERS_NAV_GROUPS.flatMap(group => group.tabs.map(entry => entry.tab)) as [TransfersTab, ...TransfersTab[]]

export const DEFAULT_TRANSFERS_TAB: TransfersTab = "transfers"

/** An unknown tab value drops to undefined, as with no tab: a wide screen shows the default tab, and a phone shows the menu. */
export const transfersSearchSchema = z.object({
  tab: z.enum(TRANSFERS_TABS).optional().catch(undefined),
})
