/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { shallowEqualTrackerIcons } from "@/lib/torrent-table/tracker-icon-equality"
import { buildTrackerCustomizationLookup, getTrackerCustomizationsCacheKey, type TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { useMemo, useRef } from "react"

export interface TrackerIconCache {
  trackerIcons: Record<string, string> | undefined
  trackerCustomizationLookup: TrackerCustomizationLookup
}

export function useTrackerIconCache(): TrackerIconCache {
  const trackerIconsQuery = useTrackerIcons()
  const trackerIconsRef = useRef<Record<string, string> | undefined>(undefined)
  const trackerIcons = useMemo(() => {
    const latest = trackerIconsQuery.data
    if (!latest) {
      return trackerIconsRef.current
    }

    const previous = trackerIconsRef.current
    if (previous && shallowEqualTrackerIcons(previous, latest)) {
      return previous
    }

    trackerIconsRef.current = latest
    return latest
  }, [trackerIconsQuery.data])

  const trackerCustomizationsQuery = useTrackerCustomizations()
  const trackerCustomizationsRef = useRef<{ key: string; lookup: TrackerCustomizationLookup } | undefined>(undefined)
  const trackerCustomizationLookup = useMemo(() => {
    const latest = trackerCustomizationsQuery.data
    if (!latest) {
      return trackerCustomizationsRef.current?.lookup ?? new Map()
    }

    const newKey = getTrackerCustomizationsCacheKey(latest)

    const previous = trackerCustomizationsRef.current
    if (previous && previous.key === newKey) {
      return previous.lookup
    }

    const newLookup = buildTrackerCustomizationLookup(latest)
    trackerCustomizationsRef.current = { key: newKey, lookup: newLookup }
    return newLookup
  }, [trackerCustomizationsQuery.data])

  return { trackerIcons, trackerCustomizationLookup }
}
