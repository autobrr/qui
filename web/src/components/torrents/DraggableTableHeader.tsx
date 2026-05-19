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
import { useMediaQuery } from "@/hooks/useMediaQuery"
import type { ViewMode } from "@/hooks/usePersistedCompactViewState"

{/* Auto-fit width for column headers on double-click*/}
const TORRENT_ROW_CELL_MEASURE = "data-torrent-column-measure"

function escapeColumnIdForSelector(columnId: string): string {
  const cssEscape = (globalThis as unknown as { CSS?: { escape?: (id: string) => string } }).CSS?.escape
  return typeof cssEscape === "function" ? cssEscape(columnId) : columnId.replace(/\\/g, "\\\\").replace(/"/g, "\\\"")
}

function measureNaturalOuterWidth(source: HTMLElement): number {
  const body = source.ownerDocument.body
  if (!body) {
    return source.scrollWidth
  }

  const clone = source.cloneNode(true) as HTMLElement
  clone.style.cssText = "position:absolute;visibility:hidden;pointer-events:none;left:-99999px;top:0;width:max-content;max-width:none;min-width:min-content;height:auto;flex-shrink:0;flex-grow:0;box-sizing:border-box"

  body.appendChild(clone)
  const rect = clone.getBoundingClientRect()
  const widthPx = Math.max(clone.scrollWidth, rect.width)
  body.removeChild(clone)
  return widthPx
}

function measureTorrentColumnFitWidth(gridRoot: HTMLElement, columnId: string): number | null {
  const nodes = gridRoot.querySelectorAll<HTMLElement>(
    `[${TORRENT_ROW_CELL_MEASURE}="${escapeColumnIdForSelector(columnId)}"]`
  )
  if (nodes.length === 0) {
    return null
  }

  let maxContent = 0
  nodes.forEach((el) => {
    maxContent = Math.max(maxContent, measureNaturalOuterWidth(el))
  })

  return maxContent
}
{/* Auto-fit width for column headers on double-click end*/}

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
  const columnHasActiveFilter = columnFilters.some(f => f.columnId === column.id)
  const primaryInputCanHover = useMediaQuery("(hover: hover)")
  const hideFilterUntilHover = primaryInputCanHover && !columnHasActiveFilter
  const columnFilterIconVisibilityClassName = hideFilterUntilHover ? "hidden group-hover:block focus-within:block [&:has(button[data-state=open])]:block" : undefined
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
          {/* Column filter button - only show for filterable columns */}
          {!isSelectHeader && !isPriorityHeader && !isTrackerIconHeader && !isStatusIconHeader && onFilterChange && (
            <span className={columnFilterIconVisibilityClassName}>
              <ColumnFilterPopover
                columnId={column.id}
                columnName={(column.columnDef.meta as { headerString?: string })?.headerString ||
                  (typeof column.columnDef.header === "string" ? column.columnDef.header : column.id)}
                columnType={getColumnType(column.id)}
                currentFilter={columnFilters.find(f => f.columnId === column.id)}
                onApply={(filter) => onFilterChange(column.id, filter)}
              />
            </span>
          )}
          {shouldShowSortIndicator && (
            column.getIsSorted() === "asc" ? (
              <ChevronUp className={`h-4 w-4 flex-shrink-0${isPriorityHeader ? " ml-1 mr-1" : ""}`} />
            ) : (
              <ChevronDown className={`h-4 w-4 flex-shrink-0${isPriorityHeader ? " ml-1 mr-1" : ""}`} />
            )
          )}
        </div>
      </div>

      {/* Resize handle */}
      {shouldShowSeparator && (
        <div
          onMouseDown={canResize ? header.getResizeHandler() : undefined}
          onTouchStart={canResize ? header.getResizeHandler() : undefined}
          onDoubleClick={(event) => {
            if (!canResize) {
              return
            }
            event.preventDefault()
            event.stopPropagation()
            const gridRoot = event.currentTarget.closest("[role=\"grid\"]") as HTMLElement | null
            if (!gridRoot) {
              column.resetSize()
              return
            }
            const measured = measureTorrentColumnFitWidth(gridRoot, column.id)
            if (measured === null) {
              column.resetSize()
              return
            }
            const minSize = column.columnDef.minSize ?? 20
            const maxSize = column.columnDef.maxSize ?? Number.MAX_SAFE_INTEGER
            const nextWidth = Math.min(Math.max(Math.ceil(measured), minSize), maxSize)
            table.setColumnSizing(prev => ({
              ...prev,
              [column.id]: nextWidth,
            }))
          }}
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
