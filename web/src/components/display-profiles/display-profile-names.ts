/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useTranslation } from "react-i18next"

import { useDisplayProfiles, type DisplayProfilesApi } from "@/lib/client-settings"
import {
  DISPLAY_PROFILE_NAME_MAX_LENGTH,
  displayProfileName,
  validateDisplayProfileName,
  type DisplayProfile,
  type DisplayProfileNameError
} from "@/lib/display-profiles"

/** useDisplayProfiles with the names that the UI shows: Default shows "Default" in the language of the app until the user renames it. */
export function useNamedDisplayProfiles(): DisplayProfilesApi {
  const { t } = useTranslation("torrents")
  const api = useDisplayProfiles()
  const defaultName = t("displayProfiles.defaultName")
  return useMemo(() => {
    const named = (profile: DisplayProfile) => ({ ...profile, name: displayProfileName(profile, defaultName) })
    return { ...api, profiles: api.profiles.map(named), active: named(api.active) }
  }, [api, defaultName])
}

export interface DisplayProfileNameCheck {
  /** The error of the name rule, also while the field is still blank. */
  error: DisplayProfileNameError | null
  /** The error to show: none while the field is blank, until the user submits. */
  shownError: DisplayProfileNameError | null
  errorTexts: Record<DisplayProfileNameError, string>
  /** The count of characters out of the limit, for example "6/32". */
  count: string
}

/** The name rule of a field that takes a profile name. Pass the id of the profile that the field renames as ignoreId. */
export function useDisplayProfileNameCheck(name: string, submitted: boolean, ignoreId?: string): DisplayProfileNameCheck {
  const { t } = useTranslation("torrents")
  const { profiles } = useNamedDisplayProfiles()
  const error = validateDisplayProfileName(name, profiles, ignoreId)
  return {
    error,
    shownError: name.trim() !== "" || submitted ? error : null,
    errorTexts: {
      empty: t("displayProfiles.nameErrors.empty"),
      tooLong: t("displayProfiles.nameErrors.tooLong", { max: DISPLAY_PROFILE_NAME_MAX_LENGTH }),
      duplicate: t("displayProfiles.nameErrors.duplicate"),
    },
    count: t("displayProfiles.newDialog.nameCount", { length: [...name.trim()].length, max: DISPLAY_PROFILE_NAME_MAX_LENGTH }),
  }
}
