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
import { useTranslation } from "react-i18next"

function useCommonTr() {
  const { t } = useTranslation("common")
  return (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
}

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
  const tr = useCommonTr()
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
  const activeFiltersCount = [filterStatus !== "all", filterTestStatus !== "all", filterBackend !== "all"].filter(Boolean).length

  const getBackendLabel = (backend: TorznabIndexer["backend"]) => {
    if (backend === "jackett") return tr("indexerTable.backends.jackett")
    if (backend === "prowlarr") return tr("indexerTable.backends.prowlarr")
    return tr("indexerTable.backends.native")
  }

  if (loading) {
    return <div className="text-center py-8 text-muted-foreground">{tr("indexerTable.loading")}</div>
  }

  if (!indexers || indexers.length === 0) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        {tr("indexerTable.empty.noIndexers")}
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
                {tr("indexerTable.filters.title")}
                {hasActiveFilters && (
                  <Badge variant="secondary" className="ml-2 h-5 px-1.5">
                    {activeFiltersCount}
                  </Badge>
                )}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-56">
              <DropdownMenuLabel>{tr("indexerTable.filters.statusLabel")}</DropdownMenuLabel>
              <DropdownMenuCheckboxItem
                checked={filterStatus === "all"}
                onCheckedChange={() => setFilterStatus("all")}
              >
                {tr("indexerTable.filters.options.all")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterStatus === "enabled"}
                onCheckedChange={() => setFilterStatus("enabled")}
              >
                {tr("indexerTable.filters.options.enabledOnly")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterStatus === "disabled"}
                onCheckedChange={() => setFilterStatus("disabled")}
              >
                {tr("indexerTable.filters.options.disabledOnly")}
              </DropdownMenuCheckboxItem>

              <DropdownMenuSeparator />
              <DropdownMenuLabel>{tr("indexerTable.filters.testStatusLabel")}</DropdownMenuLabel>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "all"}
                onCheckedChange={() => setFilterTestStatus("all")}
              >
                {tr("indexerTable.filters.options.all")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "ok"}
                onCheckedChange={() => setFilterTestStatus("ok")}
              >
                {tr("indexerTable.filters.options.workingOnly")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "error"}
                onCheckedChange={() => setFilterTestStatus("error")}
              >
                {tr("indexerTable.filters.options.failedOnly")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterTestStatus === "untested"}
                onCheckedChange={() => setFilterTestStatus("untested")}
              >
                {tr("indexerTable.filters.options.untestedOnly")}
              </DropdownMenuCheckboxItem>

              <DropdownMenuSeparator />
              <DropdownMenuLabel>{tr("indexerTable.filters.backendLabel")}</DropdownMenuLabel>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "all"}
                onCheckedChange={() => setFilterBackend("all")}
              >
                {tr("indexerTable.filters.options.all")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "jackett"}
                onCheckedChange={() => setFilterBackend("jackett")}
              >
                {tr("indexerTable.backends.jackett")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "prowlarr"}
                onCheckedChange={() => setFilterBackend("prowlarr")}
              >
                {tr("indexerTable.backends.prowlarr")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuCheckboxItem
                checked={filterBackend === "native"}
                onCheckedChange={() => setFilterBackend("native")}
              >
                {tr("indexerTable.backends.native")}
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
            {tr("indexerTable.actions.testAll")}
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
              {tr("indexerTable.actions.clearFilters")}
            </Button>
          )}

          <div className="ml-auto text-sm text-muted-foreground">
            {tr("indexerTable.summary.showing", {
              visible: filteredAndSortedIndexers.length,
              total: indexers.length,
            })}
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
                    {tr("indexerTable.columns.name")}
                  </Button>
                </TableHead>
                <TableHead className="hidden md:table-cell text-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-full justify-center data-[state=open]:bg-accent"
                    onClick={() => handleSort("backend")}
                  >
                    {tr("indexerTable.columns.backend")}
                  </Button>
                </TableHead>
                <TableHead className="text-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-full justify-center data-[state=open]:bg-accent"
                    onClick={() => handleSort("status")}
                  >
                    {tr("indexerTable.columns.status")}
                  </Button>
                </TableHead>
                <TableHead className="text-center">{tr("indexerTable.columns.testStatus")}</TableHead>
                <TableHead className="hidden xl:table-cell text-center">{tr("indexerTable.columns.capabilities")}</TableHead>
                <TableHead className="hidden sm:table-cell text-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-full justify-center data-[state=open]:bg-accent"
                    onClick={() => handleSort("priority")}
                  >
                    {tr("indexerTable.columns.priority")}
                  </Button>
                </TableHead>
                <TableHead className="hidden sm:table-cell text-center">{tr("indexerTable.columns.timeout")}</TableHead>
                <TableHead className="text-center">{tr("indexerTable.columns.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredAndSortedIndexers.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className="text-center py-8 text-muted-foreground">
                    {tr("indexerTable.empty.noMatches")}
                  </TableCell>
                </TableRow>
              ) : (
                filteredAndSortedIndexers.map((indexer) => (
                  <TableRow key={indexer.id}>
                    <TableCell className="font-medium text-center">
                      <div>
                        <div>{indexer.name}</div>
                        <div className="md:hidden text-xs text-muted-foreground mt-1">
                          {getBackendLabel(indexer.backend)}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="hidden md:table-cell text-center">
                      <Badge variant="outline" className="capitalize">
                        {getBackendLabel(indexer.backend)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-center">
                      {indexer.enabled ? (
                        <Badge variant="default" className="gap-1">
                          <Check className="h-3 w-3" />
                          <span className="hidden sm:inline">{tr("indexerTable.status.enabled")}</span>
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="gap-1">
                          <X className="h-3 w-3" />
                          <span className="hidden sm:inline">{tr("indexerTable.status.disabled")}</span>
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-center">
                      {indexer.last_test_status === "ok" ? (
                        <Badge variant="default" className="gap-1">
                          <Check className="h-3 w-3" />
                          <span className="hidden sm:inline">{tr("indexerTable.testStatus.working")}</span>
                        </Badge>
                      ) : indexer.last_test_status === "error" ? (
                        <Badge
                          variant="destructive"
                          className="gap-1"
                          title={indexer.last_test_error || tr("indexerTable.testStatus.unknownError")}
                        >
                          <X className="h-3 w-3" />
                          <span className="hidden sm:inline">{tr("indexerTable.testStatus.failed")}</span>
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="gap-1">
                          <span className="hidden sm:inline">{tr("indexerTable.testStatus.untested")}</span>
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
                                    title={tr("indexerTable.capabilities.capabilityTitle", { capability: cap })}
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
                                title={tr("indexerTable.capabilities.collapseTitle")}
                              >
                                {tr("indexerTable.capabilities.collapse")}
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
                                      aria-label={tr("indexerTable.capabilities.expandAria", {
                                        count: indexer.capabilities.length,
                                      })}
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
                            {tr("indexerTable.capabilities.none")}
                          </span>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 px-2 text-xs"
                            onClick={() => onSyncCaps(indexer.id)}
                            title={tr("indexerTable.capabilities.syncTitle")}
                            aria-label={tr("indexerTable.capabilities.syncAria")}
                          >
                            {tr("indexerTable.actions.sync")}
                          </Button>
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="hidden sm:table-cell text-center">{indexer.priority}</TableCell>
                    <TableCell className="hidden sm:table-cell text-center">
                      {tr("indexerTable.units.seconds", { value: indexer.timeout_seconds })}
                    </TableCell>
                    <TableCell className="text-center">
                      <div className="flex justify-center gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={() => onTest(indexer.id)}
                          title={tr("indexerTable.actions.testConnection")}
                          aria-label={tr("indexerTable.actions.testConnection")}
                        >
                          <TestTube className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 hidden sm:inline-flex"
                          onClick={() => onSyncCaps(indexer.id)}
                          title={tr("indexerTable.actions.syncCapabilities")}
                          aria-label={tr("indexerTable.actions.syncCapabilities")}
                        >
                          <RefreshCw className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={() => onEdit(indexer)}
                          title={tr("indexerTable.actions.edit")}
                          aria-label={tr("indexerTable.actions.edit")}
                        >
                          <Edit2 className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={() => onDelete(indexer.id)}
                          title={tr("indexerTable.actions.delete")}
                          aria-label={tr("indexerTable.actions.delete")}
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
