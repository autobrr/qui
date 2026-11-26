/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Category } from "@/types"
import { createContext, useContext } from "react"
import type { BulkActionPick } from "./bulk-action-state"

interface BulkActions {
  pick: (pick: BulkActionPick) => void
  isPending: boolean
  isExporting: boolean
}

interface BulkActionCategories {
  categories: Record<string, Category>
  useSubcategories: boolean
}

export const BulkActionsContext = createContext<BulkActions | null>(null)
export const BulkActionCategoriesContext = createContext<BulkActionCategories | null>(null)

export function useBulkActions(): BulkActions {
  const context = useContext(BulkActionsContext)
  if (!context) {
    throw new Error("useBulkActions must be used within a BulkActionProvider")
  }
  return context
}

export function useBulkActionCategories(): BulkActionCategories {
  const context = useContext(BulkActionCategoriesContext)
  if (!context) {
    throw new Error("useBulkActionCategories must be used within a BulkActionProvider")
  }
  return context
}
