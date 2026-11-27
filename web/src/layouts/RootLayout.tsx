/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Outlet } from "@tanstack/react-router"
import { useTabTitle } from "@/hooks/useTabTitle"

function TabTitle() {
  useTabTitle()
  return null
}

export function RootLayout() {
  return (
    <>
      <TabTitle />
      <Outlet />
    </>
  )
}
