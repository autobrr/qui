/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Reconcile the expanded-folder set after the file tree rebuilds.
 *
 * Folder expansion is keyed by path, so a rename changes the id of the folder
 * and all its descendants. Dropping vanished ids and auto-expanding ids that
 * were not previously known keeps a renamed folder expanded, while folders the
 * user collapsed stay collapsed across refetches (their id persists).
 *
 * Returns the original set unchanged when nothing needs to change.
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
