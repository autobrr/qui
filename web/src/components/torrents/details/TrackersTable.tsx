/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { containsLinks, renderTextWithLinks } from "@/lib/linkUtils"
import { getTrackerStatusBadge } from "@/lib/tracker-utils"
import { cn } from "@/lib/utils"
import type { TorrentTracker } from "@/types"
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table"
import { SortIcon } from "@/components/ui/sort-icon"
import { Loader2 } from "lucide-react"
import { memo, useMemo, useState } from "react"
import { TrackerContextMenu } from "./TrackerContextMenu"

interface TrackersTableProps {
  trackers: TorrentTracker[] | undefined
  loading: boolean
  incognitoMode: boolean
  onEditTracker?: (tracker: TorrentTracker) => void
  supportsTrackerEditing?: boolean
}

const columnHelper = createColumnHelper<TorrentTracker>()

export const TrackersTable = memo(function TrackersTable({
  trackers,
  loading,
  incognitoMode,
  onEditTracker,
  supportsTrackerEditing = false,
}: TrackersTableProps) {
  // Default sort by status with disabled at bottom
  const [sorting, setSorting] = useState<SortingState>([{ id: "status", desc: false }])
  const { data: trackerIcons } = useTrackerIcons()

  const columns = useMemo(() => [
    columnHelper.accessor("status", {
      header: "Status",
      cell: (info) => getTrackerStatusBadge(info.getValue(), true),
      size: 90,
      // Custom sort: disabled (0) always at bottom
      sortingFn: (rowA, rowB) => {
        const a = rowA.original.status
        const b = rowB.original.status
        if (a === 0 && b !== 0) return 1
        if (b === 0 && a !== 0) return -1
        return a - b
      },
    }),
    columnHelper.accessor("url", {
      header: "Tracker",
      cell: (info) => {
        const url = info.getValue()
        const fullUrl = incognitoMode ? "https://tracker.example.com/announce" : url

        // Extract hostname for display, fall back to full value for non-URLs (DHT, PeX, LSD)
        let hostname = ""
        let isValidUrl = false
        if (incognitoMode) {
          hostname = "tracker.example.com"
          isValidUrl = true
        } else {
          try {
            hostname = new URL(url).hostname
            isValidUrl = true
          } catch {
            hostname = url
          }
        }

        return (
          <div className="flex items-center gap-1.5 whitespace-nowrap">
            <TrackerIconImage tracker={hostname} trackerIcons={trackerIcons} />
            {isValidUrl ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="font-mono text-xs">
                    {hostname}
                  </span>
                </TooltipTrigger>
                <TooltipContent side="top" className="max-w-[500px]">
                  <p className="font-mono text-xs break-all">{fullUrl}</p>
                </TooltipContent>
              </Tooltip>
            ) : (
              <span className="font-mono text-xs">{hostname}</span>
            )}
          </div>
        )
      },
    }),
    columnHelper.accessor("msg", {
      header: "Message",
      meta: { fullWidth: true },
      cell: (info) => {
        const msg = info.getValue()
        if (!msg) return <span className="text-muted-foreground">-</span>

        const hasLinks = containsLinks(msg)

        // Render message with clickable links, no truncation - table will scroll
        return (
          <span className="whitespace-nowrap text-muted-foreground [&_a]:text-primary [&_a]:hover:underline">
            {hasLinks ? renderTextWithLinks(msg) : msg}
          </span>
        )
      },
    }),
    columnHelper.accessor("num_seeds", {
      header: "Seeds",
      cell: (info) => <span className="tabular-nums">{info.getValue()}</span>,
      size: 70,
    }),
    columnHelper.accessor("num_peers", {
      header: "Peers",
      cell: (info) => <span className="tabular-nums">{info.getValue()}</span>,
      size: 70,
    }),
    columnHelper.accessor("num_leeches", {
      header: "Leeches",
      cell: (info) => <span className="tabular-nums">{info.getValue()}</span>,
      size: 80,
    }),
    columnHelper.accessor("num_downloaded", {
      header: "DLs",
      cell: (info) => <span className="tabular-nums">{info.getValue()}</span>,
      size: 60,
    }),
  ], [incognitoMode, trackerIcons])

  const data = useMemo(() => trackers || [], [trackers])

  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  if (loading && !trackers) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin" />
      </div>
    )
  }

  if (!trackers || trackers.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No trackers found
      </div>
    )
  }

  return (
    <ScrollArea className="h-full">
      <div className="w-max min-w-full">
        <table className="text-xs">
          <thead className="sticky top-0 z-10 bg-background border-b">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th
                    key={header.id}
                    className={cn(
                      "px-3 py-2 text-left font-medium text-muted-foreground select-none whitespace-nowrap",
                      header.column.getCanSort() && "cursor-pointer hover:bg-muted/50"
                    )}
                    style={
                      (header.column.columnDef.meta as { fullWidth?: boolean })?.fullWidth
                        ? { width: "100%" }
                        : header.column.columnDef.size
                          ? { width: header.getSize() }
                          : undefined
                    }
                    onClick={header.column.getToggleSortingHandler()}
                  >
                    <div className="flex items-center gap-1">
                      {flexRender(header.column.columnDef.header, header.getContext())}
                      {header.column.getCanSort() && (
                        <SortIcon sorted={header.column.getIsSorted()} />
                      )}
                    </div>
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((row) => {
              const tracker = row.original

              return (
                <TrackerContextMenu
                  key={row.id}
                  tracker={tracker}
                  onEditTracker={onEditTracker}
                  supportsTrackerEditing={supportsTrackerEditing}
                >
                  <tr className="border-b border-border/50 hover:bg-muted/30">
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className="px-3 py-2"
                        style={
                          (cell.column.columnDef.meta as { fullWidth?: boolean })?.fullWidth
                            ? { width: "100%" }
                            : cell.column.columnDef.size
                              ? { width: cell.column.getSize() }
                              : undefined
                        }
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                </TrackerContextMenu>
              )
            })}
          </tbody>
        </table>
      </div>
      <ScrollBar orientation="horizontal" />
    </ScrollArea>
  )
})
