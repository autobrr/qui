/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useCrossSeedSearch } from "@/hooks/useCrossSeedSearch"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { isAllInstancesScope } from "@/lib/instances"
import type { Category, ServerState, Torrent, TorrentCounts, TorrentFilters } from "@/types"
import { useEffect } from "react"
import type { SelectionInfo } from "./GlobalStatusBar"
import { TorrentCardsMobile } from "./TorrentCardsMobile"
import { TorrentTableOptimized } from "./TorrentTableOptimized"

interface TorrentTableResponsiveProps {
  instanceId: number
  instanceIds?: number[]
  readOnly?: boolean
  filters?: TorrentFilters
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (
    torrents: Torrent[],
    total: number,
    counts?: TorrentCounts,
    categories?: Record<string, Category>,
    tags?: string[],
    useSubcategories?: boolean
  ) => void
  onFilterChange?: (filters: TorrentFilters) => void
  onServerStateUpdate?: (serverState: ServerState | null, listenPort?: number | null) => void
  onSelectionInfoUpdate?: (info: SelectionInfo) => void
}

export function TorrentTableResponsive(props: TorrentTableResponsiveProps) {
  const isMobile = useIsMobile()
  const { updateSelection, setFiltersAndInstance, setResetHandler } = useTorrentSelection()
  const readOnly = props.readOnly ?? false
  const isAllInstancesView = isAllInstancesScope(props.instanceId)
  const crossSeed = useCrossSeedSearch(props.instanceId)
  const allowCrossSeedSearch = !readOnly && !isAllInstancesView

  // Update context with current filters and instance
  useEffect(() => {
    setFiltersAndInstance(props.filters, props.instanceId)
  }, [props.filters, props.instanceId, setFiltersAndInstance])

  // Memoize props to avoid unnecessary re-renders
  const memoizedProps = props // If props are stable, this is fine; otherwise use useMemo

  if (isMobile && !readOnly) {
    return (
      <>
        <TorrentCardsMobile
          {...memoizedProps}
          onFilterChange={allowCrossSeedSearch ? props.onFilterChange : undefined}
          canCrossSeedSearch={allowCrossSeedSearch ? crossSeed.canCrossSeedSearch : false}
          onCrossSeedSearch={allowCrossSeedSearch ? crossSeed.openCrossSeedSearch : undefined}
          isCrossSeedSearching={allowCrossSeedSearch ? crossSeed.isCrossSeedSearching : false}
        />
        {allowCrossSeedSearch && crossSeed.crossSeedDialog}
      </>
    )
  }
  return (
    <>
      <TorrentTableOptimized
        {...memoizedProps}
        readOnly={readOnly}
        onSelectionChange={updateSelection}
        onResetSelection={setResetHandler}
        onFilterChange={allowCrossSeedSearch ? props.onFilterChange : undefined}
        canCrossSeedSearch={allowCrossSeedSearch ? crossSeed.canCrossSeedSearch : false}
        onCrossSeedSearch={allowCrossSeedSearch ? crossSeed.openCrossSeedSearch : undefined}
        isCrossSeedSearching={allowCrossSeedSearch ? crossSeed.isCrossSeedSearching : false}
      />
      {allowCrossSeedSearch && crossSeed.crossSeedDialog}
    </>
  )
}
