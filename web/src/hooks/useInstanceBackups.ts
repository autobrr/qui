/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { BackupRun, BackupSettings, RestoreMode, RestorePlan, RestoreResult } from "@/types"

export function useBackupSettings(instanceId: number) {
  return useQuery({
    queryKey: ["instance-backups", instanceId, "settings"],
    queryFn: () => api.getBackupSettings(instanceId),
    enabled: instanceId > 0,
    staleTime: 30_000,
  })
}

export function useUpdateBackupSettings(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: {
      enabled: boolean
      hourlyEnabled: boolean
      dailyEnabled: boolean
      weeklyEnabled: boolean
      monthlyEnabled: boolean
      keepLast: number
      keepHourly: number
      keepDaily: number
      keepWeekly: number
      keepMonthly: number
      includeCategories: boolean
      includeTags: boolean
      customPath?: string | null
    }) => api.updateBackupSettings(instanceId, data),
    onSuccess: (settings: BackupSettings) => {
      queryClient.setQueryData<BackupSettings>(["instance-backups", instanceId, "settings"], settings)
    },
  })
}

export function useTriggerBackup(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (payload: { kind?: string; requestedBy?: string } = {}) => api.triggerBackup(instanceId, payload),
    onSuccess: (run: BackupRun) => {
      queryClient.invalidateQueries({ queryKey: ["instance-backups", instanceId, "runs"] })
      queryClient.setQueryData<BackupRun[]>(["instance-backups", instanceId, "runs"], (existing) => {
        if (!existing) return [run]
        const filtered = existing.filter(item => item.id !== run.id)
        return [run, ...filtered]
      })
    },
  })
}

export function useBackupRuns(instanceId: number, options?: { limit?: number; offset?: number }) {
  const limit = options?.limit ?? 25
  const offset = options?.offset ?? 0

  return useQuery({
    queryKey: ["instance-backups", instanceId, "runs", { limit, offset }],
    queryFn: () => api.listBackupRuns(instanceId, { limit, offset }),
    enabled: instanceId > 0,
    refetchInterval: (query) => {
      const runs = query.state.data as BackupRun[] | undefined
      if (!runs) {
        return 5_000
      }
      const hasActiveRun = runs.some((run: BackupRun) => run.status === "pending" || run.status === "running")
      return hasActiveRun ? 3_000 : 15_000
    },
  })
}

export function useBackupManifest(instanceId: number, runId?: number) {
  return useQuery({
    queryKey: ["instance-backups", instanceId, "manifest", runId],
    queryFn: () => api.getBackupManifest(instanceId, runId as number),
    enabled: instanceId > 0 && !!runId,
  })
}

export function useDeleteBackupRun(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (runId: number) => api.deleteBackupRun(instanceId, runId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["instance-backups", instanceId, "runs"] })
    },
  })
}

export function usePreviewRestore(instanceId: number) {
  return useMutation<RestorePlan, Error, { runId: number; mode: RestoreMode }>({
    mutationFn: ({ runId, mode }) => api.previewRestore(instanceId, runId, { mode }),
  })
}

export function useExecuteRestore(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation<RestoreResult, Error, { runId: number; mode: RestoreMode; dryRun: boolean }>({
    mutationFn: ({ runId, mode, dryRun }) => api.executeRestore(instanceId, runId, { mode, dryRun }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["instance-backups", instanceId, "runs"] })
    },
  })
}
