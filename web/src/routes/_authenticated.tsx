/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useAuth } from "@/hooks/useAuth"
import { AppLayout } from "@/layouts/AppLayout"
import { createFileRoute, Navigate } from "@tanstack/react-router"

export const Route = createFileRoute("/_authenticated")({
  component: AuthLayout,
})

function AuthLayout() {
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return <div className="hidden">Loading...</div>
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" />
  }

  return <AppLayout />
}