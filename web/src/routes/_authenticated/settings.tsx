/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/settings")({
  component: () => import("@/pages/Settings").then(m => ({ default: m.Settings })),
})