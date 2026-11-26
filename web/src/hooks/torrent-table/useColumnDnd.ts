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
  defaultColumnOrder: ColumnOrderState
  getLeafColumnIds: () => string[]
}

export interface ColumnDnd {
  columnOrder: ColumnOrderState
  setColumnOrder: Dispatch<SetStateAction<ColumnOrderState>>
  sensors: Sensors
  onDragEnd: (event: DragEndEvent) => void
}

export function useColumnDnd({
  instanceId,
  defaultColumnOrder,
  getLeafColumnIds,
}: UseColumnDndParams): ColumnDnd {
  const [columnOrder, setColumnOrder] = usePersistedColumnOrder(defaultColumnOrder, instanceId)

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
