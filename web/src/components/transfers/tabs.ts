/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { z } from "zod"

// i18n-namespace: transfers

export const TRANSFERS_NAV_GROUPS = [
  {
    tabs: [
      { tab: "transfers", labelKey: "tabs.transfers" },
      { tab: "shares", labelKey: "tabs.shares" },
    ],
  },
] as const

export type TransfersTab = (typeof TRANSFERS_NAV_GROUPS)[number]["tabs"][number]["tab"]

export const TRANSFERS_TABS = TRANSFERS_NAV_GROUPS.flatMap(group => group.tabs.map(entry => entry.tab)) as [TransfersTab, ...TransfersTab[]]

export const DEFAULT_TRANSFERS_TAB: TransfersTab = "transfers"

export const transfersSearchSchema = z.object({
  tab: z.enum(TRANSFERS_TABS).optional().catch(undefined),
})
