/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useCrossSeedSearch } from "@/hooks/useCrossSeedSearch"
import type { ServerState, Torrent, TorrentFilters } from "@/types"
import { useEffect, useState } from "react"
import type { SelectionInfo } from "./GlobalStatusBar"
import { TorrentCardsMobile } from "./TorrentCardsMobile"
import { TorrentTableOptimized } from "./TorrentTableOptimized"

interface TorrentTableResponsiveProps {
  instanceId: number
  filters?: TorrentFilters
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (
    torrents: Torrent[],
    total: number,
    counts?: any,
    categories?: any,
    tags?: string[],
    useSubcategories?: boolean
  ) => void
  onFilterChange?: (filters: TorrentFilters) => void
  onServerStateUpdate?: (serverState: ServerState | null, listenPort?: number | null) => void
  onSelectionInfoUpdate?: (info: SelectionInfo) => void
}

export function TorrentTableResponsive(props: TorrentTableResponsiveProps) {
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 768)
  const { updateSelection, setFiltersAndInstance, setResetHandler } = useTorrentSelection()
  const crossSeed = useCrossSeedSearch(props.instanceId)

  // Update context with current filters and instance
  useEffect(() => {
    setFiltersAndInstance(props.filters, props.instanceId)
  }, [props.filters, props.instanceId, setFiltersAndInstance])

  // Debounced resize/orientation handler
  useEffect(() => {
    // Use number for timeoutId in browser
    let timeoutId: number | null = null
    const checkMobile = () => setIsMobile(window.innerWidth < 768)
    const handleResizeOrOrientation = () => {
      if (timeoutId) clearTimeout(timeoutId)
      timeoutId = window.setTimeout(checkMobile, 100)
    }
    window.addEventListener("resize", handleResizeOrOrientation)
    window.addEventListener("orientationchange", handleResizeOrOrientation)
    checkMobile()
    return () => {
      window.removeEventListener("resize", handleResizeOrOrientation)
      window.removeEventListener("orientationchange", handleResizeOrOrientation)
      if (timeoutId) clearTimeout(timeoutId)
    }
  }, [])

  // Media query for more accurate detection
  useEffect(() => {
    const mediaQuery = window.matchMedia("(max-width: 767px)")
    const handleChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    setIsMobile(mediaQuery.matches)
    if (mediaQuery.addEventListener) {
      mediaQuery.addEventListener("change", handleChange)
      return () => mediaQuery.removeEventListener("change", handleChange)
    } else if (mediaQuery.addListener) {
      mediaQuery.addListener(handleChange)
      return () => mediaQuery.removeListener(handleChange)
    }
  }, [])

  // Memoize props to avoid unnecessary re-renders
  const memoizedProps = props // If props are stable, this is fine; otherwise use useMemo

  if (isMobile) {
    return (
      <>
        <TorrentCardsMobile
          {...memoizedProps}
          canCrossSeedSearch={crossSeed.canCrossSeedSearch}
          onCrossSeedSearch={crossSeed.openCrossSeedSearch}
          isCrossSeedSearching={crossSeed.isCrossSeedSearching}
        />
        {crossSeed.crossSeedDialog}
      </>
    )
  }
  return (
    <>
      <TorrentTableOptimized
        {...memoizedProps}
        onSelectionChange={updateSelection}
        onResetSelection={setResetHandler}
        canCrossSeedSearch={crossSeed.canCrossSeedSearch}
        onCrossSeedSearch={crossSeed.openCrossSeedSearch}
        isCrossSeedSearching={crossSeed.isCrossSeedSearching}
      />
      {crossSeed.crossSeedDialog}
    </>
  )
}
