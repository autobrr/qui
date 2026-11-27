/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TruncatedText } from "@/components/ui/truncated-text"
import { cn } from "@/lib/utils"
import { useState, type ReactNode } from "react"

export function CellText({ children, className }: { children: ReactNode; className?: string }) {
  const [hovered, setHovered] = useState(false)
  if (hovered) return <TruncatedText className={className}>{children}</TruncatedText>
  return <span className={cn("truncate", className)} onPointerEnter={() => setHovered(true)}>{children}</span>
}
