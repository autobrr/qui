/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { CrossSeedPage } from "@/pages/CrossSeedPage"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/cross-seed")({
  component: CrossSeedRoute,
})

function CrossSeedRoute() {
  return <CrossSeedPage />
}
