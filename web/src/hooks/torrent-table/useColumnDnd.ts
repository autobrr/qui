/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePersistedColumnOrder } from "@/hooks/usePersistedColumnOrder"
import { reorderColumns } from "@/lib/torrent-table/column-order"
import {
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
  type DragEndEvent
} from "@dnd-kit/core"
import type { ColumnOrderState } from "@tanstack/react-table"
import { useCallback, useRef, type Dispatch, type SetStateAction } from "react"

type Sensors = ReturnType<typeof useSensors>

export interface UseColumnDndParams {
  instanceId: number
  /** Default order, computed by the parent (kept module-scope there for a stable reference). */
  defaultColumnOrder: ColumnOrderState
  /** Latest accessor for the table's leaf column ids — the table is created after this hook. */
  getLeafColumnIds: () => string[]
}

export interface ColumnDnd {
  columnOrder: ColumnOrderState
  setColumnOrder: Dispatch<SetStateAction<ColumnOrderState>>
  sensors: Sensors
  onDragEnd: (event: DragEndEvent) => void
}

/**
 * Owns the table's column drag-and-drop: the persisted column order, the dnd-kit
 * sensors, and the drop handler. `onDragEnd` normalizes + reorders against the
 * table's live leaf columns via the `getLeafColumnIds` latest-ref, so it works
 * even though this hook runs before the table exists.
 */
export function useColumnDnd({
  instanceId,
  defaultColumnOrder,
  getLeafColumnIds,
}: UseColumnDndParams): ColumnDnd {
  const [columnOrder, setColumnOrder] = usePersistedColumnOrder(defaultColumnOrder, instanceId)

  // Sensors must be called at the top level, not inside useMemo
  const sensors = useSensors(
    useSensor(MouseSensor, {
      activationConstraint: {
        distance: 8,
      },
    }),
    useSensor(TouchSensor, {
      activationConstraint: {
        delay: 250,
        tolerance: 5,
      },
    })
  )

  // Always read the latest leaf-column accessor (the table is created after this hook).
  const getLeafColumnIdsRef = useRef(getLeafColumnIds)
  getLeafColumnIdsRef.current = getLeafColumnIds

  const onDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event
    if (!active || !over || active.id === over.id) {
      return
    }

    setColumnOrder((currentOrder: ColumnOrderState) =>
      reorderColumns(currentOrder, active.id as string, over.id as string, getLeafColumnIdsRef.current())
    )
  }, [setColumnOrder])

  return { columnOrder, setColumnOrder, sensors, onDragEnd }
}
