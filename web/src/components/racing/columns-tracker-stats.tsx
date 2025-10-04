/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { getRatioColor } from "@/lib/utils"
import type { ColumnDef } from "@tanstack/react-table"
import { ArrowUpDown } from "lucide-react"

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${hours}h ${minutes}m`
}

export interface TrackerStatRow {
  tracker: string
  totalTorrents: number
  completedTorrents: number
  averageRatio: number
  averageCompletionTime?: number
  instanceId: number
  instanceName: string
}

export const columnsTrackerStats: ColumnDef<TrackerStatRow>[] = [
  {
    accessorKey: "tracker",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Tracker
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      return (
        <span className="text-sm font-medium">
          {row.getValue("tracker")}
        </span>
      )
    },
  },
  {
    accessorKey: "instanceName",
    header: "Instance",
    cell: ({ row }) => {
      return (
        <span className="text-sm text-muted-foreground">
          {row.getValue("instanceName")}
        </span>
      )
    },
  },
  {
    accessorKey: "totalTorrents",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Total Torrents
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      return <span className="font-medium">{row.getValue("totalTorrents")}</span>
    },
  },
  {
    accessorKey: "completedTorrents",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Completed
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      return <span className="font-medium">{row.getValue("completedTorrents")}</span>
    },
  },
  {
    accessorKey: "averageRatio",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Average Ratio
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      const ratio = row.getValue<number>("averageRatio")
      return (
        <span className={`font-medium ${getRatioColor(ratio)}`}>
          {ratio.toFixed(2)}
        </span>
      )
    },
  },
  {
    accessorKey: "averageCompletionTime",
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
          className="-ml-4 h-8 data-[state=open]:bg-accent"
        >
          Avg Completion Time
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: ({ row }) => {
      const time = row.getValue<number | undefined>("averageCompletionTime")
      return time !== undefined && time !== null ? (
        <span className="font-medium">{formatDuration(time)}</span>
      ) : (
        <span className="text-muted-foreground">-</span>
      )
    },
  },
]