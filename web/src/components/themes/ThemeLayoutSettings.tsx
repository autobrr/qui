/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { getStoredVariation } from "@/hooks/usePersistedThemeVariation"
import {
  removeMobileThemeSlot,
  setThemeLocalOnly,
  storedThemeSelection,
  useCanWriteThemeSlots,
  useThemeLocalOnly,
  writeThemeSlot
} from "@/hooks/useThemeSettingsSync"
import { api } from "@/lib/api"
import { getCurrentTheme, getCurrentThemeMode } from "@/utils/theme"

/** The switches that choose where a theme applies: one theme for each layout, and a theme of this browser only. */
export function ThemeLayoutSettings() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const localOnly = useThemeLocalOnly()
  const canWriteSlots = useCanWriteThemeSlots()
  const { data } = useQuery({
    queryKey: ["theme-settings"],
    queryFn: () => api.getThemeSettings(),
  })
  const split = Boolean(data?.mobile)

  const setSplit = async (on: boolean) => {
    try {
      if (on) {
        // The mobile layout starts from the current theme, so nothing changes
        // until a new mobile theme is picked.
        const appliedId = getCurrentTheme().id
        const current = storedThemeSelection(getCurrentThemeMode(), appliedId, getStoredVariation(appliedId) ?? undefined)
        // Turning the split off later pulls the default slot, so it must exist.
        if (!data?.default) await writeThemeSlot(current, "default")
        await writeThemeSlot(current, "mobile")
      } else {
        await removeMobileThemeSlot()
      }
    } catch {
      toast.error(t("themes.layout.saveError"))
    }
    await queryClient.invalidateQueries({ queryKey: ["theme-settings"] })
  }

  const splitNotes = [localOnly && t("themes.layout.splitNeedsSync"), split && t("themes.layout.mobileSplitOffWarning")].filter(Boolean).join(" ")

  return (
    <div className="space-y-4">
      <SwitchRow
        label={t("themes.layout.mobileSplit")}
        help={t("themes.layout.mobileSplitHelp")}
        description={splitNotes}
        checked={split}
        disabled={!data || !canWriteSlots}
        onCheckedChange={(on) => void setSplit(on)}
      />
      <SwitchRow
        label={t("themes.layout.localOnly")}
        help={t("themes.layout.localOnlyHelp")}
        checked={localOnly}
        onCheckedChange={setThemeLocalOnly}
      />
    </div>
  )
}
