/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useIsAuthed } from "@/hooks/useIsAuthed"
import {
  applyServerSettings,
  getDisplayProfiles,
  markClientSettingsUnavailable,
  onActiveDisplayProfileDeleted,
  readRaw,
  seedAndMarkReady
} from "@/lib/client-settings"
import { displayProfileName } from "@/lib/display-profiles"
import { changeLanguage, supportedLanguages, type AppLanguage } from "@/i18n"

export function useClientSettingsSync(): void {
  const isAuthed = useIsAuthed()
  const { t } = useTranslation("torrents")

  useActivityStream(isAuthed)

  const { data, error } = useQuery({
    queryKey: ["client-settings"],
    queryFn: () => api.getClientSettings(),
    enabled: isAuthed,
  })

  useEffect(() => {
    if (error) markClientSettingsUnavailable()
  }, [error])

  useEffect(() => onActiveDisplayProfileDeleted((profile) => {
    const defaultName = displayProfileName(getDisplayProfiles().active, t("displayProfiles.defaultName"))
    toast.info(t("displayProfiles.deletedToast", { name: profile.name, defaultName }))
  }), [t])

  useEffect(() => {
    if (!data) return
    const changed = applyServerSettings(data)
    if (changed.includes("qui.language")) {
      const language = readRaw("qui.language")
      if (language && supportedLanguages.includes(language as AppLanguage)) {
        void changeLanguage(language as AppLanguage)
      }
    }
    seedAndMarkReady(data)
  }, [data])
}
