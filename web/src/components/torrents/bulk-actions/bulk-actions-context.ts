/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Category } from "@/types"
import { createContext, useContext } from "react"
import type { BulkActionPick } from "./bulk-action-state"

interface BulkActions {
  pick: (pick: BulkActionPick) => void
  // The busy state of the requests. A running export does not set it.
  isPending: boolean
  isExporting: boolean
}

interface BulkActionCategories {
  categories: Record<string, Category>
  useSubcategories: boolean
}

export const BulkActionsContext = createContext<BulkActions | null>(null)
// A separate context, so a new category list does not render the table and each row menu again.
export const BulkActionCategoriesContext = createContext<BulkActionCategories | null>(null)

// What an Action surface reads from the bulk action module.
export function useBulkActions(): BulkActions {
  const context = useContext(BulkActionsContext)
  if (!context) {
    throw new Error("useBulkActions must be used within a BulkActionProvider")
  }
  return context
}

// The categories of the Set category dialog, for the category submenu.
export function useBulkActionCategories(): BulkActionCategories {
  const context = useContext(BulkActionCategoriesContext)
  if (!context) {
    throw new Error("useBulkActionCategories must be used within a BulkActionProvider")
  }
  return context
}
