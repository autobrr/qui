/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { BackupRun, BackupRunsResponse, BackupSettings, RestoreMode, RestorePlan, RestoreResult } from "@/types"

export function useBackupSettings(instanceId: number, options?: { enabled?: boolean }) {
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0

  return useQuery({
    queryKey: ["instance-backups", instanceId, "settings"],
    queryFn: () => api.getBackupSettings(instanceId),
    enabled: shouldEnable,
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
      keepHourly: number
      keepDaily: number
      keepWeekly: number
      keepMonthly: number
      includeCategories: boolean
      includeTags: boolean
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
      queryClient.setQueriesData<BackupRunsResponse>(
        {
          predicate: (query) => {
            const key = query.queryKey
            if (!Array.isArray(key)) {
              return false
            }
            const [, keyInstanceId, section, , offset] = key
            if (keyInstanceId !== instanceId || section !== "runs") {
              return false
            }
            return offset === 0 || offset === null || offset === undefined
          },
        },
        (existing) => {
          if (!existing) {
            return { runs: [run], hasMore: false }
          }
          const filtered = existing.runs.filter(item => item.id !== run.id)
          return { ...existing, runs: [run, ...filtered] }
        }
      )
    },
  })
}

export function useBackupRuns(
  instanceId: number,
  options?: { limit?: number; offset?: number; enabled?: boolean }
) {
  const limit = options?.limit
  const offset = options?.offset
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0

  return useQuery({
    queryKey: ["instance-backups", instanceId, "runs", limit ?? null, offset ?? null],
    queryFn: () =>
      api.listBackupRuns(instanceId, {
        ...(limit !== undefined ? { limit } : {}),
        ...(offset !== undefined ? { offset } : {}),
      }),
    enabled: shouldEnable,
    refetchInterval: (query) => {
      const response = query.state.data as BackupRunsResponse | undefined
      const runs = response?.runs
      if (!runs) {
        return 5_000
      }
      const hasActiveRun = runs.some((run: BackupRun) => run.status === "pending" || run.status === "running")
      return hasActiveRun ? 500 : 15_000
    },
  })
}

export function useBackupManifest(instanceId: number, runId?: number, options?: { enabled?: boolean }) {
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0 && !!runId

  return useQuery({
    queryKey: ["instance-backups", instanceId, "manifest", runId],
    queryFn: () => api.getBackupManifest(instanceId, runId as number),
    enabled: shouldEnable,
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

export function useDeleteAllBackupRuns(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.deleteAllBackups(instanceId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["instance-backups", instanceId, "runs"] })
    },
  })
}

export function usePreviewRestore(instanceId: number) {
  return useMutation<RestorePlan, Error, { runId: number; mode: RestoreMode; excludeHashes?: string[] }>({
    mutationFn: ({ runId, mode, excludeHashes }) => api.previewRestore(instanceId, runId, { mode, excludeHashes }),
  })
}

export function useExecuteRestore(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation<RestoreResult, Error, { runId: number; mode: RestoreMode; dryRun: boolean; excludeHashes?: string[]; startPaused?: boolean; skipHashCheck?: boolean; autoResumeVerified?: boolean }>({
    mutationFn: ({ runId, mode, dryRun, excludeHashes, startPaused, skipHashCheck, autoResumeVerified }) =>
      api.executeRestore(instanceId, runId, { mode, dryRun, excludeHashes, startPaused, skipHashCheck, autoResumeVerified }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["instance-backups", instanceId, "runs"] })
    },
  })
}

export function useImportBackupManifest(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (manifestFile: File) => api.importBackupManifest(instanceId, manifestFile),
    onSuccess: (run: BackupRun) => {
      queryClient.invalidateQueries({ queryKey: ["instance-backups", instanceId, "runs"] })
      queryClient.setQueriesData<BackupRunsResponse>(
        {
          predicate: (query) => {
            const key = query.queryKey
            if (!Array.isArray(key)) {
              return false
            }
            const [, keyInstanceId, section, , offset] = key
            if (keyInstanceId !== instanceId || section !== "runs") {
              return false
            }
            return offset === 0 || offset === null || offset === undefined
          },
        },
        (existing) => {
          if (!existing) {
            return { runs: [run], hasMore: false }
          }
          const filtered = existing.runs.filter(item => item.id !== run.id)
          return { ...existing, runs: [run, ...filtered] }
        }
      )
    },
  })
}
