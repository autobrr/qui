/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { TransferHistoryInstance, TransferHistoryLibraryRow } from "@/types"
import { useQueries, type UseQueryResult } from "@tanstack/react-query"
import { useCallback } from "react"

export interface InstanceLibrary {
  rows?: TransferHistoryLibraryRow[][]
  error?: Error
  pending: boolean
}

/** Reads the library rows of each instance. The rows are undefined while a query is pending or when no query runs. */
export function useTransferHistoryLibrary(instances: TransferHistoryInstance[], period: { from?: Date; to: Date } | undefined): InstanceLibrary {
  const enabled = period !== undefined
  const combine = useCallback((results: UseQueryResult<TransferHistoryLibraryRow[]>[]): InstanceLibrary => ({
    rows: enabled && results.every(result => result.data) ? results.map(result => result.data!) : undefined,
    error: results.find(result => result.error)?.error ?? undefined,
    pending: enabled && results.some(result => result.isPending),
  }), [enabled])
  return useQueries({
    queries: instances.map(instance => ({
      queryKey: ["transfer-history", "library", instance.instanceId, period?.from?.toISOString(), period?.to.toISOString()],
      queryFn: () => api.getTransferHistoryLibrary(instance.instanceId, period!),
      enabled,
    })),
    combine,
  })
}
