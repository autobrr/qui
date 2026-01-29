/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { api } from "@/lib/api"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { useQuery } from "@tanstack/react-query"
import { useEffect, useRef } from "react"

interface UseTitleBarSpeedsOptions {
  mode: "dashboard" | "instance"
  instanceId?: number
  instanceName?: string
  foregroundSpeeds?: { dl: number; up: number }
  backgroundSpeeds?: { dl: number; up: number }
}

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

export function useTitleBarSpeeds({
  mode,
  instanceId,
  instanceName,
  foregroundSpeeds,
  backgroundSpeeds: backgroundSpeedsOverride,
}: UseTitleBarSpeedsOptions) {
  const [speedUnit] = useSpeedUnits()
  const defaultTitleRef = useRef<string | null>(null)
  const lastSpeedTitleRef = useRef<string | null>(null)
  const { isHiddenDelayed, isVisibleDelayed } = useDelayedVisibility(3000)

  const shouldPollBackground = isHiddenDelayed || !foregroundSpeeds
  const backgroundSpeedsQuery = useServerStateSpeeds(
    instanceId,
    shouldPollBackground && !backgroundSpeedsOverride
  )
  const backgroundSpeeds = backgroundSpeedsOverride ?? backgroundSpeedsQuery
  const effectiveSpeeds = isHiddenDelayed ? backgroundSpeeds : foregroundSpeeds
  const shouldSetTitle = isHiddenDelayed || isVisibleDelayed

  useEffect(() => {
    if (typeof document === "undefined") {
      return
    }

    if (defaultTitleRef.current === null) {
      defaultTitleRef.current = document.title
    }

    if (!shouldSetTitle) {
      if (lastSpeedTitleRef.current) {
        document.title = lastSpeedTitleRef.current
      }
      return
    }

    if (!effectiveSpeeds) {
      document.title = lastSpeedTitleRef.current ?? defaultTitleRef.current ?? ""
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

    return () => {
      document.title = defaultTitleRef.current ?? ""
    }
  }, [effectiveSpeeds, instanceName, mode, shouldSetTitle, speedUnit])
}
