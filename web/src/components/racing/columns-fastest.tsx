/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { formatBytes, getRatioColor } from "@/lib/utils"
import type { RacingTorrent } from "@/types"
import type { ColumnDef } from "@tanstack/react-table"
import { ArrowUpDown, Clock } from "lucide-react"

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${hours}h ${minutes}m`
}

function formatDate(dateString: string): string {
  return new Date(dateString).toLocaleString()
}

export const columnsFastest: ColumnDef<RacingTorrent>[] = [
  {
    accessorKey: "name",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Name
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      const torrent = row.original
      return (
        <div className="max-w-[300px]">
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="truncate font-medium cursor-default">{torrent.name}</div>
            </TooltipTrigger>
            <TooltipContent side="top" className="max-w-[400px]">
              <p className="break-words">{torrent.name}</p>
            </TooltipContent>
          </Tooltip>
          <div className="flex gap-2 mt-1">
            {torrent.category && (
              <Badge variant="outline" className="text-xs pointer-events-none">
                {torrent.category}
              </Badge>
            )}
            {torrent.tags && (
              <Badge variant="secondary" className="text-xs pointer-events-none">
                {torrent.tags}
              </Badge>
            )}
          </div>
        </div>
      )
    },
  },
  {
    accessorKey: "instanceName",
    header: "Instance",
    cell: ({ row }) => {
      const torrent = row.original
      return (
        <Badge variant="outline" className="text-xs">
          {torrent.instanceName}
        </Badge>
      )
    },
  },
  {
    accessorKey: "size",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Size
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => formatBytes(row.getValue("size")),
  },
  {
    accessorKey: "trackerDomain",
    header: "Tracker",
    cell: ({ row }) => {
      const torrent = row.original
      return (
        <span className="text-sm pointer-events-none">
          {torrent.trackerDomain}
        </span>
      )
    },
  },
  {
    accessorKey: "ratio",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Ratio
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      const ratio = row.getValue<number>("ratio")
      return (
        <span className={`font-semibold pointer-events-none ${getRatioColor(ratio)}`}>
          {ratio.toFixed(2)}
        </span>
      )
    },
  },
  {
    accessorKey: "completionTime",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Time
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      const completionTime = row.getValue<number | undefined>("completionTime")
      return completionTime !== undefined && completionTime !== null ? (
        <div className="flex items-center gap-1 pointer-events-none">
          <Clock className="h-3 w-3" />
          {formatDuration(completionTime)}
        </div>
      ) : (
        <span className="text-muted-foreground pointer-events-none">-</span>
      )
    },
  },
  {
    accessorKey: "addedOn",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Date
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      const torrent = row.original
      return (
        <div className="text-xs text-muted-foreground pointer-events-none">
          <div>{formatDate(torrent.addedOn)}</div>
          {torrent.completedOn && (
            <div className="text-green-600 dark:text-green-400">
              Completed: {formatDate(torrent.completedOn)}
            </div>
          )}
        </div>
      )
    },
  },
]