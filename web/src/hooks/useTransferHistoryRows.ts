/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { TransferHistoryInstance, TransferHistoryQuery, TransferHistoryRow } from "@/types"
import { useQueries, type UseQueryResult } from "@tanstack/react-query"
import { useCallback } from "react"

export interface InstanceRows {
  rows?: TransferHistoryRow[][]
  error?: Error
  pending: boolean
}

/** Reads one transfer query for each instance. The rows are undefined while a query is pending or when no query runs. */
export function useTransferHistoryRows(instances: TransferHistoryInstance[], query: TransferHistoryQuery | undefined): InstanceRows {
  const enabled = query !== undefined
  const combine = useCallback((results: UseQueryResult<TransferHistoryRow[]>[]): InstanceRows => ({
    rows: enabled && results.every(result => result.data) ? results.map(result => result.data!) : undefined,
    error: results.find(result => result.error)?.error ?? undefined,
    pending: enabled && results.some(result => result.isPending),
  }), [enabled])
  return useQueries({
    queries: instances.map(instance => ({
      queryKey: ["transfer-history", instance.instanceId, query?.groupBy, query?.groupOf, query?.group, query?.limit, query?.bucket, query?.timeZone, query?.from?.toISOString(), query?.to.toISOString()],
      queryFn: () => api.getTransferHistory(instance.instanceId, query!),
      enabled,
    })),
    combine,
  })
}
