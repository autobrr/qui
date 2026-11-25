/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ReactNode } from "react"

import { CopyButton } from "@/components/ui/copy-button"
import { cn } from "@/lib/utils"

export interface Fact {
  label: string
  value: ReactNode
  hint?: ReactNode
  mono?: boolean
  copy?: string
  action?: ReactNode
}

export function Facts({ items }: { items: readonly Fact[] }) {
  return (
    <dl className="divide-y">
      {items.map((fact) => (
        <div key={fact.label} className="group grid gap-1 py-3 first:pt-0 last:pb-0 sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-4">
          <dt className="text-sm text-muted-foreground">{fact.label}</dt>
          <dd className="flex min-w-0 items-start gap-2">
            <div className="min-w-0 flex-1">
              <div className={cn("[overflow-wrap:anywhere]", fact.mono ? "font-mono text-13" : "text-sm font-medium")}>{fact.value}</div>
              {fact.hint && <div className="mt-0.5 text-xs text-muted-foreground">{fact.hint}</div>}
            </div>
            {fact.action}
            {fact.copy && (
              <CopyButton
                value={fact.copy}
                label={fact.label}
                className="relative -my-1 size-7 shrink-0 pointer-coarse:after:absolute pointer-coarse:after:-inset-2 pointer-fine:opacity-0 pointer-fine:transition-opacity pointer-fine:group-hover:opacity-100 pointer-fine:focus-visible:opacity-100"
              />
            )}
          </dd>
        </div>
      ))}
    </dl>
  )
}
