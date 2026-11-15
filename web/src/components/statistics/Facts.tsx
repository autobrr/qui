/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"

/** A label and its value. The class hides the pair where its own table column shows. */
export type FactItem = [label: string, value: string, className?: string]

/**
 * The numbers of a table row that a narrow table has no column for, as label and value pairs below the name. Tests
 * find it by data-narrow, to read a row as a wide table shows it.
 */
export function Facts({ items, className }: { items: FactItem[]; className?: string }) {
  return (
    <div className={cn("flex flex-wrap gap-x-3 pl-5.5 text-xs font-normal text-muted-foreground", className)} data-narrow>
      {items.map(([label, value, itemClassName]) => (
        <span key={label} className={cn("whitespace-nowrap", itemClassName)}>{label} <span className="tabular-nums text-foreground">{value}</span></span>
      ))}
    </div>
  )
}
