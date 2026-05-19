/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceBackups } from "@/pages/InstanceBackups"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated/backups")({
  component: BackupsRoute,
})

function BackupsRoute() {
  return <InstanceBackups />
}
