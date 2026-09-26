/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { columnLabel } from "@/lib/torrent-table/column-label"
import type { ColumnDnd } from "@/hooks/torrent-table/useColumnDnd"
import { closestCenter, DndContext } from "@dnd-kit/core"
import { restrictToVerticalAxis } from "@dnd-kit/modifiers"
import { SortableContext, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import type { ColumnOrderState } from "@tanstack/react-table"
import { GripVertical } from "lucide-react"
import { useTranslation } from "react-i18next"
import { DropdownMenuCheckboxItem } from "@/components/ui/dropdown-menu"
import type { TorrentTable, TorrentTableColumn } from "./tanstackTableFeatures"

function SortableColumnItem({ column }: { column: TorrentTableColumn }) {
  // The select column is pinned first; header drag disables it the same way.
  const disabled = column.id === "select"
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: column.id,
    disabled,
  })

  const { t } = useTranslation("torrents")
  const label = columnLabel(column)

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
          className="ml-1 flex-shrink-0 cursor-grab p-1 text-muted-foreground hover:text-foreground active:cursor-grabbing disabled:invisible"
          aria-label={t("tableView.reorderColumn", { column: label })}
          disabled={disabled}
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

// sensors/onDragEnd are shared with the header drag so both paths use the same thresholds and reorder math.
interface ColumnOrderMenuProps extends Pick<ColumnDnd, "sensors" | "onDragEnd"> {
  table: TorrentTable
  columnOrder: ColumnOrderState
}

export function ColumnOrderMenu({ table, columnOrder, sensors, onDragEnd }: ColumnOrderMenuProps) {
  const hideableColumns = table.getAllColumns().filter((col) => col.getCanHide())

  // Preserve the persisted column order within the menu list.
  const sorted = [...hideableColumns].sort((a, b) => {
    const ai = columnOrder.indexOf(a.id)
    const bi = columnOrder.indexOf(b.id)
    return (ai === -1 ? Infinity : ai) - (bi === -1 ? Infinity : bi)
  })

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      modifiers={[restrictToVerticalAxis]}
      onDragEnd={onDragEnd}
    >
      <SortableContext items={sorted.map((c) => c.id)} strategy={verticalListSortingStrategy}>
        {sorted.map((column) => (
          <SortableColumnItem key={column.id} column={column} />
        ))}
      </SortableContext>
    </DndContext>
  )
}
