/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute } from "@tanstack/react-router"
import { RacingDashboard } from "@/pages/RacingDashboard"

export const Route = createFileRoute("/_authenticated/racing")({
  component: RacingDashboard,
})