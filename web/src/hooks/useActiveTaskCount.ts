/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStream } from "@/contexts/SyncStreamContext"
import { isStreamUsable } from "@/lib/sync-stream-state"
import { api } from "@/lib/api"
import type { TorrentStreamPayload } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useState } from "react"

export function useActiveTaskCount(instanceId: number | null, enabled: boolean) {
  const activeInstanceId = enabled && instanceId !== null && instanceId > 0 ? instanceId : null
  const [streamCount, setStreamCount] = useState<{ instanceId: number; value: number } | null>(null)
  const streamParams = useMemo(() => activeInstanceId === null ? null : ({
    instanceId: activeInstanceId,
    page: 0,
    limit: 1,
    sort: "added_on",
    order: "desc" as const,
  }), [activeInstanceId])

  const onMessage = useCallback((payload: TorrentStreamPayload) => {
    const value = payload.data?.activeTaskCount
    if (activeInstanceId !== null && typeof value === "number") {
      setStreamCount({ instanceId: activeInstanceId, value })
    }
  }, [activeInstanceId])

  const streamState = useSyncStream(streamParams, {
    enabled: activeInstanceId !== null,
    onMessage,
  })
  const streamUsable = isStreamUsable(streamState)
  const streamedValue = streamCount?.instanceId === activeInstanceId ? streamCount.value : null
  const useFallback = activeInstanceId !== null && (!streamUsable || streamedValue === null)

  useEffect(() => {
    if (!streamUsable) {
      setStreamCount(null)
    }
  }, [streamUsable])

  const { data: polledCount = 0 } = useQuery({
    queryKey: ["active-task-count", activeInstanceId],
    queryFn: () => api.getActiveTaskCount(activeInstanceId as number),
    enabled: useFallback,
    refetchInterval: useFallback ? 30000 : false,
    refetchIntervalInBackground: true,
  })

  return streamUsable && streamedValue !== null ? streamedValue : polledCount
}
