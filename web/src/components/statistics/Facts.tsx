/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"

export type FactItem = [label: string, value: string, className?: string]

export function Facts({ items, className }: { items: FactItem[]; className?: string }) {
  return (
    <div className={cn("flex flex-wrap gap-x-3 pl-5.5 text-xs font-normal text-muted-foreground", className)} data-narrow>
      {items.map(([label, value, itemClassName]) => (
        <span key={label} className={cn("whitespace-nowrap", itemClassName)}>{label} <span className="tabular-nums text-foreground">{value}</span></span>
      ))}
    </div>
  )
}
