/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export function reconcileExpandedFolders(
  expanded: Set<string>,
  previousIds: Set<string>,
  currentIds: Set<string>
): Set<string> {
  let changed = false
  const next = new Set<string>()

  for (const id of expanded) {
    if (currentIds.has(id)) {
      next.add(id)
    } else {
      changed = true
    }
  }

  for (const id of currentIds) {
    if (!previousIds.has(id) && !next.has(id)) {
      next.add(id)
      changed = true
    }
  }

  return changed ? next : expanded
}
