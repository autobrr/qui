/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { arrayMove } from "@dnd-kit/sortable"

/**
 * Computes the next persisted column order after a drag. The table's live leaf
 * columns that the current order lacks are appended, and the moved column is
 * then repositioned. A column that the table does not show keeps its place, so
 * the order of every instance keeps the instance column of the Unified view. If
 * either dragged id is missing, the order with the appended columns is returned.
 */
export function reorderColumns(
  currentOrder: string[],
  activeId: string,
  overId: string,
  allColumnIds: string[]
): string[] {
  const sanitizedOrder = [
    ...new Set(currentOrder),
    ...allColumnIds.filter((id) => !currentOrder.includes(id)),
  ]

  const oldIndex = sanitizedOrder.indexOf(activeId)
  const newIndex = sanitizedOrder.indexOf(overId)

  if (oldIndex === -1 || newIndex === -1) {
    return sanitizedOrder
  }

  return arrayMove(sanitizedOrder, oldIndex, newIndex)
}
