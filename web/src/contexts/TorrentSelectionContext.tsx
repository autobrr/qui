/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent, TorrentFilters } from "@/types"
import type { ReactNode } from "react"
import { createContext, useCallback, useContext, useRef, useState } from "react"

type TorrentTarget = { instanceId: number; hash: string }

interface TorrentSelectionContextType {
  isSelectionMode: boolean
  setIsSelectionMode: (value: boolean) => void
  // Management Bar state
  showManagementBar: boolean
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  isAllSelected: boolean
  totalSelectionCount: number
  selectedTotalSize: number
  excludeHashes: string[]
  excludeTargets: TorrentTarget[]
  filters?: TorrentFilters
  instanceId?: number
  // Management Bar actions
  updateSelection: (
    selectedHashes: string[],
    selectedTorrents: Torrent[],
    isAllSelected: boolean,
    totalSelectionCount: number,
    excludeHashes: string[],
    excludeTargets: TorrentTarget[],
    selectedTotalSize: number,
    selectionFilters?: TorrentFilters
  ) => void
  clearSelection: () => void
  setFiltersAndInstance: (filters: TorrentSelectionContextType["filters"], instanceId: number) => void
  setResetHandler: (handler?: () => void) => void
}

const TorrentSelectionContext = createContext<TorrentSelectionContextType | undefined>(undefined)

export function TorrentSelectionProvider({ children }: { children: ReactNode }) {
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const [selectedHashes, setSelectedHashes] = useState<string[]>([])
  const [selectedTorrents, setSelectedTorrents] = useState<Torrent[]>([])
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [totalSelectionCount, setTotalSelectionCount] = useState(0)
  const [selectedTotalSize, setSelectedTotalSize] = useState(0)
  const [excludeHashes, setExcludeHashes] = useState<string[]>([])
  const [excludeTargets, setExcludeTargets] = useState<TorrentTarget[]>([])
  const [baseFilters, setBaseFilters] = useState<TorrentSelectionContextType["filters"]>()
  const [effectiveFilters, setEffectiveFilters] = useState<TorrentSelectionContextType["filters"]>()
  const [instanceId, setInstanceId] = useState<number>()
  const resetHandlerRef = useRef<(() => void) | undefined>(undefined)

  // Calculate showManagementBar based on current state
  const showManagementBar = selectedHashes.length > 0 || isAllSelected

  const updateSelection = useCallback((
    newSelectedHashes: string[],
    newSelectedTorrents: Torrent[],
    newIsAllSelected: boolean,
    newTotalSelectionCount: number,
    newExcludeHashes: string[],
    newExcludeTargets: TorrentTarget[],
    newSelectedTotalSize: number,
    selectionFilters?: TorrentFilters
  ) => {
    setSelectedHashes(newSelectedHashes)
    setSelectedTorrents(newSelectedTorrents)
    setIsAllSelected(newIsAllSelected)
    setTotalSelectionCount(newTotalSelectionCount)
    setExcludeHashes(newExcludeHashes)
    setExcludeTargets(newExcludeTargets)
    setSelectedTotalSize(newSelectedTotalSize)
    setEffectiveFilters(selectionFilters ?? baseFilters)
  }, [baseFilters])

  const clearSelection = useCallback(() => {
    resetHandlerRef.current?.()
    setSelectedHashes([])
    setSelectedTorrents([])
    setIsAllSelected(false)
    setTotalSelectionCount(0)
    setExcludeHashes([])
    setExcludeTargets([])
    setSelectedTotalSize(0)
    setEffectiveFilters(baseFilters)
  }, [baseFilters])

  const setFiltersAndInstance = useCallback((newFilters: TorrentSelectionContextType["filters"], newInstanceId: number) => {
    setBaseFilters(newFilters)
    setInstanceId(newInstanceId)
    setEffectiveFilters(prev => {
      if (selectedHashes.length > 0 || isAllSelected) {
        return prev ?? newFilters
      }
      return newFilters
    })
  }, [selectedHashes, isAllSelected])

  const setResetHandler = useCallback((handler?: () => void) => {
    resetHandlerRef.current = handler
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
      selectedTotalSize,
      excludeHashes,
      excludeTargets,
      filters: effectiveFilters,
      instanceId,
      updateSelection,
      clearSelection,
      setFiltersAndInstance,
      setResetHandler,
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
