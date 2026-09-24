/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { z } from "zod"

/** Nav sections in display order. A group without a label renders its tabs without a heading. */
export const CROSS_SEED_NAV_GROUPS = [
  { labelKey: "nav.sources", tabs: ["rss", "webhook", "completion", "library", "directories"] },
  { labelKey: "nav.matching", tabs: ["season-packs", "matching"] },
  { labelKey: "nav.injection", tabs: ["categories", "after-injection"] },
  { tabs: ["blocklist"] },
] as const

export type CrossSeedTab = (typeof CROSS_SEED_NAV_GROUPS)[number]["tabs"][number]

/** Tab values as they appear in the URL search parameter. The cast gives z.enum the tuple it needs. */
export const CROSS_SEED_TABS = CROSS_SEED_NAV_GROUPS.flatMap(group => group.tabs) as [CrossSeedTab, ...CrossSeedTab[]]

export const DEFAULT_CROSS_SEED_TAB: CrossSeedTab = "rss"

/** An unknown or retired tab value drops to undefined, and the route then shows the default tab. */
export const crossSeedSearchSchema = z.object({
  tab: z.enum(CROSS_SEED_TABS).optional().catch(undefined),
})
