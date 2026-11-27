/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { queryOptions } from "@tanstack/react-query"
import { createContext, useContext } from "react"
import { toast } from "sonner"

import i18n from "@/i18n"
import { api } from "@/lib/api"
import { formatBytes } from "@/lib/utils"
import type { DatabaseShrinkStatus } from "@/types"

export const databaseShrinkQueryKey = ["database-shrink"]

type ShrinkResult = NonNullable<DatabaseShrinkStatus["result"]>

function sameResult(a: ShrinkResult | undefined, b: ShrinkResult): boolean {
  return a?.beforeBytes === b.beforeBytes && a.afterBytes === b.afterBytes
}

export const databaseShrinkQuery = queryOptions({
  queryKey: databaseShrinkQueryKey,
  queryFn: async ({ client }) => {
    const shown = client.getQueryData<DatabaseShrinkStatus>(databaseShrinkQueryKey)?.result
    const status = await api.getDatabaseShrink()
    const { result } = status
    if (result && !sameResult(shown, result)) {
      toast.success(i18n.t("common:databaseShrink.done", { before: formatBytes(result.beforeBytes), after: formatBytes(result.afterBytes) }), { duration: 15_000 })
      void api.dismissDatabaseShrinkResult()
        .catch(() => undefined)
        .then(() => client.invalidateQueries({ queryKey: databaseShrinkQueryKey }))
    }
    return status
  },
})

export interface DatabaseShrinkContextValue {
  requestShrink: () => Promise<void>
  pending: boolean
}

export const DatabaseShrinkContext = createContext<DatabaseShrinkContextValue | null>(null)

export function useDatabaseShrink(): DatabaseShrinkContextValue {
  const value = useContext(DatabaseShrinkContext)
  if (!value) {
    throw new Error("useDatabaseShrink needs a DatabaseShrinkProvider")
  }
  return value
}
