/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useQuery } from "@tanstack/react-query"
import { useState, useMemo } from "react"
import { Search, X, Film, Music, Tv, Package, Download } from "lucide-react"
import type { TitlesResponse, TitlesFilterOptions } from "@/types"

interface TitlesProps {
  instanceId: number
  instanceName: string
}

const releaseTypeIcons: Record<string, React.ReactNode> = {
  movie: <Film className="h-4 w-4" />,
  music: <Music className="h-4 w-4" />,
  series: <Tv className="h-4 w-4" />,
  episode: <Tv className="h-4 w-4" />,
  app: <Package className="h-4 w-4" />,
  game: <Package className="h-4 w-4" />,
}

export function Titles({ instanceId, instanceName }: TitlesProps) {
  const [filters, setFilters] = useState<TitlesFilterOptions>({})
  const [searchInput, setSearchInput] = useState("")

  // Fetch titles data
  const { data, isLoading, error } = useQuery<TitlesResponse>({
    queryKey: ["titles", instanceId, filters],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (Object.keys(filters).length > 0) {
        params.append("filters", JSON.stringify(filters))
      }
      
      const response = await fetch(
        `/api/instances/${instanceId}/titles?${params.toString()}`,
        {
          credentials: "include",
        }
      )
      
      if (!response.ok) {
        throw new Error("Failed to fetch parsed titles")
      }
      
      return response.json()
    },
    refetchInterval: 30000, // Refresh every 30 seconds
  })

  // Extract unique values for filters
  const uniqueValues = useMemo(() => {
    if (!data?.titles) return { types: [], sources: [], resolutions: [], groups: [], years: [] }
    
    const types = new Set<string>()
    const sources = new Set<string>()
    const resolutions = new Set<string>()
    const groups = new Set<string>()
    const years = new Set<number>()
    
    data.titles.forEach((title) => {
      if (title.type) types.add(title.type)
      if (title.source) sources.add(title.source)
      if (title.resolution) resolutions.add(title.resolution)
      if (title.group) groups.add(title.group)
      if (title.year) years.add(title.year)
    })
    
    return {
      types: Array.from(types).sort(),
      sources: Array.from(sources).sort(),
      resolutions: Array.from(resolutions).sort(),
      groups: Array.from(groups).sort(),
      years: Array.from(years).sort((a, b) => b - a),
    }
  }, [data?.titles])

  const handleFilterChange = (key: keyof TitlesFilterOptions, value: string | number | undefined) => {
    setFilters((prev) => {
      if (!value || value === "all") {
        const newFilters = { ...prev }
        delete newFilters[key]
        return newFilters
      }
      return { ...prev, [key]: value }
    })
  }

  const handleSearch = () => {
    setFilters((prev) => ({
      ...prev,
      search: searchInput || undefined,
    }))
  }

  const clearFilters = () => {
    setFilters({})
    setSearchInput("")
  }

  const formatSize = (bytes: number) => {
    if (bytes === 0) return "0 B"
    const k = 1024
    const sizes = ["B", "KB", "MB", "GB", "TB"]
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
  }

  const formatDate = (timestamp: number) => {
    if (!timestamp) return "N/A"
    return new Date(timestamp * 1000).toLocaleString()
  }

  const getReleaseTypeIcon = (type: string) => {
    return releaseTypeIcons[type.toLowerCase()] || <Download className="h-4 w-4" />
  }

  const activeFiltersCount = Object.keys(filters).length

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Parsed Titles</h1>
          <p className="text-muted-foreground">
            View and filter torrent releases with parsed metadata from {instanceName}
          </p>
        </div>
        {activeFiltersCount > 0 && (
          <Button variant="outline" onClick={clearFilters}>
            <X className="mr-2 h-4 w-4" />
            Clear Filters ({activeFiltersCount})
          </Button>
        )}
      </div>

      {/* Filters */}
      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
          <CardDescription>Filter parsed titles by various criteria</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <div className="space-y-2">
              <Label htmlFor="type-filter">Type</Label>
              <Select
                value={filters.type || "all"}
                onValueChange={(value) => handleFilterChange("type", value)}
              >
                <SelectTrigger id="type-filter">
                  <SelectValue placeholder="All Types" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Types</SelectItem>
                  {uniqueValues.types.map((type) => (
                    <SelectItem key={type} value={type}>
                      {type}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="source-filter">Source</Label>
              <Select
                value={filters.source || "all"}
                onValueChange={(value) => handleFilterChange("source", value)}
              >
                <SelectTrigger id="source-filter">
                  <SelectValue placeholder="All Sources" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Sources</SelectItem>
                  {uniqueValues.sources.map((source) => (
                    <SelectItem key={source} value={source}>
                      {source}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="resolution-filter">Resolution</Label>
              <Select
                value={filters.resolution || "all"}
                onValueChange={(value) => handleFilterChange("resolution", value)}
              >
                <SelectTrigger id="resolution-filter">
                  <SelectValue placeholder="All Resolutions" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Resolutions</SelectItem>
                  {uniqueValues.resolutions.map((resolution) => (
                    <SelectItem key={resolution} value={resolution}>
                      {resolution}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="group-filter">Group</Label>
              <Select
                value={filters.group || "all"}
                onValueChange={(value) => handleFilterChange("group", value)}
              >
                <SelectTrigger id="group-filter">
                  <SelectValue placeholder="All Groups" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Groups</SelectItem>
                  {uniqueValues.groups.map((group) => (
                    <SelectItem key={group} value={group}>
                      {group}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="year-filter">Year</Label>
              <Select
                value={filters.year?.toString() || "all"}
                onValueChange={(value) => handleFilterChange("year", value === "all" ? undefined : parseInt(value))}
              >
                <SelectTrigger id="year-filter">
                  <SelectValue placeholder="All Years" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Years</SelectItem>
                  {uniqueValues.years.map((year) => (
                    <SelectItem key={year} value={year.toString()}>
                      {year}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="flex gap-2">
            <div className="flex-1 space-y-2">
              <Label htmlFor="search">Search</Label>
              <div className="flex gap-2">
                <Input
                  id="search"
                  placeholder="Search by name, title, or group..."
                  value={searchInput}
                  onChange={(e) => setSearchInput(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && handleSearch()}
                />
                <Button onClick={handleSearch}>
                  <Search className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Results */}
      <Card>
        <CardHeader>
          <CardTitle>
            Results {data && `(${data.total})`}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && (
            <div className="text-center py-8">
              <p className="text-muted-foreground">Loading parsed titles...</p>
            </div>
          )}

          {error && (
            <div className="text-center py-8">
              <p className="text-destructive">Error loading titles: {(error as Error).message}</p>
            </div>
          )}

          {data && data.titles.length === 0 && (
            <div className="text-center py-8">
              <p className="text-muted-foreground">No titles found matching your filters</p>
            </div>
          )}

          {data && data.titles.length > 0 && (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[50px]">Type</TableHead>
                    <TableHead>Title</TableHead>
                    <TableHead>Source</TableHead>
                    <TableHead>Resolution</TableHead>
                    <TableHead>Codec</TableHead>
                    <TableHead>Audio</TableHead>
                    <TableHead>Group</TableHead>
                    <TableHead>Year</TableHead>
                    <TableHead>Size</TableHead>
                    <TableHead>Added</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.titles.map((title) => (
                    <TableRow key={title.hash}>
                      <TableCell>
                        <div className="flex items-center justify-center">
                          {getReleaseTypeIcon(title.type)}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="space-y-1">
                          <div className="font-medium truncate max-w-md" title={title.title || title.name}>
                            {title.title || title.name}
                          </div>
                          {title.artist && (
                            <div className="text-xs text-muted-foreground">{title.artist}</div>
                          )}
                          {title.subtitle && (
                            <div className="text-xs text-muted-foreground">{title.subtitle}</div>
                          )}
                          {title.edition && title.edition.length > 0 && (
                            <div className="flex gap-1 flex-wrap">
                              {title.edition.map((ed, i) => (
                                <Badge key={i} variant="outline" className="text-xs">
                                  {ed}
                                </Badge>
                              ))}
                            </div>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>{title.source || "-"}</TableCell>
                      <TableCell>{title.resolution || "-"}</TableCell>
                      <TableCell>
                        {title.codec && title.codec.length > 0 ? (
                          <div className="flex gap-1 flex-wrap">
                            {title.codec.map((c, i) => (
                              <Badge key={i} variant="secondary" className="text-xs">
                                {c}
                              </Badge>
                            ))}
                          </div>
                        ) : (
                          "-"
                        )}
                      </TableCell>
                      <TableCell>
                        {title.audio && title.audio.length > 0 ? (
                          <div className="flex gap-1 flex-wrap">
                            {title.audio.map((a, i) => (
                              <Badge key={i} variant="secondary" className="text-xs">
                                {a}
                              </Badge>
                            ))}
                          </div>
                        ) : (
                          "-"
                        )}
                      </TableCell>
                      <TableCell>
                        {title.group ? (
                          <Badge variant="default">{title.group}</Badge>
                        ) : (
                          "-"
                        )}
                      </TableCell>
                      <TableCell>{title.year || "-"}</TableCell>
                      <TableCell className="text-xs">{formatSize(title.size)}</TableCell>
                      <TableCell className="text-xs">{formatDate(title.addedOn)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
