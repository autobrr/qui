/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { flexRender, type Header } from "@tanstack/react-table"
import { ChevronUp, ChevronDown } from "lucide-react"
import type { Torrent } from "@/types"

interface DraggableTableHeaderProps {
  header: Header<Torrent, unknown>
}

export function DraggableTableHeader({ header }: DraggableTableHeaderProps) {
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
      className="group"
    >
      <div
        className={`px-3 h-10 text-left text-sm font-medium text-muted-foreground flex items-center ${
          column.getCanSort() ? "cursor-pointer select-none" : ""
        } ${
          column.id !== "select" ? "cursor-grab active:cursor-grabbing" : ""
        }`}
        onClick={column.id !== "select" && column.getCanSort() ? column.getToggleSortingHandler() : undefined}
        {...(column.id !== "select" ? attributes : {})}
        {...(column.id !== "select" ? listeners : {})}
      >
        {/* Header content */}
        <div
          className={`flex items-center gap-1 flex-1 ${column.id === "select" ? "justify-center" : ""}`}
        >
          <span className={`truncate ${column.id === "select" ? "flex items-center" : ""}`}>
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