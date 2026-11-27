/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { automationsSearchSchema, type AutomationsTab } from "@/components/automations/tabs"
import { Automations } from "@/pages/Automations"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/automations")({
  staticData: {
    title: { titleKey: "nav.automations", titleNs: "common" },
  },
  validateSearch: automationsSearchSchema,
  component: AutomationsRoute,
})

function AutomationsRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const handleTabChange = (tab: AutomationsTab | undefined, { replace }: { replace: boolean }) => {
    navigate({
      search: { tab },
      replace,
    })
  }

  return (
    <Automations
      activeTab={search.tab}
      onTabChange={handleTabChange}
    />
  )
}
