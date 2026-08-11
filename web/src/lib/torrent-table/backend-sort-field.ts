/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Map TanStack Table column IDs to backend field names
export function getBackendSortField(columnId: string): string {
  if (!columnId) {
    return "added_on"
  }

  switch (columnId) {
    case "status_icon":
      return "state"
    case "num_seeds":
      return "num_complete" // Sort by total seeds, not connected
    case "num_leechs":
      return "num_incomplete" // Sort by total peers, not connected
    default:
      return columnId
  }
}
