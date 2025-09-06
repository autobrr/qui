/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext, useState, useCallback } from "react"
import type { ReactNode } from "react"
import type { Torrent } from "@/types"

interface TorrentSelectionContextType {
  isSelectionMode: boolean
  setIsSelectionMode: (value: boolean) => void
  // Management Bar state
  showManagementBar: boolean
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  isAllSelected: boolean
  totalSelectionCount: number
  excludeHashes: string[]
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
  instanceId?: number
  // Management Bar actions
  updateSelection: (
    selectedHashes: string[],
    selectedTorrents: Torrent[],
    isAllSelected: boolean,
    totalSelectionCount: number,
    excludeHashes: string[]
  ) => void
  clearSelection: () => void
  setFiltersAndInstance: (filters: TorrentSelectionContextType["filters"], instanceId: number) => void
}

const TorrentSelectionContext = createContext<TorrentSelectionContextType | undefined>(undefined)

export function TorrentSelectionProvider({ children }: { children: ReactNode }) {
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const [selectedHashes, setSelectedHashes] = useState<string[]>([])
  const [selectedTorrents, setSelectedTorrents] = useState<Torrent[]>([])
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [totalSelectionCount, setTotalSelectionCount] = useState(0)
  const [excludeHashes, setExcludeHashes] = useState<string[]>([])
  const [filters, setFilters] = useState<TorrentSelectionContextType["filters"]>()
  const [instanceId, setInstanceId] = useState<number>()

  // Calculate showManagementBar based on current state
  const showManagementBar = selectedHashes.length > 0 || isAllSelected

  const updateSelection = useCallback((
    newSelectedHashes: string[],
    newSelectedTorrents: Torrent[],
    newIsAllSelected: boolean,
    newTotalSelectionCount: number,
    newExcludeHashes: string[]
  ) => {
    setSelectedHashes(newSelectedHashes)
    setSelectedTorrents(newSelectedTorrents)
    setIsAllSelected(newIsAllSelected)
    setTotalSelectionCount(newTotalSelectionCount)
    setExcludeHashes(newExcludeHashes)
  }, [])

  const clearSelection = useCallback(() => {
    setSelectedHashes([])
    setSelectedTorrents([])
    setIsAllSelected(false)
    setTotalSelectionCount(0)
    setExcludeHashes([])
  }, [])

  const setFiltersAndInstance = useCallback((newFilters: TorrentSelectionContextType["filters"], newInstanceId: number) => {
    setFilters(newFilters)
    setInstanceId(newInstanceId)
  }, [])

  return (
    <TorrentSelectionContext.Provider value={{
      isSelectionMode,
      setIsSelectionMode,
      showManagementBar,
      selectedHashes,
      selectedTorrents,
      isAllSelected,
      totalSelectionCount,
      excludeHashes,
      filters,
      instanceId,
      updateSelection,
      clearSelection,
      setFiltersAndInstance,
    }}>
      {children}
    </TorrentSelectionContext.Provider>
  )
}

export function useTorrentSelection() {
  const context = useContext(TorrentSelectionContext)
  if (context === undefined) {
    throw new Error("useTorrentSelection must be used within a TorrentSelectionProvider")
  }
  return context
}