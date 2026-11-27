/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import { pickTrackerIconDomain } from "@/lib/tracker-icons"
import { useCallback, useMemo } from "react"
import { useTranslation } from "react-i18next"

const PUBLIC_GROUP = "Public"
const NO_TRACKER_GROUP = "No tracker"

export function useTrackerGroupName(): (group: string) => string {
  const { t } = useTranslation("statistics")
  const [incognitoMode] = useIncognitoMode()
  return useCallback((group: string) => {
    if (group === PUBLIC_GROUP) return t("groups.public")
    if (group === NO_TRACKER_GROUP) return t("groups.noTracker")
    return incognitoMode ? getLinuxTrackerDomain(group) : group
  }, [incognitoMode, t])
}

interface TrackerGroupIcon {
  tracker: string
  trackerIcons?: Record<string, string>
}

export function useTrackerGroupIcon(): (group: string) => TrackerGroupIcon | undefined {
  const [incognitoMode] = useIncognitoMode()
  const { data: trackerIcons } = useTrackerIcons()
  const { data: customizations } = useTrackerCustomizations()
  const domainsByName = useMemo(() => {
    const byName = new Map<string, string[]>()
    for (const custom of customizations ?? []) {
      const name = custom.displayName.trim()
      byName.set(name, [...byName.get(name) ?? [], ...custom.domains])
    }
    return byName
  }, [customizations])
  return useCallback((group: string) => {
    if (group === PUBLIC_GROUP || group === NO_TRACKER_GROUP) return undefined
    if (incognitoMode) return { tracker: getLinuxTrackerDomain(group), trackerIcons }
    const domains = domainsByName.get(group)
    return { tracker: domains ? pickTrackerIconDomain(trackerIcons, domains) : group, trackerIcons }
  }, [incognitoMode, trackerIcons, domainsByName])
}
