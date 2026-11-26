/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SETTINGS_TABS } from "@/components/settings/tabs"
import { Settings } from "@/pages/Settings"
import { createFileRoute } from "@tanstack/react-router"
import { z } from "zod"

const settingsSearchSchema = z.object({
  tab: z.enum(SETTINGS_TABS).optional().catch(undefined),
  modal: z.enum(["add-instance", "install-update", "theme-menu"]).optional().catch(undefined),
  checkout: z.enum(["success"]).optional().catch(undefined),
  status: z.string().optional().catch(undefined),
  payment_id: z.string().optional().catch(undefined),
})

export type SettingsSearch = z.infer<typeof settingsSearchSchema>

export const Route = createFileRoute("/_authenticated/settings")({
  staticData: {
    title: { titleKey: "nav.settings", titleNs: "common" },
  },
  validateSearch: settingsSearchSchema,
  component: SettingsRoute,
})

function SettingsRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const handleSearchChange = (newSearch: SettingsSearch, options?: { replace: boolean }) => {
    navigate({
      search: newSearch,
      replace: options?.replace ?? true,
    })
  }

  return <Settings search={search} onSearchChange={handleSearchChange} />
}
