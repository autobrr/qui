/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isStreamUsable } from "@/lib/sync-stream-state"
import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useRouteTitle } from "@/hooks/useRouteTitle"
import { api } from "@/lib/api"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { isSpreadsheetDisguiseActive, spreadsheetDocumentTitle, useSpreadsheetDisguise } from "@/lib/spreadsheet-disguise"
import type { TorrentStreamPayload } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

const DEFAULT_DOCUMENT_TITLE = "qui"

interface UseTitleBarSpeedsOptions {
  mode: "dashboard" | "instance"
  enabled?: boolean
  instanceId?: number
  instanceName?: string
  foregroundSpeeds?: { dl: number; up: number }
  backgroundSpeeds?: { dl: number; up: number }
}

function useServerStateSpeeds(instanceId?: number, enabled = true) {
  const isEnabled = typeof instanceId === "number" && enabled

  const { data, dataUpdatedAt } = useQuery({
    queryKey: ["transfer-info", instanceId],
    queryFn: () => api.getTransferInfo(instanceId as number),
    enabled: isEnabled,
    refetchInterval: 3000,
    refetchIntervalInBackground: true,
    staleTime: 0,
  })

  return {
    speeds: data ? { dl: data.dl_info_speed ?? 0, up: data.up_info_speed ?? 0 } : undefined,
    updatedAt: dataUpdatedAt,
  }
}

/**
 * Updates the document title with live transfer speeds based on visibility.
 * Falls back to the current route title when disabled or data is unavailable.
 */
export function useTitleBarSpeeds({
  mode,
  enabled = true,
  instanceId,
  instanceName,
  foregroundSpeeds,
  backgroundSpeeds: backgroundSpeedsOverride,
}: UseTitleBarSpeedsOptions) {
  const [speedUnit] = useSpeedUnits()
  const disguised = useSpreadsheetDisguise()
  const baseTitle = useRouteTitle()
  const lastSpeedTitleRef = useRef<string | null>(null)
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
    shouldUseFallbackPolling
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
    // Mark foreground as fresh when speeds update or when visibility returns.
    if (!isHidden && foregroundSpeeds) {
      lastForegroundUpdateAtRef.current = Date.now()
    }
  }, [foregroundSpeeds, foregroundSpeeds?.dl, foregroundSpeeds?.up, isHidden])

  useEffect(() => {
    if (backgroundSpeeds) {
      lastBackgroundSpeedsRef.current = backgroundSpeeds
    }
  }, [backgroundSpeeds])

  useEffect(() => {
    return () => {
      // Avoid leaving a stale route-specific title after this hook unmounts.
      // Read the disguise live so a theme switch before unmount is respected.
      document.title = isSpreadsheetDisguiseActive() ? spreadsheetDocumentTitle() : DEFAULT_DOCUMENT_TITLE
    }
  }, [])

  useEffect(() => {
    if (disguised) {
      document.title = spreadsheetDocumentTitle()
      lastSpeedTitleRef.current = null
      return
    }

    if (!enabled) {
      document.title = baseTitle
      return
    }

    if (!shouldSetTitle) {
      if (lastSpeedTitleRef.current) {
        document.title = lastSpeedTitleRef.current
      }
      return
    }

    if (!effectiveSpeeds) {
      if (needsFreshBackground) {
        document.title = baseTitle
        lastSpeedTitleRef.current = null
      } else {
        document.title = lastSpeedTitleRef.current ?? baseTitle
      }
      return
    }

    const downloadSpeed = effectiveSpeeds.dl ?? 0
    const uploadSpeed = effectiveSpeeds.up ?? 0
    const speedTitle = `D: ${formatSpeedWithUnit(downloadSpeed, speedUnit)} U: ${formatSpeedWithUnit(uploadSpeed, speedUnit)}`

    if (mode === "dashboard") {
      const nextTitle = `${speedTitle} | Dashboard`
      document.title = nextTitle
      lastSpeedTitleRef.current = nextTitle
    } else {
      const instanceSuffix = ` | ${instanceName || baseTitle}`
      const nextTitle = `${speedTitle}${instanceSuffix}`
      document.title = nextTitle
      lastSpeedTitleRef.current = nextTitle
    }
  }, [baseTitle, disguised, effectiveSpeeds, enabled, instanceName, mode, needsFreshBackground, shouldSetTitle, speedUnit])
}
