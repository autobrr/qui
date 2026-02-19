/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import { useIsMobile } from "@/hooks/useMediaQuery"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { usePersistedFilters } from "@/hooks/usePersistedFilters"
import { usePersistedFilterSidebarState } from "@/hooks/usePersistedFilterSidebarState"
import { usePersistedTitleBarSpeeds } from "@/hooks/usePersistedTitleBarSpeeds"
import { useTitleBarSpeeds } from "@/hooks/useTitleBarSpeeds"
import { api } from "@/lib/api"
import { isAllInstancesScope } from "@/lib/instances"
import { cn } from "@/lib/utils"
import type { Category, CrossInstanceTorrent, ServerState, Torrent, TorrentCounts } from "@/types"
import { useNavigate } from "@tanstack/react-router"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useDefaultLayout, usePanelRef } from "react-resizable-panels"

interface TorrentsProps {
  instanceId: number
  instanceName: string
  isAllInstancesView?: boolean
  search: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined; torrent?: string; tab?: string }
  onSearchChange: (search: { modal?: "add-torrent" | "create-torrent" | "tasks" | undefined; torrent?: string; tab?: string }) => void
}

export function Torrents({ instanceId, instanceName, isAllInstancesView = false, search, onSearchChange }: TorrentsProps) {
  const isAllInstances = isAllInstancesView || isAllInstancesScope(instanceId)
  const [filters, setFilters] = usePersistedFilters(instanceId)
  const [filterSidebarCollapsed] = usePersistedFilterSidebarState(false)
  const { viewMode } = usePersistedCompactViewState("normal")
  const { clearSelection } = useTorrentSelection()
  const { instances } = useInstances()
  const instance = useMemo(() => {
    if (isAllInstances) {
      return undefined
    }
    return instances?.find(i => i.id === instanceId)
  }, [instances, instanceId, isAllInstances])
  const [titleBarSpeedsEnabled] = usePersistedTitleBarSpeeds(false)

  // Server state for global status bar
  const [serverState, setServerState] = useState<ServerState | null>(null)
  const [listenPort, setListenPort] = useState<number | null>(null)
  const handleServerStateUpdate = useCallback((state: ServerState | null, port?: number | null) => {
    setServerState(state)
    setListenPort(port ?? null)
  }, [])

  useTitleBarSpeeds({
    mode: "instance",
    enabled: titleBarSpeedsEnabled && !isAllInstances,
    instanceId,
    instanceName: instance?.name ?? instanceName,
    foregroundSpeeds: serverState
      ? {
        dl: serverState.dl_info_speed ?? 0,
        up: serverState.up_info_speed ?? 0,
      }
      : undefined,
  })

  // Selection info for global status bar
  const [selectionInfo, setSelectionInfo] = useState<SelectionInfo | null>(null)
  const handleSelectionInfoUpdate = useCallback((info: SelectionInfo) => {
    setSelectionInfo(info)
  }, [])

  // Sidebar width: 320px normal, 260px dense (fixed px to avoid issues with non-16px root font size)
  const sidebarWidth = viewMode === "dense" ? "260px" : "320px"
  const [selectedTorrent, setSelectedTorrent] = useState<Torrent | null>(null)
  const [initialDetailsTab, setInitialDetailsTab] = useState<string | undefined>(undefined)
  const [mobileFilterOpen, setMobileFilterOpen] = useState(false)
  const handleInitialTabConsumed = useCallback(() => setInitialDetailsTab(undefined), [])
  const navigate = useNavigate()
  const getTorrentInstanceId = useCallback((torrent: Torrent | null | undefined) => {
    if (!torrent) {
      return instanceId
    }

    const crossInstanceId = (torrent as Partial<CrossInstanceTorrent>).instanceId
    if (typeof crossInstanceId === "number" && crossInstanceId > 0) {
      return crossInstanceId
    }

    return instanceId
  }, [instanceId])
  const selectedTorrentInstanceId = getTorrentInstanceId(selectedTorrent)

  // Handle deep link to a specific torrent (from cross-seed navigation)
  useEffect(() => {
    if (!search.torrent) return

    const hash = search.torrent
    const tab = search.tab
    const escapedHash = hash.replaceAll("\"", "\\\"")

    // Fetch the torrent by hash and select it
    const fetchTorrentByHash = isAllInstances
      ? api.getCrossInstanceTorrents({
        filters: {
          expr: `Hash == "${escapedHash}"`,
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [],
          excludeTrackers: [],
        },
        limit: 1,
      }).then((response) => response.crossInstanceTorrents?.[0] ?? response.cross_instance_torrents?.[0] ?? null)
      : api.getTorrents(instanceId, {
        filters: {
          expr: `Hash == "${escapedHash}"`,
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [],
          excludeTrackers: [],
        },
        limit: 1,
      }).then((response) => response.torrents[0] ?? null)

    fetchTorrentByHash.then((torrent) => {
      if (torrent) {
        setSelectedTorrent(torrent)
        if (tab) {
          setInitialDetailsTab(tab)
        }
      }
      // Clear the search params after consuming
      onSearchChange({ modal: search.modal })
    }).catch(() => {
      // Silently fail - torrent might not exist
      onSearchChange({ modal: search.modal })
    })
  }, [instanceId, isAllInstances, search, onSearchChange])

  // Navigate to a cross-seed match torrent
  const handleNavigateToTorrent = useCallback((targetInstanceId: number, torrentHash: string) => {
    if (!isAllInstances && targetInstanceId === instanceId) {
      // Same instance - fetch and select the torrent directly
      api.getTorrents(instanceId, {
        filters: {
          expr: `Hash == "${torrentHash}"`,
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [],
          excludeTrackers: [],
        },
        limit: 1,
      }).then((response) => {
        const torrent = response.torrents[0]
        if (torrent) {
          setSelectedTorrent(torrent)
          setInitialDetailsTab("general")
        }
      })
    } else {
      // Different instance - navigate with search params
      navigate({
        to: "/instances/$instanceId",
        params: { instanceId: String(targetInstanceId) },
        search: { torrent: torrentHash, tab: "general" },
      })
    }
  }, [instanceId, isAllInstances, navigate])

  // Mobile detection for responsive layout
  const isMobile = useIsMobile()

  const [detailsPanelReady, setDetailsPanelReady] = useState(false)

  const panelIds = useMemo(
    () => (selectedTorrent ? ["torrent-list", "torrent-details"] : ["torrent-list"]),
    [selectedTorrent]
  )
  const { defaultLayout, onLayoutChange } = useDefaultLayout({
    id: "qui-torrent-details-panel",
    panelIds,
  })

  // Ref for controlling the details panel imperatively (auto-expand/collapse)
  const detailsPanelRef = usePanelRef()

  // Navigation is handled by parent component via onSearchChange prop

  // Check if add torrent modal should be open
  const isAddTorrentModalOpen = !isAllInstances && search?.modal === "add-torrent"

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
  const isCreateTorrentModalOpen = !isAllInstances && search?.modal === "create-torrent"

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
  const isTasksModalOpen = !isAllInstances && search?.modal === "tasks"

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

  const isSameTorrent = useCallback((left: Torrent | null, right: Torrent | null) => {
    if (!left || !right) {
      return false
    }

    return left.hash === right.hash && getTorrentInstanceId(left) === getTorrentInstanceId(right)
  }, [getTorrentInstanceId])

  const handleTorrentSelect = useCallback((torrent: Torrent | null, initialTab?: string) => {
    // Toggle selection: if the same torrent is clicked without a tab override, deselect it
    if (torrent && isSameTorrent(selectedTorrent, torrent) && !initialTab) {
      setSelectedTorrent(null)
      setInitialDetailsTab(undefined)
    } else {
      setSelectedTorrent(torrent)
      setInitialDetailsTab(initialTab)
    }
  }, [isSameTorrent, selectedTorrent])

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

  useEffect(() => {
    if (!selectedTorrent || isMobile) {
      setDetailsPanelReady(false)
    }
  }, [isMobile, selectedTorrent])

  // Auto-expand details panel when a torrent is selected on desktop
  useEffect(() => {
    if (!isMobile && selectedTorrent && detailsPanelReady && detailsPanelRef.current?.isCollapsed()) {
      detailsPanelRef.current.expand()
    }
  }, [detailsPanelReady, detailsPanelRef, selectedTorrent, isMobile])

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
    if (!isMobile) {
      setMobileFilterOpen(false)
    }
  }, [isMobile])

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
            readOnly={isAllInstances}
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
              readOnly={isAllInstances}
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
              defaultLayout={defaultLayout}
              onLayoutChange={onLayoutChange}
            >
              <ResizablePanel
                id="torrent-list"
                defaultSize={selectedTorrent ? "60%" : "100%"}
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
                    id="torrent-details"
                    panelRef={detailsPanelRef}
                    defaultSize="40%"
                    collapsible
                    collapsedSize={0}
                    onResize={(panelSize, _panelId, prevPanelSize) => {
                      if (!detailsPanelReady) {
                        setDetailsPanelReady(true)
                      }
                      if (!selectedTorrent) {
                        return
                      }
                      if (prevPanelSize === undefined) {
                        return
                      }
                      if (panelSize.asPercentage <= 0 || panelSize.inPixels <= 0) {
                        // When user collapses the panel, deselect the torrent
                        setSelectedTorrent(null)
                      }
                    }}
                  >
                    <div className="h-full border-t bg-background">
                      <TorrentDetailsPanel
                        instanceId={selectedTorrentInstanceId}
                        torrent={selectedTorrent}
                        initialTab={initialDetailsTab}
                        onInitialTabConsumed={handleInitialTabConsumed}
                        layout="horizontal"
                        onClose={() => setSelectedTorrent(null)}
                        onNavigateToTorrent={handleNavigateToTorrent}
                      />
                    </div>
                  </ResizablePanel>
                </>
              )}
            </ResizablePanelGroup>
            {/* Global status bar - at bottom of desktop layout */}
            {!isAllInstances && (
              <GlobalStatusBar
                instanceId={instanceId}
                serverState={serverState}
                instance={instance}
                listenPort={listenPort}
                selectionInfo={selectionInfo}
              />
            )}
            {isAllInstances && selectionInfo && (
              <div className="px-2 py-1.5 border-t text-xs text-muted-foreground">
                Unified view spans all active instances.
              </div>
            )}
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
                instanceId={selectedTorrentInstanceId}
                torrent={selectedTorrent}
                initialTab={initialDetailsTab}
                onInitialTabConsumed={handleInitialTabConsumed}
                onClose={() => setSelectedTorrent(null)}
                onNavigateToTorrent={handleNavigateToTorrent}
              />
            )}
          </SheetContent>
        </Sheet>
      )}

      {/* Torrent Creator Dialog */}
      {!isAllInstances && (
        <TorrentCreatorDialog
          instanceId={instanceId}
          open={isCreateTorrentModalOpen}
          onOpenChange={handleCreateTorrentModalChange}
        />
      )}

      {/* Torrent Creation Tasks Modal */}
      {!isAllInstances && (
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
      )}
    </div>
  )
}
