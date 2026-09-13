/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useAuth } from "@/hooks/useAuth"
import { AppLayout } from "@/layouts/AppLayout"
import { createFileRoute, Navigate, useRouter } from "@tanstack/react-router"
import { useEffect } from "react"
import { useTranslation } from "react-i18next"

export const Route = createFileRoute("/_authenticated")({
  component: AuthLayout,
})

function AuthLayout() {
  const { t } = useTranslation("common")
  const { isAuthenticated, isLoading } = useAuth()
  const router = useRouter()

  // Warm the lazy route chunks after the landing surface has painted, so a
  // later navigation swaps pages synchronously instead of showing a blank route.
  useEffect(() => {
    if (!isAuthenticated) return
    const id = window.setTimeout(() => {
      for (const to of ["/settings", "/automations", "/cross-seed", "/rss", "/backups", "/search"] as const) {
        void router.preloadRoute({ to }).catch(() => {})
      }
    }, 1500)
    return () => window.clearTimeout(id)
  }, [isAuthenticated, router])

  if (isLoading) {
    return <div className="hidden">{t("mobileNav.loading")}</div>
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" />
  }

  return <AppLayout />
}
