/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isDemo } from "@/lib/demo"
import { listShares, sharesQueryKey } from "@/lib/shares-api"
import { transfersApi } from "@/lib/transfers-api"
import { useQuery } from "@tanstack/react-query"

// The sidebar shows the count on every page, so it asks less often than the Transfers page.
const REFRESH_MS = 15_000

/** The number of items that run on the Transfers page: the active Transfers and the Shares that have not ended. */
export function useRunningTransfers(): number {
  // The same keys as the Transfers page, so that the page and the sidebar share one fetch.
  const transfers = useQuery({
    queryKey: ["transfers", "list", "active"],
    queryFn: () => transfersApi.list("active"),
    refetchInterval: REFRESH_MS,
    // The demo has no Transfers page.
    enabled: !isDemo,
  })
  const shares = useQuery({ queryKey: sharesQueryKey, queryFn: listShares, refetchInterval: REFRESH_MS, enabled: !isDemo })
  const runningShares = shares.data?.filter((share) => share.state !== "ended").length ?? 0
  return (transfers.data?.counts.active ?? 0) + runningShares
}
