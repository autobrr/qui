/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { getPeerFlagDetails } from "@/lib/torrent-peer-flags"
import { cn, copyTextToClipboard, formatBytes } from "@/lib/utils"
import { formatSpeedWithUnit, type SpeedUnit } from "@/lib/speedUnits"
import type { SortedPeer, TorrentPeer } from "@/types"
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingFn,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table"
import { SortIcon } from "@/components/ui/sort-icon"
import "flag-icons/css/flag-icons.min.css"
import { Ban, Copy, Loader2 } from "lucide-react"
import { memo, useMemo, useState } from "react"
import { toast } from "sonner"

interface PeersTableProps {
  peers: SortedPeer[] | undefined
  loading: boolean
  speedUnit: SpeedUnit
  showFlags: boolean
  incognitoMode: boolean
  onBanPeer?: (peer: TorrentPeer) => void
}

const columnHelper = createColumnHelper<SortedPeer>()

// Sorting function that pushes 0/null/undefined values to the bottom
const zeroLastSortingFn: SortingFn<SortedPeer> = (rowA, rowB, columnId) => {
  const a = (rowA.getValue(columnId) as number | undefined | null) ?? 0
  const b = (rowB.getValue(columnId) as number | undefined | null) ?? 0
  if (a === 0 && b !== 0) return 1
  if (b === 0 && a !== 0) return -1
  return a - b
}

export const PeersTable = memo(function PeersTable({
  peers,
  loading,
  speedUnit,
  showFlags,
  incognitoMode,
  onBanPeer,
}: PeersTableProps) {
  const [sorting, setSorting] = useState<SortingState>([{ id: "progress", desc: true }])

  const columns = useMemo(() => [
    columnHelper.accessor("country_code", {
      header: "",
      cell: (info) => {
        const code = info.getValue()?.toLowerCase()
        if (!code) return null
        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className={cn("fi", `fi-${code}`, "rounded-sm")} />
            </TooltipTrigger>
            <TooltipContent>
              <p className="text-xs">{info.row.original.country || code.toUpperCase()}</p>
            </TooltipContent>
          </Tooltip>
        )
      },
      size: 30,
      enableSorting: false,
    }),
    columnHelper.accessor((row) => `${row.ip}:${row.port}`, {
      id: "address",
      header: "IP:Port",
      cell: (info) => {
        const displayIp = incognitoMode ? "192.168.x.x" : info.row.original.ip
        const displayPort = incognitoMode ? "xxxxx" : info.row.original.port
        return (
          <span className="font-mono text-xs">
            {displayIp}:{displayPort}
          </span>
        )
      },
      size: 150,
    }),
    columnHelper.accessor("client", {
      header: "Client",
      cell: (info) => (
        <span className="truncate block max-w-[120px]" title={info.getValue()}>
          {info.getValue() || "-"}
        </span>
      ),
      size: 120,
    }),
    columnHelper.accessor("progress", {
      header: "Progress",
      cell: (info) => {
        const progress = info.getValue() * 100
        return (
          <div className="flex items-center gap-2">
            <Progress value={progress} className="h-1.5 w-16" />
            <span className="tabular-nums text-[10px] w-10">
              {progress.toFixed(1)}%
            </span>
          </div>
        )
      },
      size: 110,
    }),
    columnHelper.accessor("dl_speed", {
      header: "DL Speed",
      cell: (info) => (
        <span className="tabular-nums text-green-500">
          {formatSpeedWithUnit(info.getValue() || 0, speedUnit)}
        </span>
      ),
      size: 90,
      sortUndefined: "last",
      sortingFn: zeroLastSortingFn,
    }),
    columnHelper.accessor("up_speed", {
      header: "UL Speed",
      cell: (info) => (
        <span className="tabular-nums text-blue-500">
          {formatSpeedWithUnit(info.getValue() || 0, speedUnit)}
        </span>
      ),
      size: 90,
      sortUndefined: "last",
      sortingFn: zeroLastSortingFn,
    }),
    columnHelper.accessor("downloaded", {
      header: "Downloaded",
      cell: (info) => (
        <span className="tabular-nums">
          {formatBytes(info.getValue() || 0)}
        </span>
      ),
      size: 90,
      sortUndefined: "last",
      sortingFn: zeroLastSortingFn,
    }),
    columnHelper.accessor("uploaded", {
      header: "Uploaded",
      cell: (info) => (
        <span className="tabular-nums">
          {formatBytes(info.getValue() || 0)}
        </span>
      ),
      size: 90,
      sortUndefined: "last",
      sortingFn: zeroLastSortingFn,
    }),
    ...(showFlags ? [
      columnHelper.accessor("flags", {
        header: "Flags",
        cell: (info) => {
          const flags = info.getValue()
          if (!flags) return <span className="text-muted-foreground">-</span>
          const details = getPeerFlagDetails(flags, info.row.original.flags_desc)
          return (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="font-mono text-[10px] text-muted-foreground cursor-help">
                  {flags}
                </span>
              </TooltipTrigger>
              <TooltipContent side="left" className="max-w-[300px]">
                <div className="space-y-1 text-xs">
                  {details.map((d, i) => (
                    <div key={i}>
                      <span className="font-mono font-bold">{d.flag}</span>: {d.description}
                    </div>
                  ))}
                </div>
              </TooltipContent>
            </Tooltip>
          )
        },
        size: 60,
      }),
    ] : []),
  ], [speedUnit, showFlags, incognitoMode])

  const data = useMemo(() => peers || [], [peers])

  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  const handleCopyIp = (peer: SortedPeer) => {
    if (incognitoMode) return
    copyTextToClipboard(`${peer.ip}:${peer.port}`)
    toast.success("IP address copied to clipboard")
  }

  if (loading && !peers) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin" />
      </div>
    )
  }

  if (!peers || peers.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No peers connected
      </div>
    )
  }

  return (
    <ScrollArea className="h-full">
      <div className="min-w-[700px]">
        <table className="w-full text-xs">
          <thead className="sticky top-0 z-10 bg-background border-b">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th
                    key={header.id}
                    className={cn(
                      "px-2 py-2 text-left font-medium text-muted-foreground select-none",
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
              <ContextMenu key={row.id}>
                <ContextMenuTrigger asChild>
                  <tr className="border-b border-border/50 hover:bg-muted/30 cursor-default">
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className="px-2 py-1.5"
                        style={{ width: cell.column.getSize() }}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ContextMenuItem
                    onClick={() => handleCopyIp(row.original)}
                    disabled={incognitoMode}
                  >
                    <Copy className="h-3.5 w-3.5 mr-2" />
                    Copy IP Address
                  </ContextMenuItem>
                  {onBanPeer && (
                    <>
                      <ContextMenuSeparator />
                      <ContextMenuItem
                        onClick={() => onBanPeer(row.original)}
                        className="text-destructive focus:text-destructive"
                      >
                        <Ban className="h-3.5 w-3.5 mr-2" />
                        Ban Peer
                      </ContextMenuItem>
                    </>
                  )}
                </ContextMenuContent>
              </ContextMenu>
            ))}
          </tbody>
        </table>
      </div>
    </ScrollArea>
  )
})
