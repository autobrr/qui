/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute } from "@tanstack/react-router"
import { Login } from "@/pages/Login"

export const Route = createFileRoute("/login")({
  component: Login,
})