/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FileText, Home } from "lucide-react"

// The getqui.com demo build. vite.config.ts defines the value for every build,
// so production bundles fold each guard away. See src/demo/main.tsx.
export const isDemo = import.meta.env.VITE_DEMO === "1"

// The way back to the site from the standalone demo. Same origin, so plain paths.
export const demoLinks = [
  { href: "/", label: "Back to getqui.com", icon: Home },
  { href: "/docs", label: "Docs", icon: FileText },
]
