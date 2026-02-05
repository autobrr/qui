/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useRouteTitle } from "@/hooks/useRouteTitle"
import { api } from "@/lib/api"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { useQuery } from "@tanstack/react-query"
import { useEffect, useRef } from "react"

const DEFAULT_DOCUMENT_TITLE = "qui"

interface UseTitleBarSpeedsOptions {
  mode: "dashboard" | "instance"
  enabled?: boolean
  instanceId?: number
  instanceName?: string
  foregroundSpeeds?: { dl: number; up: number }
  backgroundSpeeds?: { dl: number; up: number }
}

/**
 * Fetches transfer speeds for an instance with a short polling interval.
 * Returns undefined when disabled or until server data arrives.
 */
export function useServerStateSpeeds(instanceId?: number, enabled = true) {
  const isEnabled = typeof instanceId === "number" && enabled

  const { data } = useQuery({
    queryKey: ["transfer-info", instanceId],
    queryFn: () => api.getTransferInfo(instanceId as number),
    enabled: isEnabled,
    refetchInterval: 3000,
    refetchIntervalInBackground: true,
    staleTime: 0,
  })

  if (!data) {
    return undefined
  }

  return {
    dl: data.dl_info_speed ?? 0,
    up: data.up_info_speed ?? 0,
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
  const baseTitle = useRouteTitle()
  const lastSpeedTitleRef = useRef<string | null>(null)
  const lastBackgroundSpeedsRef = useRef<{ dl: number; up: number } | null>(null)
  const lastHiddenAtRef = useRef(0)
  const lastForegroundUpdateAtRef = useRef(0)
  const wasHiddenRef = useRef(false)
  const { isHidden, isHiddenDelayed, isVisible } = useDelayedVisibility(3000)

  const isForegroundStale = !isHidden && lastHiddenAtRef.current > lastForegroundUpdateAtRef.current
  const shouldPollBackground = enabled && (isHiddenDelayed || !foregroundSpeeds || isForegroundStale)
  const backgroundSpeedsQuery = useServerStateSpeeds(
    instanceId,
    shouldPollBackground && !backgroundSpeedsOverride
  )
  const backgroundSpeeds = backgroundSpeedsOverride ?? backgroundSpeedsQuery
  const cachedBackgroundSpeeds = lastBackgroundSpeedsRef.current
  const effectiveSpeeds = isHiddenDelayed
    ? (backgroundSpeeds ?? cachedBackgroundSpeeds)
    : (isForegroundStale
      ? (cachedBackgroundSpeeds ?? backgroundSpeeds)
      : (foregroundSpeeds ?? cachedBackgroundSpeeds ?? backgroundSpeeds))
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
      document.title = DEFAULT_DOCUMENT_TITLE
    }
  }, [])

  useEffect(() => {
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
      document.title = lastSpeedTitleRef.current ?? baseTitle
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
      const instanceSuffix = instanceName ? ` | ${instanceName}` : ""
      const nextTitle = `${speedTitle}${instanceSuffix}`
      document.title = nextTitle
      lastSpeedTitleRef.current = nextTitle
    }
  }, [baseTitle, effectiveSpeeds, enabled, instanceName, mode, shouldSetTitle, speedUnit])
}
