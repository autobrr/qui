/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { PaginationWrapper } from "@/components/economy/pagination-wrapper"
import { TrackerExclusionFilter } from "@/components/economy/TrackerExclusionFilter"
import { BulkActionsMenu } from "@/components/economy/BulkActionsMenu"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"
import { cn } from "@/lib/utils"
import type { EconomyAnalysis, EconomyScore, FilterOptions } from "@/types"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type SortingState,
  type VisibilityState
} from "@tanstack/react-table"
import { useVirtualizer } from "@tanstack/react-virtual"
import { Columns3, Eye, EyeOff, Loader2, RefreshCw, Search } from "lucide-react"
import { useMemo, useRef, useState, useEffect } from "react"
import { useHotkeys } from "react-hotkeys-hook"
import { createEconomyColumns } from "./EconomyTableColumns"

// Extended type for incognito mode
type EconomyScoreWithOriginal = EconomyScore & {
  originalName?: string
}

interface EconomyTableProps {
  instanceId: number
  data: EconomyAnalysis | null | undefined
  isLoading: boolean
  filters: FilterOptions
  onFilterChange: (filters: FilterOptions) => void
  currentPage: number
  pageSize: number
  onPageChange: (page: number, pageSize?: number) => void
  sortField: string
  sortOrder: "asc" | "desc"
  onSortChange: (field: string, order: "asc" | "desc") => void
  onRefresh: () => void
  error?: unknown
}

