/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useAuth } from "@/hooks/useAuth"
import { AppLayout } from "@/layouts/AppLayout"
import { createFileRoute, Navigate } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"

export const Route = createFileRoute("/_authenticated")({
  component: AuthLayout,
})

function AuthLayout() {
  const { isAuthenticated, isLoading } = useAuth()
  const { t } = useTranslation()

  if (isLoading) {
    return <div className="hidden">{t("loading")}</div>
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" />
  }

  return <AppLayout />
}
