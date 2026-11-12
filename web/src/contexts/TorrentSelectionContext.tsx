/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { EMPTY_SELECTION, selectionReducer, type ActionTarget, type TorrentSelectionEvent, type TorrentSelectionState } from "@/lib/torrent-selection"
import type { Category, InstanceCapabilities } from "@/types"
import type { Dispatch, ReactNode } from "react"
import { createContext, useContext, useMemo, useReducer, useState } from "react"

// The facts of the list that the bulk action dialogs and Export read.
export interface ListFacts {
  // The list comes from the cross-instance endpoint, and categories and tags are those of its response.
  isCrossInstance: boolean
  categories?: Record<string, Category>
  tags?: string[]
  allowSubcategories: boolean
  capabilities?: InstanceCapabilities
  sortField: string
  sortOrder: "asc" | "desc"
}

// What the desktop table or the mobile cards publish for the header management bar and the bulk action module.
export interface TorrentSelectionSummary {
  actionTarget: ActionTarget
  // In Select all mode, the total size of the filter and the size of the loaded Exclusions.
  // totalSize is 0 outside Select all mode, when qui has no total, and when the table filters in the browser.
  totalSize: number
  excludedSize: number
  // The instance of the page, and the instance subset of the Unified view.
  instanceId: number
  instanceIds?: number[]
  listFacts: ListFacts
}

interface TorrentSelectionContextType {
  selection: TorrentSelectionState
  dispatch: Dispatch<TorrentSelectionEvent>
  publishSummary: (summary: TorrentSelectionSummary | null) => void
}

const TorrentSelectionContext = createContext<TorrentSelectionContextType | undefined>(undefined)
// A separate context, so a new summary does not re-render the table that publishes it.
const TorrentSelectionSummaryContext = createContext<TorrentSelectionSummary | null>(null)

export function TorrentSelectionProvider({ children }: { children: ReactNode }) {
  const [selection, dispatch] = useReducer(selectionReducer, EMPTY_SELECTION)
  const [summary, publishSummary] = useState<TorrentSelectionSummary | null>(null)
  const value = useMemo(() => ({ selection, dispatch, publishSummary }), [selection])

  return (
    <TorrentSelectionContext.Provider value={value}>
      <TorrentSelectionSummaryContext.Provider value={summary}>
        {children}
      </TorrentSelectionSummaryContext.Provider>
    </TorrentSelectionContext.Provider>
  )
}

export function useTorrentSelectionContext() {
  const context = useContext(TorrentSelectionContext)
  if (context === undefined) {
    throw new Error("useTorrentSelectionContext must be used within a TorrentSelectionProvider")
  }
  return context
}

export function useTorrentSelectionSummary() {
  return useContext(TorrentSelectionSummaryContext)
}
