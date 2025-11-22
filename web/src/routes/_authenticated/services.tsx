/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Services } from "@/pages/Services"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/services")({
  component: Services,
})
