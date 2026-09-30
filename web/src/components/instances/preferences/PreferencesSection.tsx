/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import type { AppPreferences } from "@/types"

interface PreferencesSectionProps {
  instanceId: number
  i18nPrefix: string
  children: (preferences: AppPreferences) => ReactNode
}

// Mounts the form only once the preferences exist, so usePreferencesForm seeds it from real values.
export function PreferencesSection({ instanceId, i18nPrefix, children }: PreferencesSectionProps) {
  const { t } = useTranslation("instances")
  const { preferences, isLoading } = useInstancePreferences(instanceId)

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">{t(`${i18nPrefix}.loading`)}</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="flex items-center justify-center py-8" role="alert">
        {/* Some sections have no loadFailed string and keep their loading text. */}
        <p className="text-sm text-muted-foreground">{t([`${i18nPrefix}.loadFailed`, `${i18nPrefix}.loading`])}</p>
      </div>
    )
  }

  return children(preferences)
}