export function EconomyTable({
  instanceId,
  data,
  isLoading,
  filters,
  onFilterChange,
  onPageChange,
  sortField,
  sortOrder,
  onSortChange,
  onRefresh,
  error,
}: EconomyTableProps) {
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [globalFilter, setGlobalFilter] = useState("")
  const [sorting, setSorting] = useState<SortingState>([
    { id: sortField, desc: sortOrder === "desc" },
  ])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({
    storageValue: true,
    rarityBonus: true,
    reviewPriority: false,
    lastActivity: true,
    tags: false, // Hidden by default but available
    tracker: false,
  })
  const [rowSelection, setRowSelection] = useState({})
  const searchInputRef = useRef<HTMLInputElement>(null)

  // Extract available trackers from data
  const availableTrackers = useMemo(() => {
    if (!data?.reviewTorrents?.torrents) return []
    const trackerSet = new Set<string>()
    data.reviewTorrents.torrents.forEach((torrent) => {
      if (torrent.tracker) {
        trackerSet.add(torrent.tracker)
      }
    })
    return Array.from(trackerSet).sort()
  }, [data])

  const handleExcludedTrackersChange = (excludedTrackers: string[]) => {
    onFilterChange({ ...filters, excludeTrackers: excludedTrackers })
  }

  // Build duplicate group lookup for group-aware selection
  const duplicateGroups = useMemo(() => {
    if (!data?.duplicates) return new Map<string, string[]>()
    
    const groups = new Map<string, string[]>()
    
    // For each hash, store all hashes in its duplicate group (including itself)
    Object.entries(data.duplicates).forEach(([primary, duplicates]) => {
      const group = [primary, ...duplicates]
      // Store the same group for every hash in the group
      group.forEach(hash => {
        groups.set(hash, group)
      })
    })
    
    return groups
  }, [data?.duplicates])

  // Helper to select/deselect entire duplicate group
  const toggleGroupSelection = (hash: string, selected: boolean) => {
    const group = duplicateGroups.get(hash)
    if (!group) {
      // Not a duplicate, just toggle this torrent
      setRowSelection(prev => {
        const index = tableData.findIndex(t => t.hash === hash)
        if (index === -1) return prev
        return { ...prev, [index]: selected }
      })
      return
    }

    // Toggle entire group
    setRowSelection(prev => {
      const newSelection = { ...prev }
      group.forEach(groupHash => {
        const index = tableData.findIndex(t => t.hash === groupHash)
        if (index !== -1) {
          if (selected) {
            newSelection[index] = true
          } else {
            delete newSelection[index]
          }
        }
      })
      return newSelection
    })
  }

  const handleClearSelection = () => {
    setRowSelection({})
  }

  // Sync sorting state with props when they change (from server-side sorting)
  useEffect(() => {
    setSorting([{ id: sortField, desc: sortOrder === "desc" }])
  }, [sortField, sortOrder])

  // Detect platform for appropriate key display
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent)
  const shortcutKey = isMac ? "⌘K" : "Ctrl+K"

  // Global keyboard shortcut to focus search
  useHotkeys(
    "meta+k, ctrl+k",
    (event) => {
      event.preventDefault()
      searchInputRef.current?.focus()
    },
    {
      preventDefault: true,
      enableOnFormTags: ["input", "textarea", "select"],
    }
  )

  const columns = useMemo(() => createEconomyColumns(), [])

  // Get table data
  const tableData = useMemo((): EconomyScoreWithOriginal[] => {
    if (!data?.reviewTorrents?.torrents) return []

    // Build a lookup so torrents missing duplicate metadata can still be flagged using the global map
    const duplicateLookup = new Map<string, Set<string>>()
    if (data.duplicates) {
      for (const [primary, duplicateHashes] of Object.entries(data.duplicates)) {
        const primarySet = duplicateLookup.get(primary) ?? new Set<string>()
        duplicateHashes.forEach((hash) => primarySet.add(hash))
        duplicateLookup.set(primary, primarySet)

        duplicateHashes.forEach((dup) => {
          const dupSet = duplicateLookup.get(dup) ?? new Set<string>()
          dupSet.add(primary)
          duplicateHashes.forEach((other) => {
            if (other !== dup) {
              dupSet.add(other)
            }
          })
          duplicateLookup.set(dup, dupSet)
        })
      }
    }

    let torrents: EconomyScoreWithOriginal[] = data.reviewTorrents.torrents.map((torrent) => {
      const lookupDuplicates = duplicateLookup.get(torrent.hash)
      const normalizedDuplicates = lookupDuplicates ? Array.from(lookupDuplicates) : torrent.duplicates
      return {
        ...torrent,
        duplicates: normalizedDuplicates && normalizedDuplicates.length > 0 ? normalizedDuplicates : undefined,
      }
    })

    // Apply incognito mode transformations
    if (incognitoMode) {
      torrents = torrents.map(torrent => ({
        ...torrent,
        name: getLinuxIsoName(torrent.hash),
        originalName: torrent.name, // Preserve original name for searching
      }))
    }

    // Apply client-side search filter if needed
    if (globalFilter) {
      return torrents.filter((torrent) => {
        const searchName = incognitoMode && torrent.originalName ? torrent.originalName : torrent.name
        return searchName.toLowerCase().includes(globalFilter.toLowerCase())
      })
    }

    return torrents
  }, [data, globalFilter, incognitoMode])

  const table = useReactTable({
    data: tableData,
    columns,
    getCoreRowModel: getCoreRowModel(),
    // Server-side sorting - don't use getSortedRowModel() as it only sorts the current page
    manualSorting: true,
    onSortingChange: (updater) => {
      const newSorting = typeof updater === "function" ? updater(sorting) : updater
      setSorting(newSorting)

      // Notify parent about sort change for server-side sorting
      if (newSorting.length > 0) {
        const sort = newSorting[0]
        onSortChange(sort.id, sort.desc ? "desc" : "asc")
      }
    },
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    state: {
      sorting,
      columnVisibility,
      rowSelection,
    },
  })

  // Virtualization setup
  const tableContainerRef = useRef<HTMLDivElement>(null)
  const rowVirtualizer = useVirtualizer({
    count: table.getRowModel().rows.length,
    getScrollElement: () => tableContainerRef.current,
    estimateSize: () => 50,
    overscan: 10,
  })

  const virtualRows = rowVirtualizer.getVirtualItems()
  const totalSize = rowVirtualizer.getTotalSize()

  const paddingTop = virtualRows.length > 0 ? virtualRows?.[0]?.start || 0 : 0
  const paddingBottom =
    virtualRows.length > 0? totalSize - (virtualRows?.[virtualRows.length - 1]?.end || 0): 0

  // Get selected torrent hashes for bulk operations
  const selectedTorrents = useMemo(() => {
    return Object.keys(rowSelection)
      .filter((key) => rowSelection[key as keyof typeof rowSelection])
      .map((key) => tableData[parseInt(key)])
      .filter(Boolean)
  }, [rowSelection, tableData])

  const hasError = Boolean(error)
  const errorMessage = error instanceof Error ? error.message : undefined

  if (isLoading && !data) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-8 w-8 animate-spin" />
      </div>
    )
  }

  const pagination = data?.reviewTorrents?.pagination

  return (
    <div className="flex flex-col h-full">
      {hasError ? (
        <div className="px-6 py-3 border-b bg-destructive/10 text-sm text-destructive">
          Failed to load economy data. Try refreshing the page or reloading the instance.
          {errorMessage && ` (${errorMessage})`}
        </div>
      ) : null}
      {/* Toolbar */}
      <div className="px-6 py-3 border-b bg-background">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-2 flex-1">
            {/* Search */}
            <div className="relative max-w-sm">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                ref={searchInputRef}
                placeholder={`Search torrents... (${shortcutKey})`}
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="pl-8 h-9"
              />
            </div>

            {/* Tracker Exclusion Filter */}
            <TrackerExclusionFilter
              availableTrackers={availableTrackers}
              excludedTrackers={filters.excludeTrackers || []}
              onExcludedTrackersChange={handleExcludedTrackersChange}
              disabled={isLoading}
            />

            {/* Bulk Actions Menu */}
            {selectedTorrents.length > 0 && (
              <BulkActionsMenu
                instanceId={instanceId}
                selectedTorrents={selectedTorrents}
                onActionComplete={onRefresh}
                onClearSelection={handleClearSelection}
              />
            )}

            {/* Selected count */}
            {selectedTorrents.length > 0 && (
              <div className="text-sm text-muted-foreground">
                {selectedTorrents.length} torrent(s)
              </div>
            )}
          </div>

          <div className="flex items-center gap-2">
            {/* Incognito mode toggle */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setIncognitoMode(!incognitoMode)}
                  className={cn(incognitoMode && "bg-muted")}
                >
                  {incognitoMode ? (
                    <EyeOff className="h-4 w-4" />
                  ) : (
                    <Eye className="h-4 w-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {incognitoMode ? "Exit incognito mode" : "Enable incognito mode"}
              </TooltipContent>
            </Tooltip>

            {/* Refresh button */}
            <Button
              variant="outline"
              size="sm"
              onClick={onRefresh}
              disabled={isLoading}
            >
              <RefreshCw className={cn("h-4 w-4", isLoading && "animate-spin")} />
            </Button>

            {/* Column visibility */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <Columns3 className="h-4 w-4 mr-2" />
                  Columns
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-[180px]">
                <DropdownMenuLabel>Toggle columns</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {table
                  .getAllColumns()
                  .filter(
                    (column) =>
                      typeof column.accessorFn !== "undefined" && column.getCanHide()
                  )
                  .map((column) => {
                    return (
                      <DropdownMenuCheckboxItem
                        key={column.id}
                        className="capitalize"
                        checked={column.getIsVisible()}
                        onCheckedChange={(value) => column.toggleVisibility(!!value)}
                      >
                        {column.id}
                      </DropdownMenuCheckboxItem>
                    )
                  })}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </div>

      {/* Table */}
      <div
        ref={tableContainerRef}
        className="flex-1 px-6 overflow-auto relative"
      >
        <Table>
          <TableHeader className="sticky top-0 bg-background z-10">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    style={{
                      width: header.getSize(),
                    }}
                  >
                    {header.isPlaceholder? null: flexRender(
                      header.column.columnDef.header,
                      header.getContext()
                    )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {paddingTop > 0 && (
              <tr>
                <td style={{ height: `${paddingTop}px` }} />
              </tr>
            )}
            {virtualRows.map((virtualRow) => {
              const row = table.getRowModel().rows[virtualRow.index]
              return (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && "selected"}
                  className="h-[50px]"
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      style={{
                        width: cell.column.getSize(),
                      }}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              )
            })}
            {paddingBottom > 0 && (
              <tr>
                <td style={{ height: `${paddingBottom}px` }} />
              </tr>
            )}
          </TableBody>
        </Table>

        {tableData.length === 0 && !isLoading && (
          <div className="flex items-center justify-center h-32">
            <p className="text-muted-foreground">No torrents found</p>
          </div>
        )}
      </div>

      {/* Pagination */}
      {pagination && pagination.totalPages > 1 && (
        <div className="px-6 py-3 border-t bg-background">
          <PaginationWrapper
            currentPage={pagination.page}
            totalPages={pagination.totalPages}
            pageSize={pagination.pageSize}
            totalItems={pagination.totalItems}
            onPageChange={onPageChange}
          />
        </div>
      )}
    </div>
  )
}
