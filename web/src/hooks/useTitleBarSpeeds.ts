/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isStreamUsable, streamFallbackPollInterval } from "@/lib/sync-stream-state"
import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { api } from "@/lib/api"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import type { TorrentStreamPayload } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

interface UseTitleBarSpeedsOptions {
  enabled?: boolean
  instanceId?: number
  foregroundSpeeds?: { dl: number; up: number }
  backgroundSpeeds?: { dl: number; up: number }
}

function useServerStateSpeeds(instanceId: number | undefined, enabled: boolean, refetchInterval: number | false) {
  const isEnabled = typeof instanceId === "number" && enabled

  const { data, dataUpdatedAt } = useQuery({
    queryKey: ["transfer-info", instanceId],
    queryFn: () => api.getTransferInfo(instanceId as number),
    enabled: isEnabled,
    refetchInterval,
    refetchIntervalInBackground: true,
    staleTime: 0,
  })

  return {
    speeds: data ? { dl: data.dl_info_speed ?? 0, up: data.up_info_speed ?? 0 } : undefined,
    updatedAt: dataUpdatedAt,
  }
}

export function useTitleBarSpeeds({
  enabled = true,
  instanceId,
  foregroundSpeeds,
  backgroundSpeeds: backgroundSpeedsOverride,
}: UseTitleBarSpeedsOptions) {
  const { t } = useTranslation("common")
  const [speedUnit] = useSpeedUnits()
  const lastSpeedsTextRef = useRef<string | undefined>(undefined)
  const lastBackgroundSpeedsRef = useRef<{ dl: number; up: number } | null>(null)
  const [streamSpeeds, setStreamSpeeds] = useState<{ dl: number; up: number } | undefined>(undefined)
  const lastHiddenAtRef = useRef(0)
  const lastForegroundUpdateAtRef = useRef(0)
  const fallbackBaselineRef = useRef<{ instanceId?: number; updatedAt: number } | null>(null)
  const wasHiddenRef = useRef(false)
  const { isHidden, isHiddenDelayed, isVisible } = useDelayedVisibility(3000)

  const streamParams = useMemo(() => {
    if (!enabled || typeof instanceId !== "number") {
      return null
    }

    return {
      instanceId,
      page: 0,
      limit: 1,
      sort: "added_on",
      order: "desc" as const,
      summary: true,
    }
  }, [enabled, instanceId])

  const handleStreamMessage = useCallback((payload: TorrentStreamPayload) => {
    const serverState = payload.data?.serverState
    if (!serverState) {
      return
    }

    setStreamSpeeds({
      dl: serverState.dl_info_speed ?? 0,
      up: serverState.up_info_speed ?? 0,
    })
  }, [])

  const streamState = useSyncStream(streamParams, {
    enabled: Boolean(streamParams),
    onMessage: handleStreamMessage,
  })

  useEffect(() => {
    setStreamSpeeds(undefined)
  }, [instanceId])

  const isForegroundStale = Boolean(foregroundSpeeds) && !isHidden && lastHiddenAtRef.current > lastForegroundUpdateAtRef.current
  const stalled = streamState.dataStalled
  const streamUnusable = Boolean(streamParams) && !isStreamUsable(streamState)
  const needsFreshBackground = stalled || (isVisible && streamUnusable)
  const shouldPollBackground = enabled && (
    isHiddenDelayed ||
    !foregroundSpeeds ||
    isForegroundStale ||
    streamUnusable
  )
  const streamDataUsable =
    isStreamUsable(streamState) &&
    Boolean(streamSpeeds)
  const shouldUseFallbackPolling = shouldPollBackground &&
    !backgroundSpeedsOverride &&
    !streamDataUsable
  const backgroundQuery = useServerStateSpeeds(
    instanceId,
    shouldUseFallbackPolling,
    streamFallbackPollInterval(streamState, 3000)
  )
  if (!needsFreshBackground) {
    fallbackBaselineRef.current = null
  } else if (!fallbackBaselineRef.current || fallbackBaselineRef.current.instanceId !== instanceId) {
    fallbackBaselineRef.current = { instanceId, updatedAt: backgroundQuery.updatedAt }
  }
  const backgroundSpeedsQuery = backgroundQuery.speeds
  const backgroundSpeeds = backgroundSpeedsOverride ??
    (
      needsFreshBackground
        ? backgroundSpeedsQuery
        : shouldUseFallbackPolling
          ? (backgroundSpeedsQuery ?? streamSpeeds)
          : (streamSpeeds ?? backgroundSpeedsQuery)
    )
  const cachedBackgroundSpeeds = lastBackgroundSpeedsRef.current
  const refreshedBackgroundSpeeds = backgroundSpeedsOverride ?? (
    backgroundQuery.updatedAt > (fallbackBaselineRef.current?.updatedAt ?? 0)
      ? backgroundSpeedsQuery
      : undefined
  )
  const effectiveSpeeds = needsFreshBackground
    ? (refreshedBackgroundSpeeds ?? foregroundSpeeds)
    : isHiddenDelayed
      ? (backgroundSpeeds ?? cachedBackgroundSpeeds)
      : isForegroundStale
        ? (cachedBackgroundSpeeds ?? backgroundSpeeds)
        : (foregroundSpeeds ?? backgroundSpeeds ?? cachedBackgroundSpeeds)
  const shouldSetTitle = enabled && (isHiddenDelayed || isVisible)

  useEffect(() => {
    if (isHidden && !wasHiddenRef.current) {
      lastHiddenAtRef.current = Date.now()
    }
    wasHiddenRef.current = isHidden
  }, [isHidden])

  useEffect(() => {
    if (!isHidden && foregroundSpeeds) {
      lastForegroundUpdateAtRef.current = Date.now()
    }
  }, [foregroundSpeeds, foregroundSpeeds?.dl, foregroundSpeeds?.up, isHidden])

  useEffect(() => {
    if (backgroundSpeeds) {
      lastBackgroundSpeedsRef.current = backgroundSpeeds
    }
  }, [backgroundSpeeds])

  const currentSpeedsText = effectiveSpeeds
    ? t("titleBar.speeds", {
      download: formatSpeedWithUnit(effectiveSpeeds.dl ?? 0, speedUnit),
      upload: formatSpeedWithUnit(effectiveSpeeds.up ?? 0, speedUnit),
    })
    : undefined
  const keepsLastSpeeds = !shouldSetTitle || (!effectiveSpeeds && !needsFreshBackground)
  const speedsText = keepsLastSpeeds ? lastSpeedsTextRef.current : currentSpeedsText

  useEffect(() => {
    lastSpeedsTextRef.current = speedsText
  }, [speedsText])

  return enabled ? speedsText : undefined
}
