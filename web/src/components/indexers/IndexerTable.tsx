/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
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
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip"
import type { TorznabIndexer } from "@/types"
import { Check, Edit2, Filter, RefreshCw, TestTube, Trash2, X } from "lucide-react"
import { useMemo, useState } from "react"

type SortField = "name" | "backend" | "priority" | "status"
type SortDirection = "asc" | "desc"

interface IndexerTableProps {
  indexers: TorznabIndexer[]
  loading: boolean
  onEdit: (indexer: TorznabIndexer) => void
  onDelete: (id: number) => void
  onTest: (id: number) => void
  onSyncCaps: (id: number) => void
  onTestAll: (visibleIndexers: TorznabIndexer[]) => void
}

export function IndexerTable({
  indexers,
  loading,
  onEdit,
  onDelete,
  onTest,
  onSyncCaps,
  onTestAll,
}: IndexerTableProps) {
  const [expandedCapabilities, setExpandedCapabilities] = useState<Set<number>>(new Set())
  const [sortField, setSortField] = useState<SortField>("priority")
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc")
  const [filterStatus, setFilterStatus] = useState<"all" | "enabled" | "disabled">("all")
  const [filterTestStatus, setFilterTestStatus] = useState<"all" | "ok" | "error" | "untested">("all")
  const [filterBackend, setFilterBackend] = useState<"all" | "jackett" | "prowlarr" | "native">("all")

  const toggleCapabilities = (indexerId: number) => {
    setExpandedCapabilities(prev => {
      const next = new Set(prev)
      if (next.has(indexerId)) {
        next.delete(indexerId)
      } else {
        next.add(indexerId)
      }
      return next
    })
  }

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(sortDirection === "asc" ? "desc" : "asc")
    } else {
      setSortField(field)
      setSortDirection("asc")
    }
  }

  const filteredAndSortedIndexers = useMemo(() => {
    let filtered = [...indexers]

    // Apply filters
    if (filterStatus !== "all") {
      filtered = filtered.filter(idx =>
        filterStatus === "enabled" ? idx.enabled : !idx.enabled
      )
    }

    if (filterTestStatus !== "all") {
      filtered = filtered.filter(idx => {
        if (filterTestStatus === "ok") return idx.last_test_status === "ok"
        if (filterTestStatus === "error") return idx.last_test_status === "error"
        return idx.last_test_status !== "ok" && idx.last_test_status !== "error"
      })
    }

    if (filterBackend !== "all") {
      filtered = filtered.filter(idx => idx.backend === filterBackend)
    }

    // Apply sorting
    filtered.sort((a, b) => {
      let comparison = 0

      switch (sortField) {
        case "name":
          comparison = a.name.localeCompare(b.name)
          break
        case "backend":
          comparison = a.backend.localeCompare(b.backend)
          break
        case "priority":
          comparison = a.priority - b.priority
          break
        case "status":
          comparison = (a.enabled ? 1 : 0) - (b.enabled ? 1 : 0)
          break
      }

      return sortDirection === "asc" ? comparison : -comparison
    })

    return filtered
  }, [indexers, sortField, sortDirection, filterStatus, filterTestStatus, filterBackend])

  const hasActiveFilters = filterStatus !== "all" || filterTestStatus !== "all" || filterBackend !== "all"

  if (loading) {
    return <div className="text-center py-8 text-muted-foreground">Loading...</div>
  }

  if (!indexers || indexers.length === 0) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        No indexers configured. Add one to get started.
      </div>
    )
  }

  return (
    <TooltipProvider delayDuration={150}>
      <div className="space-y-4">
        {/* Filter Controls */}
        <div className="flex flex-wrap items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" className="h-8">
                <Filter className="mr-2 h-4 w-4" />
                Filters
                {hasActiveFilters && (
                  <Badge variant="secondary" className="ml-2 h-5 px-1.5">
                    {[filterStatus !== "all", filterTestStatus !== "all", filterBackend !== "all"].filter(Boolean).length}
                  </Badge>
                )}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-56">
              <DropdownMenuLabel>Status</DropdownMenuLabel>
              <DropdownMenuCheckboxItem
                checked={filterStatus === "all"}
                onCheckedChange={() => setFilterStatus("all")}
              >
                All
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterStatus === "enabled"}
                onCheckedChange={() => setFilterStatus("enabled")}
              >
                Enabled only
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterStatus === "disabled"}
                onCheckedChange={() => setFilterStatus("disabled")}
              >
                Disabled only
              </DropdownMenuCheckboxItem>

              <DropdownMenuSeparator />
              <DropdownMenuLabel>Test Status</DropdownMenuLabel>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "all"}
                onCheckedChange={() => setFilterTestStatus("all")}
              >
                All
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "ok"}
                onCheckedChange={() => setFilterTestStatus("ok")}
              >
                Working only
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "error"}
                onCheckedChange={() => setFilterTestStatus("error")}
              >
                Failed only
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "untested"}
                onCheckedChange={() => setFilterTestStatus("untested")}
              >
                Untested only
              </DropdownMenuCheckboxItem>

              <DropdownMenuSeparator />
              <DropdownMenuLabel>Backend</DropdownMenuLabel>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "all"}
                onCheckedChange={() => setFilterBackend("all")}
              >
                All
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "jackett"}
                onCheckedChange={() => setFilterBackend("jackett")}
              >
                Jackett
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "prowlarr"}
                onCheckedChange={() => setFilterBackend("prowlarr")}
              >
                Prowlarr
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "native"}
                onCheckedChange={() => setFilterBackend("native")}
              >
                Native
              </DropdownMenuCheckboxItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <Button
            variant="outline"
            size="sm"
            className="h-8"
            onClick={() => onTestAll(filteredAndSortedIndexers)}
            disabled={loading || filteredAndSortedIndexers.length === 0}
          >
            <RefreshCw className="mr-2 h-4 w-4" />
            Test All
          </Button>

          {hasActiveFilters && (
            <Button
              variant="ghost"
              size="sm"
              className="h-8"
              onClick={() => {
                setFilterStatus("all")
                setFilterTestStatus("all")
                setFilterBackend("all")
              }}
            >
              Clear filters
            </Button>
          )}

          <div className="ml-auto text-sm text-muted-foreground">
            Showing {filteredAndSortedIndexers.length} of {indexers.length} indexers
          </div>
        </div>

        {/* Table */}
        <div className="rounded-md border">
          <Table className="text-center">
            <TableHeader>
              <TableRow>
                <TableHead className="text-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-full justify-center data-[state=open]:bg-accent"
                    onClick={() => handleSort("name")}
                  >
                    Name
                  </Button>
                </TableHead>
                <TableHead className="hidden md:table-cell text-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-full justify-center data-[state=open]:bg-accent"
                    onClick={() => handleSort("backend")}
                  >
                    Backend
                  </Button>
                </TableHead>
                <TableHead className="text-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-full justify-center data-[state=open]:bg-accent"
                    onClick={() => handleSort("status")}
                  >
                    Status
                  </Button>
                </TableHead>
                <TableHead className="text-center">Test Status</TableHead>
                <TableHead className="hidden xl:table-cell text-center">Capabilities</TableHead>
                <TableHead className="hidden sm:table-cell text-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-full justify-center data-[state=open]:bg-accent"
                    onClick={() => handleSort("priority")}
                  >
                    Priority
                  </Button>
                </TableHead>
                <TableHead className="hidden sm:table-cell text-center">Timeout</TableHead>
                <TableHead className="text-center">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredAndSortedIndexers.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className="text-center py-8 text-muted-foreground">
                    No indexers match the current filters
                  </TableCell>
                </TableRow>
              ) : (
                filteredAndSortedIndexers.map((indexer) => (
                  <TableRow key={indexer.id}>
                    <TableCell className="font-medium text-center">
                      <div>
                        <div>{indexer.name}</div>
                        <div className="md:hidden text-xs text-muted-foreground mt-1">
                          {indexer.backend === "jackett" && "Jackett"}
                          {indexer.backend === "prowlarr" && "Prowlarr"}
                          {indexer.backend === "native" && "Native"}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="hidden md:table-cell text-center">
                      <Badge variant="outline" className="capitalize">
                        {indexer.backend}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-center">
                      {indexer.enabled ? (
                        <Badge variant="default" className="gap-1">
                          <Check className="h-3 w-3" />
                          <span className="hidden sm:inline">Enabled</span>
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="gap-1">
                          <X className="h-3 w-3" />
                          <span className="hidden sm:inline">Disabled</span>
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-center">
                      {indexer.last_test_status === "ok" ? (
                        <Badge variant="default" className="gap-1">
                          <Check className="h-3 w-3" />
                          <span className="hidden sm:inline">Working</span>
                        </Badge>
                      ) : indexer.last_test_status === "error" ? (
                        <Badge variant="destructive" className="gap-1" title={indexer.last_test_error || "Unknown error"}>
                          <X className="h-3 w-3" />
                          <span className="hidden sm:inline">Failed</span>
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="gap-1">
                          <span className="hidden sm:inline">Untested</span>
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="hidden xl:table-cell text-center">
                      {indexer.capabilities && indexer.capabilities.length > 0 ? (
                        <div className="max-w-xs">
                          {expandedCapabilities.has(indexer.id) ? (
                            <div className="space-y-1">
                              <div className="flex flex-wrap justify-center gap-1">
                                {indexer.capabilities.map((cap) => (
                                  <Badge
                                    key={cap}
                                    variant="secondary"
                                    className="text-xs"
                                    title={`Capability: ${cap}`}
                                  >
                                    {cap}
                                  </Badge>
                                ))}
                              </div>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-5 px-2 text-xs text-muted-foreground hover:text-foreground"
                                onClick={() => toggleCapabilities(indexer.id)}
                                title="Collapse capabilities"
                              >
                                Collapse
                              </Button>
                            </div>
                          ) : (
                            <div className="flex items-center justify-center gap-1 overflow-hidden">
                              {indexer.capabilities.slice(0, 2).map((cap) => (
                                <Badge key={cap} variant="secondary" className="text-xs flex-shrink-0">
                                  {cap}
                                </Badge>
                              ))}
                              {indexer.capabilities.length > 2 && (
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <Button
                                      variant="outline"
                                      size="sm"
                                      className="text-xs h-5 px-1.5 flex-shrink-0"
                                      onClick={() => toggleCapabilities(indexer.id)}
                                      aria-label={`Click to show all ${indexer.capabilities.length} capabilities`}
                                    >
                                      +{indexer.capabilities.length - 2}
                                    </Button>
                                  </TooltipTrigger>
                                  <TooltipContent>
                                    <div className="flex max-w-xs flex-wrap justify-center gap-1">
                                      {indexer.capabilities.map((cap) => (
                                        <Badge key={cap} variant="secondary" className="text-xs">
                                          {cap}
                                        </Badge>
                                      ))}
                                    </div>
                                  </TooltipContent>
                                </Tooltip>
                              )}
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="flex items-center justify-center gap-2">
                          <span className="text-xs text-muted-foreground">
                            No capabilities
                          </span>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 px-2 text-xs"
                            onClick={() => onSyncCaps(indexer.id)}
                            title="Sync capabilities from backend"
                            aria-label="Sync capabilities from backend"
                          >
                            Sync
                          </Button>
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="hidden sm:table-cell text-center">{indexer.priority}</TableCell>
                    <TableCell className="hidden sm:table-cell text-center">{indexer.timeout_seconds}s</TableCell>
                    <TableCell className="text-center">
                      <div className="flex justify-center gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={() => onTest(indexer.id)}
                          title="Test connection"
                        >
                          <TestTube className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 hidden sm:inline-flex"
                          onClick={() => onSyncCaps(indexer.id)}
                          title="Sync capabilities"
                        >
                          <RefreshCw className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={() => onEdit(indexer)}
                          title="Edit"
                        >
                          <Edit2 className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={() => onDelete(indexer.id)}
                          title="Delete"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>
    </TooltipProvider>
  )
}
