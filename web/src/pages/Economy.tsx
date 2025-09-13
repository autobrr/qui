/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { EconomyTable } from "@/components/economy/EconomyTable"
import { EconomyPremiumOverlay } from "@/components/premium/EconomyPremiumOverlay"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { useHasPremiumAccess } from "@/hooks/useThemeLicense"
import { api } from "@/lib/api"
import { generateFakeEconomyData, generateFakeEconomyStats } from "@/lib/fakeEconomyData"
import { formatBytes } from "@/lib/utils"
import type { FilterOptions } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { HardDrive, Info, Loader2, Package, TrendingDown, TrendingUp } from "lucide-react"
import { useCallback, useState } from "react"

export function Economy() {
  const { instances, isLoading: instancesLoading } = useInstances()
  const { hasPremiumAccess, isLoading: premiumLoading } = useHasPremiumAccess()
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

  // Set default instance when instances load
  useState(() => {
    if (instances && instances.length > 0 && !selectedInstanceId) {
      setSelectedInstanceId(instances[0].id)
    }
  })

  // Generate fake data for non-premium users
  const fakeEconomyData = generateFakeEconomyData()
  const fakeStats = generateFakeEconomyStats()

  // Fetch economy data
  const { data: economyData, isLoading: economyLoading, refetch } = useQuery({
    queryKey: ["economy", selectedInstanceId, currentPage, pageSize, sortField, sortOrder, filters],
    queryFn: () => {
      if (!selectedInstanceId) return null
      return api.getEconomyAnalysis(
        selectedInstanceId,
        currentPage,
        pageSize,
        sortField,
        sortOrder,
        filters
      )
    },
    enabled: !!selectedInstanceId && hasPremiumAccess,
    refetchInterval: hasPremiumAccess ? 30000 : false, // Only refresh if premium
  })

  // Fetch economy stats
  const { data: statsData } = useQuery({
    queryKey: ["economy-stats", selectedInstanceId],
    queryFn: () => {
      if (!selectedInstanceId) return null
      return api.getEconomyStats(selectedInstanceId)
    },
    enabled: !!selectedInstanceId && hasPremiumAccess,
    refetchInterval: hasPremiumAccess ? 60000 : false, // Only refresh if premium
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

  if (instancesLoading || premiumLoading) {
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

  // Automatically select first instance if none selected
  if (!selectedInstanceId && instances.length > 0) {
    setSelectedInstanceId(instances[0].id)
  }

  // Use fake data if no premium access, otherwise use real data
  const stats = hasPremiumAccess ? (statsData || economyData?.stats) : fakeStats
  const displayData = hasPremiumAccess ? economyData : fakeEconomyData
  const isLoading = hasPremiumAccess ? economyLoading : false

  return (
    <div className="flex flex-col h-full relative">
      {!hasPremiumAccess && <EconomyPremiumOverlay />}

      {/* Header */}
      <div className={`flex-shrink-0 border-b bg-background ${!hasPremiumAccess ? "blur-sm" : ""}`}>
        <div className="px-6 py-4">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h1 className="text-2xl font-bold">Economy Analysis</h1>
              <p className="text-sm text-muted-foreground mt-1">
                Analyze torrent value and optimize storage usage
              </p>
            </div>
            <Select value={selectedInstanceId?.toString()} onValueChange={handleInstanceChange}>
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
                  <CardDescription className="text-xs">Storage Savings</CardDescription>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <TrendingDown className="h-4 w-4 text-green-500" />
                    {formatBytes(stats.storageSavings)}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-2">
                    <p className="text-xs text-muted-foreground">
                      From {stats.totalTorrents - stats.rareContentCount} duplicates
                    </p>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-3 w-3 text-muted-foreground/50 cursor-help" />
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs">
                        <p className="text-xs">
                          Potential space that could be freed by removing duplicate copies while keeping the best version of each torrent based on economy score
                        </p>
                      </TooltipContent>
                    </Tooltip>
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader className="pb-2">
                  <CardDescription className="text-xs">Average Score</CardDescription>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <TrendingUp className="h-4 w-4" />
                    {stats.averageEconomyScore.toFixed(1)}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-2">
                    <p className="text-xs text-muted-foreground">
                      Retention value (0-100)
                    </p>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-3 w-3 text-muted-foreground/50 cursor-help" />
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs">
                        <p className="text-xs">
                          Score based on age, activity, ratio, and rarity. Higher scores indicate torrents worth keeping longer. Duplicates get bonus points, while old well-seeded content scores lower.
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
      <div className={`flex-1 overflow-hidden ${!hasPremiumAccess ? "blur-sm" : ""}`}>
        {selectedInstanceId && (
          <EconomyTable
            instanceId={selectedInstanceId}
            data={displayData}
            isLoading={isLoading}
            filters={filters}
            onFilterChange={handleFilterChange}
            currentPage={currentPage}
            pageSize={pageSize}
            onPageChange={handlePageChange}
            sortField={sortField}
            sortOrder={sortOrder}
            onSortChange={handleSortChange}
            onRefresh={() => refetch()}
          />
        )}
      </div>
    </div>
  )
}