/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  Archive,
  ArrowRightLeft,
  ChartColumn,
  FileText,
  GitBranch,
  Home,
  type LucideIcon,
  Rss,
  Search,
  Settings,
  Zap
} from "lucide-react"

// i18n-namespace: common

export interface NavItem {
  id: string
  titleKey: string
  href: string
  icon: LucideIcon
  params?: Record<string, string>
  search?: Record<string, unknown>
  isActive?: (pathname: string, search: Record<string, unknown> | undefined) => boolean
}

/** The pages of the app, in the order of the sidebar. */
export const NAV_ITEMS: NavItem[] = [
  {
    id: "dashboard",
    titleKey: "nav.dashboard",
    href: "/dashboard",
    icon: Home,
  },
  {
    id: "statistics",
    titleKey: "nav.statistics",
    href: "/statistics",
    icon: ChartColumn,
  },
  {
    id: "search",
    titleKey: "nav.search",
    href: "/search",
    icon: Search,
  },
  {
    id: "cross-seed",
    titleKey: "nav.crossSeed",
    href: "/cross-seed",
    icon: GitBranch,
    params: {},
  },
  {
    id: "automations",
    titleKey: "nav.automations",
    href: "/automations",
    icon: Zap,
  },
  {
    id: "transfers",
    titleKey: "nav.transfers",
    href: "/transfers",
    icon: ArrowRightLeft,
  },
  {
    id: "backups",
    titleKey: "nav.backups",
    href: "/backups",
    icon: Archive,
  },
  {
    id: "rss",
    titleKey: "nav.rss",
    href: "/rss",
    icon: Rss,
  },
  {
    id: "settings",
    titleKey: "nav.settings",
    href: "/settings",
    icon: Settings,
    isActive: (pathname, search) => pathname === "/settings" && search?.tab !== "logs",
  },
  {
    id: "logs",
    titleKey: "nav.logs",
    href: "/settings",
    icon: FileText,
    search: { tab: "logs" },
    isActive: (pathname, search) => pathname === "/settings" && search?.tab === "logs",
  },
]
