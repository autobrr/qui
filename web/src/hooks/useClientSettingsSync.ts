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

/**
 * Syncs DB-backed client settings with the server. On load the server
 * snapshot is applied to localStorage (the instant-boot cache), keys the
 * server does not know yet are seeded up from localStorage, and the debounced
 * push queue opens. A "client.settings" activity event invalidates the query
 * so other open tabs and browsers pull changes.
 */
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

  // Subscribed before the snapshot effect below runs, so the first snapshot can show the toast too.
  useEffect(() => onActiveDisplayProfileDeleted((profile) => {
    const defaultName = displayProfileName(getDisplayProfiles().active, t("displayProfiles.defaultName"))
    toast.info(t("displayProfiles.deletedToast", { name: profile.name, defaultName }))
  }), [t])

  useEffect(() => {
    if (!data) return
    const changed = applyServerSettings(data)
    // i18n boots from localStorage before React mounts; a language applied
    // from the server needs the imperative switch too.
    if (changed.includes("qui.language")) {
      const language = readRaw("qui.language")
      if (language && supportedLanguages.includes(language as AppLanguage)) {
        void changeLanguage(language as AppLanguage)
      }
    }
    seedAndMarkReady(data)
  }, [data])
}
