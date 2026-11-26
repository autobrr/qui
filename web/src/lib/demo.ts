/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FileText, Home } from "lucide-react"

// i18n-namespace: common

export const isDemo = import.meta.env.VITE_DEMO === "1"

export const demoLinks = [
  { href: "/", labelKey: "nav.backToSite", icon: Home },
  { href: "/docs/intro", labelKey: "nav.docs", icon: FileText },
]
