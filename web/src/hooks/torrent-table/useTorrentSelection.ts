/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossInstanceTorrent, Torrent } from "@/types"
import { type Dispatch, type RefObject, type SetStateAction, useCallback, useEffect, useMemo, useRef, useState } from "react"

// Minimal structural shape of a TanStack row that the selection logic reads.
export interface SelectionRow {
  id: string
  original: Torrent
}

export interface UseTorrentSelectionParams {
  sortedTorrents: Torrent[]
  isReadOnly: boolean
  isCrossInstanceEndpoint?: boolean
  instanceId: number
  onResetSelection?: (handler?: () => void) => void
  /**
   * Lazily returns the currently visible table rows. Passed as a callback
   * because the table is created *after* this hook (it consumes the hook's
   * handlers via createColumns), so the latest reference is read inside
   * effects/handlers rather than captured at call time.
   */
  getVisibleRows: () => SelectionRow[]
}

export interface TorrentSelection {
  rowSelection: Record<string, boolean>
  setRowSelection: Dispatch<SetStateAction<Record<string, boolean>>>
  isAllSelected: boolean
  setIsAllSelected: Dispatch<SetStateAction<boolean>>
  excludedFromSelectAll: Set<string>
  setExcludedFromSelectAll: Dispatch<SetStateAction<Set<string>>>
  shiftPressedRef: RefObject<boolean>
  lastSelectedIndexRef: RefObject<number | null>
  selectedRowIds: string[]
  selectedRowIdSet: Set<string>
  resetSelectionState: () => void
  getSelectionIdentity: (torrent: Torrent) => string
  handleSelectAll: () => void
  handleRowSelection: (selectionIdentity: string, checked: boolean, rowId?: string) => void
  isSelectAllChecked: boolean
  isSelectAllIndeterminate: boolean
  handleCompactCheckboxPointerDown: (event: React.PointerEvent<HTMLDivElement>) => void
  handleCompactCheckboxChange: (torrent: Torrent, rowId: string, checked: boolean) => void
}

/**
 * Owns the table's selection: the Gmail-style select-all-with-exclusions state,
 * the row/select-all handlers, the derived checked/indeterminate flags, and the
 * effects that prune now-invalid selections when the visible rows change.
 */
