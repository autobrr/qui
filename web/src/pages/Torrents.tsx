/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FilterSidebar } from "@/components/torrents/FilterSidebar"
import { TorrentCreationTasks } from "@/components/torrents/TorrentCreationTasks"
import { TorrentCreatorDialog } from "@/components/torrents/TorrentCreatorDialog"
import { TorrentDetailsPanel } from "@/components/torrents/TorrentDetailsPanel"
import { TorrentTableResponsive } from "@/components/torrents/TorrentTableResponsive"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { VisuallyHidden } from "@/components/ui/visually-hidden"
import { usePersistedFilters } from "@/hooks/usePersistedFilters"
import { usePersistedFilterSidebarState } from "@/hooks/usePersistedFilterSidebarState"
import { cn } from "@/lib/utils"
import type { Category, Torrent, TorrentCounts } from "@/types"
import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

interface TorrentsProps {
  instanceId: number
  instanceName: string
  search: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined }
  onSearchChange: (search: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined }) => void
}

export function Torrents({ instanceId, search, onSearchChange }: TorrentsProps) {
  const { t } = useTranslation()
  const [filters, setFilters] = usePersistedFilters(instanceId)
  const [filterSidebarCollapsed] = usePersistedFilterSidebarState(false)
  const [selectedTorrent, setSelectedTorrent] = useState<Torrent | null>(null)
  const [mobileFilterOpen, setMobileFilterOpen] = useState(false)
  // Navigation is handled by parent component via onSearchChange prop

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
  const isAddTorrentModalOpen = search?.modal === "add-torrent"

  const handleAddTorrentModalChange = (open: boolean) => {
    if (open) {
      onSearchChange({ ...search, modal: "add-torrent" })
    } else {
      const rest = Object.fromEntries(
        Object.entries(search).filter(([key]) => key !== "modal")
      )
      onSearchChange(rest)
    }
  }

  // Check if create torrent modal should be open
  const isCreateTorrentModalOpen = search?.modal === "create-torrent"

  const handleCreateTorrentModalChange = (open: boolean) => {
    if (open) {
      onSearchChange({ ...search, modal: "create-torrent" })
    } else {
      const rest = Object.fromEntries(
        Object.entries(search).filter(([key]) => key !== "modal")
      )
      onSearchChange(rest)
    }
  }

  // Check if tasks modal should be open
  const isTasksModalOpen = search?.modal === "tasks"

  const handleTasksModalChange = (open: boolean) => {
    if (open) {
      onSearchChange({ ...search, modal: "tasks" })
    } else {
      const rest = Object.fromEntries(
        Object.entries(search).filter(([key]) => key !== "modal")
      )
      onSearchChange(rest)
    }
  }

  // Store counts from torrent response
  const [torrentCounts, setTorrentCounts] = useState<Record<string, number> | undefined>(undefined)
  const [categories, setCategories] = useState<Record<string, Category> | undefined>(undefined)
  const [tags, setTags] = useState<string[] | undefined>(undefined)
  const [lastInstanceId, setLastInstanceId] = useState<number | null>(null)

  const handleTorrentSelect = (torrent: Torrent | null) => {
    // Toggle selection: if the same torrent is clicked, deselect it
    if (torrent && selectedTorrent?.hash === torrent.hash) {
      setSelectedTorrent(null)
    } else {
      setSelectedTorrent(torrent)
    }
  }

  // Clear selected torrent and mark data as potentially stale when instance changes
  // Don't immediately clear torrentCounts/categories/tags to prevent showing 0 values
  useEffect(() => {
    setSelectedTorrent(null) // Clear selected torrent immediately
    // Note: We keep torrentCounts/categories/tags until new data arrives to prevent flickering zeros
    // The TorrentTableOptimized callback will only update when complete data is available
  }, [instanceId])

  // Callback when filtered data updates - now receives counts, categories, and tags from backend
  const handleFilteredDataUpdate = useCallback((_torrents: Torrent[], _total: number, counts?: TorrentCounts, categoriesData?: Record<string, Category>, tagsData?: string[]) => {
    // Update the last instance ID when we receive new data
    setLastInstanceId(instanceId)

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

    // Store categories and tags only when new data arrives; preserve previous values during pagination fetches
    if (categoriesData !== undefined) {
      setCategories(categoriesData)
    }
    if (tagsData !== undefined) {
      setTags(tagsData)
    }

  }, [instanceId])

  // Calculate total active filters for badge
  // Count exists but badge is now handled in header (not used here)

  // Listen for header mobile filter button click
  useEffect(() => {
    const handler = () => setMobileFilterOpen(true)
    window.addEventListener("qui-open-mobile-filters", handler)
    return () => window.removeEventListener("qui-open-mobile-filters", handler)
  }, [])

  // Close the mobile filters sheet when viewport switches to desktop layout
  useEffect(() => {
    const mediaQuery = window.matchMedia("(min-width: 768px)")

    const handleChange = (event: MediaQueryListEvent) => {
      if (event.matches) {
        setMobileFilterOpen(false)
      }
    }

    if (mediaQuery.matches) {
      setMobileFilterOpen(false)
    }

    const supportsAddEventListener = typeof mediaQuery.addEventListener === "function"
    if (supportsAddEventListener) {
      mediaQuery.addEventListener("change", handleChange)
    } else {
      type MediaQueryListLegacy = MediaQueryList & {
        addListener?: (listener: (event: MediaQueryListEvent) => void) => void
        removeListener?: (listener: (event: MediaQueryListEvent) => void) => void
      }

      const legacyMediaQuery = mediaQuery as MediaQueryListLegacy
      legacyMediaQuery.addListener?.(handleChange)

      return () => legacyMediaQuery.removeListener?.(handleChange)
    }

    return () => mediaQuery.removeEventListener("change", handleChange)
  }, [])

  return (
    <div className="flex h-full relative">
      {/* Desktop Sidebar - slides in on tablet/desktop */}
      <div
        className={cn(
          "hidden md:flex shrink-0 h-full overflow-hidden transition-[flex-basis] duration-300 ease-in-out",
          filterSidebarCollapsed ? "basis-0" : "basis-[20rem]"
        )}
        aria-hidden={filterSidebarCollapsed}
      >
        <div
          className={cn(
            "w-[20rem] overflow-hidden transition-[transform,opacity] duration-300 ease-in-out",
            filterSidebarCollapsed? "-translate-x-full opacity-0 pointer-events-none": "translate-x-0 opacity-100"
          )}
        >
          <FilterSidebar
            key={`filter-sidebar-${instanceId}`}
            instanceId={instanceId}
            selectedFilters={filters}
            onFilterChange={debouncedSetFilters}
            torrentCounts={torrentCounts}
            categories={categories}
            tags={tags}
            isStaleData={lastInstanceId !== null && lastInstanceId !== instanceId}
            isLoading={lastInstanceId !== null && lastInstanceId !== instanceId}
            isMobile={false}
          />
        </div>
      </div>

      {/* Mobile Filter Sheet */}
      <Sheet open={mobileFilterOpen} onOpenChange={setMobileFilterOpen}>
        <SheetContent side="left" className="p-0 w-[280px] sm:w-[320px] md:hidden flex flex-col max-h-[100dvh]">
          <SheetHeader className="px-4 py-3 border-b">
            <SheetTitle className="text-lg font-semibold">{t("torrents.filters")}</SheetTitle>
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
              isStaleData={lastInstanceId !== null && lastInstanceId !== instanceId}
              isLoading={lastInstanceId !== null && lastInstanceId !== instanceId}
              isMobile={true}
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

      <Sheet
        open={!!selectedTorrent}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedTorrent(null)
          }
        }}
      >
        <SheetContent
          side="right"
          className="w-full sm:w-[480px] md:w-[540px] lg:w-[600px] xl:w-[640px] p-0 gap-0"
        >
          <SheetHeader className="sr-only">
            <VisuallyHidden>
              <SheetTitle>
                {selectedTorrent
                  ? t("torrents.details_with_name", { name: selectedTorrent.name })
                  : t("torrents.details")}
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

      {/* Torrent Creator Dialog */}
      <TorrentCreatorDialog
        instanceId={instanceId}
        open={isCreateTorrentModalOpen}
        onOpenChange={handleCreateTorrentModalChange}
      />

      {/* Torrent Creation Tasks Modal */}
      <Dialog open={isTasksModalOpen} onOpenChange={handleTasksModalChange}>
        <DialogContent className="w-full sm:max-w-screen-sm md:max-w-screen-md lg:max-w-screen-xl xl:max-w-screen-xl max-h-[85vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle>{t("dashboard.quickActions.tasks")}</DialogTitle>
          </DialogHeader>
          <div className="flex-1 overflow-auto">
            <TorrentCreationTasks instanceId={instanceId} />
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
