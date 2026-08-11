/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { arrayMove } from "@dnd-kit/sortable"

/**
 * Computes the next persisted column order after a drag. The current order is
 * first normalized against the table's live leaf columns — stale ids are
 * dropped and newly-present ids appended — and the moved column is then
 * repositioned within that normalized order. If either dragged id is missing
 * after normalization, the normalized order is returned unchanged (still worth
 * persisting, since it drops stale ids / adds new ones).
 */
export function reorderColumns(
  currentOrder: string[],
  activeId: string,
  overId: string,
  allColumnIds: string[]
): string[] {
  // Normalize current order to include all current columns exactly once
  const sanitizedOrder = [
    ...currentOrder.filter((id) => allColumnIds.includes(id)),
    ...allColumnIds.filter((id) => !currentOrder.includes(id)),
  ]

  const oldIndex = sanitizedOrder.indexOf(activeId)
  const newIndex = sanitizedOrder.indexOf(overId)

  if (oldIndex === -1 || newIndex === -1) {
    return sanitizedOrder
  }

  return arrayMove(sanitizedOrder, oldIndex, newIndex)
}
