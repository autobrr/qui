/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type {
  OrphanScanRun,
  OrphanScanRunWithFiles,
  OrphanScanSettings,
  OrphanScanSettingsUpdate,
} from "@/types"

export function useOrphanScanSettings(instanceId: number, options?: { enabled?: boolean }) {
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0

  return useQuery({
    queryKey: ["orphan-scan", instanceId, "settings"],
    queryFn: () => api.getOrphanScanSettings(instanceId),
    enabled: shouldEnable,
    staleTime: 30_000,
  })
}

export function useUpdateOrphanScanSettings(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: OrphanScanSettingsUpdate) =>
      api.updateOrphanScanSettings(instanceId, data),
    onSuccess: (settings: OrphanScanSettings) => {
      queryClient.setQueryData<OrphanScanSettings>(
        ["orphan-scan", instanceId, "settings"],
        settings
      )
    },
  })
}

export function useTriggerOrphanScan(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.triggerOrphanScan(instanceId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["orphan-scan", instanceId, "runs"] })
    },
  })
}

export function useOrphanScanRuns(
  instanceId: number,
  options?: { limit?: number; enabled?: boolean }
) {
  const limit = options?.limit
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0

  return useQuery({
    queryKey: ["orphan-scan", instanceId, "runs", limit ?? null],
    queryFn: () =>
      api.listOrphanScanRuns(instanceId, {
        ...(limit !== undefined ? { limit } : {}),
      }),
    enabled: shouldEnable,
    refetchInterval: (query) => {
      const runs = query.state.data as OrphanScanRun[] | undefined
      if (!runs) {
        return 5_000
      }
      const hasActiveRun = runs.some(
        (run: OrphanScanRun) =>
          run.status === "pending" ||
          run.status === "scanning" ||
          run.status === "deleting"
      )
      return hasActiveRun ? 1_000 : 15_000
    },
  })
}

export function useOrphanScanRun(
  instanceId: number,
  runId: number | undefined,
  options?: { limit?: number; offset?: number; enabled?: boolean }
) {
  const limit = options?.limit
  const offset = options?.offset
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0 && !!runId

  return useQuery({
    queryKey: ["orphan-scan", instanceId, "run", runId, limit ?? null, offset ?? null],
    queryFn: () =>
      api.getOrphanScanRun(instanceId, runId as number, {
        ...(limit !== undefined ? { limit } : {}),
        ...(offset !== undefined ? { offset } : {}),
      }),
    enabled: shouldEnable,
    refetchInterval: (query) => {
      const run = query.state.data as OrphanScanRunWithFiles | undefined
      if (!run) {
        return false
      }
      const isActive =
        run.status === "pending" ||
        run.status === "scanning" ||
        run.status === "deleting"
      return isActive ? 1_000 : false
    },
  })
}

export function useConfirmOrphanScanDeletion(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (runId: number) => api.confirmOrphanScanDeletion(instanceId, runId),
    onSuccess: (_data, runId) => {
      queryClient.invalidateQueries({ queryKey: ["orphan-scan", instanceId, "runs"] })
      queryClient.invalidateQueries({ queryKey: ["orphan-scan", instanceId, "run", runId] })
    },
  })
}

export function useCancelOrphanScanRun(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (runId: number) => api.cancelOrphanScanRun(instanceId, runId),
    onSuccess: (_data, runId) => {
      queryClient.invalidateQueries({ queryKey: ["orphan-scan", instanceId, "runs"] })
      queryClient.invalidateQueries({ queryKey: ["orphan-scan", instanceId, "run", runId] })
    },
  })
}
