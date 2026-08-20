/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TORRENT_SORT_OPTIONS, type TorrentSortOptionValue, getDefaultSortOrder } from "@/components/torrents/torrentSortOptions"
import { getBackendSortField } from "@/lib/torrent-table/backend-sort-field"
import type { ColumnOrderState, ColumnVisibilityState } from "@tanstack/react-table"
import { useCallback, useMemo, useRef } from "react"
import type { TorrentTable } from "@/components/torrents/tanstackTableFeatures"

export interface UseCompactViewSortParams {
  table: TorrentTable
  // Included so the derived options/label recompute when columns change.
  columnVisibility: ColumnVisibilityState
  columnOrder: ColumnOrderState
  activeSortField: string
  activeSortOrder: "asc" | "desc"
  setSorting: (sorting: Array<{ id: string; desc: boolean }>) => void
  setLastUserAction: (action: { type: string; timestamp: number }) => void
}

/**
 * Owns the compact-view sort dropdown: the available options, the current label,
 * and the field/order change handlers (which also flag a user "sort" action).
 */
export function useCompactViewSort({
  table,
  columnVisibility,
  columnOrder,
  activeSortField,
  activeSortOrder,
  setSorting,
  setLastUserAction,
}: UseCompactViewSortParams) {
  // v9's useTable returns a new table object every render; read it through a
  // latest-ref so the memos and callbacks below keep stable identities.
  const tableRef = useRef(table)
  tableRef.current = table

  const resolveSortColumnId = useCallback((field: string): string => {
    const columns = tableRef.current.getAllLeafColumns()
    const directMatch = columns.find(column => column.id === field)
    if (directMatch) {
      return directMatch.id
    }

    const backendMatch = columns.find(column => getBackendSortField(column.id) === field)
    if (backendMatch) {
      return backendMatch.id
    }

    return field
    // Reads the table at call time through tableRef, so it needs no deps.
  }, [])

  const compactSortOptions = useMemo(() => {
    const columns = tableRef.current.getAllLeafColumns()
    const availableFields = new Set<string>()

    for (const column of columns) {
      availableFields.add(column.id)
      availableFields.add(getBackendSortField(column.id))
    }

    return TORRENT_SORT_OPTIONS.filter(option => availableFields.has(option.value))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [columnVisibility, columnOrder])

  const currentCompactSortLabel = useMemo(() => {
    const directOption = compactSortOptions.find(option => option.value === activeSortField)
    if (directOption) {
      return directOption.label
    }

    const columns = tableRef.current.getAllLeafColumns()
    const directColumn = columns.find(column => column.id === activeSortField)
    if (directColumn) {
      const meta = directColumn.columnDef.meta as { headerString?: string } | undefined
      if (meta?.headerString) {
        return meta.headerString
      }
      if (typeof directColumn.columnDef.header === "string") {
        return directColumn.columnDef.header
      }
      return directColumn.id
    }

    const backendColumn = columns.find(column => getBackendSortField(column.id) === activeSortField)
    if (backendColumn) {
      const meta = backendColumn.columnDef.meta as { headerString?: string } | undefined
      if (meta?.headerString) {
        return meta.headerString
      }
      if (typeof backendColumn.columnDef.header === "string") {
        return backendColumn.columnDef.header
      }
      return backendColumn.id
    }

    return activeSortField
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [compactSortOptions, activeSortField, columnVisibility, columnOrder])

  const handleCompactSortFieldChange = useCallback((value: TorrentSortOptionValue) => {
    if (activeSortField === value) {
      return
    }

    const columnId = resolveSortColumnId(value)
    const defaultOrder = getDefaultSortOrder(value)

    setSorting([{ id: columnId, desc: defaultOrder === "desc" }])
    setLastUserAction({
      type: "sort",
      timestamp: Date.now(),
    })
  }, [activeSortField, resolveSortColumnId, setSorting, setLastUserAction])

  const handleCompactSortOrderToggle = useCallback(() => {
    const columnId = resolveSortColumnId(activeSortField)
    const nextDesc = activeSortOrder === "asc"

    setSorting([{ id: columnId, desc: nextDesc }])
    setLastUserAction({
      type: "sort",
      timestamp: Date.now(),
    })
  }, [activeSortField, activeSortOrder, resolveSortColumnId, setSorting, setLastUserAction])

  return {
    compactSortOptions,
    currentCompactSortLabel,
    handleCompactSortFieldChange,
    handleCompactSortOrderToggle,
  }
}
