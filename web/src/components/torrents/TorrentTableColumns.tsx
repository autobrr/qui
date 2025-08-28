/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnDef } from "@tanstack/react-table"
import { Progress } from "@/components/ui/progress"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { ListOrdered } from "lucide-react"
import type { Torrent } from "@/types"
import {
  getLinuxIsoName,
  getLinuxCategory,
  getLinuxTags,
  getLinuxSavePath,
  getLinuxTracker,
  getLinuxRatio
} from "@/lib/incognito"
import { formatBytes, formatSpeed, getRatioColor } from "@/lib/utils"
import { getStateLabel } from "@/lib/torrent-state-utils"

function formatEta(seconds: number): string {
  if (seconds === 8640000) return "∞"
  if (seconds < 0) return ""

  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)

  if (hours > 24) {
    const days = Math.floor(hours / 24)
    return `${days}d ${hours % 24}h`
  }

  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }

  return `${minutes}m`
}

// Calculate minimum column width based on header text
function calculateMinWidth(text: string, padding: number = 48): number {
  const charWidth = 7.5
  const extraPadding = 20
  return Math.max(60, Math.ceil(text.length * charWidth) + padding + extraPadding)
}

export const createColumns = (
  incognitoMode: boolean,
  selectionEnhancers?: {
    shiftPressedRef: { current: boolean }
    lastSelectedIndexRef: { current: number | null }
    customSelectAll?: {
      onSelectAll: (checked: boolean) => void
      isAllSelected: boolean
      isIndeterminate: boolean
    }
    onRowSelection?: (hash: string, checked: boolean) => void
    isAllSelected?: boolean
    excludedFromSelectAll?: Set<string>
  }
): ColumnDef<Torrent>[] => [
  {
    id: "select",
    header: ({ table }) => (
      <div className="flex items-center justify-center p-1 -m-1">
        <Checkbox
          checked={selectionEnhancers?.customSelectAll?.isIndeterminate ? "indeterminate" : selectionEnhancers?.customSelectAll?.isAllSelected || false}
          onCheckedChange={(checked) => {
            if (selectionEnhancers?.customSelectAll?.onSelectAll) {
              selectionEnhancers.customSelectAll.onSelectAll(!!checked)
            } else {
              // Fallback to default behavior
              table.toggleAllPageRowsSelected(!!checked)
            }
          }}
          aria-label="Select all"
          className="hover:border-ring cursor-pointer transition-colors"
        />
      </div>
    ),
    cell: ({ row, table }) => {
      const torrent = row.original
      const hash = torrent.hash
      
      // Determine if row is selected based on custom logic
      const isRowSelected = (() => {
        if (selectionEnhancers?.isAllSelected) {
          // In "select all" mode, row is selected unless excluded
          return !selectionEnhancers.excludedFromSelectAll?.has(hash)
        } else {
          // Regular mode, use table's selection state
          return row.getIsSelected()
        }
      })()
      
      return (
        <div className="flex items-center justify-center p-1 -m-1">
          <Checkbox
            checked={isRowSelected}
            onPointerDown={(e) => {
              if (selectionEnhancers) {
                selectionEnhancers.shiftPressedRef.current = e.shiftKey
              }
            }}
            onCheckedChange={(checked: boolean | "indeterminate") => {
              const isShift = selectionEnhancers?.shiftPressedRef.current === true
              const allRows = table.getRowModel().rows
              const currentIndex = allRows.findIndex(r => r.id === row.id)

              if (isShift && selectionEnhancers?.lastSelectedIndexRef.current !== null) {
                const start = Math.min(selectionEnhancers.lastSelectedIndexRef.current!, currentIndex)
                const end = Math.max(selectionEnhancers.lastSelectedIndexRef.current!, currentIndex)

                // For shift selection, use custom handler if available, otherwise fallback
                if (selectionEnhancers?.onRowSelection) {
                  for (let i = start; i <= end; i++) {
                    const r = allRows[i]
                    if (r) {
                      const rTorrent = r.original as Torrent
                      selectionEnhancers.onRowSelection(rTorrent.hash, !!checked)
                    }
                  }
                } else {
                  table.setRowSelection((prev: Record<string, boolean>) => {
                    const next: Record<string, boolean> = { ...prev }
                    for (let i = start; i <= end; i++) {
                      const r = allRows[i]
                      if (r) {
                        next[r.id] = !!checked
                      }
                    }
                    return next
                  })
                }
              } else {
                // Single row selection
                if (selectionEnhancers?.onRowSelection) {
                  selectionEnhancers.onRowSelection(hash, !!checked)
                } else {
                  row.toggleSelected(!!checked)
                }
              }

              if (selectionEnhancers) {
                selectionEnhancers.lastSelectedIndexRef.current = currentIndex
                selectionEnhancers.shiftPressedRef.current = false
              }
            }}
            aria-label="Select row"
            className="hover:border-ring cursor-pointer transition-colors"
          />
        </div>
      )
    },
    size: 40,
    enableResizing: false,
  },
  {
    accessorKey: "priority",
    header: () => (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="flex items-center justify-center">
            <ListOrdered className="h-4 w-4" />
          </div>
        </TooltipTrigger>
        <TooltipContent>Priority</TooltipContent>
      </Tooltip>
    ),
    meta: {
      headerString: "Priority",
    },
    cell: ({ row }) => {
      const priority = row.original.priority
      const state = row.original.state
      const isQueued = state === "queuedDL" || state === "queuedUP"
      
      if (priority === 0 && !isQueued) {
        return <span className="text-sm text-muted-foreground text-center block">-</span>
      }
      
      if (isQueued) {
        const queueType = state === "queuedDL" ? "DL" : "UP"
        const badgeVariant = state === "queuedDL" ? "secondary" : "outline"
        return (
          <div className="flex items-center justify-center gap-1">
            <Badge variant={badgeVariant} className="text-xs px-1 py-0">
              Q{priority || "?"}
            </Badge>
            <span className="text-xs text-muted-foreground">{queueType}</span>
          </div>
        )
      }
      
      return <span className="text-sm font-medium text-center block">{priority}</span>
    },
    size: 65,
  },
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => {
      const displayName = incognitoMode ? getLinuxIsoName(row.original.hash) : row.original.name
      return (
        <div className="truncate text-sm" title={displayName}>
          {displayName}
        </div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "size",
    header: "Size",
    cell: ({ row }) => <span className="text-sm truncate">{formatBytes(row.original.size)}</span>,
    size: 85,
  },
  {
    accessorKey: "progress",
    header: "Progress",
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Progress value={row.original.progress * 100} className="w-20" />
        <span className="text-xs text-muted-foreground">
          {Math.round(row.original.progress * 100)}%
        </span>
      </div>
    ),
    size: 120,
  },
  {
    accessorKey: "state",
    header: "Status",
    cell: ({ row }) => {
      const state = row.original.state
      const priority = row.original.priority
      const label = getStateLabel(state)
      const isQueued = state === "queuedDL" || state === "queuedUP"
      
      const variant = 
        state === "downloading" ? "default" :state === "stalledDL" ? "secondary" :state === "uploading" ? "default" :state === "stalledUP" ? "secondary" :state === "pausedDL" || state === "pausedUP" ? "secondary" :state === "queuedDL" || state === "queuedUP" ? "secondary" :state === "error" || state === "missingFiles" ? "destructive" :"outline"
      
      if (isQueued && priority > 0) {
        return (
          <div className="flex items-center gap-1">
            <Badge variant={variant} className="text-xs">
              {label}
            </Badge>
            <span className="text-xs text-muted-foreground">#{priority}</span>
          </div>
        )
      }
      
      return <Badge variant={variant} className="text-xs">{label}</Badge>
    },
    size: 130,
  },
  {
    accessorKey: "dlspeed",
    header: "Down Speed",
    cell: ({ row }) => <span className="text-sm truncate">{formatSpeed(row.original.dlspeed)}</span>,
    size: calculateMinWidth("Down Speed"),
  },
  {
    accessorKey: "upspeed",
    header: "Up Speed",
    cell: ({ row }) => <span className="text-sm truncate">{formatSpeed(row.original.upspeed)}</span>,
    size: calculateMinWidth("Up Speed"),
  },
  {
    accessorKey: "eta",
    header: "ETA",
    cell: ({ row }) => <span className="text-sm truncate">{formatEta(row.original.eta)}</span>,
    size: 80,
  },
  {
    accessorKey: "ratio",
    header: "Ratio",
    cell: ({ row }) => {
      const ratio = incognitoMode ? getLinuxRatio(row.original.hash) : row.original.ratio
      const displayRatio = ratio === -1 ? "∞" : ratio.toFixed(2)
      const colorVar = getRatioColor(ratio)
      
      return (
        <span 
          className="text-sm font-medium" 
          style={{ color: colorVar }}
        >
          {displayRatio}
        </span>
      )
    },
    size: 80,
  },
  {
    accessorKey: "added_on",
    header: "Added",
    cell: ({ row }) => {
      const addedOn = row.original.added_on
      if (!addedOn || addedOn === 0) {
        return "-"
      }
      const date = new Date(addedOn * 1000)
      const month = date.getMonth() + 1
      const day = date.getDate()
      const year = date.getFullYear()
      const hours = date.getHours()
      const minutes = date.getMinutes()
      const seconds = date.getSeconds()
      const ampm = hours >= 12 ? "PM" : "AM"
      const displayHours = hours % 12 || 12
      
      return (
        <div className="whitespace-nowrap text-sm">
          {month}/{day}/{year}, {displayHours}:{minutes.toString().padStart(2, "0")}:{seconds.toString().padStart(2, "0")} {ampm}
        </div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "category",
    header: "Category",
    cell: ({ row }) => {
      const displayCategory = incognitoMode ? getLinuxCategory(row.original.hash) : row.original.category
      return (
        <div className="truncate text-sm" title={displayCategory || "-"}>
          {displayCategory || "-"}
        </div>
      )
    },
    size: 150,
  },
  {
    accessorKey: "tags",
    header: "Tags",
    cell: ({ row }) => {
      const tags = incognitoMode ? getLinuxTags(row.original.hash) : row.original.tags
      const displayTags = Array.isArray(tags) ? tags.join(", ") : tags || "-"
      return (
        <div className="truncate text-sm" title={displayTags}>
          {displayTags}
        </div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "downloaded",
    header: "Downloaded",
    cell: ({ row }) => <span className="text-sm truncate">{formatBytes(row.original.downloaded)}</span>,
    size: calculateMinWidth("Downloaded"),
  },
  {
    accessorKey: "uploaded",
    header: "Uploaded",
    cell: ({ row }) => <span className="text-sm truncate">{formatBytes(row.original.uploaded)}</span>,
    size: calculateMinWidth("Uploaded"),
  },
  {
    accessorKey: "save_path",
    header: "Save Path",
    cell: ({ row }) => {
      const displayPath = incognitoMode ? getLinuxSavePath(row.original.hash) : row.original.save_path
      return (
        <div className="truncate text-sm" title={displayPath}>
          {displayPath}
        </div>
      )
    },
    size: 250,
  },
  {
    accessorKey: "tracker",
    header: "Tracker",
    cell: ({ row }) => {
      const tracker = incognitoMode ? getLinuxTracker(row.original.hash) : row.original.tracker
      let displayTracker = tracker
      try {
        if (tracker && tracker.includes("://")) {
          const url = new URL(tracker)
          displayTracker = url.hostname
        }
      } catch {
        // ignore
      }
      return (
        <div className="truncate text-sm" title={tracker}>
          {displayTracker || "-"}
        </div>
      )
    },
    size: 150,
  },
]


