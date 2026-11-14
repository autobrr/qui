/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { RestartOverlay } from "@/components/settings/RestartOverlay"
import { DatabaseShrinkContext, databaseShrinkQueryKey, type DatabaseShrinkContextValue } from "@/hooks/useDatabaseShrink"
import { useSelfUpdate } from "@/hooks/useSelfUpdate"
import { systemNoticesQueryKey } from "@/hooks/useSystemNotices"
import { api } from "@/lib/api"
import { formatBytes } from "@/lib/utils"

// The provider sits in the app layout, so the restart overlay stays mounted
// when the request comes from a menu that closes.
export function DatabaseShrinkProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation("common")
  const queryClient = useQueryClient()
  const { shrink, pending, overlay } = useSelfUpdate()

  const statusQuery = useQuery({ queryKey: databaseShrinkQueryKey, queryFn: () => api.getDatabaseShrink() })
  const result = statusQuery.data?.result
  const dismissResult = useMutation({
    mutationFn: () => api.dismissDatabaseShrinkResult(),
    onSettled: () => queryClient.invalidateQueries({ queryKey: databaseShrinkQueryKey }),
  })
  const { mutate: dismiss, isIdle } = dismissResult
  useEffect(() => {
    // isIdle keeps a second render from showing the result two times.
    if (!result || !isIdle) {
      return
    }
    toast.success(t("databaseShrink.done", { before: formatBytes(result.beforeBytes), after: formatBytes(result.afterBytes) }), { duration: 15_000 })
    dismiss()
  }, [result, isIdle, dismiss, t])

  const value: DatabaseShrinkContextValue = {
    pending,
    requestShrink: async () => {
      try {
        const restarting = await shrink()
        if (!restarting) {
          toast.success(t("databaseShrink.nextStart"))
        }
        // "Try again" stored a new mark, so the old failure is gone.
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
