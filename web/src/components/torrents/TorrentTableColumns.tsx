/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import {
  extractTrackerHost,
  resolveTrackerDisplay,
  type TrackerCustomizationLookup
} from "@/lib/tracker-customizations"
import { resolveTrackerIconSrc } from "@/lib/tracker-icons"
import { cn, formatBytes, formatDuration, getRatioColor } from "@/lib/utils"
import type { AppPreferences, CrossInstanceTorrent, Torrent } from "@/types"
import type { CellContext, ColumnDef, HeaderContext } from "@tanstack/react-table"
import {
  AlertCircle,
  ArrowDownAZ,
  ArrowDownZA,
  CheckCircle2,
  Download,
  Globe,
  ListOrdered,
  MoveRight,
  PlayCircle,
  RotateCw,
  StopCircle,
  Upload,
  XCircle
} from "lucide-react"
import { memo, useEffect, useState } from "react"

function formatEta(seconds: number): string {
  if (seconds === 8640000) return "∞"
  if (seconds < 0) return "-"

  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.floor(seconds % 60)

  if (hours > 24) {
    const days = Math.floor(hours / 24)
    return `${days}d ${hours % 24}h`
  }

  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }

  if (minutes > 0) {
    return `${minutes}m ${secs}s`
  }

  return `${secs}s`
}

function formatReannounce(seconds: number): string {
  // Negative values mean "never" or "not applicable"
  if (seconds < 0) return "-"

  // Zero means "now" (just announced or about to announce)
  if (seconds === 0) return "now"

  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)

  if (minutes < 1) {
    return "< 1m"
  }

  if (hours < 1) {
    return `${minutes}m`
  }

  const remainingMinutes = minutes % 60
  if (remainingMinutes === 0) {
    return `${hours}h`
  }

  return `${hours}h ${remainingMinutes}m`
}

// Calculate minimum column width based on header text
function calculateMinWidth(text: string, padding: number = 48): number {
  const charWidth = 7.5
  const extraPadding = 20
  return Math.max(60, Math.ceil(text.length * charWidth) + padding + extraPadding)
}

interface TrackerIconCellProps {
  title: string
  fallback: string
  src: string | null
}

// eslint-disable-next-line react-refresh/only-export-components
const TrackerIconCell = memo(({ title, fallback, src }: TrackerIconCellProps) => {
  const [hasError, setHasError] = useState(false)

  useEffect(() => {
    setHasError(false)
  }, [src])

  return (
    <div className="flex h-full w-full items-center justify-center" title={title}>
      <div className="flex h-4 w-4 items-center justify-center rounded-sm border border-border/40 bg-muted text-[10px] font-medium uppercase leading-none">
        {src && !hasError ? (
          <img
            src={src}
            alt=""
            className="h-full w-full rounded-[2px] object-cover"
            draggable={false}
            decoding="async"
            onError={() => setHasError(true)}
          />
        ) : (
          <span aria-hidden="true">{fallback}</span>
        )}
      </div>
    </div>
  )
})

const getTrackerDisplayMeta = (tracker?: string) => {
  const host = extractTrackerHost(tracker)
  if (!host) {
    return {
      host: "",
      fallback: "#",
      title: "",
    }
  }

  const fallbackLetter = host.charAt(0).toUpperCase()

  return {
    host,
    fallback: fallbackLetter,
    title: host,
  }
}

TrackerIconCell.displayName = "TrackerIconCell"

const STATUS_SORT_ORDER: Record<string, number> = {
  downloading: 20,
  metaDL: 21,
  forcedDL: 22,
  allocating: 23,
  checkingDL: 24,
  queuedDL: 25,
  stalledDL: 30,
  uploading: 40,
  forcedUP: 41,
  stoppedDL: 42,
  stoppedUP: 43,
  queuedUP: 44,
  stalledUP: 45,
  pausedDL: 50,
  pausedUP: 51,
  checkingUP: 60,
  checkingResumeData: 61,
  moving: 70,
  error: 80,
  missingFiles: 81,
}

