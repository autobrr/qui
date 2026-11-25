/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstances } from "@/hooks/useInstances"
import { useCallback } from "react"
import { useTranslation } from "react-i18next"

export function useInstanceName(): (id: number) => string {
  const { t } = useTranslation("transfers")
  const { instances } = useInstances()
  return useCallback(
    (id: number) => instances?.find(instance => instance.id === id)?.name ?? t("row.unknownInstance", { id }),
    [instances, t]
  )
}
