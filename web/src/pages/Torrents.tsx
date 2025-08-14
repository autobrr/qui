/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useCallback, useEffect, useRef } from 'react'
import { TorrentTableResponsive } from '@/components/torrents/TorrentTableResponsive'
import { FilterSidebar } from '@/components/torrents/FilterSidebar'
import { TorrentDetailsPanel } from '@/components/torrents/TorrentDetailsPanel'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { VisuallyHidden } from '@/components/ui/visually-hidden'
import { usePersistedFilters } from '@/hooks/usePersistedFilters'
import { usePersistedFilterSidebarState } from '@/hooks/usePersistedFilterSidebarState'
import { useNavigate, useSearch } from '@tanstack/react-router'
import type { Torrent } from '@/types'

interface TorrentsProps {
  instanceId: number
  instanceName: string
}

export function Torrents({ instanceId }: TorrentsProps) {
  const [filters, setFilters] = usePersistedFilters(instanceId)
  const [filterSidebarCollapsed, setFilterSidebarCollapsed] = usePersistedFilterSidebarState(false)
  const [selectedTorrent, setSelectedTorrent] = useState<Torrent | null>(null)
  const [mobileFilterOpen, setMobileFilterOpen] = useState(false)
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as any
  
  // Debounced filter updates to prevent excessive API calls during rapid filter changes
  const filterTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const debouncedSetFilters = useCallback((newFilters: typeof filters) => {
    if (filterTimeoutRef.current) {
      clearTimeout(filterTimeoutRef.current)
    }
    filterTimeoutRef.current = setTimeout(() => {
      setFilters(newFilters)
    }, 150) // 150ms delay to batch rapid filter changes
  }, [setFilters])
  
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

  // Clear filter data when instance changes to prevent showing stale data
  useEffect(() => {
    setTorrentCounts(undefined)
    setCategories(undefined)
    setTags(undefined)
    setSelectedTorrent(null) // Also clear selected torrent
    // Note: We don't clear filters here as they are persisted per user preference
  }, [instanceId])

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
  // Count exists but badge is now handled in header (not used here)

  // Listen for header mobile filter button click
  useEffect(() => {
    const handler = () => setMobileFilterOpen(true)
    window.addEventListener('qui-open-mobile-filters', handler)
    return () => window.removeEventListener('qui-open-mobile-filters', handler)
  }, [])

  return (
    <div className="flex h-full relative">
      {/* Desktop Sidebar - hidden on mobile, with slide animation */}
      <div className={`hidden xl:block ${filterSidebarCollapsed ? 'w-0' : 'w-full xl:max-w-xs'} transition-all duration-300 ease-in-out overflow-hidden`}>
        <FilterSidebar
          key={`filter-sidebar-${instanceId}`}
          instanceId={instanceId}
          selectedFilters={filters}
          onFilterChange={debouncedSetFilters}
          torrentCounts={torrentCounts}
          categories={categories}
          tags={tags}
          collapsed={filterSidebarCollapsed}
          onCollapsedChange={setFilterSidebarCollapsed}
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
              key={`filter-sidebar-mobile-${instanceId}`}
              instanceId={instanceId}
              selectedFilters={filters}
              onFilterChange={debouncedSetFilters}
              torrentCounts={torrentCounts}
              categories={categories}
              tags={tags}
            />
          </div>
        </SheetContent>
      </Sheet>
      
      {/* Main content */}
        <div className="flex-1 flex flex-col min-w-0">
          <div className="px-4 sm:px-0 flex flex-col h-full">
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
          className="w-full fixed inset-y-0 right-0 h-full sm:w-[480px] md:w-[540px] lg:w-[600px] xl:w-[640px] sm:max-w-[480px] md:max-w-[540px] lg:max-w-[600px] xl:max-w-[640px] p-0 z-[100] gap-0 !transition-none !duration-0 data-[state=open]:!animate-none data-[state=closed]:!animate-none data-[state=open]:!transition-none data-[state=closed]:!transition-none"
        >
          <SheetHeader className="sr-only">
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