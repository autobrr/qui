/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { transfersSearchSchema, type TransfersTab } from "@/components/transfers/tabs"
import { TransfersPage } from "@/pages/Transfers"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/transfers")({
  staticData: {
    title: { titleKey: "nav.transfers", titleNs: "common" },
  },
  validateSearch: transfersSearchSchema,
  component: TransfersRoute,
})

function TransfersRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const handleTabChange = (tab: TransfersTab | undefined, { replace }: { replace: boolean }) => {
    navigate({
      search: { tab },
      replace,
    })
  }

  return <TransfersPage activeTab={search.tab} onTabChange={handleTabChange} />
}
