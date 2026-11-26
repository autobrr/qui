/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"

import { useIsMobile } from "@/hooks/useMediaQuery"
import { useThemeLocalOnly } from "@/hooks/useThemeSettingsSync"
import { api } from "@/lib/api"

export function useThemeTargetHint(): string | null {
  const { t } = useTranslation("settings")
  const isMobile = useIsMobile()
  const localOnly = useThemeLocalOnly()
  const { data } = useQuery({
    queryKey: ["theme-settings"],
    queryFn: () => api.getThemeSettings(),
  })
  if (localOnly) return t("themes.layout.changesLocal")
  if (data?.mobile) return isMobile ? t("themes.layout.changesMobile") : t("themes.layout.changesDesktop")
  return null
}
