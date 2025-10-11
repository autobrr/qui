/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getColumnType } from "@/lib/column-filter-utils"
import type { Torrent } from "@/types"
import { useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { flexRender, type Header } from "@tanstack/react-table"
import { ChevronDown, ChevronUp } from "lucide-react"
import { type ColumnFilter, ColumnFilterPopover } from "./ColumnFilterPopover"

interface DraggableTableHeaderProps {
  header: Header<Torrent, unknown>
  columnFilters?: ColumnFilter[]
  onFilterChange?: (columnId: string, filter: ColumnFilter | null) => void
}

export function DraggableTableHeader({ header, columnFilters = [], onFilterChange }: DraggableTableHeaderProps) {
  const { column } = header

  const isCompactHeader = column.id === "priority" || column.id === "tracker_icon" || column.id === "status_icon"
  const headerPadding = isCompactHeader ? "px-0" : "px-3"

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

  const canResize = column.getCanResize()
  const shouldShowSeparator = canResize || column.columnDef.enableResizing === false

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
        className={`${headerPadding} h-10 text-left text-sm font-medium text-muted-foreground flex items-center ${
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
          className={`flex items-center ${isCompactHeader ? "gap-0" : "gap-1"} flex-1 min-w-0 ${
            column.id === "select" || isCompactHeader ? "justify-center" : ""
          }`}
        >
          <span
            className={`whitespace-nowrap flex items-center ${
              isCompactHeader? "w-full justify-center": "overflow-hidden flex-1 min-w-0"
            } ${column.id === "select" ? "justify-center" : ""}`}
          >
            {header.isPlaceholder ? null : flexRender(
              column.columnDef.header,
              header.getContext()
            )}
          </span>
          {column.id !== "select" && !isCompactHeader && column.getIsSorted() && (
            column.getIsSorted() === "asc" ? (
              <ChevronUp className="h-4 w-4 flex-shrink-0"/>
            ) : (
              <ChevronDown className="h-4 w-4 flex-shrink-0"/>
            )
          )}
          {/* Column filter button - only show for filterable columns */}
          {column.id !== "select" && column.id !== "priority" && column.id !== "tracker_icon" && column.id !== "status_icon" && onFilterChange && (
            <ColumnFilterPopover
              columnId={column.id}
              columnName={(column.columnDef.meta as { headerString?: string })?.headerString ||
                (typeof column.columnDef.header === "string" ? column.columnDef.header : column.id)}
              columnType={getColumnType(column.id)}
              currentFilter={columnFilters.find(f => f.columnId === column.id)}
              onApply={(filter) => onFilterChange(column.id, filter)}
            />
          )}
        </div>
      </div>

      {/* Resize handle */}
      {shouldShowSeparator && (
        <div
          onMouseDown={canResize ? header.getResizeHandler() : undefined}
          onTouchStart={canResize ? header.getResizeHandler() : undefined}
          className={`absolute right-0 top-0 h-full w-2 select-none group/resize flex justify-end ${
            canResize ? "cursor-col-resize touch-none" : "pointer-events-none"
          }`}
        >
          <div
            className={`h-full w-px ${
              canResize && column.getIsResizing()? "bg-primary": canResize? "bg-border group-hover/resize:bg-primary/50": "bg-border"
            }`}
          />
        </div>
      )}
    </div>
  )
}
