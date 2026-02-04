/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type {
  DirScanDirectory,
  DirScanDirectoryCreate,
  DirScanDirectoryUpdate,
  DirScanRun,
  DirScanRunInjection,
  DirScanRunStatus,
  DirScanSettings,
  DirScanSettingsUpdate
} from "@/types"

const ACTIVE_STATUSES: DirScanRunStatus[] = ["queued", "scanning", "searching", "injecting"]

export function isRunActive(run: DirScanRun): boolean {
  return ACTIVE_STATUSES.includes(run.status)
}

export function useDirScanSettings(options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: ["dir-scan", "settings"],
    queryFn: () => api.getDirScanSettings(),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
  })
}

export function useUpdateDirScanSettings() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: DirScanSettingsUpdate) => api.updateDirScanSettings(data),
    onSuccess: (settings: DirScanSettings) => {
      queryClient.setQueryData<DirScanSettings>(["dir-scan", "settings"], settings)
    },
  })
}

export function useDirScanDirectories(options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: ["dir-scan", "directories"],
    queryFn: () => api.listDirScanDirectories(),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
  })
}

export function useDirScanDirectory(directoryId: number, options?: { enabled?: boolean }) {
  const shouldEnable = (options?.enabled ?? true) && directoryId > 0

  return useQuery({
    queryKey: ["dir-scan", "directory", directoryId],
    queryFn: () => api.getDirScanDirectory(directoryId),
    enabled: shouldEnable,
    staleTime: 30_000,
  })
}

export function useCreateDirScanDirectory() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: DirScanDirectoryCreate) => api.createDirScanDirectory(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directories"] })
    },
  })
}

export function useUpdateDirScanDirectory(directoryId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: DirScanDirectoryUpdate) =>
      api.updateDirScanDirectory(directoryId, data),
    onSuccess: (directory: DirScanDirectory) => {
      queryClient.setQueryData<DirScanDirectory>(
        ["dir-scan", "directory", directoryId],
        directory
      )
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directories"] })
    },
  })
}

export function useDeleteDirScanDirectory() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (directoryId: number) => api.deleteDirScanDirectory(directoryId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directories"] })
    },
  })
}

export function useResetDirScanFiles(directoryId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.resetDirScanFiles(directoryId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directory", directoryId, "files"] })
    },
  })
}

export function useTriggerDirScan(directoryId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.triggerDirScan(directoryId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directory", directoryId, "status"] })
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directory", directoryId, "runs"] })
    },
  })
}

export function useCancelDirScan(directoryId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.cancelDirScan(directoryId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directory", directoryId, "status"] })
      queryClient.invalidateQueries({ queryKey: ["dir-scan", "directory", directoryId, "runs"] })
    },
  })
}

export function useDirScanStatus(directoryId: number, options?: { enabled?: boolean }) {
  const shouldEnable = (options?.enabled ?? true) && directoryId > 0

  return useQuery({
    queryKey: ["dir-scan", "directory", directoryId, "status"],
    queryFn: () => api.getDirScanStatus(directoryId),
    enabled: shouldEnable,
    refetchInterval: (query) => {
      const data = query.state.data
      if (!data || ("status" in data && data.status === "idle")) {
        return 15_000
      }
      return isRunActive(data as DirScanRun) ? 1_000 : 15_000
    },
  })
}

export function useDirScanRuns(
  directoryId: number,
  options?: { limit?: number; enabled?: boolean }
) {
  const limit = options?.limit
  const shouldEnable = (options?.enabled ?? true) && directoryId > 0

  return useQuery({
    queryKey: ["dir-scan", "directory", directoryId, "runs", limit ?? null],
    queryFn: () =>
      api.listDirScanRuns(directoryId, {
        ...(limit !== undefined ? { limit } : {}),
      }),
    enabled: shouldEnable,
    refetchInterval: (query) => {
      const runs = query.state.data as DirScanRun[] | undefined
      if (!runs) {
        return 5_000
      }
      return runs.some(isRunActive) ? 1_000 : 15_000
    },
  })
}

export function useDirScanFiles(
  directoryId: number,
  options?: { limit?: number; offset?: number; status?: string; enabled?: boolean }
) {
  const { limit, offset, status, enabled } = options ?? {}
  const shouldEnable = (enabled ?? true) && directoryId > 0

  return useQuery({
    queryKey: ["dir-scan", "directory", directoryId, "files", { limit, offset, status }],
    queryFn: () =>
      api.listDirScanFiles(directoryId, {
        ...(limit !== undefined ? { limit } : {}),
        ...(offset !== undefined ? { offset } : {}),
        ...(status !== undefined ? { status } : {}),
      }),
    enabled: shouldEnable,
    staleTime: 30_000,
  })
}

export function useDirScanRunInjections(
  directoryId: number,
  runId: number,
  options?: { limit?: number; offset?: number; enabled?: boolean; active?: boolean }
) {
  const { limit, offset, enabled, active } = options ?? {}
  const shouldEnable = (enabled ?? true) && directoryId > 0 && runId > 0

  return useQuery({
    queryKey: ["dir-scan", "directory", directoryId, "run", runId, "injections", { limit, offset }],
    queryFn: () =>
      api.listDirScanRunInjections(directoryId, runId, {
        ...(limit !== undefined ? { limit } : {}),
        ...(offset !== undefined ? { offset } : {}),
      }),
    enabled: shouldEnable,
    refetchInterval: shouldEnable && active ? 2_000 : false,
    placeholderData: (previousData: DirScanRunInjection[] | undefined) => previousData ?? [],
  })
}
