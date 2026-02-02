/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute, Navigate } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/instances/")({
  component: InstancesRedirect,
})

function InstancesRedirect() {
  const search = Route.useSearch() as Record<string, unknown>
  const nextSearch = {
    ...search,
    tab: "instances" as const,
  }

  return (
    <Navigate
      to="/settings"
      search={nextSearch}
      replace
    />
  )
}