const getTrackerAwareStatusLabel = (torrent: Torrent, supportsTrackerHealth: boolean): string => {
  if (supportsTrackerHealth) {
    if (torrent.tracker_health === "unregistered") {
      return "Unregistered"
    }
    if (torrent.tracker_health === "tracker_down") {
      return "Tracker Down"
    }
  }

  return getStateLabel(torrent.state)
}

const getTrackerAwareStatusSortMeta = (torrent: Torrent, supportsTrackerHealth: boolean) => {
  if (supportsTrackerHealth) {
    if (torrent.tracker_health === "unregistered") {
      return {
        priority: 0,
        statePriority: -1,
        label: "Unregistered",
      }
    }
    if (torrent.tracker_health === "tracker_down") {
      return {
        priority: 1,
        statePriority: -1,
        label: "Tracker Down",
      }
    }
  }

  const statePriority = STATUS_SORT_ORDER[torrent.state] ?? 1000

  return {
    priority: 10,
    statePriority,
    label: getStateLabel(torrent.state),
  }
}

const getStatusIcon = (state: string, trackerHealth?: string | null, supportsTrackerHealth: boolean = true) => {
  // Check tracker health first if supported
  if (supportsTrackerHealth && trackerHealth) {
    if (trackerHealth === "unregistered") {
      return XCircle
    }
    if (trackerHealth === "tracker_down") {
      return AlertCircle
    }
  }

  // Map states to icons matching FilterSidebar.tsx
  switch (state) {
    case "downloading":
    case "metaDL":
    case "forcedDL":
    case "queuedDL":
    case "stalledDL":
    case "stalled_downloading":
      return Download
    case "uploading":
    case "forcedUP":
    case "queuedUP":
    case "stalledUP":
    case "stalled_uploading":
      return Upload
    case "pausedUP":
    case "stoppedUP":
      return CheckCircle2
    case "pausedDL":
    case "stopped":
    case "stoppedDL":
    case "inactive":
      return StopCircle
    case "checkingDL":
    case "checkingUP":
    case "checkingResumeData":
    case "checking":
      return RotateCw
    case "allocating":
    case "moving":
      return MoveRight
    case "error":
    case "missingFiles":
      return XCircle
    case "active":
    case "running":
      return PlayCircle
    case "stalled":
      return AlertCircle
    default:
      // For completed state or any other state
      if (state.includes("complet")) {
        return CheckCircle2
      }
      return CheckCircle2
  }
}

type StatusBadgeVariant = "default" | "secondary" | "destructive" | "outline"

const compareTrackerAwareStatus = (torrentA: Torrent, torrentB: Torrent, supportsTrackerHealth: boolean): number => {
  const metaA = getTrackerAwareStatusSortMeta(torrentA, supportsTrackerHealth)
  const metaB = getTrackerAwareStatusSortMeta(torrentB, supportsTrackerHealth)

  if (metaA.priority !== metaB.priority) {
    return metaA.priority - metaB.priority
  }

  if (metaA.statePriority !== metaB.statePriority) {
    return metaA.statePriority - metaB.statePriority
  }

  const labelComparison = metaA.label.localeCompare(metaB.label, undefined, { sensitivity: "accent", numeric: false })
  if (labelComparison !== 0) {
    return labelComparison
  }

  const stateA = torrentA.state || ""
  const stateB = torrentB.state || ""

  const stateComparison = stateA.localeCompare(stateB, undefined, { sensitivity: "accent", numeric: false })
  if (stateComparison !== 0) {
    return stateComparison
  }

  const nameA = torrentA.name || ""
  const nameB = torrentB.name || ""

  return nameA.localeCompare(nameB, undefined, { sensitivity: "accent", numeric: false })
}

