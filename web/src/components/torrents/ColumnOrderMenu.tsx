/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { reorderColumns } from "@/lib/torrent-table/column-order"
import { closestCenter, DndContext, type DragEndEvent } from "@dnd-kit/core"
import { restrictToVerticalAxis } from "@dnd-kit/modifiers"
import { SortableContext, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import type { ColumnOrderState } from "@tanstack/react-table"
import { GripVertical } from "lucide-react"
import type { Dispatch, SetStateAction } from "react"
import { useTranslation } from "react-i18next"
import { DropdownMenuCheckboxItem } from "@/components/ui/dropdown-menu"
import type { TorrentTable } from "./tanstackTableFeatures"

interface SortableColumnItemProps {
  column: ReturnType<TorrentTable["getAllColumns"]>[number]
}

function SortableColumnItem({ column }: SortableColumnItemProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: column.id,
  })

  const { t } = useTranslation("torrents")
  const label =
    (column.columnDef.meta as { headerString?: string })?.headerString ||
    (typeof column.columnDef.header === "string" ? column.columnDef.header : column.id)

  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
        display: "flex",
        alignItems: "center",
      }}
    >
      {/*
       * The wrapper div stops pointer-event bubbling after dnd-kit's listeners
       * have fired on the button, preventing Radix's DismissableLayer from
       * treating the drag-start as a "click outside" and closing the dropdown.
       */}
      <div onPointerDown={(e) => e.stopPropagation()}>
        <button
          className="ml-1 flex-shrink-0 cursor-grab p-1 text-muted-foreground hover:text-foreground active:cursor-grabbing"
          aria-label={t("tableView.reorderColumn", { column: label })}
          {...attributes}
          {...listeners}
        >
          <GripVertical className="h-3.5 w-3.5" />
        </button>
      </div>
      <DropdownMenuCheckboxItem
        className="flex-1 capitalize"
        checked={column.getIsVisible()}
        onCheckedChange={(value) => column.toggleVisibility(!!value)}
        onSelect={(e) => e.preventDefault()}
      >
        <span className="truncate">{label}</span>
      </DropdownMenuCheckboxItem>
    </div>
  )
}

interface ColumnOrderMenuProps {
  table: TorrentTable
  columnOrder: ColumnOrderState
  setColumnOrder: Dispatch<SetStateAction<ColumnOrderState>>
}

/**
 * Renders the column-visibility checkboxes as a vertically sortable list.
 * Drag handles let the user reorder columns without touching the table header.
 * The order is persisted by the same usePersistedColumnOrder machinery that
 * drives header drag.
 */
export function ColumnOrderMenu({ table, columnOrder, setColumnOrder }: ColumnOrderMenuProps) {
  const hideableColumns = table.getAllColumns().filter((col) => col.getCanHide())

  // Preserve the persisted column order within the menu list.
  const sorted = [...hideableColumns].sort((a, b) => {
    const ai = columnOrder.indexOf(a.id)
    const bi = columnOrder.indexOf(b.id)
    return (ai === -1 ? Infinity : ai) - (bi === -1 ? Infinity : bi)
  })

  const allColumnIds = table.getAllLeafColumns().map((c) => c.id)

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event
    if (!active || !over || active.id === over.id) return
    setColumnOrder((current) =>
      reorderColumns(current, active.id as string, over.id as string, allColumnIds)
    )
  }

  return (
    <DndContext
      collisionDetection={closestCenter}
      modifiers={[restrictToVerticalAxis]}
      onDragEnd={handleDragEnd}
    >
      <SortableContext items={sorted.map((c) => c.id)} strategy={verticalListSortingStrategy}>
        {sorted.map((column) => (
          <SortableColumnItem key={column.id} column={column} />
        ))}
      </SortableContext>
    </DndContext>
  )
}
