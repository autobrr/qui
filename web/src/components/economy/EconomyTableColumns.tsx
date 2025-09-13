/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Progress } from "@/components/ui/progress"
import { cn, formatBytes } from "@/lib/utils"
import type { EconomyScore } from "@/types"
import type { Column, ColumnDef } from "@tanstack/react-table"
import { AlertTriangle, ArrowDown, ArrowUp, ArrowUpDown, Package, TrendingUp } from "lucide-react"

// Helper to create sortable header - defined inside the column factory to avoid export issues
function createSortableHeader(column: Column<EconomyScore>, children: React.ReactNode) {
  return (
    <Button
      variant="ghost"
      size="sm"
      className="-ml-3 h-8 data-[state=open]:bg-accent"
      onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
    >
      {children}
      {column.getIsSorted() === "asc" ? (
        <ArrowUp className="ml-2 h-4 w-4" />
      ) : column.getIsSorted() === "desc" ? (
        <ArrowDown className="ml-2 h-4 w-4" />
      ) : (
        <ArrowUpDown className="ml-2 h-4 w-4" />
      )}
    </Button>
  )
}

// Format age in days to human readable
const formatAge = (days: number): string => {
  if (days === 0) return "Today"
  if (days === 1) return "1 day"
  if (days < 7) return `${days} days`
  if (days < 30) return `${Math.floor(days / 7)}w`
  if (days < 365) return `${Math.floor(days / 30)}mo`
  return `${Math.floor(days / 365)}y`
}

// Get state badge color
const getStateBadgeVariant = (state: string): "default" | "secondary" | "outline" | "destructive" => {
  const stateMap: Record<string, "default" | "secondary" | "outline" | "destructive"> = {
    downloading: "outline",
    seeding: "default",
    paused: "secondary",
    queued: "outline",
    error: "destructive",
    stalled: "outline",
    checking: "outline",
    moving: "outline",
  }
  return stateMap[state.toLowerCase()] || "default"
}

// Get economy score color
const getScoreColor = (score: number): string => {
  if (score >= 80) return "text-green-500"
  if (score >= 60) return "text-blue-500"
  if (score >= 40) return "text-yellow-500"
  if (score >= 20) return "text-orange-500"
  return "text-red-500"
}

