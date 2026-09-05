/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { DiscScanRun, DiscScanStatus, TorrentFile } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useMemo, useRef } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// useDiscScans holds the Disc scans of one torrent for the Content tab: the
// newest run per Disc path, and the start and cancel calls. The list refetches
// on the discscan.run activity event; a start or cancel response replaces the
// row for its Disc path at once so the dialog does not wait for that round trip.
//
// A start on a Disc another torrent already scanned returns that torrent's
// cached row. The list of this torrent never carries it, so it is not written
// into the list; the dialog reads it from start.data instead.
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

  // The completion toast fires when a refetch shows a run this hook already
  // knew move to completed or failed. A row that is already finished when the
  // list first loads stays silent, and so does a cancel.
  const { t } = useTranslation("torrents")
  const knownStatus = useRef(new Map<number, DiscScanStatus>())
  useEffect(() => {
    for (const run of runs ?? []) {
      const previous = knownStatus.current.get(run.id)
      knownStatus.current.set(run.id, run.status)
      if (previous === undefined || previous === run.status) continue
      if (run.status === "completed") toast.success(t("discScan.toast.completed"))
      if (run.status === "failed") toast.error(t("discScan.toast.failed", { error: run.errorMessage ?? "" }))
    }
  }, [runs, t])

  const replaceRun = (run: DiscScanRun) => {
    queryClient.setQueryData<DiscScanRun[]>(queryKey, old => [...(old ?? []).filter(r => r.discPath !== run.discPath), run])
  }
  const start = useMutation({
    mutationFn: ({ discPath, force }: { discPath: string; force: boolean }) => api.startDiscScan(instanceId, torrentHash, discPath, force),
    onSuccess: (run) => {
      if (run.torrentHash === torrentHash) replaceRun(run)
    },
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
