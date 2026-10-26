/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentSelectionContext } from "@/contexts/TorrentSelectionContext"
import {
  getSelectionIdentity as selectionIdentity,
  hasSelection as selectionIsActive,
  headerCheckboxEvent,
  isKeySelected,
  selectAllCheckboxFlags,
  type TorrentSelectionEvent
} from "@/lib/torrent-selection"
import type { Torrent } from "@/types"
import { type RefObject, useCallback, useEffect, useMemo, useRef } from "react"

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
  /**
   * Lazily returns the currently visible table rows. Passed as a callback
   * because the table is created *after* this hook (it consumes the hook's
   * handlers via createColumns), so the latest reference is read inside
   * handlers rather than captured at call time.
   */
  getVisibleRows: () => SelectionRow[]
}

export interface TorrentSelection {
  isAllSelected: boolean
  selectedKeys: ReadonlySet<string>
  excludedFromSelectAll: ReadonlySet<string>
  hasSelection: boolean
  lastSelectedIndexRef: RefObject<number | null>
  getSelectionIdentity: (torrent: Torrent) => string
  isTorrentSelected: (torrent: Torrent) => boolean
  handleSelectAll: () => void
  selectAll: () => void
  selectOnly: (torrent: Torrent, index: number) => void
  clearSelection: () => void
  tickRows: (rows: SelectionRow[], checked: boolean) => void
  isSelectAllChecked: boolean
  isSelectAllIndeterminate: boolean
  handleCheckboxPointerDown: (event: React.PointerEvent<HTMLElement>) => void
  handleCheckboxChange: (torrent: Torrent, rowId: string, checked: boolean) => void
}

/**
 * The desktop table's view of the shared selection: it sends the table's
 * events to the selection provider, ignores them in read-only mode, and keeps
 * the shift-click anchor, which depends on row order.
 */
export function useTorrentSelection({
  sortedTorrents,
  isReadOnly,
  isCrossInstanceEndpoint,
  instanceId,
  getVisibleRows,
}: UseTorrentSelectionParams): TorrentSelection {
  const { selection, dispatch } = useTorrentSelectionContext()
  const hasSelection = selectionIsActive(selection)

  // State for range select capabilities for checkboxes
  const shiftPressedRef = useRef<boolean>(false)
  const lastSelectedIndexRef = useRef<number | null>(null)

  // Always read the latest row accessor (the table is created after this hook).
  const getVisibleRowsRef = useRef(getVisibleRows)
  getVisibleRowsRef.current = getVisibleRows

  // Escape and finished deletes clear the selection from outside the table.
  useEffect(() => {
    if (!hasSelection) {
      lastSelectedIndexRef.current = null
    }
  }, [hasSelection])

  const send = useCallback((event: TorrentSelectionEvent) => {
    if (!isReadOnly) {
      dispatch(event)
    }
  }, [dispatch, isReadOnly])

  const getSelectionIdentity = useCallback(
    (torrent: Torrent) => selectionIdentity(torrent, isCrossInstanceEndpoint ?? false, instanceId),
    [isCrossInstanceEndpoint, instanceId]
  )

  const isTorrentSelected = useCallback(
    (torrent: Torrent) => isKeySelected(selection, getSelectionIdentity(torrent)),
    [selection, getSelectionIdentity]
  )

  const handleSelectAll = useCallback(() => send(headerCheckboxEvent(selection)), [send, selection])
  const selectAll = useCallback(() => {
    send({ type: "selectAll" })
    lastSelectedIndexRef.current = null
  }, [send])
  const clearSelection = useCallback(() => send({ type: "clear" }), [send])

  const selectOnly = useCallback((torrent: Torrent, index: number) => {
    if (isReadOnly) {
      return
    }
    dispatch({ type: "selectOnly", key: getSelectionIdentity(torrent) })
    lastSelectedIndexRef.current = index
  }, [dispatch, getSelectionIdentity, isReadOnly])

  const tickRows = useCallback((rows: SelectionRow[], checked: boolean) => {
    send({ type: "tick", keys: rows.map(row => getSelectionIdentity(row.original)), checked })
  }, [send, getSelectionIdentity])

  const rowKeys = useMemo(() => sortedTorrents.map(getSelectionIdentity), [sortedTorrents, getSelectionIdentity])
  const { checked: isSelectAllChecked, indeterminate: isSelectAllIndeterminate } = selectAllCheckboxFlags(selection, rowKeys)

  const handleCheckboxPointerDown = useCallback((event: React.PointerEvent<HTMLElement>) => {
    shiftPressedRef.current = event.shiftKey
  }, [])

  const handleCheckboxChange = useCallback((torrent: Torrent, rowId: string, checked: boolean) => {
    if (isReadOnly) {
      return
    }

    const allRows = getVisibleRowsRef.current()
    const currentIndex = allRows.findIndex(r => r.id === rowId)
    const anchor = lastSelectedIndexRef.current

    if (shiftPressedRef.current && anchor !== null && currentIndex !== -1) {
      tickRows(allRows.slice(Math.min(anchor, currentIndex), Math.max(anchor, currentIndex) + 1), checked)
    } else {
      tickRows([{ id: rowId, original: torrent }], checked)
    }

    if (currentIndex !== -1) {
      lastSelectedIndexRef.current = currentIndex
    }
    shiftPressedRef.current = false
  }, [tickRows, isReadOnly])

  return {
    isAllSelected: selection.isAllSelected,
    selectedKeys: selection.selected,
    excludedFromSelectAll: selection.excluded,
    hasSelection,
    lastSelectedIndexRef,
    getSelectionIdentity,
    isTorrentSelected,
    handleSelectAll,
    selectAll,
    selectOnly,
    clearSelection,
    tickRows,
    isSelectAllChecked,
    isSelectAllIndeterminate,
    handleCheckboxPointerDown,
    handleCheckboxChange,
  }
}