export const createEconomyColumns = (): ColumnDef<EconomyScore>[] => [
  {
    id: "select",
    header: ({ table }) => (
      <Checkbox
        checked={
          table.getIsAllPageRowsSelected() ||
          (table.getIsSomePageRowsSelected() && "indeterminate")
        }
        onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
        aria-label="Select all"
        className="translate-y-[2px]"
      />
    ),
    cell: ({ row }) => (
      <Checkbox
        checked={row.getIsSelected()}
        onCheckedChange={(value) => row.toggleSelected(!!value)}
        aria-label="Select row"
        className="translate-y-[2px]"
      />
    ),
    enableSorting: false,
    enableHiding: false,
    size: 40,
  },
  {
    accessorKey: "name",
    header: ({ column }) => createSortableHeader(column, "Name"),
    cell: ({ row }) => {
      const name = row.original.name
      const isDuplicate = row.original.duplicates && row.original.duplicates.length > 0
      const isLastSeed = row.original.seeds === 0

      return (
        <div className="flex items-center gap-2 min-w-[200px] max-w-[400px]">
          <span className="truncate" title={name}>
            {name}
          </span>
          {isDuplicate && (
            <Badge variant="secondary" className="text-xs">
              Dup
            </Badge>
          )}
          {isLastSeed && (
            <span title="Last seed">
              <AlertTriangle className="h-4 w-4 text-red-500" />
            </span>
          )}
        </div>
      )
    },
  },
  {
    accessorKey: "size",
    header: ({ column }) => createSortableHeader(column, "Size"),
    cell: ({ row }) => (
      <span className="font-mono text-sm">{formatBytes(row.getValue("size"))}</span>
    ),
    size: 100,
  },
  {
    accessorKey: "economyScore",
    header: ({ column }) => createSortableHeader(column, "Score"),
    cell: ({ row }) => {
      const score = row.getValue("economyScore") as number
      return (
        <div className="flex items-center gap-2">
          <Progress value={score} className="w-16 h-2" />
          <span className={cn("font-semibold text-sm", getScoreColor(score))}>
            {score.toFixed(1)}
          </span>
        </div>
      )
    },
    size: 120,
  },
  {
    accessorKey: "seeds",
    header: ({ column }) => createSortableHeader(column, "Seeds"),
    cell: ({ row }) => {
      const seeds = row.getValue("seeds") as number
      const peers = row.original.peers
      return (
        <div className="flex items-center gap-1 font-mono text-sm">
          <span className={seeds === 0 ? "text-red-500 font-bold" : seeds < 5 ? "text-orange-500" : ""}>
            {seeds}
          </span>
          <span className="text-muted-foreground">/</span>
          <span className="text-muted-foreground">{peers}</span>
        </div>
      )
    },
    size: 80,
  },
  {
    accessorKey: "ratio",
    header: ({ column }) => createSortableHeader(column, "Ratio"),
    cell: ({ row }) => {
      const ratio = row.getValue("ratio") as number
      return (
        <span
          className={cn(
            "font-mono text-sm",
            ratio >= 1 ? "text-green-500" : ratio >= 0.5 ? "text-yellow-500" : "text-red-500"
          )}
        >
          {ratio.toFixed(2)}
        </span>
      )
    },
    size: 80,
  },
  {
    accessorKey: "age",
    header: ({ column }) => createSortableHeader(column, "Age"),
    cell: ({ row }) => {
      const age = row.getValue("age") as number
      return <span className="text-sm">{formatAge(age)}</span>
    },
    size: 80,
  },
  {
    accessorKey: "storageValue",
    header: ({ column }) => createSortableHeader(column, "Storage Value"),
    cell: ({ row }) => {
      const value = row.getValue("storageValue") as number
      return <span className="font-mono text-sm">{value.toFixed(2)} GB</span>
    },
    size: 120,
  },
  {
    accessorKey: "rarityBonus",
    header: ({ column }) => createSortableHeader(column, "Rarity"),
    cell: ({ row }) => {
      const bonus = row.getValue("rarityBonus") as number
      let icon = null
      let color = ""

      if (bonus >= 10) {
        icon = <Package className="h-4 w-4" />
        color = "text-red-500"
      } else if (bonus >= 5) {
        icon = <Package className="h-4 w-4" />
        color = "text-orange-500"
      } else if (bonus >= 2) {
        icon = <TrendingUp className="h-4 w-4" />
        color = "text-yellow-500"
      }

      return (
        <div className={cn("flex items-center gap-1", color)}>
          {icon}
          <span className="text-sm">+{bonus.toFixed(1)}</span>
        </div>
      )
    },
    size: 100,
  },
  {
    accessorKey: "reviewPriority",
    header: ({ column }) => createSortableHeader(column, "Priority"),
    cell: ({ row }) => {
      const priority = row.getValue("reviewPriority") as number
      const color = priority < 30 ? "text-red-500" : priority < 50 ? "text-orange-500" : ""
      return (
        <span className={cn("font-mono text-sm", color)}>
          {priority.toFixed(1)}
        </span>
      )
    },
    size: 100,
  },
  {
    accessorKey: "state",
    header: ({ column }) => createSortableHeader(column, "State"),
    cell: ({ row }) => {
      const state = row.getValue("state") as string
      return (
        <Badge variant={getStateBadgeVariant(state)} className="text-xs">
          {state}
        </Badge>
      )
    },
    size: 100,
  },
  {
    accessorKey: "category",
    header: ({ column }) => createSortableHeader(column, "Category"),
    cell: ({ row }) => {
      const category = row.getValue("category") as string
      return category ? (
        <Badge variant="outline" className="text-xs">
          {category}
        </Badge>
      ) : (
        <span className="text-muted-foreground text-xs">-</span>
      )
    },
    size: 120,
  },
  {
    accessorKey: "tracker",
    header: ({ column }) => createSortableHeader(column, "Tracker"),
    cell: ({ row }) => {
      const tracker = row.getValue("tracker") as string
      // Extract domain from tracker URL
      const domain = tracker ? new URL(tracker).hostname.replace("www.", "") : ""
      return (
        <span className="text-xs text-muted-foreground truncate max-w-[150px]" title={tracker}>
          {domain || "-"}
        </span>
      )
    },
    size: 150,
  },
]