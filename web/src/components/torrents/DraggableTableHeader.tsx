/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ViewMode } from "@/hooks/usePersistedCompactViewState"
import { cn } from "@/lib/utils"
import type { Torrent } from "@/types"
import { useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { flexRender, type Header } from "@tanstack/react-table"
import { ChevronDown, ChevronUp } from "lucide-react"

// Pre-computed header classNames for performance (avoid cn() calls in render loop)
const HEADER_CLASSNAMES = {
  select: {
    normal: "font-medium text-muted-foreground flex items-center justify-center h-10",
    compact: "font-medium text-muted-foreground flex items-center justify-center h-8",
    "ultra-compact": "font-medium text-muted-foreground flex items-center justify-center h-7",
  },
  default: {
    normal: "font-medium text-muted-foreground flex items-center text-left px-3 h-10",
    compact: "font-medium text-muted-foreground flex items-center text-left px-2 h-8",
    "ultra-compact": "font-medium text-muted-foreground flex items-center text-left px-1 h-7",
  },
} as const

interface DraggableTableHeaderProps {
  header: Header<Torrent, unknown>
  viewMode?: ViewMode
}

export function DraggableTableHeader({ header, viewMode = "normal" }: DraggableTableHeaderProps) {
  const { column } = header

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: column.id,
    disabled: column.id === "select",
  })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.8 : 1,
    position: "relative" as const,
    width: header.getSize(),
    flexShrink: 0,
  }

  return (
    <div
      ref={setNodeRef}
      style={style}
      className="group overflow-hidden"
    >
      <div
        className={cn(
          HEADER_CLASSNAMES[column.id === "select" ? "select" : "default"][viewMode],
          column.getCanSort() && "cursor-pointer select-none",
          column.id !== "select" && "cursor-grab active:cursor-grabbing"
        )}
        onClick={column.id !== "select" && column.getCanSort() ? column.getToggleSortingHandler() : undefined}
        {...(column.id !== "select" ? attributes : {})}
        {...(column.id !== "select" ? listeners : {})}
      >
        {/* Header content */}
        <div
          className="flex items-center gap-1 flex-1 min-w-0"
        >
          <span className={`overflow-hidden whitespace-nowrap ${column.id === "select" ? "flex items-center" : ""}`}>
            {header.isPlaceholder? null: flexRender(
              column.columnDef.header,
              header.getContext()
            )}
          </span>
          {column.id !== "select" && column.getIsSorted() && (
            column.getIsSorted() === "asc" ? (
              <ChevronUp className="h-4 w-4 flex-shrink-0" />
            ) : (
              <ChevronDown className="h-4 w-4 flex-shrink-0" />
            )
          )}
        </div>
      </div>

      {/* Resize handle */}
      {column.getCanResize() && (
        <div
          onMouseDown={header.getResizeHandler()}
          onTouchStart={header.getResizeHandler()}
          className="absolute right-0 top-0 h-full w-2 cursor-col-resize select-none touch-none group/resize flex justify-center"
        >
          <div
            className={`h-full w-px ${
              column.getIsResizing()? "bg-primary": "bg-border group-hover/resize:bg-primary/50"
            }`}
          />
        </div>
      )}
    </div>
  )
}