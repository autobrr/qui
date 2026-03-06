/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"
import { useTranslation } from "react-i18next"

export function useCommonTr() {
  const { t } = useTranslation("common")
  return useCallback((key: string, options?: Record<string, unknown>) => String(t(key as never, options as never)), [t])
}
