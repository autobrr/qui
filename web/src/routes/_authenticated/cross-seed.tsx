/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { crossSeedSearchSchema, type CrossSeedTab } from "@/components/cross-seed/tabs"
import { CrossSeedPage } from "@/pages/CrossSeedPage"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/cross-seed")({
  staticData: {
    title: { titleKey: "nav.crossSeed", titleNs: "common" },
  },
  validateSearch: crossSeedSearchSchema,
  component: CrossSeedRoute,
})

function CrossSeedRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const handleTabChange = (tab: CrossSeedTab | undefined, { replace }: { replace: boolean }) => {
    navigate({
      search: { tab },
      replace,
    })
  }

  return (
    <CrossSeedPage
      activeTab={search.tab}
      onTabChange={handleTabChange}
    />
  )
}
