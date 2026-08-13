/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SpeedUnit } from "@/lib/speedUnits"
import { getRowBackgroundClass } from "@/lib/torrent-table/row-display"
import type { TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { cn } from "@/lib/utils"
import type { Torrent } from "@/types"
import {
  flexRender,
  type ColumnDef,
  type ColumnOrderState,
  type ColumnSizingState,
  type Row,
  type VisibilityState
} from "@tanstack/react-table"
import { memo } from "react"
import { TorrentContextMenu, type TorrentContextMenuProps } from "../TorrentContextMenu"
import type { TableViewMode } from "../TorrentTableColumns"
import { CompactRow } from "./CompactRow"

// Everything the per-row context menu needs except the row-specific fields.
// The parent memoizes ONE bundle for all rows, so the row comparator checks a
// single reference instead of ~30 individual menu props.
export type TorrentRowMenuProps = Omit<TorrentContextMenuProps, "children" | "torrent" | "isSelected">

// Display/config props shared by every compact row, memoized by the parent.
export interface CompactRowSharedProps {
  showCheckbox: boolean
  incognitoMode: boolean
  speedUnit: SpeedUnit
  supportsTrackerHealth: boolean
  trackerIcons?: Record<string, string>
  trackerCustomizationLookup: TrackerCustomizationLookup
  onCheckboxPointerDown: (event: React.PointerEvent<HTMLDivElement>) => void
  onCheckboxChange: (torrent: Torrent, rowId: string, checked: boolean) => void
}

export interface TorrentTableRowProps {
  row: Row<Torrent>
  virtualIndex: number
  virtualStart: number
  virtualSize: number
  isSelected: boolean
  isRowSelected: boolean
  desktopViewMode: TableViewMode
  minTableWidth: number
  // Spreadsheet theme only: render the row number in a left gutter cell.
  showRowGutter?: boolean
  // Comparison tokens: cells read column config lazily through `row`, so these
  // exist only to invalidate the memo when table-level display state changes
  // (column set, resize, visibility, order).
  columns: ColumnDef<Torrent>[]
  columnSizing: ColumnSizingState
  columnVisibility: VisibilityState
  columnOrder: ColumnOrderState
  menu: TorrentRowMenuProps
  compact: CompactRowSharedProps
  onRowClick: (event: React.MouseEvent, row: Row<Torrent>, isSelected: boolean, isRowSelected: boolean) => void
  onRowContextMenu: (row: Row<Torrent>, isRowSelected: boolean) => void
}

// TanStack Table rebuilds the Row wrapper whenever the data array changes, so
// comparing `row` by reference would defeat the memo on every stream tick. The
// render-relevant identity of a row is the underlying torrent object — stable
// for unchanged rows thanks to structural sharing in useTorrentsList — plus the
// row id. Every other prop, handlers included (the parent keeps them stable),
// is compared generically so a newly added prop is never silently ignored.
// eslint-disable-next-line react-refresh/only-export-components
export function torrentTableRowPropsAreEqual(
  prev: Readonly<TorrentTableRowProps>,
  next: Readonly<TorrentTableRowProps>
): boolean {
  if (prev.row.id !== next.row.id || prev.row.original !== next.row.original) {
    return false
  }

  for (const key of Object.keys(next) as Array<keyof TorrentTableRowProps>) {
    if (key === "row") {
      continue
    }
    if (!Object.is(prev[key], next[key])) {
      return false
    }
  }

  return true
}

export const TorrentTableRow = memo(function TorrentTableRow({
  row,
  virtualIndex,
  virtualStart,
  virtualSize,
  isSelected,
  isRowSelected,
  desktopViewMode,
  minTableWidth,
  showRowGutter,
  menu,
  compact,
  onRowClick,
  onRowContextMenu,
}: TorrentTableRowProps) {
  const torrent = row.original

  if (desktopViewMode === "compact") {
    return (
      <TorrentContextMenu {...menu} torrent={torrent} isSelected={isRowSelected}>
        <CompactRow
          torrent={torrent}
          rowId={row.id}
          rowIndex={virtualIndex}
          isSelected={isSelected}
          isRowSelected={isRowSelected}
          showCheckbox={compact.showCheckbox}
          onClick={(e) => onRowClick(e, row, isSelected, isRowSelected)}
          onContextMenu={() => onRowContextMenu(row, isRowSelected)}
          incognitoMode={compact.incognitoMode}
          speedUnit={compact.speedUnit}
          supportsTrackerHealth={compact.supportsTrackerHealth}
          trackerIcons={compact.trackerIcons}
          trackerCustomizationLookup={compact.trackerCustomizationLookup}
          onCheckboxPointerDown={compact.onCheckboxPointerDown}
          onCheckboxChange={compact.onCheckboxChange}
          style={{
            position: "absolute",
            top: 0,
            left: 0,
            width: "100%",
            height: `${virtualSize}px`,
            transform: `translateY(${virtualStart}px)`,
          }}
        />
      </TorrentContextMenu>
    )
  }

  return (
    <TorrentContextMenu {...menu} torrent={torrent} isSelected={isRowSelected}>
      <div
        className={`flex cursor-pointer hover:bg-accent/40 ${getRowBackgroundClass(isRowSelected, isSelected, virtualIndex)}`}
        style={{
          position: "absolute",
          top: 0,
          left: 0,
          minWidth: `${minTableWidth}px`,
          height: `${virtualSize}px`,
          transform: `translateY(${virtualStart}px)`,
        }}
        onClick={(e) => onRowClick(e, row, isSelected, isRowSelected)}
        onContextMenu={() => onRowContextMenu(row, isRowSelected)}
      >
        {showRowGutter && (
          <div className="ss-row-gutter" aria-hidden="true">{virtualIndex + 1}</div>
        )}
        {row.getVisibleCells().map(cell => {
          // Compact columns (tracker_icon, status_icon) use px-0 to match header
          const isCompactColumn = cell.column.id === "tracker_icon" || cell.column.id === "status_icon"
          const isSelectColumn = cell.column.id === "select"
          return (
            <div
              key={cell.id}
              data-torrent-column-measure={cell.column.id}
              style={{
                width: cell.column.getSize(),
                flexShrink: 0,
              }}
              className={cn(
                "flex items-center overflow-hidden min-w-0",
                // Select and compact columns are centered to match header
                (isSelectColumn || isCompactColumn) && "justify-center",
                isCompactColumn? (desktopViewMode === "dense" ? "px-0 py-0.5" : "px-0 py-2"): (desktopViewMode === "dense" ? "px-2 py-0.5" : "px-3 py-2")
              )}
            >
              {flexRender(
                cell.column.columnDef.cell,
                cell.getContext()
              )}
            </div>
          )
        })}
      </div>
    </TorrentContextMenu>
  )
}, torrentTableRowPropsAreEqual)
