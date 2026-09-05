/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { DiscScanRun, TorrentFile } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useMemo } from "react"

// useDiscScans holds the Disc scans of one torrent for the Content tab: the
// newest run per Disc path, and the start and cancel calls. The list refetches
// on the discscan.run activity event; a start or cancel response replaces the
// row for its Disc path at once so the dialog does not wait for that round trip.
//
// A start on a Disc another torrent already scanned returns that torrent's
// cached row. The list of this torrent never carries it, so it is not written
// into the list; the dialog reads it from start.data instead. The completion
// toast lives in DiscScanToasts, which stays mounted when this hook is not.
export function useDiscScans(instanceId: number, torrentHash: string, files: TorrentFile[] | undefined, enabled: boolean) {
  const queryClient = useQueryClient()
  const queryKey = ["disc-scans", instanceId, torrentHash]

  // files only load on the Content tab, so this query is scoped to it too.
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

  // runFor picks the row the dialog shows: the list row of this torrent, else
  // the cached row of another torrent that the start of this Disc returned.
  const runFor = (discPath: string): DiscScanRun | undefined =>
    runsByPath.get(discPath) ?? (start.variables?.discPath === discPath ? start.data : undefined)

  return { runsByPath, runFor, start, cancel }
}

export type DiscScans = ReturnType<typeof useDiscScans>
