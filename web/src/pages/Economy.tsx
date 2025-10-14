/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { EconomyTable } from "@/components/economy/EconomyTable"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { formatBytes } from "@/lib/utils"
import type { FilterOptions } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { HardDrive, Info, Loader2, Package, TrendingDown, TrendingUp } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"

export function Economy() {
  const { instances, isLoading: instancesLoading } = useInstances()
  const [selectedInstanceId, setSelectedInstanceId] = useState<number | null>(null)
  const [filters, setFilters] = useState<FilterOptions>({
    status: [],
    categories: [],
    tags: [],
    trackers: [],
  })
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [sortField, setSortField] = useState<string>("economyScore")
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("asc")

  useEffect(() => {
    if (!instances || instances.length === 0) return

    const hasSelectedInstance =
      selectedInstanceId !== null &&
      instances.some((instance) => instance.id === selectedInstanceId)

    if (!hasSelectedInstance) {
      setSelectedInstanceId(instances[0].id)
    }
  }, [instances, selectedInstanceId])

  // Fetch economy data
  const { data: economyData, isLoading: economyLoading, refetch, error: economyError } = useQuery({
    queryKey: ["economy", selectedInstanceId, currentPage, pageSize, sortField, sortOrder, filters],
    queryFn: () => {
      if (selectedInstanceId === null) return null
      return api.getEconomyAnalysis(
        selectedInstanceId,
        currentPage,
        pageSize,
        sortField,
        sortOrder,
        filters
      )
    },
    enabled: selectedInstanceId !== null,
    refetchInterval: 30000, // Refresh every 30 seconds
  })

  // Fetch economy stats
  const { data: statsData, error: statsError } = useQuery({
    queryKey: ["economy-stats", selectedInstanceId],
    queryFn: () => {
      if (selectedInstanceId === null) return null
      return api.getEconomyStats(selectedInstanceId)
    },
    enabled: selectedInstanceId !== null,
    refetchInterval: 60000, // Refresh every minute
  })

  const handleInstanceChange = useCallback((value: string) => {
    setSelectedInstanceId(parseInt(value))
    setCurrentPage(1) // Reset to first page when changing instance
  }, [])

  const handlePageChange = useCallback((page: number, newPageSize?: number) => {
    setCurrentPage(page)
    if (newPageSize && newPageSize !== pageSize) {
      setPageSize(newPageSize)
    }
  }, [pageSize])

  const handleSortChange = useCallback((field: string, order: "asc" | "desc") => {
    setSortField(field)
    setSortOrder(order)
  }, [])

  const handleFilterChange = useCallback((newFilters: FilterOptions) => {
    setFilters(newFilters)
    setCurrentPage(1) // Reset to first page when filtering
  }, [])

  const stats = statsData || economyData?.stats

  const reviewThreshold = economyData?.reviewThreshold ?? null

  // Estimate duplicate copy count from economy analysis when available
  const duplicateCopyCount = useMemo(() => {
    if (!economyData?.duplicates) return null

    let count = 0
    for (const duplicateHashes of Object.values(economyData.duplicates)) {
      count += duplicateHashes.length
    }

    return count
  }, [economyData?.duplicates])

  if (instancesLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-8 w-8 animate-spin" />
      </div>
    )
  }

  if (!instances || instances.length === 0) {
    return (
      <div className="p-6">
        <h1 className="text-2xl font-bold mb-4">Economy Analysis</h1>
        <p className="text-muted-foreground">No instances configured. Please add an instance first.</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex-shrink-0 border-b bg-background">
        <div className="px-6 py-4">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h1 className="text-2xl font-bold">Economy Analysis</h1>
              <p className="text-sm text-muted-foreground mt-1">
                Analyze torrent value and optimize storage usage
              </p>
            </div>
            <Select value={selectedInstanceId !== null ? selectedInstanceId.toString() : undefined} onValueChange={handleInstanceChange}>
              <SelectTrigger className="w-[200px]">
                <SelectValue placeholder="Select instance" />
              </SelectTrigger>
              <SelectContent>
                {instances.map((instance) => (
                  <SelectItem key={instance.id} value={instance.id.toString()}>
                    {instance.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Stats Cards */}
          {stats && (
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
              <Card>
                <CardHeader className="pb-2">
                  <CardDescription className="text-xs">Total Storage</CardDescription>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <HardDrive className="h-4 w-4" />
                    {formatBytes(stats.totalStorage)}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-2">
                    <p className="text-xs text-muted-foreground">
                      {stats.totalTorrents} torrents
                    </p>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-3 w-3 text-muted-foreground/50 cursor-help" />
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs">
                        <p className="text-xs">
                          Combined size of all torrents currently in your library, including duplicates
                        </p>
                      </TooltipContent>
                    </Tooltip>
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader className="pb-2">
                  <CardDescription className="text-xs">Potential Dedup Savings</CardDescription>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <TrendingDown className="h-4 w-4 text-green-500" />
                    {formatBytes(stats.storageSavings)}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-2">
                    <p className="text-xs text-muted-foreground">
                      {duplicateCopyCount !== null? `If you remove ${duplicateCopyCount} duplicate ${duplicateCopyCount === 1 ? "copy" : "copies"}`: "If you keep only the best copy in each duplicate set"}
                    </p>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-3 w-3 text-muted-foreground/50 cursor-help" />
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs">
                        <p className="text-xs">
                          Calculated as total storage minus deduplicated storage — the space reclaimed by keeping just the top-ranked torrent in every duplicate group.
                        </p>
                      </TooltipContent>
                    </Tooltip>
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader className="pb-2">
                  <CardDescription className="text-xs">Average Score (all torrents)</CardDescription>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <TrendingUp className="h-4 w-4" />
                    {stats.averageEconomyScore.toFixed(1)}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-2">
                    <p className="text-xs text-muted-foreground">
                      Retention score (higher is better) across your entire library
                    </p>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-3 w-3 text-muted-foreground/50 cursor-help" />
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs">
                        <p className="text-xs">
                          Score based on age, activity, ratio, and rarity. Exceptional ratios, recent activity, and duplicate bonuses can push this above 100.
                          {reviewThreshold !== null && (
                            <> Torrents in the review table fall below {reviewThreshold.toFixed(1)}.</>
                          )}
                        </p>
                      </TooltipContent>
                    </Tooltip>
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader className="pb-2">
                  <CardDescription className="text-xs">Rare Content</CardDescription>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <Package className="h-4 w-4 text-orange-500" />
                    {stats.rareContentCount}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-2">
                    <p className="text-xs text-muted-foreground">
                      Torrents with &lt;5 seeds
                    </p>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-3 w-3 text-muted-foreground/50 cursor-help" />
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs">
                        <p className="text-xs">
                          Content at risk of becoming permanently unavailable. These torrents are critical to preserve as you may be one of the only remaining seeders.
                        </p>
                      </TooltipContent>
                    </Tooltip>
                  </div>
                </CardContent>
              </Card>
            </div>
          )}
        </div>
      </div>

      {/* Table */}
      <div className="flex-1 overflow-hidden">
        {selectedInstanceId !== null && (
          <EconomyTable
            instanceId={selectedInstanceId}
            data={economyData}
            isLoading={economyLoading}
            filters={filters}
            onFilterChange={handleFilterChange}
            currentPage={currentPage}
            pageSize={pageSize}
            onPageChange={handlePageChange}
            sortField={sortField}
            sortOrder={sortOrder}
            onSortChange={handleSortChange}
            onRefresh={() => refetch()}
            error={economyError || statsError}
          />
        )}
      </div>
    </div>
  )
}
