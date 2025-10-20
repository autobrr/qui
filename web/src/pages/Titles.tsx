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
import { useQuery } from "@tanstack/react-query"
import { useState, useMemo, useRef } from "react"
import { Search, X, Film, Music, Tv, Package, Download, ChevronDown, ChevronRight, HardDrive, Zap, Crown, Play, Pause, Trash2, FolderOpen, RotateCcw } from "lucide-react"
import { useVirtualizer } from "@tanstack/react-virtual"
import type { TitlesResponse, TitlesFilterOptions, ParsedTitle } from "@/types"

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

interface GroupedTitle {
  title: string
  type: string
  items: ParsedTitle[]
  totalSize: number
  subGroups: Map<string, ParsedTitle[]>
  bestQualityItem?: ParsedTitle
  upgrades: ParsedTitle[]
}

interface VirtualItem {
  type: 'group' | 'item'
  data: GroupedTitle | ParsedTitle
  groupTitle?: string
  subGroupKey?: string
  depth: number
}

// Quality scoring and comparison utilities
interface QualityScore {
  score: number
  level: 'SD' | 'HD' | 'FHD' | 'UHD' | 'HDR'
  label: string
  color: string
}

function calculateQualityScore(item: ParsedTitle): QualityScore {
  let score = 0
  let level: QualityScore['level'] = 'SD'
  let label = 'SD'
  let color = 'secondary'

  // Resolution scoring
  const resolution = item.resolution?.toLowerCase()
  if (resolution?.includes('2160p') || resolution?.includes('4k')) {
    score += 100
    level = 'UHD'
    label = '4K'
    color = 'default'
  } else if (resolution?.includes('1080p') || resolution?.includes('fhd')) {
    score += 75
    level = 'FHD'
    label = '1080p'
    color = 'default'
  } else if (resolution?.includes('720p') || resolution?.includes('hd')) {
    score += 50
    level = 'HD'
    label = '720p'
    color = 'secondary'
  } else {
    score += 25
    level = 'SD'
    label = 'SD'
    color = 'outline'
  }

  // HDR bonus
  if (item.hdr && item.hdr.length > 0) {
    score += 20
    if (level === 'UHD') {
      label += ' HDR'
    }
  }

  // Source quality bonus
  const source = item.source?.toLowerCase()
  if (source?.includes('bluray') || source?.includes('bd')) {
    score += 15
  } else if (source?.includes('web')) {
    score += 10
  } else if (source?.includes('hdtv')) {
    score += 5
  }

  // Codec quality bonus
  if (item.codec && item.codec.some(c => c.toLowerCase().includes('x265') || c.toLowerCase().includes('hevc'))) {
    score += 10
  }

  return { score, level, label, color }
}

