/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createFileRoute, Navigate } from "@tanstack/react-router"
import { useAuth } from "@/hooks/useAuth"

export const Route = createFileRoute("/")({
  component: IndexComponent,
})

function IndexComponent() {
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return <div>Loading...</div>
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" />
  }

  return <Navigate to="/dashboard" />
}