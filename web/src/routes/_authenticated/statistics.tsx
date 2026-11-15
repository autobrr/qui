/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { PERIOD_PRESETS, type PeriodPreset } from "@/lib/statistics"
import { createFileRoute, lazyRouteComponent } from "@tanstack/react-router"
import { z } from "zod"

const statisticsSearchSchema = z.object({
  period: z.enum(PERIOD_PRESETS as [PeriodPreset, ...PeriodPreset[]]).optional().catch(undefined),
})

export const Route = createFileRoute("/_authenticated/statistics")({
  validateSearch: statisticsSearchSchema,
  component: lazyRouteComponent(() => import("@/pages/Statistics"), "StatisticsRoute"),
  staticData: {
    titleKey: "nav.statistics",
    titleNs: "common",
  },
})
