/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext } from "react"
import type { BulkActionPick } from "./bulk-action-state"

interface BulkActions {
  pick: (pick: BulkActionPick) => void
  isPending: boolean
}

export const BulkActionsContext = createContext<BulkActions | null>(null)

// What an Action surface reads from the bulk action module.
export function useBulkActions(): BulkActions {
  const context = useContext(BulkActionsContext)
  if (!context) {
    throw new Error("useBulkActions must be used within a BulkActionProvider")
  }
  return context
}
