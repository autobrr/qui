/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { getStoredVariation } from "@/hooks/usePersistedThemeVariation"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { setThemeLocalOnly, storedThemeSelection, useThemeLocalOnly } from "@/hooks/useThemeSettingsSync"
import { api } from "@/lib/api"
import { getCurrentTheme, getCurrentThemeMode } from "@/utils/theme"

/** The layout switches under the theme picker, with a hint that names what the picker changes. */
export function ThemeLayoutSettings() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const isMobile = useIsMobile()
  const localOnly = useThemeLocalOnly()
  const { data } = useQuery({
    queryKey: ["theme-settings"],
    queryFn: () => api.getThemeSettings(),
  })
  const split = Boolean(data?.mobile)

  const setSplit = async (on: boolean) => {
    try {
      if (on) {
        // The mobile layout starts from the current theme, so nothing changes
        // until a new mobile theme is picked. A local-only browser copies the
        // server theme, so its local theme never reaches the server.
        const appliedId = getCurrentTheme().id
        const current = storedThemeSelection(getCurrentThemeMode(), appliedId, getStoredVariation(appliedId) ?? undefined)
        // Turning the split off later pulls the default slot, so it must exist.
        if (!data?.default) await api.updateThemeSettings(current, "default")
        await api.updateThemeSettings(localOnly && data?.default ? data.default : current, "mobile")
      } else {
        await api.deleteMobileThemeSettings()
      }
    } catch {
      toast.error(t("themes.layout.saveError"))
    }
    await queryClient.invalidateQueries({ queryKey: ["theme-settings"] })
  }

  let hint: string | null = null
  if (localOnly) hint = t("themes.layout.changesLocal")
  else if (split) hint = isMobile ? t("themes.layout.changesMobile") : t("themes.layout.changesDesktop")

  return (
    <div className="space-y-3">
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      <div className="flex items-center gap-2">
        <Switch id="theme-mobile-split" checked={split} disabled={!data} onCheckedChange={(on) => void setSplit(on)} />
        <Label htmlFor="theme-mobile-split" className="cursor-pointer">{t("themes.layout.mobileSplit")}</Label>
        <FieldHelp>{t("themes.layout.mobileSplitHelp")}</FieldHelp>
      </div>
      {split && <p className="text-xs text-muted-foreground">{t("themes.layout.mobileSplitOffWarning")}</p>}
      <div className="flex items-center gap-2">
        <Switch id="theme-local-only" checked={localOnly} onCheckedChange={setThemeLocalOnly} />
        <Label htmlFor="theme-local-only" className="cursor-pointer">{t("themes.layout.localOnly")}</Label>
        <FieldHelp>{t("themes.layout.localOnlyHelp")}</FieldHelp>
      </div>
    </div>
  )
}
