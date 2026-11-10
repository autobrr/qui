/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: throwaway route for the Statistics page variants (issue #3991).

import { createFileRoute } from "@tanstack/react-router"
import { PrototypeStatistics } from "@/pages/prototype-statistics"
import type { Palette } from "@/pages/prototype-statistics/charts"

interface PrototypeSearch {
  variant: string
  palette: Palette
}

export const Route = createFileRoute("/_authenticated/prototype/statistics")({
  validateSearch: (search: Record<string, unknown>): PrototypeSearch => ({
    variant: typeof search.variant === "string" ? search.variant : "A",
    palette: search.palette === "theme" ? "theme" : "categorical",
  }),
  component: PrototypeStatisticsRoute,
})

function PrototypeStatisticsRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  return (
    <PrototypeStatistics
      variant={search.variant}
      palette={search.palette}
      onChange={next => navigate({ search: prev => ({ ...prev, ...next }), replace: true })}
    />
  )
}
