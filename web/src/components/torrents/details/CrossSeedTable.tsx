/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { SortIcon } from "@/components/ui/sort-icon"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { isHardlinkManaged, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { getLinuxFileName, getLinuxTracker } from "@/lib/incognito"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { cn, copyTextToClipboard, formatBytes } from "@/lib/utils"
import type { Instance } from "@/types"
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type SortingState,
} from "@tanstack/react-table"
import { Copy, Loader2, Trash2 } from "lucide-react"
import { memo, useMemo, useState } from "react"
import { toast } from "sonner"

interface CrossSeedTableProps {
  matches: CrossSeedTorrent[]
  loading: boolean
  incognitoMode: boolean
  selectedTorrents: Set<string>
  onToggleSelection: (key: string) => void
  onSelectAll: () => void
  onDeselectAll: () => void
  onDeleteMatches: () => void
  onDeleteCurrent: () => void
  instanceById: Map<number, Instance>
  onNavigateToTorrent?: (instanceId: number, torrentHash: string) => void
}

const columnHelper = createColumnHelper<CrossSeedTorrent>()

function getStatusInfo(match: CrossSeedTorrent): { label: string; variant: "default" | "secondary" | "destructive" | "outline"; className: string } {
  const trackerHealth = match.tracker_health ?? null
  let label = getStateLabel(match.state)
  let variant: "default" | "secondary" | "destructive" | "outline" = "outline"
  let className = ""

  if (trackerHealth === "unregistered") {
    return { label: "Unregistered", variant: "outline", className: "text-destructive border-destructive/40 bg-destructive/10" }
  } else if (trackerHealth === "tracker_down") {
    return { label: "Tracker Down", variant: "outline", className: "text-yellow-500 border-yellow-500/40 bg-yellow-500/10" }
  }

  if (match.state === "downloading" || match.state === "uploading") {
    variant = "default"
  } else if (
    match.state === "stalledDL" ||
    match.state === "stalledUP" ||
    match.state === "pausedDL" ||
    match.state === "pausedUP" ||
    match.state === "queuedDL" ||
    match.state === "queuedUP"
  ) {
    variant = "secondary"
  } else if (match.state === "error" || match.state === "missingFiles") {
    variant = "destructive"
  }

  return { label, variant, className }
}

function getMatchTypeLabel(matchType: string): { label: string; description: string } {
  switch (matchType) {
    case "content_path":
      return { label: "Content", description: "Same content location on disk" }
    case "name":
      return { label: "Name", description: "Same torrent name" }
    case "release":
      return { label: "Release", description: "Same release (matched by metadata)" }
    default:
      return { label: matchType, description: matchType }
  }
}

