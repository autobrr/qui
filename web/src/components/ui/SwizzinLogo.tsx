/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import { withBasePath } from "@/lib/base-url"

interface SwizzinLogoProps {
  className?: string
}

export function SwizzinLogo({ className }: SwizzinLogoProps) {
  return (
    <img
      src={withBasePath("/swizzin.png")}
      alt="Swizzin"
      className={cn("h-6 w-6 flex-shrink-0 object-contain align-baseline", className)}
    />
  )
}