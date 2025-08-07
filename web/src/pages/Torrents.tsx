import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { TorrentTableOptimized } from '@/components/torrents/TorrentTableOptimized'
import { FilterSidebar } from '@/components/torrents/FilterSidebar'
import { TorrentDetailsPanel } from '@/components/torrents/TorrentDetailsPanel'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { VisuallyHidden } from '@/components/ui/visually-hidden'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Filter, ChevronDown, ChevronUp } from 'lucide-react'
import { useTorrentCounts } from '@/hooks/useTorrentCounts'
import { usePersistedFilters } from '@/hooks/usePersistedFilters'
import { useInstanceStats } from '@/hooks/useInstanceStats'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { api } from '@/lib/api'
import { formatSpeed } from '@/lib/utils'
import type { Torrent } from '@/types'

interface TorrentsProps {
  instanceId: number
  instanceName: string
}

export function Torrents({ instanceId, instanceName }: TorrentsProps) {
  const [filters, setFilters] = usePersistedFilters(instanceId)
  const [selectedTorrent, setSelectedTorrent] = useState<Torrent | null>(null)
  const [mobileFilterOpen, setMobileFilterOpen] = useState(false)
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as any
  
  // Get instance stats for speeds
  const { data: stats } = useInstanceStats(instanceId)
  
  // Check if add torrent modal should be open
  const isAddTorrentModalOpen = search?.modal === 'add-torrent'
  
  const handleAddTorrentModalChange = (open: boolean) => {
    if (open) {
      navigate({ 
        search: { ...search, modal: 'add-torrent' },
        replace: true 
      })
    } else {
      const { modal, ...restSearch } = search || {}
      navigate({ 
        search: restSearch,
        replace: true 
      })
    }
  }
  
  // Get all torrents for accurate counting (separate from table's progressive loading)
  const { data: allTorrentsForCounts } = useQuery({
    queryKey: ['all-torrents-for-counts', instanceId],
    queryFn: async () => {
      // Load torrents in batches until we get them all
      let allTorrents: any[] = []
      let page = 0
      const pageSize = 1000
      let hasMore = true
      
      while (hasMore) {
        const response = await api.getTorrents(instanceId, { 
          page,
          limit: pageSize,
          sort: 'addedOn',
          order: 'desc'
        })
        
        allTorrents = [...allTorrents, ...response.torrents]
        hasMore = response.torrents.length === pageSize && allTorrents.length < response.total
        page++
        
        // Safety break to prevent infinite loops
        if (page > 10) break
      }
      
      console.log(`Loaded ${allTorrents.length} torrents for counting in ${page} batches`)
      return allTorrents
    },
    staleTime: 30000, // Cache for 30 seconds
    refetchInterval: 60000, // Refresh every minute
  })

  // Fetch categories and tags for count calculation
  const { data: categories = {} } = useQuery({
    queryKey: ['categories', instanceId],
    queryFn: () => api.getCategories(instanceId),
    staleTime: 60000,
  })

  const { data: tags = [] } = useQuery({
    queryKey: ['tags', instanceId],
    queryFn: () => api.getTags(instanceId),
    staleTime: 60000,
  })
  
  // Calculate torrent counts for the sidebar
  const torrentCounts = useTorrentCounts({ 
    torrents: allTorrentsForCounts || [], 
    allCategories: categories, 
    allTags: tags 
  })
  
  const handleTorrentSelect = (torrent: Torrent | null) => {
    setSelectedTorrent(torrent)
  }

  // Calculate total active filters for badge
  const activeFilterCount = useMemo(() => {
    return filters.status.length + 
           filters.categories.length + 
           filters.tags.length + 
           filters.trackers.length
  }, [filters])

  return (
    <div className="flex h-full">
      {/* Desktop Sidebar - hidden on mobile */}
      <div className="hidden xl:block">
        <FilterSidebar
          instanceId={instanceId}
          selectedFilters={filters}
          onFilterChange={setFilters}
          torrentCounts={torrentCounts}
        />
      </div>
      
      {/* Mobile Filter Sheet */}
      <Sheet open={mobileFilterOpen} onOpenChange={setMobileFilterOpen}>
        <SheetContent side="left" className="p-0 w-[280px] sm:w-[320px] xl:hidden">
          <SheetHeader className="px-4 py-3 border-b">
            <SheetTitle className="text-lg font-semibold">Filters</SheetTitle>
          </SheetHeader>
          <div className="h-[calc(100vh-3.5rem)]">
            <FilterSidebar
              instanceId={instanceId}
              selectedFilters={filters}
              onFilterChange={setFilters}
              torrentCounts={torrentCounts}
            />
          </div>
        </SheetContent>
      </Sheet>
      
      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0">
        <div className="p-4 sm:p-4 lg:p-6 flex flex-col h-full">
          <div className="flex-shrink-0 mb-4 sm:mb-4 lg:mb-6">
            <div className="flex items-center justify-between gap-2 sm:gap-4">
              <div className="flex-1 min-w-0">
                <div className="flex items-center justify-between">
                  <h1 className="text-lg sm:text-xl lg:text-3xl font-bold truncate">{instanceName}</h1>
                  {/* Mobile stats - inline with title */}
                  <div className="sm:hidden text-[11px] text-muted-foreground ml-2 flex-shrink-0 flex items-center gap-1">
                    <span className="font-medium text-foreground">{stats?.torrents?.total || 0}</span>
                    <span className="text-[10px] flex items-center gap-0.5">
                      <ChevronDown className="h-3 w-3" />
                      {formatSpeed(stats?.serverState?.downloadSpeed || 0, true)}
                      <ChevronUp className="h-3 w-3 ml-1" />
                      {formatSpeed(stats?.serverState?.uploadSpeed || 0, true)}
                    </span>
                  </div>
                </div>
                <p className="hidden sm:block text-muted-foreground mt-1 lg:mt-2 text-sm lg:text-base">
                  Manage torrents for this qBittorrent instance
                </p>
              </div>
              
              {/* Mobile Filter Button */}
              <Button 
                variant="outline" 
                size="icon"
                className="relative xl:hidden flex-shrink-0 sm:w-auto sm:px-3"
                onClick={() => setMobileFilterOpen(true)}
              >
                <Filter className="h-4 w-4" />
                <span className="hidden sm:inline sm:ml-2">Filters</span>
                {activeFilterCount > 0 && (
                  <Badge 
                    variant="secondary" 
                    className="absolute -top-1 -right-1 h-4 min-w-[16px] px-0.5 text-[10px] sm:relative sm:top-0 sm:right-0 sm:ml-2 sm:h-5 sm:min-w-[20px] sm:px-1 sm:text-xs"
                  >
                    {activeFilterCount}
                  </Badge>
                )}
              </Button>
            </div>
          </div>
          <div className="flex-1 min-h-0">
            <TorrentTableOptimized 
              instanceId={instanceId} 
              filters={filters}
              selectedTorrent={selectedTorrent}
              onTorrentSelect={handleTorrentSelect}
              addTorrentModalOpen={isAddTorrentModalOpen}
              onAddTorrentModalChange={handleAddTorrentModalChange}
            />
          </div>
        </div>
      </div>
      
      <Sheet open={!!selectedTorrent} onOpenChange={(open) => !open && setSelectedTorrent(null)}>
        <SheetContent 
          side="right"
          className="w-full sm:w-[480px] md:w-[540px] lg:w-[600px] xl:w-[640px] sm:max-w-[480px] md:max-w-[540px] lg:max-w-[600px] xl:max-w-[640px] p-0"
        >
          <SheetHeader>
            <VisuallyHidden>
              <SheetTitle>
                {selectedTorrent ? `Torrent Details: ${selectedTorrent.name}` : 'Torrent Details'}
              </SheetTitle>
            </VisuallyHidden>
          </SheetHeader>
          {selectedTorrent && (
            <TorrentDetailsPanel
              instanceId={instanceId}
              torrent={selectedTorrent}
            />
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}