export const CrossSeedTable = memo(function CrossSeedTable({
  matches,
  loading,
  incognitoMode,
  selectedTorrents,
  onToggleSelection,
  onSelectAll,
  onDeselectAll,
  onDeleteMatches,
  onDeleteCurrent,
  instanceById,
  onNavigateToTorrent,
}: CrossSeedTableProps) {
  const [sorting, setSorting] = useState<SortingState>([])
  const { data: trackerIcons } = useTrackerIcons()
  const { data: trackerCustomizations } = useTrackerCustomizations()

  const trackerDisplayNames = useMemo(() => {
    const map = new Map<string, string>()
    for (const custom of trackerCustomizations ?? []) {
      for (const domain of custom.domains) {
        map.set(domain.toLowerCase(), custom.displayName)
      }
    }
    return map
  }, [trackerCustomizations])

  const columns = useMemo(() => [
    columnHelper.display({
      id: "select",
      header: () => null,
      cell: ({ row }) => {
        const key = `${row.original.instanceId}-${row.original.hash}`
        return (
          <Checkbox
            checked={selectedTorrents.has(key)}
            onCheckedChange={() => onToggleSelection(key)}
            className="h-3.5 w-3.5"
          />
        )
      },
      size: 30,
    }),
    columnHelper.accessor("name", {
      header: "Name",
      cell: (info) => {
        const name = incognitoMode
          ? getLinuxFileName(info.row.original.hash, 0)
          : info.getValue()
        const isHardlink = isHardlinkManaged(info.row.original, instanceById.get(info.row.original.instanceId))
        return (
          <div className="flex items-center gap-1.5 min-w-0">
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="truncate block max-w-[220px]">{name}</span>
              </TooltipTrigger>
              <TooltipContent side="top" className="max-w-[400px]">
                <p className="text-xs break-all">{name}</p>
              </TooltipContent>
            </Tooltip>
            {isHardlink && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="outline" className="text-[9px] px-1 py-0 shrink-0 text-blue-500 border-blue-500/40">
                    Hardlink
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>
                  <p className="text-xs">Files stored in hardlink directory (separate from source)</p>
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        )
      },
      size: 300,
    }),
    columnHelper.accessor("instanceName", {
      header: "Instance",
      cell: (info) => (
        <span className="truncate block">{info.getValue()}</span>
      ),
      size: 70,
    }),
    columnHelper.accessor("matchType", {
      header: "Match",
      cell: (info) => {
        const { label, description } = getMatchTypeLabel(info.getValue() as string)
        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="text-muted-foreground cursor-help">{label}</span>
            </TooltipTrigger>
            <TooltipContent>
              <p className="text-xs">{description}</p>
            </TooltipContent>
          </Tooltip>
        )
      },
      size: 70,
    }),
    columnHelper.accessor("tracker", {
      header: "Tracker",
      cell: (info) => {
        const tracker = info.getValue()
        if (!tracker) return <span className="text-muted-foreground">-</span>

        let hostname = tracker
        try {
          hostname = new URL(tracker).hostname
        } catch {
          // Keep original if parsing fails
        }

        const displayName = incognitoMode
          ? getLinuxTracker(`${info.row.original.hash}-0`)
          : trackerDisplayNames.get(hostname.toLowerCase()) || hostname

        // In incognito mode, pass obfuscated key to prevent real tracker icon lookup
        const iconKey = incognitoMode ? displayName : hostname

        return (
          <div className="flex items-center gap-1.5">
            <TrackerIconImage tracker={iconKey} trackerIcons={trackerIcons} />
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="truncate block max-w-[100px] text-muted-foreground">
                  {displayName}
                </span>
              </TooltipTrigger>
              <TooltipContent>
                <p className="text-xs">{displayName}</p>
              </TooltipContent>
            </Tooltip>
          </div>
        )
      },
      size: 130,
    }),
    columnHelper.accessor("state", {
      header: "Status",
      cell: (info) => {
        const { label, variant, className } = getStatusInfo(info.row.original)
        return (
          <Badge variant={variant} className={cn("text-[10px] px-1.5 py-0", className)}>
            {label}
          </Badge>
        )
      },
      size: 90,
    }),
    columnHelper.accessor("progress", {
      header: "Progress",
      cell: (info) => {
        const progress = info.getValue() * 100
        const isComplete = progress === 100
        return (
          <div className="flex items-center gap-1.5">
            <Progress value={progress} className="h-1.5 w-14" />
            <span className={cn("tabular-nums text-[10px]", isComplete ? "text-green-500" : "text-muted-foreground")}>
              {progress.toFixed(0)}%
            </span>
          </div>
        )
      },
      size: 85,
    }),
    columnHelper.accessor("size", {
      header: "Size",
      cell: (info) => (
        <span className="tabular-nums">{formatBytes(info.getValue())}</span>
      ),
      size: 80,
    }),
    columnHelper.accessor("save_path", {
      header: "Save Path",
      cell: (info) => {
        const path = info.getValue()
        if (!path) return <span className="text-muted-foreground">-</span>
        return (
          <div className="flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="truncate block max-w-[100px] text-muted-foreground font-mono text-[10px]">
                  {path}
                </span>
              </TooltipTrigger>
              <TooltipContent side="top" className="max-w-[400px]">
                <p className="text-xs font-mono break-all">{path}</p>
              </TooltipContent>
            </Tooltip>
            <Button
              variant="ghost"
              size="icon"
              className="h-5 w-5 shrink-0"
              onClick={(e) => {
                e.stopPropagation()
                copyTextToClipboard(path).then(() => {
                  toast.success("Save path copied")
                }).catch(() => {
                  toast.error("Failed to copy")
                })
              }}
            >
              <Copy className="h-3 w-3" />
            </Button>
          </div>
        )
      },
      size: 130,
    }),
  ], [incognitoMode, selectedTorrents, onToggleSelection, trackerDisplayNames, trackerIcons, instanceById])

  const table = useReactTable({
    data: matches,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  if (loading && matches.length === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin" />
      </div>
    )
  }

  if (matches.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No matching torrents found on other instances
      </div>
    )
  }

  return (
    <div className="h-full flex flex-col min-h-0">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-3 py-1.5 border-b text-xs gap-2">
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">
            {selectedTorrents.size > 0
              ? `${selectedTorrents.size} of ${matches.length} selected`
              : `${matches.length} match${matches.length !== 1 ? "es" : ""}`}
          </span>
          {loading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
        </div>
        <div className="flex items-center gap-1">
          {selectedTorrents.size > 0 ? (
            <>
              <Button
                variant="ghost"
                size="sm"
                className="h-6 text-xs"
                onClick={onDeselectAll}
              >
                Deselect
              </Button>
              <Button
                variant="destructive"
                size="sm"
                className="h-6 text-xs"
                onClick={onDeleteMatches}
              >
                <Trash2 className="h-3 w-3 mr-1" />
                Delete ({selectedTorrents.size})
              </Button>
            </>
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-6 text-xs"
              onClick={onSelectAll}
            >
              Select All
            </Button>
          )}
          <Button
            variant="destructive"
            size="sm"
            className="h-6 text-xs"
            onClick={onDeleteCurrent}
          >
            <Trash2 className="h-3 w-3 mr-1" />
            Delete This
          </Button>
        </div>
      </div>

      <ScrollArea className="flex-1 min-h-0">
        <div className="min-w-[800px]">
          <table className="w-full text-xs">
            <thead className="sticky top-0 z-10 bg-background border-b">
              {table.getHeaderGroups().map((headerGroup) => (
                <tr key={headerGroup.id}>
                  {headerGroup.headers.map((header) => (
                    <th
                      key={header.id}
                      className={cn(
                        "px-2 py-1.5 text-left font-medium text-muted-foreground select-none whitespace-nowrap",
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
                  className={cn(
                    "border-b border-border/50 hover:bg-muted/30",
                    onNavigateToTorrent && "cursor-pointer"
                  )}
                  onClick={(e) => {
                    // Don't navigate if clicking checkbox or button
                    if ((e.target as HTMLElement).closest('button, [role="checkbox"]')) return
                    if (onNavigateToTorrent) {
                      onNavigateToTorrent(row.original.instanceId, row.original.hash)
                    }
                  }}
                >
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
              ))}
            </tbody>
          </table>
        </div>
      </ScrollArea>
    </div>
  )
})
