/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { z } from "zod"

// i18n-namespace: crossseed

export const CROSS_SEED_NAV_GROUPS = [
  { labelKey: "nav.sources", tabs: ["rss", "webhook", "completion", "library", "directories"] },
  { labelKey: "nav.matching", tabs: ["season-packs", "matching"] },
  { labelKey: "nav.injection", tabs: ["categories", "after-injection"] },
  { tabs: ["blocklist"] },
] as const

export type CrossSeedTab = (typeof CROSS_SEED_NAV_GROUPS)[number]["tabs"][number]

export const CROSS_SEED_TABS = CROSS_SEED_NAV_GROUPS.flatMap(group => group.tabs) as [CrossSeedTab, ...CrossSeedTab[]]

export const DEFAULT_CROSS_SEED_TAB: CrossSeedTab = "rss"

export const crossSeedSearchSchema = z.object({
  tab: z.enum(CROSS_SEED_TABS).optional().catch(undefined),
})
