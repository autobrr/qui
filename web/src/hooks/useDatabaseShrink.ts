/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext } from "react"

export const databaseShrinkQueryKey = ["database-shrink"]

export interface DatabaseShrinkContextValue {
  requestShrink: () => Promise<void>
  pending: boolean
}

export const DatabaseShrinkContext = createContext<DatabaseShrinkContextValue | null>(null)

export function useDatabaseShrink(): DatabaseShrinkContextValue {
  const value = useContext(DatabaseShrinkContext)
  if (!value) {
    throw new Error("useDatabaseShrink needs a DatabaseShrinkProvider")
  }
  return value
}
