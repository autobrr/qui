/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isDemo } from "@/lib/demo"
import { listShares, sharesQueryKey } from "@/lib/shares-api"
import { transferListQuery } from "@/lib/transfers-api"
import { useQuery } from "@tanstack/react-query"

const REFRESH_MS = 15_000

export function useTransferQueue(): number {
  const transfers = useQuery({
    ...transferListQuery("active", 0),
    refetchInterval: REFRESH_MS,
    enabled: !isDemo,
  })
  const shares = useQuery({ queryKey: sharesQueryKey, queryFn: listShares, refetchInterval: REFRESH_MS, enabled: !isDemo })
  const runningShares = shares.data?.filter((share) => share.state !== "ended").length ?? 0
  const counts = transfers.data?.counts
  return (counts ? counts.active + counts.waiting : 0) + runningShares
}
