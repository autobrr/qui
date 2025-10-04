/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { CompletionTimeChart } from "@/components/racing/charts/CompletionTimeChart"
import { TrackerPerformanceChart } from "@/components/racing/charts/TrackerPerformanceChart"
import { VolumeChart } from "@/components/racing/charts/VolumeChart"
import { columnsFastest } from "@/components/racing/columns-fastest"
import { columnsRatios } from "@/components/racing/columns-ratios"
import type { TrackerStatRow } from "@/components/racing/columns-tracker-stats"
import { columnsTrackerStats } from "@/components/racing/columns-tracker-stats"
import {
  API_LIMIT_OPTIONS,
  DEFAULT_API_LIMIT,
  DEFAULT_TORRENTS_PAGE_SIZE,
  DEFAULT_TRACKER_STATS_PAGE_SIZE
} from "@/components/racing/constants"
import { DataTable } from "@/components/racing/data-table"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { RacingDashboardOptions } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { format } from "date-fns"
import { Activity, AlertCircle, BarChart3, CalendarIcon, ChevronDown, Clock, Database, Filter, HardDrive, LineChart, ListFilter, Percent, RotateCcw, Settings2, Tag, TrendingDown, TrendingUp } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import type { DateRange } from "react-day-picker"

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${hours}h ${minutes}m`
}

function formatDate(dateString: string): string {
  return new Date(dateString).toLocaleString()
}


// Custom hook for persisting Racing Dashboard filters state
function usePersistedRacingFiltersState(defaultOpen: boolean = true) {
  const storageKey = "qui-racing-filters-collapsed"

  // Initialize state from localStorage or default value
  const [filtersOpen, setFiltersOpenState] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        // Note: we store "collapsed" but use "open" in state, so invert
        return stored === "false"
      }
    } catch (error) {
      console.error("Failed to load racing filters state from localStorage:", error)
    }
    return defaultOpen
  })

  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      // Store as "collapsed" (inverse of "open")
      localStorage.setItem(storageKey, (!filtersOpen).toString())
    } catch (error) {
      console.error("Failed to save racing filters state to localStorage:", error)
    }
  }, [filtersOpen])

  return [filtersOpen, setFiltersOpenState] as const
}

export function RacingDashboard() {
  const { instances, isLoading: instancesLoading } = useInstances()

  // Initialize selectedInstanceIds from localStorage (now supporting multiple)
  const [selectedInstanceIds, setSelectedInstanceIdsState] = useState<number[]>(() => {
    try {
      const stored = localStorage.getItem("qui-racing-selected-instances")
      if (stored !== null) {
        const ids = JSON.parse(stored)
        if (Array.isArray(ids)) {
          return ids.filter(id => typeof id === "number")
        }
      }
    } catch (error) {
      console.error("Failed to load selected instances from localStorage:", error)
    }
    return []
  })

  // Wrapper to persist instance selection
  const setSelectedInstanceIds = useCallback((ids: number[]) => {
    setSelectedInstanceIdsState(ids)
    try {
      localStorage.setItem("qui-racing-selected-instances", JSON.stringify(ids))
    } catch (error) {
      console.error("Failed to save selected instances to localStorage:", error)
    }
  }, [])

  const [options, setOptions] = useState<RacingDashboardOptions>({
    limit: DEFAULT_API_LIMIT,
    trackerFilter: [],
    categoryFilter: [],
    minRatio: 0,
    timeRange: "",
    startDate: "",
    endDate: "",
  })
  const [categoryInput, setCategoryInput] = useState("")
  const [customDateRange, setCustomDateRange] = useState(false)
  const [dateRange, setDateRange] = useState<DateRange | undefined>()
  const [filtersOpen, setFiltersOpen] = usePersistedRacingFiltersState(true)

  // Auto-select first instance if none selected or if selected instance doesn't exist
  useEffect(() => {
    if (instances && instances.length > 0) {
      if (selectedInstanceIds.length === 0) {
        // Select all instances by default
        setSelectedInstanceIds(instances.map(i => i.id))
      } else {
        // Filter out any non-existent instances
        const validIds = selectedInstanceIds.filter(id =>
          instances.some(i => i.id === id)
        )
        if (validIds.length !== selectedInstanceIds.length) {
          setSelectedInstanceIds(validIds.length > 0 ? validIds : instances.map(i => i.id))
        }
      }
    }
  }, [instances, selectedInstanceIds, setSelectedInstanceIds])

  const { data: dashboard, isLoading, error, refetch } = useQuery({
    queryKey: ["racing-dashboard", selectedInstanceIds, options],
    queryFn: () => api.getRacingDashboard(null, { ...options, instanceIds: selectedInstanceIds }),
    enabled: selectedInstanceIds.length > 0,
    refetchInterval: 30000, // Refresh every 30 seconds
    staleTime: 10000,
  })

  // Transform tracker stats to table data
  const trackerStatsTableData = useMemo<TrackerStatRow[]>(() => {
    if (!dashboard?.trackerStats.byTracker) return []

    return Object.entries(dashboard.trackerStats.byTracker).map(([compositeKey, data]) => {
      // Parse the composite key format: "tracker_instanceId"
      const lastUnderscoreIndex = compositeKey.lastIndexOf("_")
      const tracker = lastUnderscoreIndex > 0 ? compositeKey.substring(0, lastUnderscoreIndex) : compositeKey

      return {
        tracker,
        totalTorrents: data.totalTorrents,
        completedTorrents: data.completedTorrents,
        averageRatio: data.averageRatio,
        averageCompletionTime: data.averageCompletionTime,
        instanceId: data.instanceId || 0,
        instanceName: data.instanceName || "Unknown",
      }
    })
  }, [dashboard?.trackerStats.byTracker])

  const handleRemoveTracker = (tracker: string) => {
    setOptions(prev => ({
      ...prev,
      trackerFilter: prev.trackerFilter?.filter(t => t !== tracker),
    }))
  }

  const handleAddCategory = () => {
    if (categoryInput && !options.categoryFilter?.includes(categoryInput)) {
      setOptions(prev => ({
        ...prev,
        categoryFilter: [...(prev.categoryFilter || []), categoryInput],
      }))
      setCategoryInput("")
    }
  }

  const handleRemoveCategory = (category: string) => {
    setOptions(prev => ({
      ...prev,
      categoryFilter: prev.categoryFilter?.filter(c => c !== category),
    }))
  }

  const resetFilters = () => {
    setOptions({
      limit: DEFAULT_API_LIMIT,
      trackerFilter: [],
      categoryFilter: [],
      minRatio: 0,
      timeRange: "",
      startDate: "",
      endDate: "",
    })
    setCategoryInput("")
    setCustomDateRange(false)
  }

  if (instancesLoading) {
    return (
      <div className="container mx-auto p-6">
        <div className="text-center py-8">
          <div className="inline-block animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
          <p className="mt-2 text-muted-foreground">Loading instances...</p>
        </div>
      </div>
    )
  }

  if (!instances || instances.length === 0) {
    return (
      <div className="container mx-auto p-6">
        <Alert>
          <AlertCircle className="h-4 w-4 text-primary" />
          <AlertDescription>
            No instances configured. Please add an instance first.
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold flex items-center gap-2">
            <Activity className="h-8 w-8" />
            Racing Dashboard
          </h1>
          <p className="text-muted-foreground mt-1">
            Track and analyze your torrent racing performance
          </p>
        </div>
        <Button onClick={() => refetch()} variant="outline" size="sm">
          <RotateCcw className="h-4 w-4 mr-2" />
          Refresh
        </Button>
      </div>

      {/* Filters Section */}
      <Card>
        <Collapsible open={filtersOpen} onOpenChange={setFiltersOpen}>
          <CollapsibleTrigger asChild>
            <CardHeader className="cursor-pointer hover:opacity-80 transition-opacity">
              <div className="flex items-center justify-between">
                <div className="space-y-1 flex-1">
                  <CardTitle className="flex items-center gap-2">
                    <Filter className="h-5 w-5 text-primary" />
                    Filters
                  </CardTitle>
                  <CardDescription>
                    Configure filters to analyze specific conditions
                  </CardDescription>
                </div>
                <ChevronDown className={cn(
                  "h-5 w-5 transition-transform duration-200",
                  filtersOpen ? "" : "-rotate-90"
                )} />
              </div>
            </CardHeader>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <CardContent className="space-y-6">
              {/* Basic Settings Section */}
              <div className="space-y-4 pb-2">
                <div className="flex items-center gap-2 mt-4 text-sm font-medium">
                  <Settings2 className="h-4 w-4 text-primary" />
                  Basic Settings
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {/* Instance Selector */}
                  <div className="space-y-2">
                    <Label className="flex items-center gap-2">
                      <Database className="h-3 w-3 text-primary" />
                      Instances
                    </Label>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="outline" className="w-full justify-between">
                          <span className="truncate">
                            {selectedInstanceIds.length === 0? "Select instances": selectedInstanceIds.length === instances?.length? "All instances": `${selectedInstanceIds.length} instance${selectedInstanceIds.length > 1 ? "s" : ""} selected`}
                          </span>
                          <ChevronDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent className="w-56">
                        <DropdownMenuCheckboxItem
                          checked={selectedInstanceIds.length === instances?.length}
                          onCheckedChange={(checked) => {
                            if (checked && instances) {
                              setSelectedInstanceIds(instances.map(i => i.id))
                            } else {
                              setSelectedInstanceIds([])
                            }
                          }}
                        >
                          Select All
                        </DropdownMenuCheckboxItem>
                        <DropdownMenuSeparator />
                        {instances?.map(instance => (
                          <DropdownMenuCheckboxItem
                            key={instance.id}
                            checked={selectedInstanceIds.includes(instance.id)}
                            onCheckedChange={(checked) => {
                              if (checked) {
                                setSelectedInstanceIds([...selectedInstanceIds, instance.id])
                              } else {
                                setSelectedInstanceIds(selectedInstanceIds.filter(id => id !== instance.id))
                              }
                            }}
                          >
                            {instance.name}
                          </DropdownMenuCheckboxItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                    <p className="text-xs text-muted-foreground">
                      Select one or more qBittorrent instances to analyze
                    </p>
                  </div>

                  {/* Results per section */}
                  <div className="space-y-2">
                    <Label className="flex items-center gap-2">
                      <ListFilter className="h-3 w-3 text-primary" />
                      Results per section
                    </Label>
                    <Select
                      value={options.limit?.toString()}
                      onValueChange={(value) => setOptions(prev => ({ ...prev, limit: Number(value) }))}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {API_LIMIT_OPTIONS.map(limit => (
                          <SelectItem key={limit} value={limit.toString()}>
                            {limit} torrents
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">
                      Number of torrents to display in each category
                    </p>
                  </div>
                </div>
              </div>

              <Separator />

              {/* Performance Filters Section */}
              <div className="space-y-4">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <TrendingUp className="h-4 w-4 text-primary" />
                  Performance Filters
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {/* Min Ratio */}
                  <div className="space-y-2">
                    <Label className="flex items-center gap-2">
                      <Percent className="h-3 w-3 text-primary" />
                      Minimum Ratio
                    </Label>
                    <div className="relative">
                      <Input
                        type="number"
                        step="0.1"
                        min="0"
                        placeholder="0.0"
                        value={options.minRatio || 0}
                        onChange={(e) => setOptions(prev => ({ ...prev, minRatio: parseFloat(e.target.value) || 0 }))}
                        className="pr-8"
                      />
                      <span className="absolute right-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">
                        x
                      </span>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      Filter torrents below this ratio
                    </p>
                  </div>

                  {/* Size Range */}
                  <div className="space-y-2">
                    <Label className="flex items-center gap-2">
                      <HardDrive className="h-3 w-3 text-primary" />
                      Size Range
                    </Label>
                    <div className="flex items-center gap-2">
                      <div className="relative flex-1">
                        <Input
                          type="number"
                          step="0.1"
                          min="0"
                          placeholder="Min"
                          value={options.minSize ? (options.minSize / 1073741824).toFixed(1) : ""}
                          onChange={(e) => setOptions(prev => ({
                            ...prev,
                            minSize: e.target.value ? Math.floor(parseFloat(e.target.value) * 1073741824) : undefined,
                          }))}
                          className="pr-10"
                        />
                        <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-muted-foreground">
                          GB
                        </span>
                      </div>
                      <span className="text-muted-foreground">—</span>
                      <div className="relative flex-1">
                        <Input
                          type="number"
                          step="0.1"
                          min="0"
                          placeholder="Max"
                          value={options.maxSize ? (options.maxSize / 1073741824).toFixed(1) : ""}
                          onChange={(e) => setOptions(prev => ({
                            ...prev,
                            maxSize: e.target.value ? Math.floor(parseFloat(e.target.value) * 1073741824) : undefined,
                          }))}
                          className="pr-10"
                        />
                        <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-muted-foreground">
                          GB
                        </span>
                      </div>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      Filter torrents by size range
                    </p>
                  </div>
                </div>
              </div>

              <Separator />

              {/* Time Period Section */}
              <div className="space-y-4">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Clock className="h-4 w-4 text-primary" />
                  Time Period
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {/* Time Range Preset */}
                  <div className="space-y-2">
                    <Label className="flex items-center gap-2">
                      <CalendarIcon className="h-3 w-3 text-primary" />
                      Time Range
                    </Label>
                    <Select
                      value={customDateRange ? "custom" : options.timeRange || "all"}
                      onValueChange={(value) => {
                        if (value === "custom") {
                          setCustomDateRange(true)
                          setOptions(prev => ({ ...prev, timeRange: "" }))
                        } else if (value === "all") {
                          setCustomDateRange(false)
                          setOptions(prev => ({ ...prev, timeRange: "", startDate: "", endDate: "" }))
                        } else {
                          setCustomDateRange(false)
                          setOptions(prev => ({ ...prev, timeRange: value, startDate: "", endDate: "" }))
                        }
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="all">All Time</SelectItem>
                        <SelectItem value="24h">Last 24 Hours</SelectItem>
                        <SelectItem value="7d">Last 7 Days</SelectItem>
                        <SelectItem value="30d">Last 30 Days</SelectItem>
                        <SelectItem value="custom">Custom Range</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">
                      Filter torrents by time period
                    </p>
                  </div>

                  {/* Custom Date Range */}
                  {customDateRange && (
                    <div className="space-y-2">
                      <Label>Custom Date Range</Label>
                      <Popover>
                        <PopoverTrigger asChild>
                          <Button
                            variant="outline"
                            className={cn(
                              "w-full justify-start text-left font-normal",
                              !dateRange && "text-muted-foreground"
                            )}
                          >
                            <CalendarIcon className="mr-2 h-4 w-4" />
                            {dateRange?.from ? (
                              dateRange.to ? (
                                <>
                                  {format(dateRange.from, "LLL dd, y")} -{" "}
                                  {format(dateRange.to, "LLL dd, y")}
                                </>
                              ) : (
                                format(dateRange.from, "LLL dd, y")
                              )
                            ) : (
                              <span>Pick a date range</span>
                            )}
                          </Button>
                        </PopoverTrigger>
                        <PopoverContent className="w-auto p-0" align="start">
                          <Calendar
                            autoFocus
                            mode="range"
                            defaultMonth={dateRange?.from}
                            selected={dateRange}
                            onSelect={(newRange: DateRange | undefined) => {
                              setDateRange(newRange)
                              if (newRange?.from && newRange?.to) {
                                setOptions(prev => ({
                                  ...prev,
                                  startDate: newRange.from?.toISOString() || "",
                                  endDate: newRange.to?.toISOString() || "",
                                }))
                              }
                            }}
                            numberOfMonths={2}
                            className="rounded-md border p-3"
                            classNames={{
                              months: "flex flex-col space-y-4 sm:flex-row sm:space-x-4 sm:space-y-0 relative",
                              month: "space-y-4",
                              caption: "flex justify-center pt-1 relative items-center px-10",
                              caption_label: "text-sm font-medium",
                              nav: "absolute inset-x-0 top-0 flex w-full items-center justify-between",
                              nav_button: cn(
                                "h-7 w-7 bg-transparent p-0 opacity-50 hover:opacity-100 inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50"
                              ),
                              nav_button_previous: "absolute left-1",
                              nav_button_next: "absolute right-1",
                              table: "w-full border-collapse space-y-1",
                              head_row: "flex",
                              head_cell: "text-muted-foreground rounded-md w-8 font-normal text-[0.8rem]",
                              row: "flex w-full mt-2",
                              cell: "relative h-8 w-8 text-center text-sm p-0 [&:has([aria-selected])]:bg-accent first:[&:has([aria-selected])]:rounded-l-md last:[&:has([aria-selected])]:rounded-r-md focus-within:relative focus-within:z-20",
                              day: cn(
                                "h-8 w-8 p-0 font-normal aria-selected:opacity-100"
                              ),
                              day_selected: "bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground focus:bg-primary focus:text-primary-foreground",
                              day_today: "bg-accent text-accent-foreground",
                              day_outside: "text-muted-foreground opacity-50",
                              day_disabled: "text-muted-foreground opacity-50",
                              day_range_middle: "aria-selected:bg-accent aria-selected:text-accent-foreground",
                              day_hidden: "invisible",
                            }}
                          />
                        </PopoverContent>
                      </Popover>
                      <p className="text-xs text-muted-foreground">
                        Select specific start and end dates
                      </p>
                    </div>
                  )}
                </div>
              </div>

              <Separator />

              {/* Advanced Filters Section */}
              <div className="space-y-4">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Settings2 className="h-4 w-4 text-primary" />
                  Advanced Filters
                </div>

                {/* Tracker Filter */}
                <div className="space-y-2">
                  <Label className="flex items-center gap-2">
                    <Database className="h-3 w-3 text-primary" />
                    Tracker Filter
                  </Label>
                  <div className="flex gap-2">
                    <Select
                      onValueChange={(value) => {
                        if (value && !options.trackerFilter?.includes(value)) {
                          setOptions(prev => ({
                            ...prev,
                            trackerFilter: [...(prev.trackerFilter || []), value],
                          }))
                        }
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select a tracker to filter" />
                      </SelectTrigger>
                      <SelectContent>
                        {dashboard?.trackerStats.byTracker && Object.keys(dashboard.trackerStats.byTracker)
                          .filter(tracker => !options.trackerFilter?.includes(tracker))
                          .sort()
                          .map(tracker => (
                            <SelectItem key={tracker} value={tracker}>
                              {tracker} ({dashboard.trackerStats.byTracker[tracker].totalTorrents} torrents)
                            </SelectItem>
                          ))
                        }
                      </SelectContent>
                    </Select>
                  </div>
                  {options.trackerFilter && options.trackerFilter.length > 0 && (
                    <div className="flex flex-wrap gap-2 mt-2">
                      {options.trackerFilter.map(tracker => (
                        <Badge key={tracker} variant="secondary" className="pl-2">
                          <Database className="h-3 w-3 mr-1" />
                          {tracker}
                          <button
                            className="ml-2 hover:text-destructive transition-colors"
                            onClick={() => handleRemoveTracker(tracker)}
                          >
                            ×
                          </button>
                        </Badge>
                      ))}
                    </div>
                  )}
                  <p className="text-xs text-muted-foreground">
                    Filter results by specific trackers
                  </p>
                </div>

                {/* Category Filter */}
                <div className="space-y-2">
                  <Label className="flex items-center gap-2">
                    <Tag className="h-3 w-3 text-primary" />
                    Category Filter
                  </Label>
                  <div className="flex gap-2">
                    <Input
                      placeholder="e.g., movies, tv-shows"
                      value={categoryInput}
                      onChange={(e) => setCategoryInput(e.target.value)}
                      onKeyDown={(e) => e.key === "Enter" && handleAddCategory()}
                    />
                    <Button onClick={handleAddCategory} size="sm" variant="secondary">
                      Add
                    </Button>
                  </div>
                  {options.categoryFilter && options.categoryFilter.length > 0 && (
                    <div className="flex flex-wrap gap-2 mt-2">
                      {options.categoryFilter.map(category => (
                        <Badge key={category} variant="secondary" className="pl-2">
                          <Tag className="h-3 w-3 mr-1" />
                          {category}
                          <button
                            className="ml-2 hover:text-destructive transition-colors"
                            onClick={() => handleRemoveCategory(category)}
                          >
                            ×
                          </button>
                        </Badge>
                      ))}
                    </div>
                  )}
                  <p className="text-xs text-muted-foreground">
                    Filter results by torrent categories
                  </p>
                </div>
              </div>

              <div className="flex justify-end pb-2">
                <Button onClick={resetFilters} variant="outline" size="sm">
                  <RotateCcw className="h-4 w-4 mr-2" />
                  Reset All Filters
                </Button>
              </div>
            </CardContent>
          </CollapsibleContent>
        </Collapsible>
      </Card>

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4 text-primary" />
          <AlertDescription>
            Failed to load racing dashboard: {(error as Error).message}
          </AlertDescription>
        </Alert>
      )}

      {isLoading && (
        <div className="text-center py-8">
          <div className="inline-block animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
          <p className="mt-2 text-muted-foreground">Loading racing data...</p>
        </div>
      )}

      {dashboard && (
        <>
          {/* Tracker Statistics */}
          <Card className="mb-6 mb-2">
            <CardHeader>
              <CardTitle>Tracker Statistics</CardTitle>
              <CardDescription>
                Overall performance metrics across all trackers
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
                <div>
                  <div className="text-2xl font-bold">{dashboard.trackerStats.totalTorrents}</div>
                  <p className="text-xs text-muted-foreground">Total Torrents</p>
                </div>
                <div>
                  <div className="text-2xl font-bold">{dashboard.trackerStats.completedTorrents}</div>
                  <p className="text-xs text-muted-foreground">Completed</p>
                </div>
                <div>
                  <div className="text-2xl font-bold">{dashboard.trackerStats.averageRatio.toFixed(2)}</div>
                  <p className="text-xs text-muted-foreground">Average Ratio</p>
                </div>
                {dashboard.trackerStats.averageCompletionTime && (
                  <div>
                    <div className="text-2xl font-bold">
                      {formatDuration(dashboard.trackerStats.averageCompletionTime)}
                    </div>
                    <p className="text-xs text-muted-foreground">Avg Completion Time</p>
                  </div>
                )}
              </div>

              {Object.keys(dashboard.trackerStats.byTracker).length > 0 && (
                <>
                  <Separator className="my-4" />
                  <div className="space-y-3">
                    <h4 className="text-sm font-semibold">Per Tracker Breakdown</h4>
                    <DataTable
                      columns={columnsTrackerStats}
                      data={trackerStatsTableData}
                      searchColumn="tracker"
                      searchPlaceholder="Search trackers..."
                      pageSize={DEFAULT_TRACKER_STATS_PAGE_SIZE}
                    />
                  </div>
                </>
              )}
            </CardContent>
          </Card>

          {/* Racing Tables Tabs */}
          <Card className="mb-6">
            <CardHeader>
              <CardTitle>Racing Performance Details</CardTitle>
              <CardDescription>
                Detailed torrent performance metrics
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Tabs defaultValue="fastest" className="w-full">
                <TabsList className="grid w-full grid-cols-3">
                  <TabsTrigger value="fastest" className="flex items-center gap-2">
                    <Clock className="h-4 w-4 text-primary" />
                    Fastest Completions
                  </TabsTrigger>
                  <TabsTrigger value="top-ratios" className="flex items-center gap-2">
                    <TrendingUp className="h-4 w-4 text-primary" />
                    Top Ratios
                  </TabsTrigger>
                  <TabsTrigger value="bottom-ratios" className="flex items-center gap-2">
                    <TrendingDown className="h-4 w-4 text-primary" />
                    Bottom Ratios
                  </TabsTrigger>
                </TabsList>

                <TabsContent value="fastest" className="mt-4">
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Torrents that completed in the shortest time
                    </p>
                    {dashboard.topFastest && dashboard.topFastest.length > 0 ? (
                      <DataTable
                        columns={columnsFastest}
                        data={dashboard.topFastest}
                        searchColumn="name"
                        searchPlaceholder="Search torrents..."
                        pageSize={DEFAULT_TORRENTS_PAGE_SIZE}
                        showPagination={true}
                      />
                    ) : (
                      <p className="text-center text-muted-foreground py-8">No data available</p>
                    )}
                  </div>
                </TabsContent>

                <TabsContent value="top-ratios" className="mt-4">
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Torrents with the highest upload ratios
                    </p>
                    {dashboard.topRatios && dashboard.topRatios.length > 0 ? (
                      <DataTable
                        columns={columnsRatios}
                        data={dashboard.topRatios}
                        searchColumn="name"
                        searchPlaceholder="Search torrents..."
                        pageSize={DEFAULT_TORRENTS_PAGE_SIZE}
                        showPagination={true}
                      />
                    ) : (
                      <p className="text-center text-muted-foreground py-8">No data available</p>
                    )}
                  </div>
                </TabsContent>

                <TabsContent value="bottom-ratios" className="mt-4">
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Torrents with the lowest upload ratios
                    </p>
                    {dashboard.bottomRatios && dashboard.bottomRatios.length > 0 ? (
                      <DataTable
                        columns={columnsRatios}
                        data={dashboard.bottomRatios}
                        searchColumn="name"
                        searchPlaceholder="Search torrents..."
                        pageSize={DEFAULT_TORRENTS_PAGE_SIZE}
                        showPagination={true}
                      />
                    ) : (
                      <p className="text-center text-muted-foreground py-8">No data available</p>
                    )}
                  </div>
                </TabsContent>
              </Tabs>
            </CardContent>
          </Card>

          {/* Performance Charts */}
          <Card className="mb-6">
            <CardHeader>
              <CardTitle>Performance Analytics</CardTitle>
              <CardDescription>
                Visual analysis of torrent performance metrics
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Tabs defaultValue="completion-trends" className="w-full">
                <TabsList className="grid w-full grid-cols-3">
                  <TabsTrigger value="completion-trends" className="flex items-center gap-2">
                    <LineChart className="h-4 w-4 text-primary" />
                    Completion Trends
                  </TabsTrigger>
                  <TabsTrigger value="data-volume" className="flex items-center gap-2">
                    <TrendingUp className="h-4 w-4 text-primary" />
                    Data Volume
                  </TabsTrigger>
                  <TabsTrigger value="tracker-performance" className="flex items-center gap-2">
                    <BarChart3 className="h-4 w-4 text-primary" />
                    Tracker Performance
                  </TabsTrigger>
                </TabsList>

                <TabsContent value="completion-trends" className="mt-4">
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Average completion times over time
                    </p>
                    <CompletionTimeChart
                      data={[
                        ...(dashboard.topFastest || []),
                        ...(dashboard.topRatios || []),
                        ...(dashboard.bottomRatios || []),
                      ]}
                      timeRange={options.timeRange || "7d"}
                    />
                  </div>
                </TabsContent>

                <TabsContent value="data-volume" className="mt-4">
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Upload and download volume over time
                    </p>
                    <VolumeChart
                      data={[
                        ...(dashboard.topFastest || []),
                        ...(dashboard.topRatios || []),
                        ...(dashboard.bottomRatios || []),
                      ]}
                      timeRange={options.timeRange || "7d"}
                    />
                  </div>
                </TabsContent>

                <TabsContent value="tracker-performance" className="mt-4">
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Compare performance metrics across trackers
                    </p>
                    <TrackerPerformanceChart data={trackerStatsTableData} />
                  </div>
                </TabsContent>
              </Tabs>
            </CardContent>
          </Card>

          {/* Last Updated */}
          <div className="text-center text-sm text-muted-foreground">
            Last updated: {formatDate(dashboard.lastUpdated)}
          </div>
        </>
      )}
    </div>
  )
}