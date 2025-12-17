/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FilterSidebar } from "@/components/torrents/FilterSidebar"
import { GlobalStatusBar, type SelectionInfo } from "@/components/torrents/GlobalStatusBar"
import { TorrentCreationTasks } from "@/components/torrents/TorrentCreationTasks"
import { TorrentCreatorDialog } from "@/components/torrents/TorrentCreatorDialog"
import { TorrentDetailsPanel } from "@/components/torrents/TorrentDetailsPanel"
import { TorrentTableResponsive } from "@/components/torrents/TorrentTableResponsive"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { VisuallyHidden } from "@/components/ui/visually-hidden"
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { usePersistedFilters } from "@/hooks/usePersistedFilters"
import { usePersistedFilterSidebarState } from "@/hooks/usePersistedFilterSidebarState"
import { cn } from "@/lib/utils"
import type { Category, ServerState, Torrent, TorrentCounts } from "@/types"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { ImperativePanelHandle } from "react-resizable-panels"

interface TorrentsProps {
  instanceId: number
  instanceName: string
  search: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined }
  onSearchChange: (search: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined }) => void
}

export function Torrents({ instanceId, search, onSearchChange }: TorrentsProps) {
  const [filters, setFilters] = usePersistedFilters(instanceId)
  const [filterSidebarCollapsed] = usePersistedFilterSidebarState(false)
  const { viewMode } = usePersistedCompactViewState("normal")
  const { clearSelection } = useTorrentSelection()
  const { instances } = useInstances()
  const instance = useMemo(() => instances?.find(i => i.id === instanceId), [instances, instanceId])

  // Server state for global status bar
  const [serverState, setServerState] = useState<ServerState | null>(null)
  const [listenPort, setListenPort] = useState<number | null>(null)
  const handleServerStateUpdate = useCallback((state: ServerState | null, port?: number | null) => {
    setServerState(state)
    setListenPort(port ?? null)
  }, [])

  // Selection info for global status bar
  const [selectionInfo, setSelectionInfo] = useState<SelectionInfo | null>(null)
  const handleSelectionInfoUpdate = useCallback((info: SelectionInfo) => {
    setSelectionInfo(info)
  }, [])

  // Sidebar width: 320px normal, 260px dense
  const sidebarWidth = viewMode === "dense" ? "16.25rem" : "20rem"
  const [selectedTorrent, setSelectedTorrent] = useState<Torrent | null>(null)
  const [initialDetailsTab, setInitialDetailsTab] = useState<string | undefined>(undefined)
  const [mobileFilterOpen, setMobileFilterOpen] = useState(false)
  const handleInitialTabConsumed = useCallback(() => setInitialDetailsTab(undefined), [])

  // Mobile detection for responsive layout
  const [isMobile, setIsMobile] = useState(() => {
    if (typeof window === "undefined") return false
    return window.innerWidth < 768
  })

  // Ref for controlling the details panel imperatively (auto-expand/collapse)
  const detailsPanelRef = useRef<ImperativePanelHandle>(null)

  // Navigation is handled by parent component via onSearchChange prop

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
  const [categorySizes, setCategorySizes] = useState<Record<string, number> | undefined>(undefined)
  const [tagSizes, setTagSizes] = useState<Record<string, number> | undefined>(undefined)
  const [categories, setCategories] = useState<Record<string, Category> | undefined>(undefined)
  const [tags, setTags] = useState<string[] | undefined>(undefined)
  const [useSubcategories, setUseSubcategories] = useState<boolean>(false)
  const [lastInstanceId, setLastInstanceId] = useState<number | null>(null)

  const handleTorrentSelect = useCallback((torrent: Torrent | null, initialTab?: string) => {
    // Toggle selection: if the same torrent is clicked without a tab override, deselect it
    if (torrent && selectedTorrent?.hash === torrent.hash && !initialTab) {
      setSelectedTorrent(null)
      setInitialDetailsTab(undefined)
    } else {
      setSelectedTorrent(torrent)
      setInitialDetailsTab(initialTab)
    }
  }, [selectedTorrent?.hash])

  // Clear selected torrent and mark data as potentially stale when instance changes
  // Don't immediately clear torrentCounts/categories/tags to prevent showing 0 values
  useEffect(() => {
    setSelectedTorrent(null) // Clear selected torrent immediately
    // Note: We keep torrentCounts/categories/tags until new data arrives to prevent flickering zeros
    // The TorrentTableOptimized callback will only update when complete data is available
  }, [instanceId])

  // Callback when filtered data updates - now receives counts, categories, tags, and useSubcategories from backend
  const handleFilteredDataUpdate = useCallback((_torrents: Torrent[], _total: number, counts?: TorrentCounts, categoriesData?: Record<string, Category>, tagsData?: string[], subcategoriesEnabled?: boolean) => {
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

      // Add filtered total count for cross-seed display
      transformedCounts.filtered = _total

      setTorrentCounts(transformedCounts)

      // Store size data for sidebar display
      if (counts.categorySizes) {
        setCategorySizes(counts.categorySizes)
      }
      if (counts.tagSizes) {
        setTagSizes(counts.tagSizes)
      }
    }

    // Store categories and tags only when new data arrives; preserve previous values during pagination fetches
    if (categoriesData !== undefined) {
      setCategories(categoriesData)
    }
    if (tagsData !== undefined) {
      setTags(tagsData)
    }

    // Update subcategories flag when provided
    if (subcategoriesEnabled !== undefined) {
      setUseSubcategories(subcategoriesEnabled)
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

  // Mobile detection media query listener
  useEffect(() => {
    const mediaQuery = window.matchMedia("(max-width: 767px)")

    const handleMobileChange = (event: MediaQueryListEvent) => {
      setIsMobile(event.matches)
    }

    // Set initial value
    setIsMobile(mediaQuery.matches)

    // Add listener
    if (typeof mediaQuery.addEventListener === "function") {
      mediaQuery.addEventListener("change", handleMobileChange)
      return () => mediaQuery.removeEventListener("change", handleMobileChange)
    } else {
      // Legacy fallback
      const legacyMediaQuery = mediaQuery as MediaQueryList & {
        addListener?: (listener: (event: MediaQueryListEvent) => void) => void
        removeListener?: (listener: (event: MediaQueryListEvent) => void) => void
      }
      legacyMediaQuery.addListener?.(handleMobileChange)
      return () => legacyMediaQuery.removeListener?.(handleMobileChange)
    }
  }, [])

  // Auto-expand details panel when a torrent is selected on desktop
  useEffect(() => {
    if (!isMobile && selectedTorrent && detailsPanelRef.current?.isCollapsed()) {
      detailsPanelRef.current.expand()
    }
  }, [selectedTorrent, isMobile])

  // Unified Escape handler: close panel and clear selection atomically
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return
      if (e.defaultPrevented) return

      // Skip if a dialog is open (dialogs handle their own Escape)
      if (document.querySelector("[role=\"dialog\"]")) return

      e.preventDefault()

      // Close panel and clear selection in one action
      setSelectedTorrent(null)
      clearSelection()
    }

    window.addEventListener("keydown", handleEscape)
    return () => window.removeEventListener("keydown", handleEscape)
  }, [clearSelection])

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
          "hidden md:flex shrink-0 h-full overflow-hidden transition-[flex-basis,width] duration-300 ease-in-out",
          filterSidebarCollapsed && "basis-0"
        )}
        style={{ flexBasis: filterSidebarCollapsed ? 0 : sidebarWidth }}
        aria-hidden={filterSidebarCollapsed}
      >
        <div
          className={cn(
            "h-full overflow-hidden transition-[transform,opacity,width] duration-300 ease-in-out",
            filterSidebarCollapsed ? "-translate-x-full opacity-0 pointer-events-none" : "translate-x-0 opacity-100"
          )}
          style={{ width: sidebarWidth }}
        >
          <FilterSidebar
            key={`filter-sidebar-${instanceId}`}
            instanceId={instanceId}
            selectedFilters={filters}
            onFilterChange={setFilters}
            torrentCounts={torrentCounts}
            categorySizes={categorySizes}
            tagSizes={tagSizes}
            categories={categories}
            tags={tags}
            useSubcategories={useSubcategories}
            isStaleData={lastInstanceId !== null && lastInstanceId !== instanceId}
            isLoading={lastInstanceId !== null && lastInstanceId !== instanceId}
            isMobile={false}
          />
        </div>
      </div>

      {/* Mobile Filter Sheet */}
      <Sheet open={mobileFilterOpen} onOpenChange={setMobileFilterOpen}>
        <SheetContent
          side="left"
          className="p-0 w-[280px] sm:w-[320px] md:hidden flex flex-col max-h-[100dvh]"
          onOpenAutoFocus={(event) => {
            event.preventDefault()

            const content = event.currentTarget as HTMLElement | null
            const closeButton = content?.querySelector<HTMLElement>("[data-slot=\"sheet-close\"]")
            closeButton?.focus()
          }}
        >
          <SheetHeader className="px-4 py-3 border-b">
            <SheetTitle className="text-lg font-semibold">Filters</SheetTitle>
          </SheetHeader>
          <div className="flex-1 min-h-0 overflow-hidden">
            <FilterSidebar
              key={`filter-sidebar-mobile-${instanceId}`}
              instanceId={instanceId}
              selectedFilters={filters}
              onFilterChange={setFilters}
              torrentCounts={torrentCounts}
              categorySizes={categorySizes}
              tagSizes={tagSizes}
              categories={categories}
              tags={tags}
              useSubcategories={useSubcategories}
              isStaleData={lastInstanceId !== null && lastInstanceId !== instanceId}
              isLoading={lastInstanceId !== null && lastInstanceId !== instanceId}
              isMobile={true}
            />
          </div>
        </SheetContent>
      </Sheet>

      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Desktop: Resizable vertical layout with bottom details panel */}
        {/* Use React conditional rendering to avoid duplicate dialogs */}
        {!isMobile && (
          <div className="flex flex-col h-full">
            <ResizablePanelGroup
              direction="vertical"
              autoSaveId="qui-torrent-details-panel"
            >
              <ResizablePanel
                defaultSize={selectedTorrent ? 60 : 100}
                minSize={30}
              >
                <div className="h-full">
                  <TorrentTableResponsive
                    instanceId={instanceId}
                    filters={filters}
                    selectedTorrent={selectedTorrent}
                    onTorrentSelect={handleTorrentSelect}
                    addTorrentModalOpen={isAddTorrentModalOpen}
                    onAddTorrentModalChange={handleAddTorrentModalChange}
                    onFilteredDataUpdate={handleFilteredDataUpdate}
                    onFilterChange={setFilters}
                    onServerStateUpdate={handleServerStateUpdate}
                    onSelectionInfoUpdate={handleSelectionInfoUpdate}
                  />
                </div>
              </ResizablePanel>

              {selectedTorrent && (
                <>
                  <ResizableHandle withHandle />
                  <ResizablePanel
                    ref={detailsPanelRef}
                    defaultSize={40}
                    minSize={15}
                    maxSize={70}
                    collapsible
                    collapsedSize={0}
                    onCollapse={() => {
                      // When user collapses the panel, deselect the torrent
                      setSelectedTorrent(null)
                    }}
                  >
                    <div className="h-full border-t bg-background">
                      <TorrentDetailsPanel
                        instanceId={instanceId}
                        torrent={selectedTorrent}
                        initialTab={initialDetailsTab}
                        onInitialTabConsumed={handleInitialTabConsumed}
                        layout="horizontal"
                        onClose={() => setSelectedTorrent(null)}
                      />
                    </div>
                  </ResizablePanel>
                </>
              )}
            </ResizablePanelGroup>
            {/* Global status bar - at bottom of desktop layout */}
            <GlobalStatusBar
              instanceId={instanceId}
              serverState={serverState}
              instance={instance}
              listenPort={listenPort}
              selectionInfo={selectionInfo}
            />
          </div>
        )}

        {/* Mobile: Full height table with Sheet overlay */}
        {isMobile && (
          <div className="flex flex-col h-full px-4">
            <TorrentTableResponsive
              instanceId={instanceId}
              filters={filters}
              selectedTorrent={selectedTorrent}
              onTorrentSelect={handleTorrentSelect}
              addTorrentModalOpen={isAddTorrentModalOpen}
              onAddTorrentModalChange={handleAddTorrentModalChange}
              onFilteredDataUpdate={handleFilteredDataUpdate}
              onFilterChange={setFilters}
            />
          </div>
        )}
      </div>

      {/* Mobile Details Sheet - only renders on mobile */}
      {isMobile && (
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
            className="w-full p-0 gap-0"
            hideClose
          >
            <SheetHeader className="sr-only">
              <VisuallyHidden>
                <SheetTitle>
                  {selectedTorrent ? `Torrent Details: ${selectedTorrent.name}` : "Torrent Details"}
                </SheetTitle>
              </VisuallyHidden>
            </SheetHeader>
            {selectedTorrent && (
              <TorrentDetailsPanel
                instanceId={instanceId}
                torrent={selectedTorrent}
                initialTab={initialDetailsTab}
                onInitialTabConsumed={handleInitialTabConsumed}
                onClose={() => setSelectedTorrent(null)}
              />
            )}
          </SheetContent>
        </Sheet>
      )}

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
            <DialogTitle>Torrent Creation Tasks</DialogTitle>
          </DialogHeader>
          <div className="flex-1 overflow-auto">
            <TorrentCreationTasks instanceId={instanceId} />
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