const getStatusBadgeMeta = (
  torrent: Torrent,
  supportsTrackerHealth: boolean
): {
  label: string
  variant: StatusBadgeVariant
  className: string
  iconClass: string
} => {
  const state = torrent.state
  const baseLabel = getTrackerAwareStatusLabel(torrent, supportsTrackerHealth)
  const trackerHealth = torrent.tracker_health ?? null

  let badgeVariant: StatusBadgeVariant = "outline"
  if (state === "downloading" || state === "uploading") {
    badgeVariant = "default"
  } else if (
    state === "stalledDL" ||
    state === "stalledUP" ||
    state === "pausedDL" ||
    state === "pausedUP" ||
    state === "queuedDL" ||
    state === "queuedUP"
  ) {
    badgeVariant = "secondary"
  } else if (state === "error" || state === "missingFiles") {
    badgeVariant = "destructive"
  }

  let badgeClass = ""
  let label = baseLabel
  let iconClass = "text-muted-foreground"

  if (supportsTrackerHealth) {
    if (trackerHealth === "tracker_down") {
      label = "Tracker Down"
      badgeVariant = "outline"
      badgeClass = "text-yellow-500 border-yellow-500/40 bg-yellow-500/10"
      iconClass = "text-yellow-500"
    } else if (trackerHealth === "unregistered") {
      label = "Unregistered"
      badgeVariant = "outline"
      badgeClass = "text-destructive border-destructive/40 bg-destructive/10"
      iconClass = "text-destructive"
    }
  }

  if (badgeClass === "") {
    switch (badgeVariant) {
      case "default":
        iconClass = "text-primary"
        break
      case "secondary":
        iconClass = "text-secondary-foreground"
        break
      case "destructive":
        iconClass = "text-destructive"
        break
      default:
        iconClass = "text-muted-foreground"
        break
    }
  } else if (!iconClass) {
    iconClass = "text-muted-foreground"
  }

  return {
    label,
    variant: badgeVariant,
    className: badgeClass,
    iconClass,
  }
}

