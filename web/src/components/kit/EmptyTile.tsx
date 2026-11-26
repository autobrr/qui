/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Plus } from "lucide-react"
import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export interface EmptyTileProps {
  title: string
  description?: ReactNode
  onAdd?: () => void
}

export function EmptyTile({ title, description, onAdd }: EmptyTileProps) {
  const content = (
    <>
      {onAdd && (
        <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-md border border-dashed">
          <Plus className="size-4" />
        </span>
      )}
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium text-foreground">{title}</span>
        {description && <span className="block text-xs text-muted-foreground">{description}</span>}
      </span>
    </>
  )
  const tileClass = "flex min-h-14 w-full items-center gap-3 rounded-lg border border-dashed px-3 py-2.5 text-left text-muted-foreground"

  return onAdd ? (
    <button
      type="button"
      onClick={onAdd}
      className={cn(tileClass, "outline-none transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50")}
    >
      {content}
    </button>
  ) : (
    <div className={tileClass}>{content}</div>
  )
}
