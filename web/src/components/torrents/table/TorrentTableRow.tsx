/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SpeedUnit } from "@/lib/speedUnits"
import { getRowBackgroundClass } from "@/lib/torrent-table/row-display"
import type { TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import type { Torrent } from "@/types"
import {
  flexRender,
  type ColumnOrderState,
  type ColumnSizingState,
  type ColumnVisibilityState
} from "@tanstack/react-table"
import { AlertTriangle, ChevronRight } from "lucide-react"
import { memo } from "react"
import { useTranslation } from "react-i18next"
import { TorrentContextMenu, type TorrentContextMenuProps } from "../TorrentContextMenu"
import type { TableViewMode } from "../TorrentTableColumns"
import type { TorrentRow, TorrentTableColumnDef } from "../tanstackTableFeatures"
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
  /** Only on a collapsed group whose first-sorted member is not the top row. */
  sortedTorrent?: Torrent
  sortedColumnId?: string
  /** A member under the top row of an open group. */
  nestedCopy?: boolean
  onToggleCopyGroup: (id: string) => void
}

interface CopyGroupControlsProps {
  id: string
  open: boolean
  copies: number
  hiddenProblem: boolean
  onToggle: (id: string) => void
}

// The chevron and the icon keep their size; in a narrow cell the badge clips,
// so the name of the chevron also gives the count.
function CopyGroupControls({ id, open, copies, hiddenProblem, onToggle }: CopyGroupControlsProps) {
  const { t } = useTranslation("torrents")
  const problemLabel = t("copyGroups.hiddenProblem")
  return (
    <span className="flex shrink-0 items-center gap-1 mr-1">
      <button
        type="button"
        className="flex shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:text-foreground"
        aria-expanded={open}
        aria-label={open ? t("copyGroups.collapse", { count: copies }) : t("copyGroups.expand", { count: copies })}
        onClick={(event) => {
          event.stopPropagation()
          onToggle(id)
        }}
      >
        <ChevronRight className={cn("h-4 w-4 transition-transform", open && "rotate-90")} />
      </button>
      {hiddenProblem && (
        <span role="img" aria-label={problemLabel} title={problemLabel} className="flex shrink-0">
          <AlertTriangle className="h-4 w-4 text-destructive" />
        </span>
      )}
      {!open && (
        <Badge variant="secondary" className="px-1 py-0 text-xs">
          {t("copyGroups.badge", { hidden: copies })}
        </Badge>
      )}
    </span>
  )
}

// Matches the chevron and its gap, so the copies of an open group look nested.
const COPY_INDENT = <span className="w-5 shrink-0" aria-hidden="true" />

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
  sortedTorrent,
  sortedColumnId,
  nestedCopy = false,
  onToggleCopyGroup,
}: TorrentTableRowProps) {
  const torrent = row.original
  const groupControls = copyGroupId !== undefined && (
    <CopyGroupControls
      id={copyGroupId}
      open={copyGroupOpen}
      copies={copies}
      hiddenProblem={hiddenCopyProblem}
      onToggle={onToggleCopyGroup}
    />
  )

  if (desktopViewMode === "compact") {
    // A Stacked row has no cells: it shows the field of the sorted column from the first-sorted member.
    const displayTorrent = sortedTorrent && sortedColumnId && sortedColumnId in torrent ? { ...torrent, [sortedColumnId]: sortedTorrent[sortedColumnId as keyof Torrent] } : torrent
    return (
      <TorrentContextMenu {...menu} torrent={torrent} isSelected={isRowSelected}>
        <CompactRow
          torrent={torrent}
          displayTorrent={displayTorrent}
          groupControls={groupControls}
          nested={nestedCopy}
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
  const groupCellId = groupControls || nestedCopy ? (cells.find(cell => cell.column.id === "name") ?? cells.find(cell => cell.column.id !== "select"))?.id : undefined

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
                (isSelectColumn || isCompactColumn) && "justify-center",
                desktopViewMode === "dense" ? "px-2 py-0.5" : "px-3 py-2"
              )}
            >
              {cell.id === groupCellId && (nestedCopy ? COPY_INDENT : groupControls)}
              {flexRender(
                cell.column.columnDef.cell,
                sortedTorrent && cell.column.id === sortedColumnId ? sortedCellContext(cell.getContext(), sortedTorrent) : cell.getContext()
              )}
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
