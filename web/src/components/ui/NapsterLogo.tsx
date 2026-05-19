/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { withBasePath } from "@/lib/base-url"
import { cn } from "@/lib/utils"

interface NapsterLogoProps {
  className?: string
}

export function NapsterLogo({ className }: NapsterLogoProps) {
  return (
    <img
      src={withBasePath("/napster.png")}
      alt="Napster"
      className={cn("h-6 w-6 flex-shrink-0 object-contain align-baseline", className)}
    />
  )
}
