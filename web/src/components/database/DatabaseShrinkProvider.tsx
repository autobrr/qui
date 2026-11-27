/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { RestartOverlay } from "@/components/settings/RestartOverlay"
import { DatabaseShrinkContext, databaseShrinkQuery, databaseShrinkQueryKey, type DatabaseShrinkContextValue } from "@/hooks/useDatabaseShrink"
import { useSelfUpdate } from "@/hooks/useSelfUpdate"
import { systemNoticesQueryKey } from "@/hooks/useSystemNotices"

export function DatabaseShrinkProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation("common")
  const queryClient = useQueryClient()
  const { shrink, pending, overlay } = useSelfUpdate()

  useQuery(databaseShrinkQuery)

  const value: DatabaseShrinkContextValue = {
    pending,
    requestShrink: async () => {
      try {
        const restarting = await shrink()
        if (!restarting) {
          toast.success(t("databaseShrink.nextStart"))
        }
        await queryClient.invalidateQueries({ queryKey: systemNoticesQueryKey })
        await queryClient.invalidateQueries({ queryKey: databaseShrinkQueryKey })
      } catch (error) {
        toast.error(error instanceof Error ? error.message : t("databaseShrink.failed"))
      }
    },
  }

  return (
    <DatabaseShrinkContext.Provider value={value}>
      {children}
      <RestartOverlay state={overlay} />
    </DatabaseShrinkContext.Provider>
  )
}
