/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
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

interface TrackersTableProps {
  trackers: TorrentTracker[] | undefined
  loading: boolean
  incognitoMode: boolean
}

const columnHelper = createColumnHelper<TorrentTracker>()

function getStatusBadge(status: number) {
  switch (status) {
    case 0:
      return <Badge variant="secondary" className="text-[10px] px-1.5 py-0">Disabled</Badge>
    case 1:
      return <Badge variant="secondary" className="text-[10px] px-1.5 py-0">Not contacted</Badge>
    case 2:
      return <Badge variant="default" className="text-[10px] px-1.5 py-0 bg-green-600">Working</Badge>
    case 3:
      return <Badge variant="default" className="text-[10px] px-1.5 py-0 bg-blue-600">Updating</Badge>
    case 4:
      return <Badge variant="destructive" className="text-[10px] px-1.5 py-0">Error</Badge>
    default:
      return <Badge variant="outline" className="text-[10px] px-1.5 py-0">Unknown</Badge>
  }
}

export const TrackersTable = memo(function TrackersTable({
  trackers,
  loading,
  incognitoMode,
}: TrackersTableProps) {
  const [sorting, setSorting] = useState<SortingState>([])
  const { data: trackerIcons } = useTrackerIcons()

  const columns = useMemo(() => [
    columnHelper.accessor("status", {
      header: "Status",
      cell: (info) => getStatusBadge(info.getValue()),
      size: 90,
    }),
    columnHelper.accessor("url", {
      header: "URL",
      cell: (info) => {
        const url = info.getValue()
        const displayUrl = incognitoMode ? "https://tracker.example.com/announce" : url

        let hostname = ""
        try {
          hostname = new URL(url).hostname
        } catch {
          hostname = url
        }

        return (
          <div className="flex items-center gap-1.5">
            <TrackerIconImage tracker={hostname} trackerIcons={trackerIcons} />
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="font-mono text-xs truncate block max-w-[500px]">
                  {displayUrl}
                </span>
              </TooltipTrigger>
              <TooltipContent side="top" className="max-w-[400px]">
                <p className="font-mono text-xs break-all">{displayUrl}</p>
              </TooltipContent>
            </Tooltip>
          </div>
        )
      },
      size: 520,
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
      size: 70,
    }),
    columnHelper.accessor("num_downloaded", {
      header: "Downloaded",
      cell: (info) => <span className="tabular-nums">{info.getValue()}</span>,
      size: 90,
    }),
    columnHelper.accessor("msg", {
      header: "Message",
      cell: (info) => {
        const msg = info.getValue()
        if (!msg) return <span className="text-muted-foreground">-</span>
        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="truncate block max-w-[200px] text-muted-foreground">
                {msg}
              </span>
            </TooltipTrigger>
            <TooltipContent side="top" className="max-w-[400px]">
              <p className="text-xs break-all">{msg}</p>
            </TooltipContent>
          </Tooltip>
        )
      },
      size: 200,
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
      <div className="min-w-[600px]">
        <table className="w-full text-xs">
          <thead className="sticky top-0 z-10 bg-background border-b">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th
                    key={header.id}
                    className={cn(
                      "px-3 py-2 text-left font-medium text-muted-foreground select-none",
                      header.column.getCanSort() && "cursor-pointer hover:bg-muted/50"
                    )}
                    style={{ width: header.getSize() }}
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
            {table.getRowModel().rows.map((row) => (
              <tr
                key={row.id}
                className="border-b border-border/50 hover:bg-muted/30"
              >
                {row.getVisibleCells().map((cell) => (
                  <td
                    key={cell.id}
                    className="px-3 py-2"
                    style={{ width: cell.column.getSize() }}
                  >
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </ScrollArea>
  )
})
