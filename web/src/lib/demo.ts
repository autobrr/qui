/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FileText, Home } from "lucide-react"

// The getqui.com demo build. vite.config.ts defines the value for every build,
// so production bundles fold each guard away. See src/demo/main.tsx.
export const isDemo = import.meta.env.VITE_DEMO === "1"

// The way back to the site. Same origin, so plain paths; target=_top leaves the landing page iframe.
// labelKey is a key in the common namespace.
export const demoLinks = [
  { href: "/", labelKey: "nav.backToSite", icon: Home },
  { href: "/docs/intro", labelKey: "nav.docs", icon: FileText },
]