export type TableViewMode = "normal" | "dense" | "compact"
type ColumnTranslateFn = (key: string, options?: Record<string, unknown>) => string

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
    onRowSelection?: (selectionIdentity: string, checked: boolean, rowId?: string) => void
    getSelectionIdentity?: (torrent: Torrent) => string
    isAllSelected?: boolean
    excludedFromSelectAll?: Set<string>
  },
  speedUnit: SpeedUnit = "bytes",
  trackerIcons?: Record<string, string>,
  formatTimestamp?: (timestamp: number) => string,
  instancePreferences?: AppPreferences | null,
  supportsTrackerHealth: boolean = true,
  showInstanceColumn: boolean = false,
  viewMode: TableViewMode = "normal",
  trackerCustomizationLookup?: TrackerCustomizationLookup,
  includeSelectionColumn: boolean = true,
  translate?: ColumnTranslateFn
): ColumnDef<Torrent>[] => {
  const tr = (key: string, fallback: string, options?: Record<string, unknown>) => {
    if (translate) {
      return translate(key, options)
    }
    return fallback
  }
  const headers = {
    instance: tr("torrentTableColumns.headers.instance", "Instance"),
    selection: tr("torrentTableColumns.headers.selection", "Selection"),
    priority: tr("torrentTableColumns.headers.priority", "Priority"),
    name: tr("torrentTableColumns.headers.name", "Name"),
    size: tr("torrentTableColumns.headers.size", "Size"),
    totalSize: tr("torrentTableColumns.headers.totalSize", "Total Size"),
    progress: tr("torrentTableColumns.headers.progress", "Progress"),
    statusIcon: tr("torrentTableColumns.headers.statusIcon", "Status Icon"),
    status: tr("torrentTableColumns.headers.status", "Status"),
    seeds: tr("torrentTableColumns.headers.seeds", "Seeds"),
    peers: tr("torrentTableColumns.headers.peers", "Peers"),
    downSpeed: tr("torrentTableColumns.headers.downSpeed", "Down Speed"),
    upSpeed: tr("torrentTableColumns.headers.upSpeed", "Up Speed"),
    eta: tr("torrentTableColumns.headers.eta", "ETA"),
    ratio: tr("torrentTableColumns.headers.ratio", "Ratio"),
    popularity: tr("torrentTableColumns.headers.popularity", "Popularity"),
    category: tr("torrentTableColumns.headers.category", "Category"),
    tags: tr("torrentTableColumns.headers.tags", "Tags"),
    added: tr("torrentTableColumns.headers.added", "Added"),
    completedOn: tr("torrentTableColumns.headers.completedOn", "Completed On"),
    trackerIcon: tr("torrentTableColumns.headers.trackerIcon", "Tracker Icon"),
    tracker: tr("torrentTableColumns.headers.tracker", "Tracker"),
    downLimit: tr("torrentTableColumns.headers.downLimit", "Down Limit"),
    upLimit: tr("torrentTableColumns.headers.upLimit", "Up Limit"),
    downloaded: tr("torrentTableColumns.headers.downloaded", "Downloaded"),
    uploaded: tr("torrentTableColumns.headers.uploaded", "Uploaded"),
    sessionDownloaded: tr("torrentTableColumns.headers.sessionDownloaded", "Session Downloaded"),
    sessionUploaded: tr("torrentTableColumns.headers.sessionUploaded", "Session Uploaded"),
    remaining: tr("torrentTableColumns.headers.remaining", "Remaining"),
    timeActive: tr("torrentTableColumns.headers.timeActive", "Time Active"),
    seedingTime: tr("torrentTableColumns.headers.seedingTime", "Seeding Time"),
    savePath: tr("torrentTableColumns.headers.savePath", "Save Path"),
    completed: tr("torrentTableColumns.headers.completed", "Completed"),
    ratioLimit: tr("torrentTableColumns.headers.ratioLimit", "Ratio Limit"),
    lastSeenComplete: tr("torrentTableColumns.headers.lastSeenComplete", "Last Seen Complete"),
    lastActivity: tr("torrentTableColumns.headers.lastActivity", "Last Activity"),
    availability: tr("torrentTableColumns.headers.availability", "Availability"),
    infoHashV1: tr("torrentTableColumns.headers.infoHashV1", "Info Hash v1"),
    infoHashV2: tr("torrentTableColumns.headers.infoHashV2", "Info Hash v2"),
    reannounceIn: tr("torrentTableColumns.headers.reannounceIn", "Reannounce In"),
    private: tr("torrentTableColumns.headers.private", "Private"),
  } as const

  // Badge padding classes based on view mode
  const badgePadding = viewMode === "dense" ? "px-1.5 py-0" : ""

  const instanceColumn: ColumnDef<Torrent> = {
    id: "instance",
    accessorKey: "instanceName",
    header: headers.instance,
    cell: ({ row }) => {
      const instanceName = (row.original as CrossInstanceTorrent).instanceName ?? ""
      return (
        <div className="overflow-hidden whitespace-nowrap text-sm font-medium" title={instanceName}>
          <Badge variant="outline" className="text-xs">
            {instanceName}
          </Badge>
        </div>
      )
    },
    size: calculateMinWidth(headers.instance),
  }

  return [
    ...(includeSelectionColumn ? [{
      id: "select",
      header: ({ table }: HeaderContext<Torrent, unknown>) => (
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
            aria-label={tr("torrentTableColumns.selection.selectAllAria", "Select all")}
            className="hover:border-ring cursor-pointer transition-colors"
          />
        </div>
      ),
      cell: ({ row, table }: CellContext<Torrent, unknown>) => {
        const torrent = row.original
        const hash = torrent.hash
        const selectionIdentity = selectionEnhancers?.getSelectionIdentity?.(torrent) ?? hash

        // Determine if row is selected based on custom logic
        const isRowSelected = (() => {
          if (selectionEnhancers?.isAllSelected) {
          // In "select all" mode, row is selected unless excluded
            return !selectionEnhancers.excludedFromSelectAll?.has(selectionIdentity)
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
                const currentIndex = allRows.findIndex((r: { id: string }) => r.id === row.id)

                if (isShift && selectionEnhancers?.lastSelectedIndexRef.current !== null) {
                  const start = Math.min(selectionEnhancers.lastSelectedIndexRef.current!, currentIndex)
                  const end = Math.max(selectionEnhancers.lastSelectedIndexRef.current!, currentIndex)

                  // For shift selection, use custom handler if available, otherwise fallback
                  if (selectionEnhancers?.onRowSelection) {
                    for (let i = start; i <= end; i++) {
                      const r = allRows[i]
                      if (r) {
                        const rTorrent = r.original as Torrent
                        const rSelectionIdentity = selectionEnhancers.getSelectionIdentity?.(rTorrent) ?? rTorrent.hash
                        selectionEnhancers.onRowSelection(rSelectionIdentity, !!checked, r.id)
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
                    selectionEnhancers.onRowSelection(selectionIdentity, !!checked, row.id)
                  } else {
                    row.toggleSelected(!!checked)
                  }
                }

                if (selectionEnhancers) {
                  selectionEnhancers.lastSelectedIndexRef.current = currentIndex
                  selectionEnhancers.shiftPressedRef.current = false
                }
              }}
              aria-label={tr("torrentTableColumns.selection.selectRowAria", "Select row")}
              className="hover:border-ring cursor-pointer transition-colors"
            />
          </div>
        )
      },
      size: 40,
      enableResizing: false,
      meta: {
        headerString: headers.selection,
      },
    }] : []),
    {
      accessorKey: "priority",
      header: () => (
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="flex items-center justify-center">
              <ListOrdered className="h-4 w-4" />
            </div>
          </TooltipTrigger>
          <TooltipContent>{tr("torrentTableColumns.priority.tooltip", headers.priority)}</TooltipContent>
        </Tooltip>
      ),
      meta: {
        headerString: headers.priority,
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
      header: headers.name,
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
    ...(showInstanceColumn ? [instanceColumn] : []),
    {
      accessorKey: "size",
      header: headers.size,
      cell: ({ row }) => <span className="text-sm overflow-hidden whitespace-nowrap">{formatBytes(row.original.size)}</span>,
      size: 85,
    },
    {
      accessorKey: "total_size",
      header: headers.totalSize,
      cell: ({ row }) => <span className="text-sm overflow-hidden whitespace-nowrap">{formatBytes(row.original.total_size)}</span>,
      size: 115,
    },
    {
      accessorKey: "progress",
      header: headers.progress,
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          <Progress value={row.original.progress * 100} className="w-20" />
          <span className="text-xs text-muted-foreground">
            {row.original.progress >= 0.99 && row.original.progress < 1 ? (
              (Math.floor(row.original.progress * 1000) / 10).toFixed(1)
            ) : (
              Math.round(row.original.progress * 100)
            )}%
          </span>
        </div>
      ),
      size: 120,
    },
    {
      id: "status_icon",
      accessorFn: (torrent) => torrent.state,
      header: () => (
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="flex h-full w-full items-center justify-center text-muted-foreground" aria-label={tr("torrentTableColumns.statusIcon.ariaLabel", headers.statusIcon)}>
              <PlayCircle className="h-4 w-4" aria-hidden="true" />
            </div>
          </TooltipTrigger>
          <TooltipContent>{tr("torrentTableColumns.statusIcon.tooltip", headers.statusIcon)}</TooltipContent>
        </Tooltip>
      ),
      meta: {
        headerString: headers.statusIcon,
      },
      sortingFn: (rowA, rowB) => compareTrackerAwareStatus(rowA.original, rowB.original, supportsTrackerHealth),
      cell: ({ row }) => {
        const torrent = row.original
        const StatusIcon = getStatusIcon(torrent.state, torrent.tracker_health ?? null, supportsTrackerHealth)
        const { label: statusLabel, iconClass } = getStatusBadgeMeta(torrent, supportsTrackerHealth)

        return (
          <div
            className="flex h-full w-full items-center justify-center"
            title={statusLabel}
            aria-label={statusLabel}
          >
            <StatusIcon className={cn("h-4 w-4", iconClass)} aria-hidden="true" />
          </div>
        )
      },
      size: 48,
      minSize: 48,
      maxSize: 48,
      enableResizing: false,
      enableSorting: true,
    },
    {
      accessorKey: "state",
      header: headers.status,
      sortingFn: (rowA, rowB) => compareTrackerAwareStatus(rowA.original, rowB.original, supportsTrackerHealth),
      cell: ({ row }) => {
        const torrent = row.original
        const state = torrent.state
        const priority = torrent.priority
        const isQueued = state === "queuedDL" || state === "queuedUP"
        const { label: displayLabel, variant: badgeVariant, className: badgeClass } = getStatusBadgeMeta(torrent, supportsTrackerHealth)

        if (isQueued && priority > 0) {
          return (
            <div className="flex items-center gap-1">
              <Badge variant={badgeVariant} className={cn("text-xs", badgePadding, badgeClass)}>
                {displayLabel}
              </Badge>
              <span className="text-xs text-muted-foreground">#{priority}</span>
            </div>
          )
        }

        return (
          <Badge variant={badgeVariant} className={cn("text-xs", badgePadding, badgeClass)}>
            {displayLabel}
          </Badge>
        )
      },
      size: 130,
    },
    {
      accessorKey: "num_seeds",
      header: headers.seeds,
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
      header: headers.peers,
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
      header: headers.downSpeed,
      cell: ({ row }) => {
        const speed = row.original.dlspeed
        return <span className="text-sm overflow-hidden whitespace-nowrap">{speed === 0 ? "-" : formatSpeedWithUnit(speed, speedUnit)}</span>
      },
      size: calculateMinWidth(headers.downSpeed),
    },
    {
      accessorKey: "upspeed",
      header: headers.upSpeed,
      cell: ({ row }) => {
        const speed = row.original.upspeed
        return <span className="text-sm overflow-hidden whitespace-nowrap">{speed === 0 ? "-" : formatSpeedWithUnit(speed, speedUnit)}</span>
      },
      size: calculateMinWidth(headers.upSpeed),
    },
    {
      accessorKey: "eta",
      header: headers.eta,
      cell: ({ row }) => <span className="text-sm overflow-hidden whitespace-nowrap">{formatEta(row.original.eta)}</span>,
      size: 80,
    },
    {
      accessorKey: "ratio",
      header: headers.ratio,
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
      sortingFn: (rowA, rowB) => {
        const ratioA = incognitoMode ? getLinuxRatio(rowA.original.hash) : rowA.original.ratio
        const ratioB = incognitoMode ? getLinuxRatio(rowB.original.hash) : rowB.original.ratio

        // Handle infinity values: -1 should be treated as the highest value
        if (ratioA === -1 && ratioB === -1) return 0
        if (ratioA === -1) return 1  // ratioA is infinity, so it's greater
        if (ratioB === -1) return -1 // ratioB is infinity, so it's greater

        // Normal numeric comparison
        return ratioA - ratioB
      },
      size: 90,
    },
    {
      accessorKey: "popularity",
      header: headers.popularity,
      cell: ({ row }) => {
        return (
          <div className="overflow-hidden whitespace-nowrap text-sm">
            {row.original.popularity.toFixed(2)}
          </div>
        )
      },
      size: 120,
    },
    {
      accessorKey: "category",
      header: headers.category,
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
      header: headers.tags,
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
      header: headers.added,
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
      header: headers.completedOn,
      cell: ({ row }) => {
        const completionOn = row.original.completion_on
        if (!completionOn || completionOn === -1) {
          return "-"
        }

        return (
          <div className="overflow-hidden whitespace-nowrap text-sm">{formatTimestamp ? formatTimestamp(completionOn) : new Date(completionOn * 1000).toLocaleString()}</div>
        )
      },
      size: 200,
    },
    {
      id: "tracker_icon",
      header: ({ table }) => {
        const trackerColumn = table.getColumn("tracker")
        const sortState = trackerColumn?.getIsSorted()
        const Icon = sortState === "asc" ? ArrowDownAZ : sortState === "desc" ? ArrowDownZA : Globe

        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="flex h-full w-full items-center justify-center text-muted-foreground" aria-label={tr("torrentTableColumns.trackerIcon.ariaLabel", headers.trackerIcon)}>
                <Icon className="h-4 w-4" aria-hidden="true" />
              </div>
            </TooltipTrigger>
            <TooltipContent>{tr("torrentTableColumns.trackerIcon.tooltip", headers.trackerIcon)}</TooltipContent>
          </Tooltip>
        )
      },
      meta: {
        headerString: headers.trackerIcon,
      },
      cell: ({ row }) => {
        const tracker = incognitoMode ? getLinuxTracker(row.original.hash) : row.original.tracker
        const { host, fallback, title } = getTrackerDisplayMeta(tracker)
        // Use primary domain from customization for icon lookup (if customized)
        const trackerDisplayInfo = trackerCustomizationLookup ? resolveTrackerDisplay(host, trackerCustomizationLookup) : null
        const iconDomain = trackerDisplayInfo?.primaryDomain || host
        const iconSrc = resolveTrackerIconSrc(trackerIcons, iconDomain, host)

        return (
          <TrackerIconCell
            title={title}
            fallback={fallback}
            src={iconSrc}
          />
        )
      },
      size: 48,
      minSize: 48,
      maxSize: 48,
      enableResizing: false,
      enableSorting: false,
    },
    {
      accessorKey: "tracker",
      header: headers.tracker,
      // For client-side sorting in cross-seed mode, use the resolved display name.
      // Return undefined for empty/unknown so sortUndefined: "last" keeps them at the end.
      accessorFn: trackerCustomizationLookup ? (torrent) => {
        const tracker = incognitoMode ? getLinuxTracker(torrent.hash) : torrent.tracker
        const host = extractTrackerHost(tracker)
        if (!host || host === "unknown") {
          return undefined
        }
        const displayInfo = resolveTrackerDisplay(host, trackerCustomizationLookup)
        return displayInfo.displayName || undefined
      } : undefined,
      // Keep empty/unknown trackers at the end regardless of sort direction
      sortUndefined: "last",
      cell: ({ row }) => {
        const tracker = incognitoMode ? getLinuxTracker(row.original.hash) : row.original.tracker
        const host = extractTrackerHost(tracker)
        // Resolve display name from customizations
        const displayInfo = trackerCustomizationLookup ? resolveTrackerDisplay(host, trackerCustomizationLookup) : { displayName: host, primaryDomain: host, isCustomized: false }

        // Build tooltip content: custom name (if any), hostname, and full URL
        const tooltipParts: string[] = []
        if (displayInfo.isCustomized) {
          tooltipParts.push(`Name: ${displayInfo.displayName}`)
          tooltipParts.push(`Host: ${host}`)
        }
        if (tracker && tracker !== host) {
          tooltipParts.push(`URL: ${tracker}`)
        }
        const tooltipText = tooltipParts.length > 0 ? tooltipParts.join("\n") : (tracker || "")

        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="overflow-hidden whitespace-nowrap text-sm cursor-default">
                {displayInfo.displayName || "-"}
              </div>
            </TooltipTrigger>
            {tooltipText && (
              <TooltipContent className="max-w-xs">
                <p className="whitespace-pre-line text-xs">{tooltipText}</p>
              </TooltipContent>
            )}
          </Tooltip>
        )
      },
      size: 150,
    },
    {
      accessorKey: "dl_limit",
      header: headers.downLimit,
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
      size: calculateMinWidth(headers.downLimit, 30),
    },
    {
      accessorKey: "up_limit",
      header: headers.upLimit,
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
      size: calculateMinWidth(headers.upLimit, 30),
    },
    {
      accessorKey: "downloaded",
      header: headers.downloaded,
      cell: ({ row }) => {
        const downloaded = row.original.downloaded
        return <span className="text-sm overflow-hidden whitespace-nowrap">{downloaded === 0 ? "-" : formatBytes(downloaded)}</span>
      },
      size: calculateMinWidth(headers.downloaded),
    },
    {
      accessorKey: "uploaded",
      header: headers.uploaded,
      cell: ({ row }) => {
        const uploaded = row.original.uploaded
        return <span className="text-sm overflow-hidden whitespace-nowrap">{uploaded === 0 ? "-" : formatBytes(uploaded)}</span>
      },
      size: calculateMinWidth(headers.uploaded),
    },
    {
      accessorKey: "downloaded_session",
      header: headers.sessionDownloaded,
      cell: ({ row }) => {
        const sessionDownloaded = row.original.downloaded_session
        return <span className="text-sm overflow-hidden whitespace-nowrap">{sessionDownloaded === 0 ? "-" : formatBytes(sessionDownloaded)}</span>
      },
      size: calculateMinWidth(headers.sessionDownloaded),
    },
    {
      accessorKey: "uploaded_session",
      header: headers.sessionUploaded,
      cell: ({ row }) => {
        const sessionUploaded = row.original.uploaded_session
        return <span className="text-sm overflow-hidden whitespace-nowrap">{sessionUploaded === 0 ? "-" : formatBytes(sessionUploaded)}</span>
      },
      size: calculateMinWidth(headers.sessionUploaded),
    },
    {
      accessorKey: "amount_left",
      header: headers.remaining,
      cell: ({ row }) => {
        const amountLeft = row.original.amount_left
        return <span className="text-sm overflow-hidden whitespace-nowrap">{amountLeft === 0 ? "-" : formatBytes(amountLeft)}</span>
      },
      size: calculateMinWidth(headers.remaining),
    },
    {
      accessorKey: "time_active",
      header: headers.timeActive,
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
      header: headers.seedingTime,
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
      header: headers.savePath,
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
      header: headers.completed,
      cell: ({ row }) => {
        const completed = row.original.completed
        return <span className="text-sm overflow-hidden whitespace-nowrap">{completed === 0 ? "-" : formatBytes(completed)}</span>
      },
      size: calculateMinWidth(headers.completed),
    },
    {
      accessorKey: "ratio_limit",
      header: headers.ratioLimit,
      cell: ({ row }) => {
        const ratioLimit = row.original.ratio_limit
        const instanceRatioLimit = instancePreferences?.max_ratio
        const displayRatioLimit = ratioLimit === -2 ? (instanceRatioLimit === -1 ? "∞" : instanceRatioLimit?.toFixed(2) || "∞") :ratioLimit === -1 ? "∞" :ratioLimit.toFixed(2)

        return (
          <span
            className="text-sm font-medium overflow-hidden whitespace-nowrap"
          >
            {displayRatioLimit}
          </span>
        )
      },
      size: calculateMinWidth(headers.ratioLimit, 24),
    },
    {
      accessorKey: "seen_complete",
      header: headers.lastSeenComplete,
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
      header: headers.lastActivity,
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
      header: headers.availability,
      cell: ({ row }) => {
        const availability = row.original.availability
        return <span className="text-sm overflow-hidden whitespace-nowrap">{availability.toFixed(3)}</span>
      },
      size: calculateMinWidth(headers.availability),
    },
    // incomplete save path is not exposed by the API?
    {
      accessorKey: "infohash_v1",
      header: headers.infoHashV1,
      cell: ({ row }) => {
        const original = row.original.infohash_v1
        const maskBase = row.original.hash || row.original.infohash_v1 || row.original.infohash_v2 || row.id
        const infoHash = incognitoMode && original ? getLinuxHash(maskBase || "") : original
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
      header: headers.infoHashV2,
      cell: ({ row }) => {
        const original = row.original.infohash_v2
        const maskBase = row.original.hash || row.original.infohash_v1 || row.original.infohash_v2 || row.id
        const infoHash = incognitoMode && original ? getLinuxHash(maskBase || "") : original
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
      header: headers.reannounceIn,
      cell: ({ row }) => {
        return (
          <div className="overflow-hidden whitespace-nowrap text-sm">
            {formatReannounce(row.original.reannounce)}
          </div>
        )
      },
      size: calculateMinWidth(headers.reannounceIn),
    },
    {
      accessorKey: "private",
      header: headers.private,
      cell: ({ row }) => {
        return (
          <div className="overflow-hidden whitespace-nowrap text-sm">
            {row.original.private ? "Yes" : "No"}
          </div>
        )
      },
      size: calculateMinWidth(headers.private),
    },
  ]}
