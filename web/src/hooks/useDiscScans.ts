/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { DiscScanRun, TorrentFile } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useMemo } from "react"

export function useDiscScans(instanceId: number, torrentHash: string, files: TorrentFile[] | undefined, enabled: boolean) {
  const queryClient = useQueryClient()
  const queryKey = ["disc-scans", instanceId, torrentHash]

  const { data: runs } = useQuery({
    queryKey,
    queryFn: () => api.listDiscScans(instanceId, torrentHash),
    enabled: enabled && !!files?.length && !!torrentHash,
  })
  const runsByPath = useMemo(() => new Map(runs?.map(run => [run.discPath, run])), [runs])

  const replaceRun = (run: DiscScanRun) => {
    if (run.torrentHash !== torrentHash) return
    queryClient.setQueryData<DiscScanRun[]>(queryKey, old => [...(old ?? []).filter(r => r.discPath !== run.discPath), run])
  }
  const start = useMutation({
    mutationFn: ({ discPath, force }: { discPath: string; force: boolean }) => api.startDiscScan(instanceId, torrentHash, discPath, force),
    onSuccess: replaceRun,
  })
  const cancel = useMutation({
    mutationFn: (runId: number) => api.cancelDiscScan(instanceId, runId),
    onSuccess: replaceRun,
  })

  const runFor = (discPath: string): DiscScanRun | undefined => {
    const started = start.variables?.discPath === discPath ? start.data : undefined
    return runsByPath.get(discPath) ?? (cancel.data && cancel.data.id === started?.id ? cancel.data : started)
  }

  return { runsByPath, runFor, start, cancel }
}

export type DiscScans = ReturnType<typeof useDiscScans>
