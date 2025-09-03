/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import swizzinLogo from "@/assets/swizzin.png"
import { cn } from "@/lib/utils"

interface SwizzinLogoProps {
  className?: string
}

export function SwizzinLogo({ className }: SwizzinLogoProps) {
  return (
    <img
      src={swizzinLogo}
      alt="Swizzin"
      className={cn("h-6 w-6 flex-shrink-0 object-contain align-baseline", className)}
    />
  )
}