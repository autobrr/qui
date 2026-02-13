/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Settings } from "@/pages/Settings"
import { createFileRoute } from "@tanstack/react-router"
import { z } from "zod"

const settingsSearchSchema = z.object({
  tab: z.enum([
    "instances",
    "indexers",
    "search-cache",
    "integrations",
    "client-api",
    "api",
    "external-programs",
    "datetime",
    "themes",
    "security",
    "logs",
  ]).optional().catch(undefined),
  modal: z.enum(["add-instance"]).optional().catch(undefined),
  checkout: z.enum(["success"]).optional().catch(undefined),
  status: z.string().optional().catch(undefined),
  payment_id: z.string().optional().catch(undefined),
})

export type SettingsSearch = z.infer<typeof settingsSearchSchema>

export const Route = createFileRoute("/_authenticated/settings")({
  validateSearch: settingsSearchSchema,
  component: SettingsRoute,
})

function SettingsRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const handleSearchChange = (newSearch: SettingsSearch) => {
    navigate({
      search: newSearch,
      replace: true,
    })
  }

  return <Settings search={search} onSearchChange={handleSearchChange} />
}
