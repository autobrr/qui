/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useIsAuthed } from "@/hooks/useIsAuthed"
import { applyServerSettings, readRaw, seedAndMarkReady } from "@/lib/client-settings"
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

  useActivityStream(isAuthed)

  const { data } = useQuery({
    queryKey: ["client-settings"],
    queryFn: () => api.getClientSettings(),
    enabled: isAuthed,
  })

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
