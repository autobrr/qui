/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { arrayMove } from "@dnd-kit/sortable"

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
