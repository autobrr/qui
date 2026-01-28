/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TransferTitleManager } from "@/components/TransferTitleManager"
import { NotFound } from "@/pages/NotFound"
import { createRootRoute, Outlet } from "@tanstack/react-router"

export const Route = createRootRoute({
  component: () => (
    <>
      <TransferTitleManager />
      <Outlet />
    </>
  ),
  notFoundComponent: NotFound,
})
