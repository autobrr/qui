/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnDnd } from "@/hooks/torrent-table/useColumnDnd"
import type { ViewMode } from "@/hooks/usePersistedCompactViewState"
import type { ColumnFilter } from "@/lib/column-filter-utils"
import { closestCenter, DndContext } from "@dnd-kit/core"
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers"
import { horizontalListSortingStrategy, SortableContext } from "@dnd-kit/sortable"
import type { Dispatch, SetStateAction } from "react"
import { DraggableTableHeader } from "../DraggableTableHeader"
import type { TorrentTable } from "../tanstackTableFeatures"

export interface TableColumnHeaderProps {
  table: TorrentTable
  sensors: ColumnDnd["sensors"]
  onDragEnd: ColumnDnd["onDragEnd"]
  columnFilters: ColumnFilter[]
  setColumnFilters: Dispatch<SetStateAction<ColumnFilter[]>>
  minTableWidth: number
  viewMode: ViewMode
  // Spreadsheet theme only: reserve the blank corner above the row-number gutter.
  showRowGutter?: boolean
}

/**
 * The draggable, filterable table header row. Rendered only in the normal/dense
 * table views (returns null for compact). The DnD wiring — sensors and the drop
 * handler — is owned by useColumnDnd and threaded in; this component renders the
 * header sub-tree verbatim.
 */
export function TableColumnHeader({
  table,
  sensors,
  onDragEnd,
  columnFilters,
  setColumnFilters,
  minTableWidth,
  viewMode,
  showRowGutter,
}: TableColumnHeaderProps) {
  if (viewMode === "compact") {
    return null
  }

  return (
    <div className="sticky top-0 bg-background border-b" style={{ zIndex: 50 }}>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={onDragEnd}
        modifiers={[restrictToHorizontalAxis]}
      >
        {table.getHeaderGroups().map(headerGroup => {
          const headers = headerGroup.headers
          const headerIds = headers.map(h => h.column.id)

          return (
            <SortableContext
              key={headerGroup.id}
              items={headerIds}
              strategy={horizontalListSortingStrategy}
            >
              <div className="flex" style={{ minWidth: `${minTableWidth}px` }}>
                {showRowGutter && <div className="ss-corner" aria-hidden="true" />}
                {headers.map(header => (
                  <DraggableTableHeader
                    key={header.id}
                    header={header}
                    columnFilters={columnFilters}
                    viewMode={viewMode}
                    onFilterChange={(columnId, filter) => {
                      if (filter === null) {
                        setColumnFilters(columnFilters.filter(f => f.columnId !== columnId))
                      } else {
                        const existing = columnFilters.findIndex(f => f.columnId === columnId)
                        if (existing >= 0) {
                          const newFilters = [...columnFilters]
                          newFilters[existing] = filter
                          setColumnFilters(newFilters)
                        } else {
                          setColumnFilters([...columnFilters, filter])
                        }
                      }
                    }}
                  />
                ))}
              </div>
            </SortableContext>
          )
        })}
      </DndContext>
    </div>
  )
}
