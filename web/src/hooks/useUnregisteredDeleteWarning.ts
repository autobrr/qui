/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"

import { api } from "@/lib/api"
import { buildTorrentActionTargets } from "@/lib/torrent-action-targets"
import type { Torrent, TorrentSelection, UnregisteredDeleteWarning } from "@/types"

interface UseUnregisteredDeleteWarningOptions {
  open: boolean
  instanceId: number
  torrents: Torrent[]
  selectAll?: Omit<TorrentSelection, "targets" | "selectAll">
}

export function useUnregisteredDeleteWarning({
  open,
  instanceId,
  torrents,
  selectAll,
}: UseUnregisteredDeleteWarningOptions): UnregisteredDeleteWarning | null {
  const selection = useMemo<TorrentSelection>(
    () => selectAll ? { selectAll: true, ...selectAll } : { targets: buildTorrentActionTargets(torrents, instanceId) },
    [instanceId, torrents, selectAll]
  )
  const enabled = open && (selection.selectAll === true || (selection.targets?.length ?? 0) > 0)
  const { data, isError } = useQuery({
    queryKey: ["unregistered-delete-warning", instanceId, selection],
    queryFn: ({ signal }) => api.getUnregisteredDeleteWarning(instanceId, selection, signal),
    enabled,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  })
  return enabled && !isError && data && data.count > 0 ? data : null
}
