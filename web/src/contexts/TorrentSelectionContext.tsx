/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { EMPTY_SELECTION, selectionReducer, type ActionTarget, type TorrentSelectionEvent, type TorrentSelectionState } from "@/lib/torrent-selection"
import type { Category, InstanceCapabilities } from "@/types"
import type { Dispatch, ReactNode } from "react"
import { createContext, useContext, useMemo, useReducer, useState } from "react"

export interface ListFacts {
  isCrossInstance: boolean
  categories?: Record<string, Category>
  tags?: string[]
  allowSubcategories: boolean
  capabilities?: InstanceCapabilities
  sortField: string
  sortOrder: "asc" | "desc"
}

export interface TorrentSelectionSummary {
  actionTarget: ActionTarget
  totalSize: number
  excludedSize: number
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
