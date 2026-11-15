/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext } from "react"

export const databaseShrinkQueryKey = ["database-shrink"]

export interface DatabaseShrinkContextValue {
  /** Stores the "shrink pending" mark and restarts qui, or tells the user that the shrink waits for the next start. */
  requestShrink: () => Promise<void>
  pending: boolean
}

export const DatabaseShrinkContext = createContext<DatabaseShrinkContextValue | null>(null)

/** The shrink request of DatabaseShrinkProvider in the app layout. */
export function useDatabaseShrink(): DatabaseShrinkContextValue {
  const value = useContext(DatabaseShrinkContext)
  if (!value) {
    throw new Error("useDatabaseShrink needs a DatabaseShrinkProvider")
  }
  return value
}
