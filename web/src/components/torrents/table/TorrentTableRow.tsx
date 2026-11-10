/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SpeedUnit } from "@/lib/speedUnits"
import { getBackendSortField } from "@/lib/torrent-table/backend-sort-field"
import { withSortedField } from "@/lib/copy-groups"
import { getLinuxIsoName } from "@/lib/incognito"
import { centersSelectCheckbox, getCopyGroupAccentClass, getRowBackgroundClass } from "@/lib/torrent-table/row-display"
import type { TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { cn } from "@/lib/utils"
import type { Torrent } from "@/types"
import {
  flexRender,
  type ColumnOrderState,
  type ColumnSizingState,
  type ColumnVisibilityState
} from "@tanstack/react-table"
import { memo } from "react"
import { TorrentContextMenu, type TorrentContextMenuProps } from "../TorrentContextMenu"
import type { TableViewMode } from "../TorrentTableColumns"
import type { TorrentRow, TorrentTableColumnDef } from "../tanstackTableFeatures"
import { CompactRow } from "./CompactRow"
import { CopyGroupPill, type CopyGroupPillSize } from "./CopyGroupPill"

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
  row: TorrentRow
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
  columns: TorrentTableColumnDef[]
  columnSizing: ColumnSizingState
  columnVisibility: ColumnVisibilityState
  columnOrder: ColumnOrderState
  menu: TorrentRowMenuProps
  compact: CompactRowSharedProps
  onRowClick: (event: React.MouseEvent, row: TorrentRow, isSelected: boolean, isRowSelected: boolean) => void
  onRowContextMenu: (row: TorrentRow, isRowSelected: boolean) => void
  // Copy groups. Primitives and stable objects only, so the row memo sees a
  // change in a hidden copy, which does not change the object of the top row.
  /** Only on the top row of a Copy group with two or more loaded members. */
  copyGroupId?: string
  copyGroupOpen?: boolean
  /** The members other than the top row. */
  copies?: number
  /** Only on a collapsed group: a hidden copy has a problem. */
  hiddenCopyProblem?: boolean
  /** Only on a collapsed group outside Select all mode: the ticked copies that it hides. */
  hiddenCopyTicks?: number
  /** Only on a collapsed group whose first-sorted member is not the top row. */
  sortedTorrent?: Torrent
  sortedColumnId?: string
  /** A member under the top row of an open group. */
  nestedCopy?: boolean
  onToggleCopyGroup: (id: string) => void
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
  copyGroupId,
  copyGroupOpen = false,
  copies = 0,
  hiddenCopyProblem = false,
  hiddenCopyTicks = 0,
  sortedTorrent,
  sortedColumnId,
  nestedCopy = false,
  onToggleCopyGroup,
}: TorrentTableRowProps) {
  const torrent = row.original
  // The pill names the torrent whose name the row shows, so a screen reader
  // hears the same name, also in incognito mode, where the hash picks the name.
  const pill = (size: CopyGroupPillSize, shownTorrent: Torrent) => copyGroupId !== undefined && (
    <CopyGroupPill
      id={copyGroupId}
      open={copyGroupOpen}
      copies={copies}
      hiddenProblem={hiddenCopyProblem}
      hiddenTicks={hiddenCopyTicks}
      name={compact.incognitoMode ? getLinuxIsoName(shownTorrent.hash) : shownTorrent.name}
      size={size}
      onToggle={onToggleCopyGroup}
    />
  )
  const groupAccent = (copyGroupOpen || nestedCopy) && getCopyGroupAccentClass(copyGroupOpen)

  if (desktopViewMode === "compact") {
    // A Stacked row has no cells: it shows the sorted field of the first-sorted member.
    const displayTorrent = withSortedField(torrent, sortedTorrent, sortedColumnId && getBackendSortField(sortedColumnId))
    return (
      <TorrentContextMenu {...menu} torrent={torrent} isSelected={isRowSelected}>
        <CompactRow
          torrent={torrent}
          displayTorrent={displayTorrent}
          groupPill={pill("stacked", displayTorrent)}
          groupAccentClass={groupAccent || undefined}
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

  const cells = row.getVisibleCells()
  // The pill sits at the end of the Name cell. With Name hidden it sits in the
  // first cell that is not an icon column, which are too narrow for it. That is
  // the select cell, which the table widens for it, unless select is hidden too.
  const pillCellId = copyGroupId === undefined ? undefined : (cells.find(cell => cell.column.id === "name")
    ?? cells.find(cell => cell.column.id !== "status_icon" && cell.column.id !== "tracker_icon"))?.id

  return (
    <TorrentContextMenu {...menu} torrent={torrent} isSelected={isRowSelected}>
      <div
        className={cn("flex cursor-pointer hover:bg-accent/40", getRowBackgroundClass(isRowSelected, isSelected, virtualIndex), groupAccent)}
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
        {cells.map(cell => {
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
                (isCompactColumn || (isSelectColumn && centersSelectCheckbox(cell.column.getSize()))) && "justify-center",
                desktopViewMode === "dense" ? "px-2 py-0.5" : "px-3 py-2"
              )}
            >
              {flexRender(
                cell.column.columnDef.cell,
                sortedTorrent && cell.column.id === sortedColumnId ? sortedCellContext(cell.getContext(), sortedTorrent) : cell.getContext()
              )}
              {cell.id === pillCellId && pill(desktopViewMode === "dense" ? "dense" : "normal", sortedTorrent && sortedColumnId === "name" ? sortedTorrent : torrent)}
            </div>
          )
        })}
      </div>
    </TorrentContextMenu>
  )
}, torrentTableRowPropsAreEqual)

// The cells read row.original; only this cell reads the first-sorted member.
// Rows are prototype objects, so the copy keeps their methods.
function sortedCellContext<C extends { row: TorrentRow }>(context: C, sortedTorrent: Torrent): C {
  return { ...context, row: Object.create(context.row, { original: { value: sortedTorrent } }) as TorrentRow }
}
