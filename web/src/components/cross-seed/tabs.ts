/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { z } from "zod"

/** Tab values as they appear in the URL search parameter. Order is the tab strip order. */
export const CROSS_SEED_TABS = ["rss", "webhook", "library", "directories", "season-packs", "rules", "blocklist"] as const
export type CrossSeedTab = (typeof CROSS_SEED_TABS)[number]

export const DEFAULT_CROSS_SEED_TAB: CrossSeedTab = "rss"

/** An unknown or retired tab value drops to undefined, and the route then shows the default tab. */
export const crossSeedSearchSchema = z.object({
  tab: z.enum(CROSS_SEED_TABS).optional().catch(undefined),
})
