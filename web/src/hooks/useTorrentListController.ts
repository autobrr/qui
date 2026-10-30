/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { combineFilterExpr, isCrossSeedExpr } from "@/lib/torrent-filters"
import type { ActionTarget } from "@/lib/torrent-selection"
import { anyTorrentHasTag } from "@/lib/torrent-utils"
import type { TorrentFilters } from "@/types"
import { useMemo } from "react"

export { TORRENT_STREAM_POLL_INTERVAL_SECONDS } from "@/hooks/useTorrentsList"

export interface UseTorrentListControllerParams {
  instanceId: number
  instanceIds?: number[]
  filters: TorrentFilters
  /** The column filters as one expression. The mobile cards have none. */
  columnFiltersExpr?: string | null
  search: string
  sort: string
  order: "asc" | "desc"
}

/**
 * Loads the torrent list for the desktop table and the mobile cards.
 *
 * Polling stops while the tab is hidden, the list refetches when the window
 * gets focus again, and the list hook closes the live stream after the tab is
 * hidden for STREAM_HIDDEN_PAUSE_DELAY_MS.
 */
export function useTorrentListController({
  instanceId,
  instanceIds,
  filters,
  columnFiltersExpr = null,
  search,
  sort,
  order,
}: UseTorrentListControllerParams) {
  // React Query stops the polling while the tab is hidden. A tab that comes
  // back within 3 s skips the focus refetch; a later return refetches.
  const { isHiddenDelayed, isVisible } = useDelayedVisibility(3000)

  // In cross-seed mode the column filters apply client-side, so the request
  // gets the hash expression alone. The Action target always joins them (#1925).
  const listFilters = useMemo(() => {
    const expr = isCrossSeedExpr(filters.expr) ? filters.expr : combineFilterExpr(columnFiltersExpr, filters.expr)
    return { ...filters, expr: expr || undefined }
  }, [filters, columnFiltersExpr])

  return useTorrentsList(instanceId, {
    pollingEnabled: isHiddenDelayed || isVisible,
    instanceIds,
    search,
    filters: listFilters,
    sort,
    order,
  })
}

/**
 * The cross-seed state of the Action target: the warning (the cross-seeds that
 * a delete affects), whether the target carries the cross-seed tag, whether a
 * delete blocks the cross-seeds, and the blocklist action. It is a separate
 * hook because each Layout builds the Action target from the controller's rows.
 */
export function useTorrentListCrossSeed({
  instanceId,
  actionTarget,
  blockCrossSeeds,
}: {
  instanceId: number
  actionTarget: ActionTarget
  blockCrossSeeds: boolean
}) {
  const crossSeedWarning = useCrossSeedWarning({
    instanceId,
    torrents: actionTarget.previewTorrents,
    selectAll: actionTarget.kind === "selectAll" ? actionTarget.selectAll : undefined,
  })
  const hasCrossSeedTag = useMemo(
    () => anyTorrentHasTag(actionTarget.previewTorrents, "cross-seed") || anyTorrentHasTag(crossSeedWarning.affectedTorrents, "cross-seed"),
    [actionTarget.previewTorrents, crossSeedWarning.affectedTorrents]
  )
  const shouldBlockCrossSeeds = hasCrossSeedTag && blockCrossSeeds
  const { blockCrossSeedHashes } = useCrossSeedBlocklistActions(instanceId)

  return { crossSeedWarning, hasCrossSeedTag, shouldBlockCrossSeeds, blockCrossSeedHashes }
}
