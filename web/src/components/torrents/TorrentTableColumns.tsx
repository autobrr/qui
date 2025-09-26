/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Progress } from "@/components/ui/progress"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import {
  getLinuxCategory,
  getLinuxHash,
  getLinuxIsoName,
  getLinuxRatio,
  getLinuxSavePath,
  getLinuxTags,
  getLinuxTracker
} from "@/lib/incognito"
import { formatSpeedWithUnit, type SpeedUnit } from "@/lib/speedUnits"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { formatBytes, formatDuration, getRatioColor } from "@/lib/utils"
import type { Torrent } from "@/types"
import type { ColumnDef } from "@tanstack/react-table"
import { ListOrdered } from "lucide-react"

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

function formatReannounce(seconds: number): string {
  if (seconds < 0) return "-"

  const minutes = Math.floor(seconds / 60)

  if (minutes < 1) {
    return "< 1m"
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
    onRowSelection?: (hash: string, checked: boolean, rowId?: string) => void
    isAllSelected?: boolean
    excludedFromSelectAll?: Set<string>
  },
  speedUnit: SpeedUnit = "bytes",
  formatTimestamp?: (timestamp: number) => string
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
                      selectionEnhancers.onRowSelection(rTorrent.hash, !!checked, r.id)
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
                  selectionEnhancers.onRowSelection(hash, !!checked, row.id)
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
        <div className="overflow-hidden whitespace-nowrap text-sm" title={displayName}>
          {displayName}
        </div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "size",
    header: "Size",
    cell: ({ row }) => <span className="text-sm overflow-hidden whitespace-nowrap">{formatBytes(row.original.size)}</span>,
    size: 85,
  },
  {
    accessorKey: "total_size",
    header: "Total Size",
    cell: ({ row }) => <span className="text-sm overflow-hidden whitespace-nowrap">{formatBytes(row.original.total_size)}</span>,
    size: 115,
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
    accessorKey: "num_seeds",
    header: "Seeds",
    cell: ({ row }) => {
      const connected = row.original.num_seeds >= 0 ? row.original.num_seeds : 0
      const total = row.original.num_complete >= 0 ? row.original.num_complete : 0
      if (total < 0 && connected < 0) return <span className="text-sm overflow-hidden whitespace-nowrap">-</span>
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {connected} ({total})
        </span>
      )
    },
    size: 85,
  },
  {
    accessorKey: "num_leechs",
    header: "Peers",
    cell: ({ row }) => {
      const connected = row.original.num_leechs >= 0 ? row.original.num_leechs : 0
      const total = row.original.num_incomplete >= 0 ? row.original.num_incomplete : 0
      if (total < 0 && connected < 0) return <span className="text-sm overflow-hidden whitespace-nowrap">-</span>
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">
          {connected} ({total})
        </span>
      )
    },
    size: 85,
  },
  {
    accessorKey: "dlspeed",
    header: "Down Speed",
    cell: ({ row }) => {
      const speed = row.original.dlspeed
      return <span className="text-sm overflow-hidden whitespace-nowrap">{speed === 0 ? "-" : formatSpeedWithUnit(speed, speedUnit)}</span>
    },
    size: calculateMinWidth("Down Speed"),
  },
  {
    accessorKey: "upspeed",
    header: "Up Speed",
    cell: ({ row }) => {
      const speed = row.original.upspeed
      return <span className="text-sm overflow-hidden whitespace-nowrap">{speed === 0 ? "-" : formatSpeedWithUnit(speed, speedUnit)}</span>
    },
    size: calculateMinWidth("Up Speed"),
  },
  {
    accessorKey: "eta",
    header: "ETA",
    cell: ({ row }) => <span className="text-sm overflow-hidden whitespace-nowrap">{formatEta(row.original.eta)}</span>,
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
          className="text-sm font-medium overflow-hidden whitespace-nowrap"
          style={{ color: colorVar }}
        >
          {displayRatio}
        </span>
      )
    },
    size: 80,
  },
  {
    accessorKey: "popularity",
    header: "Popularity",
    cell: ({ row }) => {
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {row.original.popularity.toFixed(2)}
        </div>
      )
    },
    size: 115,
  },
  {
    accessorKey: "category",
    header: "Category",
    cell: ({ row }) => {
      const displayCategory = incognitoMode ? getLinuxCategory(row.original.hash) : row.original.category
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm" title={displayCategory || ""}>
          {displayCategory || ""}
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
      const displayTags = Array.isArray(tags) ? tags.join(", ") : tags || ""
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm" title={displayTags}>
          {displayTags}
        </div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "added_on",
    header: "Added",
    cell: ({ row }) => {
      const addedOn = row.original.added_on
      if (!addedOn || addedOn === 0) {
        return "-"
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">{formatTimestamp ? formatTimestamp(addedOn) : new Date(addedOn * 1000).toLocaleString()}</div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "completion_on",
    header: "Completed On",
    cell: ({ row }) => {
      const completionOn = row.original.completion_on
      if (!completionOn || completionOn === 0) {
        return "-"
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">{formatTimestamp ? formatTimestamp(completionOn) : new Date(completionOn * 1000).toLocaleString()}</div>
      )
    },
    size: 200,
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
        <div className="overflow-hidden whitespace-nowrap text-sm" title={tracker}>
          {displayTracker || "-"}
        </div>
      )
    },
    size: 150,
  },
  {
    accessorKey: "dl_limit",
    header: "Down Limit",
    cell: ({ row }) => {
      const downLimit = row.original.dl_limit
      const displayDownLimit = downLimit === 0 ? "∞" : formatSpeedWithUnit(downLimit, speedUnit)

      return (
        <span
          className="text-sm font-medium overflow-hidden whitespace-nowrap"
        >
          {displayDownLimit}
        </span>
      )
    },
    size: calculateMinWidth("Down Limit", 24),
  },
  {
    accessorKey: "up_limit",
    header: "Up Limit",
    cell: ({ row }) => {
      const upLimit = row.original.up_limit
      const displayUpLimit = upLimit === 0 ? "∞" : formatSpeedWithUnit(upLimit, speedUnit)

      return (
        <span
          className="text-sm font-medium overflow-hidden whitespace-nowrap"
        >
          {displayUpLimit}
        </span>
      )
    },
    size: calculateMinWidth("Up Limit", 24),
  },
  {
    accessorKey: "downloaded",
    header: "Downloaded",
    cell: ({ row }) => {
      const downloaded = row.original.downloaded
      return <span className="text-sm overflow-hidden whitespace-nowrap">{downloaded === 0 ? "-" : formatBytes(downloaded)}</span>
    },
    size: calculateMinWidth("Downloaded"),
  },
  {
    accessorKey: "uploaded",
    header: "Uploaded",
    cell: ({ row }) => {
      const uploaded = row.original.uploaded
      return <span className="text-sm overflow-hidden whitespace-nowrap">{uploaded === 0 ? "-" : formatBytes(uploaded)}</span>
    },
    size: calculateMinWidth("Uploaded"),
  },
  {
    accessorKey: "downloaded_session",
    header: "Session Downloaded",
    cell: ({ row }) => {
      const sessionDownloaded = row.original.downloaded_session
      return <span className="text-sm overflow-hidden whitespace-nowrap">{sessionDownloaded === 0 ? "-" : formatBytes(sessionDownloaded)}</span>
    },
    size: calculateMinWidth("Session Downloaded"),
  },
  {
    accessorKey: "uploaded_session",
    header: "Session Uploaded",
    cell: ({ row }) => {
      const sessionUploaded = row.original.uploaded_session
      return <span className="text-sm overflow-hidden whitespace-nowrap">{sessionUploaded === 0 ? "-" : formatBytes(sessionUploaded)}</span>
    },
    size: calculateMinWidth("Session Uploaded"),
  },
  {
    accessorKey: "amount_left",
    header: "Remaining",
    cell: ({ row }) => {
      const amountLeft = row.original.amount_left
      return <span className="text-sm overflow-hidden whitespace-nowrap">{amountLeft === 0 ? "-" : formatBytes(amountLeft)}</span>
    },
    size: calculateMinWidth("Remaining"),
  },
  {
    accessorKey: "time_active",
    header: "Time Active",
    cell: ({ row }) => {
      const timeActive = row.original.time_active
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">{formatDuration(timeActive)}</span>
      )
    },
    size: 250,
  },
  {
    accessorKey: "seeding_time",
    header: "Seeding Time",
    cell: ({ row }) => {
      const timeSeeded = row.original.seeding_time
      return (
        <span className="text-sm overflow-hidden whitespace-nowrap">{formatDuration(timeSeeded)}</span>
      )
    },
    size: 250,
  },
  {
    accessorKey: "save_path",
    header: "Save Path",
    cell: ({ row }) => {
      const displayPath = incognitoMode ? getLinuxSavePath(row.original.hash) : row.original.save_path
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm" title={displayPath}>
          {displayPath}
        </div>
      )
    },
    size: 250,
  },
  {
    accessorKey: "completed",
    header: "Completed",
    cell: ({ row }) => {
      const completed = row.original.completed
      return <span className="text-sm overflow-hidden whitespace-nowrap">{completed === 0 ? "-" : formatBytes(completed)}</span>
    },
    size: calculateMinWidth("Completed"),
  },
  {
    accessorKey: "ratio_limit",
    header: "Ratio Limit",
    cell: ({ row }) => {
      const ratioLimit = row.original.ratio_limit
      const displayRatioLimit = ratioLimit === -2 ? "∞" : ratioLimit.toFixed(2)

      return (
        <span
          className="text-sm font-medium overflow-hidden whitespace-nowrap"
        >
          {displayRatioLimit}
        </span>
      )
    },
    size: calculateMinWidth("Ratio Limit", 24),
  },
  {
    accessorKey: "seen_complete",
    header: "Last Seen Complete",
    cell: ({ row }) => {
      const lastSeenComplete = row.original.seen_complete
      if (!lastSeenComplete || lastSeenComplete === 0) {
        return "-"
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">{formatTimestamp ? formatTimestamp(lastSeenComplete) : new Date(lastSeenComplete * 1000).toLocaleString()}</div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "last_activity",
    header: "Last Activity",
    cell: ({ row }) => {
      const lastActivity = row.original.last_activity
      if (!lastActivity || lastActivity === 0) {
        return "-"
      }

      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">{formatTimestamp ? formatTimestamp(lastActivity) : new Date(lastActivity * 1000).toLocaleString()}</div>
      )
    },
    size: 200,
  },
  {
    accessorKey: "availability",
    header: "Availability",
    cell: ({ row }) => {
      const availability = row.original.availability
      return <span className="text-sm overflow-hidden whitespace-nowrap">{availability.toFixed(3)}</span>
    },
    size: calculateMinWidth("Availability"),
  },
  // incomplete save path is not exposed by the API?
  {
    accessorKey: "infohash_v1",
    header: "Info Hash v1",
    cell: ({ row }) => {
      const infoHash = incognitoMode ? getLinuxHash(row.original.infohash_v1) : row.original.infohash_v1
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm" title={infoHash}>
          {infoHash || "-"}
        </div>
      )
    },
    size: 370,
  },
  {
    accessorKey: "infohash_v2",
    header: "Info Hash v2",
    cell: ({ row }) => {
      const infoHash = incognitoMode ? getLinuxHash(row.original.infohash_v2) : row.original.infohash_v2
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm" title={infoHash}>
          {infoHash || "-"}
        </div>
      )
    },
    size: 370,
  },
  {
    accessorKey: "reannounce",
    header: "Reannounce In",
    cell: ({ row }) => {
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {formatReannounce(row.original.reannounce)}
        </div>
      )
    },
    size: calculateMinWidth("Reannounce In"),
  },
  {
    accessorKey: "private",
    header: "Private",
    cell: ({ row }) => {
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm">
          {row.original.private ? "Yes" : "No"}
        </div>
      )
    },
    size: calculateMinWidth("Private"),
  },
]
