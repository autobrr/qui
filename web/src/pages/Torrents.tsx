/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SpreadsheetFormulaBar } from "@/components/spreadsheet/SpreadsheetFormulaBar"
import { SpreadsheetSheetTabs } from "@/components/spreadsheet/SpreadsheetSheetTabs"
import { FilterSidebar } from "@/components/torrents/FilterSidebar"
import { TorrentCreationTasks } from "@/components/torrents/TorrentCreationTasks"
import { TorrentCreatorDialog } from "@/components/torrents/TorrentCreatorDialog"
import { TorrentDetailsPanel } from "@/components/torrents/TorrentDetailsPanel"
import { TorrentTableResponsive } from "@/components/torrents/TorrentTableResponsive"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useTorrentSelectionContext } from "@/contexts/TorrentSelectionContext"
import { useInstances } from "@/hooks/useInstances"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { useMobileDetailsHistory } from "@/hooks/useMobileDetailsHistory"
import { usePersistedColumnFilters } from "@/hooks/usePersistedColumnFilters"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { usePersistedFilters } from "@/hooks/usePersistedFilters"
import { usePersistedFilterSidebarState } from "@/hooks/usePersistedFilterSidebarState"
import { usePersistedTitleBarSpeeds } from "@/hooks/usePersistedTitleBarSpeeds"
import { usePersistedUnifiedInstanceFilter } from "@/hooks/usePersistedUnifiedInstanceFilter"
import { useSwipeToClose } from "@/hooks/useSwipeToClose"
import { usePageTitle } from "@/hooks/usePageTitle"
import { useTitleBarSpeeds } from "@/hooks/useTitleBarSpeeds"
import { useTorrentListEscape } from "@/hooks/useTorrentListEscape"
import { api } from "@/lib/api"
import { isAllInstancesScope, normalizeUnifiedInstanceIds } from "@/lib/instances"
import { useSpreadsheetDisguise } from "@/lib/spreadsheet-disguise"
import { navigateWithSearch } from "@/lib/router-search"
import { hasSelection, selectionScope } from "@/lib/torrent-selection"
import { cn } from "@/lib/utils"
import type { Category, CrossInstanceTorrent, Torrent, TorrentCounts } from "@/types"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useDefaultLayout, usePanelRef } from "react-resizable-panels"

export interface TorrentsSearch {
  modal?: "add-torrent" | "create-torrent" | "tasks"
  torrent?: string
  tab?: string
  instance?: number
}

interface TorrentsProps {
  instanceId: number
  instanceName: string
  isAllInstancesView?: boolean
  search: TorrentsSearch
  onSearchChange: (search: TorrentsSearch) => void
}

