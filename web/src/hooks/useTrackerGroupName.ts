/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import { useCallback } from "react"
import { useTranslation } from "react-i18next"

// Names that the backend gives to the Tracker groups without a tracker domain.
const PUBLIC_GROUP = "Public"
const NO_TRACKER_GROUP = "No tracker"

/** Returns the name of a Tracker group as the Statistics charts show it. Incognito mode masks the tracker names. */
export function useTrackerGroupName(): (group: string) => string {
  const { t } = useTranslation("statistics")
  const [incognitoMode] = useIncognitoMode()
  return useCallback((group: string) => {
    if (group === PUBLIC_GROUP) return t("groups.public")
    if (group === NO_TRACKER_GROUP) return t("groups.noTracker")
    return incognitoMode ? getLinuxTrackerDomain(group) : group
  }, [incognitoMode, t])
}
