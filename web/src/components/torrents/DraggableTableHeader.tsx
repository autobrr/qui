/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getColumnType, type ColumnFilter } from "@/lib/column-filter-utils"
import type { Torrent } from "@/types"
import { useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { flexRender, type Header } from "@tanstack/react-table"
import { ChevronDown, ChevronUp } from "lucide-react"
import { ColumnFilterPopover } from "./ColumnFilterPopover"
import type { ViewMode } from "@/hooks/usePersistedCompactViewState"

interface DraggableTableHeaderProps {
  header: Header<Torrent, unknown>
  columnFilters?: ColumnFilter[]
  viewMode?: ViewMode
  onFilterChange?: (columnId: string, filter: ColumnFilter | null) => void
}

export function DraggableTableHeader({ header, columnFilters = [], viewMode = "normal", onFilterChange }: DraggableTableHeaderProps) {
  const { column } = header

  const isSelectHeader = column.id === "select"
  const isPriorityHeader = column.id === "priority"
  const isTrackerIconHeader = column.id === "tracker_icon"
  const isStatusIconHeader = column.id === "status_icon"
  const isCompactHeader = isTrackerIconHeader || isStatusIconHeader
  // Match cell padding: compact columns use px-0, others use px-2 (dense) or px-3 (normal)
  const headerPadding = isCompactHeader ? "px-0" : (viewMode === "dense" ? "px-2" : "px-3")

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
  const table = header.getContext().table
  const trackerColumn = isTrackerIconHeader ? table.getColumn("tracker") : null

  const canResize = column.getCanResize()
  const shouldShowSeparator = canResize || column.columnDef.enableResizing === false
  const shouldShowSortIndicator = !isSelectHeader && column.getIsSorted() && (isPriorityHeader || !isCompactHeader)
  const canSort = column.getCanSort() || (!!trackerColumn && trackerColumn.getCanSort())
  const toggleSortingHandler = column.getToggleSortingHandler()
  const trackerToggleHandler = trackerColumn?.getToggleSortingHandler()

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
        className={`${headerPadding} ${viewMode === "dense" ? "h-7 text-xs" : "h-10 text-sm"} text-left font-medium text-muted-foreground flex items-center ${canSort ? "cursor-pointer select-none" : ""
          } ${column.id !== "select" ? "cursor-grab active:cursor-grabbing" : ""
          }`}
        onClick={event => {
          if (column.id === "select" || !canSort) {
            return
          }

          if (isTrackerIconHeader && trackerToggleHandler) {
            trackerToggleHandler(event)
            return
          }

          if (toggleSortingHandler) {
            toggleSortingHandler(event)
          }
        }}
        {...(column.id !== "select" ? attributes : {})}
        {...(column.id !== "select" ? listeners : {})}
      >
        {/* Header content */}
        <div
          className={`flex items-center ${isCompactHeader ? "gap-0" : "gap-1"} flex-1 min-w-0 ${isSelectHeader || isCompactHeader ? "justify-center" : ""
            }`}
        >
          <span
            className={`whitespace-nowrap ${!isPriorityHeader && !isCompactHeader ? "overflow-hidden flex-1 min-w-0" : ""
              } ${isCompactHeader ? "flex items-center w-full justify-center" : ""
              } ${isSelectHeader ? "flex items-center justify-center" : ""}`}
          >
            {header.isPlaceholder ? null : flexRender(
              column.columnDef.header,
              header.getContext()
            )}
          </span>
          {shouldShowSortIndicator && (
            column.getIsSorted() === "asc" ? (
              <ChevronUp className={`h-4 w-4 flex-shrink-0${isPriorityHeader ? " ml-1 mr-1" : ""}`} />
            ) : (
              <ChevronDown className={`h-4 w-4 flex-shrink-0${isPriorityHeader ? " ml-1 mr-1" : ""}`} />
            )
          )}
          {/* Column filter button - only show for filterable columns */}
          {!isSelectHeader && !isPriorityHeader && !isTrackerIconHeader && !isStatusIconHeader && onFilterChange && (
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
          className={`absolute right-0 top-0 h-full w-2 select-none group/resize flex justify-end ${canResize ? "cursor-col-resize touch-none" : "pointer-events-none"
            }`}
        >
          <div
            className={`h-full w-px ${canResize && column.getIsResizing() ? "bg-primary" : canResize ? "bg-border group-hover/resize:bg-primary/50" : "bg-border"
              }`}
          />
        </div>
      )}
    </div>
  )
}
