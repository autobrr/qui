/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Logo } from "@/components/ui/Logo"
import { Separator } from "@/components/ui/separator"
import { SwizzinLogo } from "@/components/ui/SwizzinLogo"
import { UpdateBanner } from "@/components/ui/UpdateBanner"
import { useAuth } from "@/hooks/useAuth"
import { useTheme } from "@/hooks/useTheme"
import { getAppVersion } from "@/lib/build-info"
import { cn } from "@/lib/utils"
import { Link, useLocation } from "@tanstack/react-router"
import {
  Archive,
  Copyright,
  Github,
  Home,
  LogOut,
  Settings
} from "lucide-react"
import { useTranslation } from "react-i18next"

interface NavItem {
  title: string
  href: string
  icon: React.ComponentType<{ className?: string }>
}

export function Sidebar() {
  const { t } = useTranslation()
  const location = useLocation()
  const { logout } = useAuth()
  const { theme } = useTheme()

  const navigation: NavItem[] = [
    {
      title: t("nav.dashboard"),
      href: "/dashboard",
      icon: Home,
    },
    {
      title: t("nav.backups"),
      href: "/backups",
      icon: Archive,
    },
    {
      title: t("common.titles.settings"),
      href: "/settings",
      icon: Settings,
    },
  ]

  const appVersion = getAppVersion()

  return (
    <div className="flex h-full w-64 flex-col border-r bg-sidebar border-sidebar-border">
      <div className="p-6">
        <h2 className="flex items-center gap-2 text-lg font-semibold text-sidebar-foreground">
          {theme === "swizzin" ? (
            <SwizzinLogo className="h-5 w-5" />
          ) : (
            <Logo className="h-5 w-5" />
          )}
          qui
        </h2>
      </div>

      <nav className="flex flex-1 min-h-0 flex-col px-3">
        <div className="space-y-1">
          {navigation.map((item) => {
            const Icon = item.icon
            const isActive = location.pathname === item.href

            return (
              <Link
                key={item.href}
                to={item.href}
                className={cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-200 ease-out",
                  isActive
                    ? "bg-sidebar-primary text-sidebar-primary-foreground"
                    : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                )}
              >
                <Icon className="h-4 w-4" />
                {item.title}
              </Link>
            )
          })}
        </div>

        <div className="flex-1" />
      </nav>

      <div className="mt-auto space-y-3 p-3">
        <UpdateBanner />

        <Button
          variant="ghost"
          className="w-full justify-start"
          onClick={() => logout()}
        >
          <LogOut className="mr-2 h-4 w-4" />
          {t("nav.logout")}
        </Button>

        <Separator className="mx-3 mb-3" />

        <div className="flex items-center justify-between px-3 pb-3">
          <div className="flex flex-col gap-1 text-[10px] text-sidebar-foreground/40 select-none">
            <span className="font-medium text-sidebar-foreground/50">{t("nav.version", { version: appVersion })}</span>
            <div className="flex items-center gap-1">
              <Copyright className="h-2.5 w-2.5" />
              <span>{new Date().getFullYear()} autobrr</span>
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 text-sidebar-foreground/40 hover:text-sidebar-foreground"
            asChild
          >
            <a
              href="https://github.com/autobrr/qui"
              target="_blank"
              rel="noopener noreferrer"
              aria-label={t("nav.viewOnGitHub")}
            >
              <Github className="h-3.5 w-3.5" />
            </a>
          </Button>
        </div>
      </div>
    </div>
  )
}