export function Torrents({ instanceId, instanceName, isAllInstancesView = false, search, onSearchChange }: TorrentsProps) {
  const { t } = useTranslation("torrents")
  const isAllInstances = isAllInstancesView || isAllInstancesScope(instanceId)
  const [filters, setFilters] = usePersistedFilters(instanceId)
  const [filterSidebarCollapsed] = usePersistedFilterSidebarState(false)
  const { viewMode } = usePersistedCompactViewState("desktop")
  const { selection, dispatch: dispatchSelection } = useTorrentSelectionContext()
  const routeSearch = useSearch({ strict: false }) as { q?: string }

  const { instances } = useInstances()
  const spreadsheetDisguise = useSpreadsheetDisguise()
  const [persistedUnifiedFilter] = usePersistedUnifiedInstanceFilter()
  const activeInstanceIds = useMemo(
    () => (instances ?? []).filter(current => current.isActive).map(current => current.id),
    [instances]
  )
  const unifiedScopeInstanceIds = useMemo(() => {
    if (!isAllInstances) {
      return undefined
    }

    const normalized = normalizeUnifiedInstanceIds(persistedUnifiedFilter, activeInstanceIds)
    return normalized.length > 0 ? normalized : undefined
  }, [isAllInstances, persistedUnifiedFilter, activeInstanceIds])

  const isMobile = useIsMobile()
  const [columnFilters] = usePersistedColumnFilters(instanceId)

  const selectionScopeKey = selectionScope(instanceId, unifiedScopeInstanceIds, filters, (routeSearch?.q ?? "").trim(), columnFilters, isMobile)
  useEffect(() => {
    dispatchSelection({ type: "scope", scope: selectionScopeKey })
  }, [dispatchSelection, selectionScopeKey])
  useEffect(() => () => dispatchSelection({ type: "clear" }), [dispatchSelection])
  const [titleBarSpeedsEnabled] = usePersistedTitleBarSpeeds(false)

  const titleBarSpeeds = useTitleBarSpeeds({
    enabled: titleBarSpeedsEnabled && !isAllInstances,
    instanceId,
  })
  usePageTitle({ detail: isAllInstances ? undefined : instanceName, speeds: titleBarSpeeds })

  const sidebarWidth = viewMode === "dense" ? "260px" : "320px"
  const [selectedTorrent, setSelectedTorrent] = useState<Torrent | null>(null)
  const [selectionInstanceId, setSelectionInstanceId] = useState(instanceId)
  if (selectionInstanceId !== instanceId) {
    setSelectionInstanceId(instanceId)
    setSelectedTorrent(null)
  }
  const [initialDetailsTab, setInitialDetailsTab] = useState<string | undefined>(undefined)
  const [revealTorrent, setRevealTorrent] = useState<Torrent | null>(null)
  const handleRevealDone = useCallback(() => setRevealTorrent(null), [])
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

  const hideMobileDetails = useCallback(() => {
    setSelectedTorrent(null)
    setInitialDetailsTab(undefined)
  }, [])
  const { open: openMobileDetails, close: closeMobileDetails } = useMobileDetailsHistory({
    enabled: isMobile,
    urlHash: search.torrent,
    shownHash: selectedTorrent?.hash,
    onClosed: hideMobileDetails,
  })

  const pushMobileDetailsEntry = useCallback((torrent: Torrent, initialTab?: string) => {
    openMobileDetails(torrent.hash, (prev) => ({
      ...prev,
      torrent: torrent.hash,
      tab: initialTab,
      instance: isAllInstances ? getTorrentInstanceId(torrent) : undefined,
    }))
  }, [getTorrentInstanceId, isAllInstances, openMobileDetails])

  useEffect(() => {
    if (!search.torrent) return
    const clearDeepLinkParams = () => onSearchChange({ ...search, torrent: undefined, tab: undefined, instance: undefined })
    if (
      selectedTorrent?.hash === search.torrent &&
      (!search.instance || getTorrentInstanceId(selectedTorrent) === search.instance)
    ) {
      if (!isMobile) {
        setRevealTorrent(selectedTorrent)
        clearDeepLinkParams()
      }
      return
    }
    let cancelled = false

    const hash = search.torrent
    const tab = search.tab
    const escapedHash = hash.replaceAll("\"", "\\\"")

    const fetchTorrentByHash = isAllInstances? api.getCrossInstanceTorrents({
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
      instanceIds: search.instance? [search.instance]: activeInstanceIds.length > 0 ? activeInstanceIds : undefined,
    }).then((response) => response.crossInstanceTorrents?.[0] ?? response.cross_instance_torrents?.[0] ?? null): api.getTorrents(instanceId, {
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
      if (cancelled) {
        return
      }
      if (torrent) {
        setSelectedTorrent(torrent)
        setRevealTorrent(torrent)
        if (tab) {
          setInitialDetailsTab(tab)
        }
      }
      if (torrent && isMobile) return
      clearDeepLinkParams()
    }).catch(() => {
      if (cancelled) {
        return
      }
      clearDeepLinkParams()
    })

    return () => {
      cancelled = true
    }
  }, [activeInstanceIds, getTorrentInstanceId, instanceId, isAllInstances, isMobile, onSearchChange, search, selectedTorrent])

  const handleNavigateToTorrent = useCallback((targetInstanceId: number, torrentHash: string) => {
    if (!isAllInstances && targetInstanceId === instanceId) {
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
          if (isMobile) {
            pushMobileDetailsEntry(torrent, "general")
          }
        }
      })
    } else {
      navigate({
        to: "/instances/$instanceId",
        params: { instanceId: String(targetInstanceId) },
        search: { torrent: torrentHash, tab: "general" },
      })
    }
  }, [instanceId, isAllInstances, isMobile, navigate, pushMobileDetailsEntry])

  const [detailsPanelReady, setDetailsPanelReady] = useState(false)

  const panelIds = useMemo(
    () => (selectedTorrent ? ["torrent-list", "torrent-details"] : ["torrent-list"]),
    [selectedTorrent]
  )
  const { defaultLayout, onLayoutChange } = useDefaultLayout({
    id: "qui-torrent-details-panel",
    panelIds,
  })

  const detailsPanelRef = usePanelRef()

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

  const [torrentCounts, setTorrentCounts] = useState<Record<string, number> | undefined>(undefined)
  const [categorySizes, setCategorySizes] = useState<Record<string, number> | undefined>(undefined)
  const [tagSizes, setTagSizes] = useState<Record<string, number> | undefined>(undefined)
  const [categories, setCategories] = useState<Record<string, Category> | undefined>(undefined)
  const [tags, setTags] = useState<string[] | undefined>(undefined)
  const [useSubcategories, setUseSubcategories] = useState<boolean>(false)
  const [supportsTrackerHealth, setSupportsTrackerHealth] = useState<boolean>(false)
  const [lastInstanceId, setLastInstanceId] = useState<number | null>(null)

  const isSameTorrent = useCallback((left: Torrent | null, right: Torrent | null) => {
    if (!left || !right) {
      return false
    }

    return left.hash === right.hash && getTorrentInstanceId(left) === getTorrentInstanceId(right)
  }, [getTorrentInstanceId])
  if (revealTorrent && !isSameTorrent(revealTorrent, selectedTorrent)) setRevealTorrent(null)

  const handleTorrentSelect = useCallback((torrent: Torrent | null) => {
    if (torrent && isSameTorrent(selectedTorrent, torrent)) {
      closeMobileDetails()
    } else {
      setSelectedTorrent(torrent)
      setInitialDetailsTab(undefined)
      if (isMobile && torrent) {
        pushMobileDetailsEntry(torrent)
      }
    }
  }, [closeMobileDetails, isMobile, isSameTorrent, pushMobileDetailsEntry, selectedTorrent])

  const swipeToClose = useSwipeToClose(closeMobileDetails)

  const handleFilteredDataUpdate = useCallback((_torrents: Torrent[], _total: number, counts?: TorrentCounts, categoriesData?: Record<string, Category>, tagsData?: string[], subcategoriesEnabled?: boolean, trackerHealthEnabled?: boolean) => {
    setLastInstanceId(instanceId)

    if (counts) {
      const transformedCounts: Record<string, number> = {}

      Object.entries(counts.status || {}).forEach(([status, count]) => {
        transformedCounts[`status:${status}`] = count as number
      })

      Object.entries(counts.categories || {}).forEach(([category, count]) => {
        transformedCounts[`category:${category}`] = count as number
      })

      Object.entries(counts.tags || {}).forEach(([tag, count]) => {
        transformedCounts[`tag:${tag}`] = count as number
      })

      Object.entries(counts.trackers || {}).forEach(([tracker, count]) => {
        transformedCounts[`tracker:${tracker}`] = count as number
      })

      transformedCounts.filtered = _total

      setTorrentCounts(transformedCounts)

      if (counts.categorySizes) {
        setCategorySizes(counts.categorySizes)
      }
      if (counts.tagSizes) {
        setTagSizes(counts.tagSizes)
      }
    }

    if (categoriesData !== undefined) {
      setCategories(categoriesData)
    }
    if (tagsData !== undefined) {
      setTags(tagsData)
    }

    if (subcategoriesEnabled !== undefined) {
      setUseSubcategories(subcategoriesEnabled)
    }
    if (trackerHealthEnabled !== undefined) {
      setSupportsTrackerHealth(trackerHealthEnabled)
    }
  }, [instanceId])

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

  useEffect(() => {
    if (!isMobile && selectedTorrent && detailsPanelReady && detailsPanelRef.current?.isCollapsed()) {
      detailsPanelRef.current.expand()
    }
  }, [detailsPanelReady, detailsPanelRef, selectedTorrent, isMobile])

  const clearListSelection = useCallback(() => {
    setSelectedTorrent(null)
    dispatchSelection({ type: "clear" })
  }, [dispatchSelection])
  const clearListSearch = useCallback(() => {
    navigateWithSearch({ navigate, search: { ...routeSearch, q: undefined }, replace: true })
  }, [navigate, routeSearch])
  useTorrentListEscape({
    hasSelection: selectedTorrent !== null || hasSelection(selection),
    hasSearch: Boolean(routeSearch?.q),
    clearSelection: clearListSelection,
    clearSearch: clearListSearch,
  })

  useEffect(() => {
    if (!isMobile) {
      setMobileFilterOpen(false)
    }
  }, [isMobile])

  return (
    <div className="flex h-full relative">
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
            instanceIds={unifiedScopeInstanceIds}
            readOnly={isAllInstances}
            supportsTrackerHealth={isAllInstances ? supportsTrackerHealth : undefined}
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
            <SheetTitle className="text-lg font-semibold">{t("filterSidebar.title")}</SheetTitle>
          </SheetHeader>
          <div className="flex-1 min-h-0 overflow-hidden">
            <FilterSidebar
              key={`filter-sidebar-mobile-${instanceId}`}
              instanceId={instanceId}
              instanceIds={unifiedScopeInstanceIds}
              readOnly={isAllInstances}
              supportsTrackerHealth={isAllInstances ? supportsTrackerHealth : undefined}
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

      <div className="flex-1 flex flex-col min-w-0">
        {!isMobile && (
          <div className="flex flex-col h-full">
            {spreadsheetDisguise && <SpreadsheetFormulaBar />}
            <ResizablePanelGroup
              className="flex-1 min-h-0"
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
                    instanceIds={unifiedScopeInstanceIds}
                    filters={filters}
                    selectedTorrent={selectedTorrent}
                    onTorrentSelect={handleTorrentSelect}
                    revealTorrent={revealTorrent}
                    onRevealDone={handleRevealDone}
                    addTorrentModalOpen={isAddTorrentModalOpen}
                    onAddTorrentModalChange={handleAddTorrentModalChange}
                    onFilteredDataUpdate={handleFilteredDataUpdate}
                    onFilterChange={setFilters}
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
                        setSelectedTorrent(null)
                      }
                    }}
                  >
                    <div className="h-full border-t bg-background" data-torrent-details-panel>
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
            {spreadsheetDisguise && <SpreadsheetSheetTabs currentInstanceId={instanceId} />}
            <div id="qui-status-bar-container" className="flex-shrink-0 bg-background" />
          </div>
        )}

        {isMobile && (
          <div className="flex flex-col h-full px-4">
            <TorrentTableResponsive
              instanceId={instanceId}
              instanceIds={unifiedScopeInstanceIds}
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

      {isMobile && (
        <Sheet
          open={!!selectedTorrent}
          onOpenChange={(open) => {
            if (!open) {
              closeMobileDetails()
            }
          }}
        >
          <SheetContent
            side="right"
            className="w-full p-0 gap-0 outline-none ease-out data-[state=open]:duration-200 data-[state=closed]:duration-150"
            hideClose
            {...swipeToClose}
          >
            <SheetHeader className="sr-only">
              <SheetTitle>
                {selectedTorrent ? t("page.torrentDetailsWithName", { name: selectedTorrent.name }) : t("page.torrentDetails")}
              </SheetTitle>
            </SheetHeader>
            {selectedTorrent && (
              <TorrentDetailsPanel
                instanceId={selectedTorrentInstanceId}
                torrent={selectedTorrent}
                initialTab={initialDetailsTab}
                onInitialTabConsumed={handleInitialTabConsumed}
                layout="mobile"
                onClose={closeMobileDetails}
                onNavigateToTorrent={handleNavigateToTorrent}
              />
            )}
          </SheetContent>
        </Sheet>
      )}

      {!isAllInstances && (
        <TorrentCreatorDialog
          instanceId={instanceId}
          open={isCreateTorrentModalOpen}
          onOpenChange={handleCreateTorrentModalChange}
        />
      )}

      {!isAllInstances && (
        <Dialog open={isTasksModalOpen} onOpenChange={handleTasksModalChange}>
          <DialogContent className="w-full sm:max-w-screen-sm md:max-w-screen-md lg:max-w-screen-xl xl:max-w-screen-xl max-h-[85vh] overflow-hidden flex flex-col">
            <DialogHeader>
              <DialogTitle>{t("creationTasks.dialogTitle")}</DialogTitle>
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
