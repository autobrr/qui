import { useState, useMemo, useCallback } from 'react'
import { TorrentTableResponsive } from '@/components/torrents/TorrentTableResponsive'
import { FilterSidebar } from '@/components/torrents/FilterSidebar'
import { TorrentDetailsPanel } from '@/components/torrents/TorrentDetailsPanel'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { VisuallyHidden } from '@/components/ui/visually-hidden'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Filter } from 'lucide-react'
import { usePersistedFilters } from '@/hooks/usePersistedFilters'
import { useNavigate, useSearch } from '@tanstack/react-router'
import type { Torrent } from '@/types'

interface TorrentsProps {
  instanceId: number
  instanceName: string
}

export function Torrents({ instanceId, instanceName }: TorrentsProps) {
  const [filters, setFilters] = usePersistedFilters()
  const [selectedTorrent, setSelectedTorrent] = useState<Torrent | null>(null)
  const [mobileFilterOpen, setMobileFilterOpen] = useState(false)
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as any
  
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
  
  // Store counts from torrent response
  const [torrentCounts, setTorrentCounts] = useState<Record<string, number> | undefined>(undefined)
  const [categories, setCategories] = useState<Record<string, { name: string; savePath: string }> | undefined>(undefined)
  const [tags, setTags] = useState<string[] | undefined>(undefined)
  
  const handleTorrentSelect = (torrent: Torrent | null) => {
    setSelectedTorrent(torrent)
  }

  // Callback when filtered data updates - now receives counts, categories, and tags from backend
  const handleFilteredDataUpdate = useCallback((_torrents: Torrent[], _total: number, counts?: any, categoriesData?: any, tagsData?: string[]) => {
    if (counts) {
      // Transform backend counts to match the expected format for FilterSidebar
      const transformedCounts: Record<string, number> = {}
      
      // Add status counts
      Object.entries(counts.status || {}).forEach(([status, count]) => {
        transformedCounts[`status:${status}`] = count as number
      })
      
      // Add category counts
      Object.entries(counts.categories || {}).forEach(([category, count]) => {
        transformedCounts[`category:${category}`] = count as number
      })
      
      // Add tag counts
      Object.entries(counts.tags || {}).forEach(([tag, count]) => {
        transformedCounts[`tag:${tag}`] = count as number
      })
      
      // Add tracker counts
      Object.entries(counts.trackers || {}).forEach(([tracker, count]) => {
        transformedCounts[`tracker:${tracker}`] = count as number
      })
      
      setTorrentCounts(transformedCounts)
    }
    
    // Store categories and tags
    if (categoriesData) {
      // Transform to match expected format: Record<string, { name: string; savePath: string }>
      const transformedCategories: Record<string, { name: string; savePath: string }> = {}
      Object.entries(categoriesData).forEach(([key, value]: [string, any]) => {
        transformedCategories[key] = {
          name: value.name || key,
          savePath: value.save_path || value.savePath || ''
        }
      })
      setCategories(transformedCategories)
    }
    
    if (tagsData) {
      setTags(tagsData)
    }
  }, [])

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
          categories={categories}
          tags={tags}
        />
      </div>
      
      {/* Mobile Filter Sheet */}
      <Sheet open={mobileFilterOpen} onOpenChange={setMobileFilterOpen}>
        <SheetContent side="left" className="p-0 w-[280px] sm:w-[320px] xl:hidden flex flex-col max-h-[100dvh]">
          <SheetHeader className="px-4 py-3 border-b">
            <SheetTitle className="text-lg font-semibold">Filters</SheetTitle>
          </SheetHeader>
          <div className="flex-1 min-h-0 overflow-hidden">
            <FilterSidebar
              instanceId={instanceId}
              selectedFilters={filters}
              onFilterChange={setFilters}
              torrentCounts={torrentCounts}
              categories={categories}
              tags={tags}
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
                <h1 className="text-lg sm:text-xl lg:text-3xl font-bold truncate">{instanceName}</h1>
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
            <TorrentTableResponsive 
              instanceId={instanceId} 
              filters={filters}
              selectedTorrent={selectedTorrent}
              onTorrentSelect={handleTorrentSelect}
              addTorrentModalOpen={isAddTorrentModalOpen}
              onAddTorrentModalChange={handleAddTorrentModalChange}
              onFilteredDataUpdate={handleFilteredDataUpdate}
            />
          </div>
        </div>
      </div>
      
      <Sheet open={!!selectedTorrent} onOpenChange={(open) => !open && setSelectedTorrent(null)}>
        <SheetContent 
          side="right"
          className="w-full fixed inset-y-0 right-0 h-full sm:w-[480px] md:w-[540px] lg:w-[600px] xl:w-[640px] sm:max-w-[480px] md:max-w-[540px] lg:max-w-[600px] xl:max-w-[640px] p-0 z-[100]"
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