export function useTorrentSelection({
  sortedTorrents,
  isReadOnly,
  isCrossInstanceEndpoint,
  instanceId,
  onResetSelection,
  getVisibleRows,
}: UseTorrentSelectionParams): TorrentSelection {
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [excludedFromSelectAll, setExcludedFromSelectAll] = useState<Set<string>>(new Set())

  // State for range select capabilities for checkboxes
  const shiftPressedRef = useRef<boolean>(false)
  const lastSelectedIndexRef = useRef<number | null>(null)

  // Always read the latest row accessor (the table is created after this hook).
  const getVisibleRowsRef = useRef(getVisibleRows)
  getVisibleRowsRef.current = getVisibleRows

  const handleCompactCheckboxPointerDown = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    shiftPressedRef.current = event.shiftKey
  }, [])

  const resetSelectionState = useCallback(() => {
    setIsAllSelected(false)
    setExcludedFromSelectAll(new Set())
    setRowSelection({})
    lastSelectedIndexRef.current = null
  }, [setIsAllSelected, setExcludedFromSelectAll, setRowSelection])

  useEffect(() => {
    if (!onResetSelection) {
      return
    }

    onResetSelection(resetSelectionState)
    return () => {
      onResetSelection(undefined)
    }
  }, [onResetSelection, resetSelectionState])

  const getSelectionIdentity = useCallback((torrent: Torrent): string => {
    if (!isCrossInstanceEndpoint) {
      return torrent.hash
    }

    const crossInstanceId = (torrent as Partial<CrossInstanceTorrent>).instanceId
    const resolvedInstanceId = typeof crossInstanceId === "number" && crossInstanceId > 0 ? crossInstanceId : instanceId
    return `${resolvedInstanceId}:${torrent.hash}`
  }, [isCrossInstanceEndpoint, instanceId])

  const selectedRowIds = useMemo(() => {
    const ids: string[] = []
    for (const [rowId, isSelected] of Object.entries(rowSelection)) {
      if (isSelected) {
        ids.push(rowId)
      }
    }
    return ids
  }, [rowSelection])
  const selectedRowIdSet = useMemo(() => new Set(selectedRowIds), [selectedRowIds])

  useEffect(() => {
    if (isAllSelected) {
      if (excludedFromSelectAll.size === 0) {
        return
      }

      const visibleSelectionIdentities = new Set(sortedTorrents.map(getSelectionIdentity))
      const hasInvalidExclusion = Array.from(excludedFromSelectAll).some(identity => !visibleSelectionIdentities.has(identity))

      if (hasInvalidExclusion) {
        resetSelectionState()
      }

      return
    }

    if (Object.keys(rowSelection).length === 0) {
      return
    }

    const visibleRowIds = new Set(getVisibleRowsRef.current().map(row => row.id))
    const hasInvalidSelection = Object.entries(rowSelection).some(([rowId, selected]) => selected && !visibleRowIds.has(rowId))

    if (hasInvalidSelection) {
      resetSelectionState()
    }
  }, [
    excludedFromSelectAll,
    getSelectionIdentity,
    isAllSelected,
    resetSelectionState,
    rowSelection,
    sortedTorrents,
  ])

  // Reset selection when table becomes empty
  useEffect(() => {
    if (sortedTorrents.length === 0 && (isAllSelected || Object.keys(rowSelection).length > 0)) {
      resetSelectionState()
    }
  }, [sortedTorrents.length, isAllSelected, rowSelection, resetSelectionState])

  // Custom selection handlers for "select all" functionality
  const handleSelectAll = useCallback(() => {
    if (isReadOnly) {
      return
    }

    // Gmail-style behavior: if any rows are selected, always deselect all
    const hasAnySelection = isAllSelected || selectedRowIds.length > 0

    if (hasAnySelection) {
      // Deselect all mode - regardless of checked state
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
      setRowSelection({})
      lastSelectedIndexRef.current = null // Reset anchor on deselect all
    } else {
      // Select all mode - only when nothing is selected
      setIsAllSelected(true)
      setExcludedFromSelectAll(new Set())
      setRowSelection({})
    }
  }, [setRowSelection, isAllSelected, selectedRowIds.length, isReadOnly])

  const handleRowSelection = useCallback((selectionIdentity: string, checked: boolean, rowId?: string) => {
    if (isReadOnly) {
      return
    }

    if (isAllSelected) {
      if (!checked) {
        // When deselecting a row in "select all" mode, add to exclusions
        setExcludedFromSelectAll(prev => new Set(prev).add(selectionIdentity))
      } else {
        // When selecting a row that was excluded, remove from exclusions
        setExcludedFromSelectAll(prev => {
          const newSet = new Set(prev)
          newSet.delete(selectionIdentity)
          return newSet
        })
      }
    } else {
      // Regular selection mode - use table's built-in selection with correct row ID
      const keyToUse = rowId || selectionIdentity // Use rowId if provided, fallback for backward compatibility
      setRowSelection(prev => ({
        ...prev,
        [keyToUse]: checked,
      }))
    }
  }, [isAllSelected, setRowSelection, isReadOnly])

  const isSelectAllChecked = useMemo(() => {
    if (isAllSelected) {
      // When in "select all" mode, only show checked if no exclusions exist
      return excludedFromSelectAll.size === 0
    }
    const regularSelectionCount = selectedRowIds.length
    return regularSelectionCount === sortedTorrents.length && sortedTorrents.length > 0
  }, [isAllSelected, excludedFromSelectAll.size, selectedRowIds.length, sortedTorrents.length])

  const isSelectAllIndeterminate = useMemo(() => {
    // Show indeterminate (dash) when SOME but not ALL items are selected
    if (isAllSelected) {
      // In "select all" mode, show indeterminate if some are excluded
      return excludedFromSelectAll.size > 0
    }

    const regularSelectionCount = selectedRowIds.length

    // Indeterminate when some (but not all) are selected
    return regularSelectionCount > 0 && regularSelectionCount < sortedTorrents.length
  }, [isAllSelected, excludedFromSelectAll.size, selectedRowIds.length, sortedTorrents.length])

  const handleCompactCheckboxChange = useCallback((torrent: Torrent, rowId: string, checked: boolean) => {
    if (isReadOnly) {
      return
    }

    const nextChecked = !!checked
    const allRows = getVisibleRowsRef.current()
    const currentIndex = allRows.findIndex(r => r.id === rowId)

    if (shiftPressedRef.current && lastSelectedIndexRef.current !== null && currentIndex !== -1) {
      const start = Math.min(lastSelectedIndexRef.current, currentIndex)
      const end = Math.max(lastSelectedIndexRef.current, currentIndex)

      for (let i = start; i <= end; i++) {
        const targetRow = allRows[i]
        if (targetRow) {
          handleRowSelection(getSelectionIdentity(targetRow.original), nextChecked, targetRow.id)
        }
      }
    } else {
      handleRowSelection(getSelectionIdentity(torrent), nextChecked, rowId)
    }

    if (currentIndex !== -1) {
      lastSelectedIndexRef.current = currentIndex
    }
    shiftPressedRef.current = false
  }, [handleRowSelection, getSelectionIdentity, isReadOnly])

  return {
    rowSelection,
    setRowSelection,
    isAllSelected,
    setIsAllSelected,
    excludedFromSelectAll,
    setExcludedFromSelectAll,
    shiftPressedRef,
    lastSelectedIndexRef,
    selectedRowIds,
    selectedRowIdSet,
    resetSelectionState,
    getSelectionIdentity,
    handleSelectAll,
    handleRowSelection,
    isSelectAllChecked,
    isSelectAllIndeterminate,
    handleCompactCheckboxPointerDown,
    handleCompactCheckboxChange,
  }
}
