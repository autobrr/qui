/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
  const { t } = useTranslation()
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return <div className="hidden">{t("common.loading")}</div>
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" />
  }

  return <AppLayout />
}