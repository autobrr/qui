/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { CROSS_SEED_TABS, type CrossSeedTab } from "@/components/cross-seed/tabs"
import { CrossSeedPage } from "@/pages/CrossSeedPage"
import { createFileRoute } from "@tanstack/react-router"
import { z } from "zod"

const crossSeedSearchSchema = z.object({
  tab: z.enum(CROSS_SEED_TABS).optional().catch(undefined),
})

export const Route = createFileRoute("/_authenticated/cross-seed")({
  validateSearch: crossSeedSearchSchema,
  component: CrossSeedRoute,
})

function CrossSeedRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const handleTabChange = (tab: CrossSeedTab) => {
    navigate({
      search: { tab },
      replace: true,
    })
  }

  return (
    <CrossSeedPage
      activeTab={search.tab ?? "rss"}
      onTabChange={handleTabChange}
    />
  )
}
