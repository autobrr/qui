/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute } from "@tanstack/react-router"
import { Dashboard } from "@/pages/Dashboard"

export const Route = createFileRoute("/_authenticated/dashboard")({
  component: Dashboard,
  // PROTOTYPE: dashboard variants, see src/pages/dashboard-prototype.
  validateSearch: (search: Record<string, unknown>): { variant?: string; data?: string } => ({
    variant: typeof search.variant === "string" ? search.variant : undefined,
    data: typeof search.data === "string" ? search.data : undefined,
  }),
  staticData: {
    titleKey: "nav.dashboard",
    titleNs: "common",
  },
})
