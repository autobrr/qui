/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useAuth } from "@/hooks/useAuth"
import { AppLayout } from "@/layouts/AppLayout"
import { isDemo } from "@/lib/demo"
import { createFileRoute, Navigate, redirect } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ location }) => {
    // The demo has no backend behind any page but the torrent list.
    if (isDemo && !location.pathname.includes("/instances")) {
      throw redirect({ to: "/instances/$instanceId", params: { instanceId: "1" } })
    }
  },
  component: AuthLayout,
})

function AuthLayout() {
  const { t } = useTranslation("common")
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return <div className="hidden">{t("mobileNav.loading")}</div>
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" />
  }

  return <AppLayout />
}
