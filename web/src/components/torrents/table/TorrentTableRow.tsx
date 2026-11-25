/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SpeedUnit } from "@/lib/speedUnits"
import { getBackendSortField } from "@/lib/torrent-table/backend-sort-field"
import { themeArea } from "@/lib/theme-areas"
import { withSortedField } from "@/lib/copy-groups"
import { getLinuxIsoName } from "@/lib/incognito"
import { centersSelectCheckbox, getCopyGroupAccentClass, getRowBackgroundClass, type CopyProblem } from "@/lib/torrent-table/row-display"
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

export type TorrentRowMenuProps = Omit<TorrentContextMenuProps, "children" | "torrent" | "isSelected">

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
  showRowGutter?: boolean
  columns: TorrentTableColumnDef[]
  columnSizing: ColumnSizingState
  columnVisibility: ColumnVisibilityState
  columnOrder: ColumnOrderState
  menu: TorrentRowMenuProps
  compact: CompactRowSharedProps
  onRowClick: (event: React.MouseEvent, row: TorrentRow, isSelected: boolean, isRowSelected: boolean) => void
  onRowContextMenu: (row: TorrentRow, isRowSelected: boolean) => void
  copyGroupId?: string
  copyGroupOpen?: boolean
  copies?: number
  hiddenCopyProblem?: CopyProblem
  hiddenCopyTicks?: number
  sortedTorrent?: Torrent
  sortedColumnId?: string
  nestedCopy?: boolean
  onToggleCopyGroup: (id: string) => void
}

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
  hiddenCopyProblem,
  hiddenCopyTicks = 0,
  sortedTorrent,
  sortedColumnId,
  nestedCopy = false,
  onToggleCopyGroup,
}: TorrentTableRowProps) {
  const torrent = row.original
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
  const pillCellId = copyGroupId === undefined ? undefined : (cells.find(cell => cell.column.id === "name")
    ?? cells.find(cell => cell.column.id !== "status_icon" && cell.column.id !== "tracker_icon"))?.id

  return (
    <TorrentContextMenu {...menu} torrent={torrent} isSelected={isRowSelected}>
      <div
        {...themeArea("torrent-row")}
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

function sortedCellContext<C extends { row: TorrentRow }>(context: C, sortedTorrent: Torrent): C {
  return { ...context, row: Object.create(context.row, { original: { value: sortedTorrent } }) as TorrentRow }
}
