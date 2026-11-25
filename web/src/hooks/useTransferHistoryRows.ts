/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { HISTORY_WRITE_MS } from "@/lib/statistics"
import type { TransferHistoryInstance, TransferHistoryQuery, TransferHistoryRow } from "@/types"
import { useQueries, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { useCallback } from "react"

export interface InstanceRows {
  rows?: TransferHistoryRow[][]
  error?: Error
  pending: boolean
}

function transferQuery(instanceId: number, query: TransferHistoryQuery | undefined) {
  return {
    queryKey: ["transfer-history", instanceId, query?.groupBy, query?.groupOf, query?.group, query?.limit, query?.bucket, query?.timeZone, query?.from?.toISOString(), query?.to.toISOString()],
    queryFn: () => api.getTransferHistory(instanceId, query!),
  }
}

export function useTransferHistoryRows(instances: TransferHistoryInstance[], query: TransferHistoryQuery | undefined): InstanceRows {
  const enabled = query !== undefined
  const combine = useCallback((results: UseQueryResult<TransferHistoryRow[]>[]): InstanceRows => ({
    rows: enabled && results.every(result => result.data) ? results.map(result => result.data!) : undefined,
    error: results.find(result => result.error)?.error ?? undefined,
    pending: enabled && results.some(result => result.isPending),
  }), [enabled])
  return useQueries({
    queries: instances.map(instance => ({ ...transferQuery(instance.instanceId, query), enabled })),
    combine,
  })
}

export function usePrefetchTransferHistoryRows() {
  const queryClient = useQueryClient()
  return useCallback((instances: TransferHistoryInstance[], query: TransferHistoryQuery) => {
    for (const instance of instances) {
      void queryClient.prefetchQuery({ ...transferQuery(instance.instanceId, query), staleTime: HISTORY_WRITE_MS })
    }
  }, [queryClient])
}

export function useTransferHistoryStatus() {
  return useQuery({ queryKey: ["transfer-history", "status"], queryFn: () => api.getTransferHistoryStatus(), refetchInterval: HISTORY_WRITE_MS })
}