export function Titles({ instanceId, instanceName }: TitlesProps) {
  const [filters, setFilters] = useState<TitlesFilterOptions>({})
  const [searchInput, setSearchInput] = useState("")
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
  const [showUpgrades, setShowUpgrades] = useState(false)
  const [selectedItems, setSelectedItems] = useState<Set<string>>(new Set())
  const parentRef = useRef<HTMLDivElement>(null)

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

  // Group titles by title, then by year/season
  const groupedTitles = useMemo(() => {
    if (!data?.titles) return []
    
    const groups = new Map<string, GroupedTitle>()
    
    data.titles.forEach((item) => {
      const titleKey = item.title || item.name || "Unknown"
      
      if (!groups.has(titleKey)) {
        groups.set(titleKey, {
          title: titleKey,
          type: item.type,
          items: [],
          totalSize: 0,
          subGroups: new Map(),
          upgrades: [],
        })
      }
      
      const group = groups.get(titleKey)!
      group.items.push(item)
      group.totalSize += item.size
      
      // Create subgroups based on type
      let subGroupKey = "Other"
      if (item.type === "episode" || item.type === "series") {
        // Group by season
        if (item.series !== undefined && item.series !== null) {
          subGroupKey = `Season ${item.series}`
        } else if (item.year) {
          subGroupKey = `${item.year}`
        }
      } else if (item.year) {
        // For movies, group by year
        subGroupKey = `${item.year}`
      }
      
      if (!group.subGroups.has(subGroupKey)) {
        group.subGroups.set(subGroupKey, [])
      }
      group.subGroups.get(subGroupKey)!.push(item)
    })
    
    // Calculate best quality items and upgrades for each group
    groups.forEach((group) => {
      if (group.items.length > 0) {
        // Find the best quality item (highest score)
        let bestItem = group.items[0]
        let bestScore = calculateQualityScore(bestItem).score
        
        group.items.forEach((item) => {
          const score = calculateQualityScore(item).score
          if (score > bestScore) {
            bestScore = score
            bestItem = item
          }
        })
        
        group.bestQualityItem = bestItem
        
        // Find upgrades (items that are significantly better than others)
        const upgrades: ParsedTitle[] = []
        group.items.forEach((item) => {
          if (item.hash !== bestItem.hash && calculateQualityScore(item).score >= bestScore - 10) {
            // This is a high-quality item, could be an upgrade
            upgrades.push(item)
          }
        })
        
        group.upgrades = upgrades
      }
    })

    return Array.from(groups.values()).sort((a, b) => 
      a.title.localeCompare(b.title)
    )
  }, [data?.titles])

  // Create upgrade recommendations
  const upgradeRecommendations = useMemo(() => {
    if (!data?.titles) return []
    
    const recommendations: Array<{
      title: string
      type: string
      currentBest: ParsedTitle
      potentialUpgrades: ParsedTitle[]
      reason: string
    }> = []
    
    groupedTitles.forEach((group) => {
      if (group.upgrades.length > 0 && group.bestQualityItem) {
        const currentScore = calculateQualityScore(group.bestQualityItem)
        const upgradeReasons: string[] = []
        
        // Analyze why these are upgrades
        const qualityImprovements = group.upgrades.map(upgrade => {
          const upgradeScore = calculateQualityScore(upgrade)
          const improvements: string[] = []
          
          if (upgradeScore.level !== currentScore.level) {
            improvements.push(`${currentScore.level} → ${upgradeScore.level}`)
          }
          
          if (upgrade.resolution && group.bestQualityItem.resolution && 
              upgrade.resolution !== group.bestQualityItem.resolution) {
            improvements.push(`Resolution: ${group.bestQualityItem.resolution} → ${upgrade.resolution}`)
          }
          
          if (upgrade.hdr && upgrade.hdr.length > 0 && 
              (!group.bestQualityItem.hdr || group.bestQualityItem.hdr.length === 0)) {
            improvements.push('Adds HDR')
          }
          
          if (upgrade.codec && upgrade.codec.some(c => c.toLowerCase().includes('x265') || c.toLowerCase().includes('hevc')) &&
              (!group.bestQualityItem.codec || !group.bestQualityItem.codec.some(c => c.toLowerCase().includes('x265') || c.toLowerCase().includes('hevc')))) {
            improvements.push('Better codec (x265/HEVC)')
          }
          
          return {
            item: upgrade,
            improvements,
            scoreDiff: upgradeScore.score - currentScore.score
          }
        })
        
        if (qualityImprovements.some(u => u.improvements.length > 0)) {
          upgradeReasons.push('Quality improvements available')
        }
        
        if (upgradeReasons.length > 0) {
          recommendations.push({
            title: group.title,
            type: group.type,
            currentBest: group.bestQualityItem,
            potentialUpgrades: group.upgrades,
            reason: upgradeReasons[0]
          })
        }
      }
    })
    
    return recommendations.sort((a, b) => b.potentialUpgrades.length - a.potentialUpgrades.length)
  }, [groupedTitles, data?.titles])

  // Create virtual items for rendering (only when not showing upgrades)
  const virtualItems = useMemo(() => {
    if (showUpgrades) return []
    
    const items: VirtualItem[] = []
    
    groupedTitles.forEach((group) => {
      // Add group header
      items.push({
        type: 'group',
        data: group,
        depth: 0,
      })
      
      // If expanded, add subgroups and their items
      if (expandedGroups.has(group.title)) {
        const sortedSubGroups = Array.from(group.subGroups.entries()).sort((a, b) => {
          // Sort seasons/years in reverse order (newest first)
          return b[0].localeCompare(a[0])
        })
        
        sortedSubGroups.forEach(([subGroupKey, subGroupItems]) => {
          // Add subgroup header
          items.push({
            type: 'group',
            data: { ...group, items: subGroupItems } as GroupedTitle,
            groupTitle: group.title,
            subGroupKey,
            depth: 1,
          })
          
          // If subgroup is expanded, add individual items
          const subGroupId = `${group.title}::${subGroupKey}`
          if (expandedGroups.has(subGroupId)) {
            subGroupItems.forEach((item) => {
              items.push({
                type: 'item',
                data: item,
                groupTitle: group.title,
                subGroupKey,
                depth: 2,
              })
            })
          }
        })
      }
    })
    
    return items
  }, [groupedTitles, expandedGroups, showUpgrades])

  const virtualizer = useVirtualizer({
    count: virtualItems.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      const item = virtualItems[index]
      if (item.type === 'group') {
        return item.depth === 0 ? 80 : 60 // Main group vs subgroup
      }
      return 120 // Individual item
    },
    overscan: 5,
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

  const toggleGroup = (groupId: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev)
      if (next.has(groupId)) {
        next.delete(groupId)
        // Also collapse all subgroups
        Array.from(next).forEach((id) => {
          if (id.startsWith(`${groupId}::`)) {
            next.delete(id)
          }
        })
      } else {
        next.add(groupId)
      }
      return next
    })
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

  // Calculate analytics
  const analytics = useMemo(() => {
    if (!data?.titles) return null
    
    const totalSize = data.titles.reduce((sum, item) => sum + item.size, 0)
    const totalItems = data.titles.length
    
    // Content type breakdown
    const typeBreakdown = data.titles.reduce((acc, item) => {
      acc[item.type] = (acc[item.type] || 0) + 1
      return acc
    }, {} as Record<string, number>)
    
    // Quality breakdown
    const qualityBreakdown = data.titles.reduce((acc, item) => {
      const quality = calculateQualityScore(item)
      acc[quality.level] = (acc[quality.level] || 0) + 1
      return acc
    }, {} as Record<string, number>)
    
    // Source breakdown
    const sourceBreakdown = data.titles.reduce((acc, item) => {
      if (item.source) {
        acc[item.source] = (acc[item.source] || 0) + 1
      }
      return acc
    }, {} as Record<string, number>)
    
    // Missing episodes/seasons detection
    const seriesData: Record<string, { episodes: Set<number>, seasons: Set<number> }> = {}
    data.titles.forEach((item) => {
      if ((item.type === 'episode' || item.type === 'series') && item.title) {
        const title = item.title
        if (!seriesData[title]) {
          seriesData[title] = { episodes: new Set(), seasons: new Set() }
        }
        if (item.episode) seriesData[title].episodes.add(item.episode)
        if (item.series) seriesData[title].seasons.add(item.series)
      }
    })
    
    // Calculate missing content (simplified - just count gaps)
    let totalEpisodes = 0
    let missingEpisodes = 0
    Object.values(seriesData).forEach((data) => {
      totalEpisodes += data.episodes.size
      // Simple gap detection for episodes 1-50 (could be improved)
      for (let i = 1; i <= Math.max(...Array.from(data.episodes)); i++) {
        if (!data.episodes.has(i)) missingEpisodes++
      }
    })
    
    // Completion rates (assuming completed means not in error state)
    const completedItems = data.titles.filter(item => 
      item.state !== 'error' && item.state !== 'missingFiles'
    ).length
    const completionRate = totalItems > 0 ? (completedItems / totalItems) * 100 : 0
    
    return {
      totalSize,
      totalItems,
      typeBreakdown,
      qualityBreakdown,
      sourceBreakdown,
      completionRate,
      completedItems,
      missingEpisodes,
      seriesCount: Object.keys(seriesData).length
    }
  }, [data?.titles])

  const activeFiltersCount = Object.keys(filters).length

  const handleTorrentAction = async (action: string, hash: string) => {
    try {
      const response = await fetch(`/api/instances/${instanceId}/torrents/${hash}/${action}`, {
        method: 'POST',
        credentials: 'include',
      })
      
      if (!response.ok) {
        throw new Error(`Failed to ${action} torrent`)
      }
      
      // Refresh data
      // This would trigger a refetch of the titles data
    } catch (error) {
      console.error(`Error ${action} torrent:`, error)
    }
  }

  const handleCategoryChange = async (hash: string, category: string) => {
    try {
      const response = await fetch(`/api/instances/${instanceId}/torrents/${hash}/category`, {
        method: 'POST',
        body: JSON.stringify({ category }),
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
      })
      
      if (!response.ok) {
        throw new Error('Failed to change category')
      }
    } catch (error) {
      console.error('Error changing category:', error)
    }
  }

  const handleBulkAction = async (action: string) => {
    if (selectedItems.size === 0) return
    
    try {
      const hashes = Array.from(selectedItems)
      const response = await fetch(`/api/instances/${instanceId}/torrents/bulk/${action}`, {
        method: 'POST',
        body: JSON.stringify({ hashes }),
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
      })
      
      if (!response.ok) {
        throw new Error(`Failed to ${action} torrents`)
      }
      
      setSelectedItems(new Set())
    } catch (error) {
      console.error(`Error bulk ${action}:`, error)
    }
  }

  const toggleItemSelection = (hash: string) => {
    setSelectedItems(prev => {
      const next = new Set(prev)
      if (next.has(hash)) {
        next.delete(hash)
      } else {
        next.add(hash)
      }
      return next
    })
  }

  const clearSelection = () => {
    setSelectedItems(new Set())
  }

  const applyPresetFilter = (preset: string) => {
    switch (preset) {
      case 'recent-4k':
        setFilters({ resolution: '2160p', type: 'movie' })
        break
      case 'incomplete-series':
        setFilters({ type: 'episode' })
        break
      case 'high-quality':
        setFilters({ resolution: '1080p', source: 'bluray' })
        break
      case 'new-releases':
        // Could filter by recent dates
        setFilters({})
        break
      default:
        setFilters({})
    }
  }

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Parsed Titles</h1>
          <p className="text-muted-foreground">
            View and filter torrent releases with parsed metadata from {instanceName}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button 
            variant={showUpgrades ? "default" : "outline"} 
            size="sm"
            onClick={() => setShowUpgrades(!showUpgrades)}
          >
            <Crown className="mr-2 h-4 w-4" />
            {showUpgrades ? "Hide Upgrades" : "Show Upgrades"}
          </Button>
          {selectedItems.size > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">
                {selectedItems.size} selected
              </span>
              <Button variant="outline" size="sm" onClick={clearSelection}>
                Clear
              </Button>
              <Button variant="outline" size="sm" onClick={() => handleBulkAction('pause')}>
                <Pause className="mr-2 h-4 w-4" />
                Pause All
              </Button>
              <Button variant="outline" size="sm" onClick={() => handleBulkAction('resume')}>
                <Play className="mr-2 h-4 w-4" />
                Resume All
              </Button>
              <Button variant="destructive" size="sm" onClick={() => handleBulkAction('delete')}>
                <Trash2 className="mr-2 h-4 w-4" />
                Delete All
              </Button>
            </div>
          )}
          {activeFiltersCount > 0 && (
            <Button variant="outline" onClick={clearFilters}>
              <X className="mr-2 h-4 w-4" />
              Clear Filters ({activeFiltersCount})
            </Button>
          )}
        </div>
      </div>

      {/* Analytics Dashboard */}
      {!showUpgrades && analytics && (
        <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-5 gap-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Size</CardTitle>
              <HardDrive className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatSize(analytics.totalSize)}</div>
              <p className="text-xs text-muted-foreground">
                {analytics.totalItems} items
              </p>
            </CardContent>
          </Card>
          
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Completion Rate</CardTitle>
              <Zap className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{analytics.completionRate.toFixed(1)}%</div>
              <p className="text-xs text-muted-foreground">
                {analytics.completedItems} of {analytics.totalItems} complete
              </p>
            </CardContent>
          </Card>
          
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Content Types</CardTitle>
              <Film className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{Object.keys(analytics.typeBreakdown).length}</div>
              <p className="text-xs text-muted-foreground">
                {Object.entries(analytics.typeBreakdown)
                  .sort(([,a], [,b]) => b - a)
                  .slice(0, 2)
                  .map(([type, count]) => `${type}: ${count}`)
                  .join(', ')}
              </p>
            </CardContent>
          </Card>
          
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Quality Distribution</CardTitle>
              <Crown className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {analytics.qualityBreakdown.UHD || 0} UHD
              </div>
              <p className="text-xs text-muted-foreground">
                {analytics.qualityBreakdown.FHD || 0} FHD, {analytics.qualityBreakdown.HD || 0} HD
              </p>
            </CardContent>
          </Card>
          
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Series Collection</CardTitle>
              <Tv className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {analytics.seriesCount || 0}
              </div>
              <p className="text-xs text-muted-foreground">
                series • {analytics.missingEpisodes || 0} missing episodes
              </p>
            </CardContent>
          </Card>
        </div>
      )}
        <Card>
          <CardHeader>
            <CardTitle>Filters</CardTitle>
            <CardDescription>Filter parsed titles by various criteria</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Preset Filters */}
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={() => applyPresetFilter('recent-4k')}>
                <Crown className="mr-2 h-4 w-4" />
                Recent 4K Movies
              </Button>
              <Button variant="outline" size="sm" onClick={() => applyPresetFilter('incomplete-series')}>
                <Tv className="mr-2 h-4 w-4" />
                Incomplete Series
              </Button>
              <Button variant="outline" size="sm" onClick={() => applyPresetFilter('high-quality')}>
                <Zap className="mr-2 h-4 w-4" />
                High Quality
              </Button>
              <Button variant="outline" size="sm" onClick={() => applyPresetFilter('new-releases')}>
                <Package className="mr-2 h-4 w-4" />
                New Releases
              </Button>
            </div>

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
      )}

      {/* Results */}
      <Card>
        <CardHeader>
          <CardTitle>
            {showUpgrades 
              ? `Upgrade Recommendations (${upgradeRecommendations.length} titles)`
              : `Grouped Titles ${data ? `(${groupedTitles.length} titles, ${data.total} items)` : ''}`
            }
          </CardTitle>
          <CardDescription>
            {showUpgrades 
              ? "Potential quality upgrades for your collection with recommended actions"
              : "Click on a title to expand and view releases grouped by year or season"
            }
          </CardDescription>
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

          {data && showUpgrades && upgradeRecommendations.length === 0 && (
            <div className="text-center py-8">
              <p className="text-muted-foreground">No upgrade recommendations found. Your collection is up to date!</p>
            </div>
          )}

          {data && !showUpgrades && groupedTitles.length === 0 && (
            <div className="text-center py-8">
              <p className="text-muted-foreground">No titles found matching your filters</p>
            </div>
          )}

          {data && showUpgrades && upgradeRecommendations.length > 0 && (
            <div className="space-y-4">
              {upgradeRecommendations.map((rec, index) => (
                <UpgradeRecommendationCard
                  key={index}
                  recommendation={rec}
                  formatSize={formatSize}
                  formatDate={formatDate}
                  getReleaseTypeIcon={getReleaseTypeIcon}
                  onTorrentAction={handleTorrentAction}
                  onCategoryChange={handleCategoryChange}
                />
              ))}
            </div>
          )}

          {data && !showUpgrades && groupedTitles.length > 0 && (
            <div
              ref={parentRef}
              className="h-[600px] overflow-auto border rounded-lg"
            >
              <div
                style={{
                  height: `${virtualizer.getTotalSize()}px`,
                  width: '100%',
                  position: 'relative',
                }}
              >
                {virtualizer.getVirtualItems().map((virtualRow) => {
                  const item = virtualItems[virtualRow.index]
                  
                  return (
                    <div
                      key={virtualRow.index}
                      style={{
                        position: 'absolute',
                        top: 0,
                        left: 0,
                        width: '100%',
                        height: `${virtualRow.size}px`,
                        transform: `translateY(${virtualRow.start}px)`,
                      }}
                    >
                      {item.type === 'group' ? (
                        <GroupHeader
                          group={item.data as GroupedTitle}
                          isExpanded={expandedGroups.has(
                            item.subGroupKey 
                              ? `${item.groupTitle}::${item.subGroupKey}`
                              : (item.data as GroupedTitle).title
                          )}
                          onToggle={() => toggleGroup(
                            item.subGroupKey 
                              ? `${item.groupTitle}::${item.subGroupKey}`
                              : (item.data as GroupedTitle).title
                          )}
                          depth={item.depth}
                          subGroupKey={item.subGroupKey}
                          formatSize={formatSize}
                          getReleaseTypeIcon={getReleaseTypeIcon}
                        />
                      ) : (
                        <TitleItem
                          item={item.data as ParsedTitle}
                          formatSize={formatSize}
                          formatDate={formatDate}
                          getReleaseTypeIcon={getReleaseTypeIcon}
                          showUpgrades={showUpgrades}
                          isUpgrade={false} // TODO: Calculate based on group.upgrades
                          onTorrentAction={handleTorrentAction}
                          onCategoryChange={handleCategoryChange}
                          isSelected={selectedItems.has((item.data as ParsedTitle).hash)}
                          onToggleSelection={toggleItemSelection}
                        />
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

interface GroupHeaderProps {
  group: GroupedTitle
  isExpanded: boolean
  onToggle: () => void
  depth: number
  subGroupKey?: string
  formatSize: (bytes: number) => string
  getReleaseTypeIcon: (type: string) => React.ReactNode
}

function GroupHeader({ 
  group, 
  isExpanded, 
  onToggle, 
  depth, 
  subGroupKey,
  formatSize,
  getReleaseTypeIcon 
}: GroupHeaderProps) {
  const paddingLeft = depth * 24 + 16
  
  if (depth === 0) {
    // Main group header
    return (
      <button
        onClick={onToggle}
        className="w-full text-left px-4 py-4 hover:bg-accent/50 transition-colors border-b flex items-center gap-3"
        style={{ paddingLeft: `${paddingLeft}px` }}
      >
        {isExpanded ? (
          <ChevronDown className="h-5 w-5 flex-shrink-0" />
        ) : (
          <ChevronRight className="h-5 w-5 flex-shrink-0" />
        )}
        <div className="flex items-center gap-2 flex-shrink-0">
          {getReleaseTypeIcon(group.type)}
        </div>
        <div className="flex-1 min-w-0">
          <div className="font-semibold text-lg truncate">{group.title}</div>
          <div className="text-sm text-muted-foreground">
            {group.items.length} release{group.items.length !== 1 ? 's' : ''} • {formatSize(group.totalSize)}
            {group.subGroups.size > 1 && ` • ${group.subGroups.size} ${group.type === 'episode' || group.type === 'series' ? 'seasons' : 'years'}`}
          </div>
        </div>
      </button>
    )
  }
  
  // Subgroup header
  return (
    <button
      onClick={onToggle}
      className="w-full text-left px-4 py-3 hover:bg-accent/30 transition-colors border-b border-border/50 flex items-center gap-3"
      style={{ paddingLeft: `${paddingLeft}px` }}
    >
      {isExpanded ? (
        <ChevronDown className="h-4 w-4 flex-shrink-0" />
      ) : (
        <ChevronRight className="h-4 w-4 flex-shrink-0" />
      )}
      <div className="flex-1 flex items-center justify-between min-w-0">
        <div className="font-medium truncate">{subGroupKey}</div>
        <div className="text-sm text-muted-foreground flex-shrink-0 ml-4">
          {group.items.length} item{group.items.length !== 1 ? 's' : ''} • {formatSize(group.items.reduce((sum, item) => sum + item.size, 0))}
        </div>
      </div>
    </button>
  )
}

interface TitleItemProps {
  item: ParsedTitle
  formatSize: (bytes: number) => string
  formatDate: (timestamp: number) => string
  getReleaseTypeIcon: (type: string) => React.ReactNode
  showUpgrades: boolean
  isUpgrade: boolean
  onTorrentAction: (action: string, hash: string) => Promise<void>
  onCategoryChange: (hash: string, category: string) => Promise<void>
  isSelected: boolean
  onToggleSelection: (hash: string) => void
}

function TitleItem({ item, formatSize, formatDate, getReleaseTypeIcon, showUpgrades, isUpgrade, onTorrentAction, onCategoryChange, isSelected, onToggleSelection }: TitleItemProps) {
  const qualityScore = calculateQualityScore(item)
  
  return (
    <div className="px-4 py-3 border-b border-border/50 bg-card hover:bg-accent/20 transition-colors" style={{ paddingLeft: '72px' }}>
      <div className="flex items-start gap-4">
        <div className="flex items-center gap-2 flex-shrink-0">
          <input
            type="checkbox"
            checked={isSelected}
            onChange={() => onToggleSelection(item.hash)}
            className="rounded border-gray-300"
          />
          {getReleaseTypeIcon(item.type)}
        </div>
        <div className="flex-1 min-w-0 space-y-2">
          <div className="flex items-center gap-2">
            <div className="font-medium truncate" title={item.name}>
              {item.name}
            </div>
            <Badge variant={qualityScore.color as any} className="flex-shrink-0">
              {qualityScore.label}
            </Badge>
            {showUpgrades && isUpgrade && (
              <Badge variant="destructive" className="flex-shrink-0">
                <Crown className="mr-1 h-3 w-3" />
                Upgrade Available
              </Badge>
            )}
          </div>
          
          <div className="flex flex-wrap gap-2 items-center text-sm">
            {item.resolution && (
              <Badge variant="secondary">{item.resolution}</Badge>
            )}
            {item.source && (
              <Badge variant="secondary">{item.source}</Badge>
            )}
            {item.codec && item.codec.length > 0 && item.codec.map((c, i) => (
              <Badge key={i} variant="outline">{c}</Badge>
            ))}
            {item.audio && item.audio.length > 0 && item.audio.map((a, i) => (
              <Badge key={i} variant="outline">{a}</Badge>
            ))}
            {item.hdr && item.hdr.length > 0 && item.hdr.map((h, i) => (
              <Badge key={i} variant="default">{h}</Badge>
            ))}
            {item.group && (
              <Badge>{item.group}</Badge>
            )}
          </div>
          
          {item.edition && item.edition.length > 0 && (
            <div className="flex gap-1 flex-wrap">
              {item.edition.map((ed, i) => (
                <Badge key={i} variant="outline" className="text-xs">
                  {ed}
                </Badge>
              ))}
            </div>
          )}
          
          <div className="flex items-center gap-4 text-xs text-muted-foreground">
            {item.episode !== undefined && item.episode !== null && (
              <span>Episode {item.episode}</span>
            )}
            <span className="flex items-center gap-1">
              <HardDrive className="h-3 w-3" />
              {formatSize(item.size)}
            </span>
            <span>Added {formatDate(item.addedOn)}</span>
          </div>
        </div>
        
        {/* Action buttons */}
        <div className="flex items-center gap-1 flex-shrink-0">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onTorrentAction(item.state === 'paused' ? 'resume' : 'pause', item.hash)}
            title={item.state === 'paused' ? 'Resume torrent' : 'Pause torrent'}
          >
            {item.state === 'paused' ? <Play className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onTorrentAction('recheck', item.hash)}
            title="Force recheck"
          >
            <RotateCcw className="h-3 w-3" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onCategoryChange(item.hash, 'change-category')}
            title="Change category"
          >
            <FolderOpen className="h-3 w-3" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onTorrentAction('delete', item.hash)}
            title="Delete torrent"
            className="text-destructive hover:text-destructive"
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        </div>
      </div>
    </div>
  )
}

interface UpgradeRecommendation {
  title: string
  type: string
  currentBest: ParsedTitle
  potentialUpgrades: ParsedTitle[]
  reason: string
}

interface UpgradeRecommendationCardProps {
  recommendation: UpgradeRecommendation
  formatSize: (bytes: number) => string
  formatDate: (timestamp: number) => string
  getReleaseTypeIcon: (type: string) => React.ReactNode
  onTorrentAction: (action: string, hash: string) => Promise<void>
  onCategoryChange: (hash: string, category: string) => Promise<void>
}

function UpgradeRecommendationCard({
  recommendation,
  formatSize,
  formatDate,
  getReleaseTypeIcon,
  onTorrentAction,
  onCategoryChange
}: UpgradeRecommendationCardProps) {
  const [selectedUpgrade, setSelectedUpgrade] = useState<string | null>(null)
  
  const currentScore = calculateQualityScore(recommendation.currentBest)
  
  const handleUpgradeAction = async (action: string) => {
    if (!selectedUpgrade) return
    
    if (action === 'replace') {
      // Pause current best and resume upgrade
      await onTorrentAction('pause', recommendation.currentBest.hash)
      await onTorrentAction('resume', selectedUpgrade)
    } else if (action === 'keep-both') {
      // Just resume the upgrade
      await onTorrentAction('resume', selectedUpgrade)
    }
  }
  
  return (
    <Card className="border-l-4 border-l-blue-500">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            {getReleaseTypeIcon(recommendation.type)}
            <CardTitle className="text-lg">{recommendation.title}</CardTitle>
            <Badge variant="secondary">{recommendation.reason}</Badge>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Current Best */}
        <div className="border rounded-lg p-3 bg-muted/30">
          <div className="flex items-center gap-2 mb-2">
            <Badge variant="outline">Current Best</Badge>
            <Badge variant={currentScore.color as any}>{currentScore.label}</Badge>
          </div>
          <div className="text-sm text-muted-foreground">
            {recommendation.currentBest.name} • {formatSize(recommendation.currentBest.size)} • Added {formatDate(recommendation.currentBest.addedOn)}
          </div>
        </div>
        
        {/* Potential Upgrades */}
        <div className="space-y-2">
          <h4 className="font-medium text-sm">Potential Upgrades</h4>
          {recommendation.potentialUpgrades.map((upgrade, index) => {
            const upgradeScore = calculateQualityScore(upgrade)
            const improvements: string[] = []
            
            if (upgradeScore.level !== currentScore.level) {
              improvements.push(`${currentScore.level} → ${upgradeScore.level}`)
            }
            
            if (upgrade.resolution && recommendation.currentBest.resolution && 
                upgrade.resolution !== recommendation.currentBest.resolution) {
              improvements.push(`Resolution: ${recommendation.currentBest.resolution} → ${upgrade.resolution}`)
            }
            
            if (upgrade.hdr && upgrade.hdr.length > 0 && 
                (!recommendation.currentBest.hdr || recommendation.currentBest.hdr.length === 0)) {
              improvements.push('Adds HDR')
            }
            
            if (upgrade.codec && upgrade.codec.some(c => c.toLowerCase().includes('x265') || c.toLowerCase().includes('hevc')) &&
                (!recommendation.currentBest.codec || !recommendation.currentBest.codec.some(c => c.toLowerCase().includes('x265') || c.toLowerCase().includes('hevc')))) {
              improvements.push('Better codec (x265/HEVC)')
            }
            
            return (
              <div key={index} className="border rounded-lg p-3">
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-2">
                      <input
                        type="radio"
                        name={`upgrade-${recommendation.title}`}
                        checked={selectedUpgrade === upgrade.hash}
                        onChange={() => setSelectedUpgrade(upgrade.hash)}
                        className="mt-0.5"
                      />
                      <Badge variant={upgradeScore.color as any}>{upgradeScore.label}</Badge>
                      {improvements.length > 0 && (
                        <Badge variant="default" className="text-xs">
                          {improvements.join(', ')}
                        </Badge>
                      )}
                    </div>
                    <div className="text-sm font-medium mb-1">{upgrade.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {formatSize(upgrade.size)} • Added {formatDate(upgrade.addedOn)}
                    </div>
                    {upgrade.resolution && (
                      <div className="text-xs text-muted-foreground mt-1">
                        {upgrade.resolution}
                        {upgrade.source && ` • ${upgrade.source}`}
                        {upgrade.codec && upgrade.codec.length > 0 && ` • ${upgrade.codec.join(', ')}`}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-1 ml-4">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => onTorrentAction(upgrade.state === 'paused' ? 'resume' : 'pause', upgrade.hash)}
                      title={upgrade.state === 'paused' ? 'Resume torrent' : 'Pause torrent'}
                    >
                      {upgrade.state === 'paused' ? <Play className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => onTorrentAction('delete', upgrade.hash)}
                      title="Delete torrent"
                      className="text-destructive hover:text-destructive"
                    >
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
        
        {/* Action Buttons */}
        {selectedUpgrade && (
          <div className="flex gap-2 pt-2 border-t">
            <Button 
              variant="default" 
              size="sm"
              onClick={() => handleUpgradeAction('replace')}
              className="flex-1"
            >
              <Crown className="mr-2 h-4 w-4" />
              Replace Current with Upgrade
            </Button>
            <Button 
              variant="outline" 
              size="sm"
              onClick={() => handleUpgradeAction('keep-both')}
              className="flex-1"
            >
              Keep Both
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
