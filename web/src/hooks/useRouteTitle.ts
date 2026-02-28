/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRouterState } from "@tanstack/react-router"
import { useCallback, useMemo } from "react"
import { useTranslation } from "react-i18next"

const DEFAULT_TITLE = "qui"

function getStaticTitle(staticData: unknown, resolveKey: (key: string) => string): string | undefined {
  if (!staticData || typeof staticData !== "object") {
    return undefined
  }

  const titleKey = (staticData as { titleKey?: unknown }).titleKey
  if (typeof titleKey === "string") {
    const translated = resolveKey(titleKey).trim()
    if (translated.length > 0) {
      return translated
    }
  }

  const title = (staticData as { title?: unknown }).title
  if (typeof title !== "string") {
    return undefined
  }

  const trimmed = title.trim()
  return trimmed.length > 0 ? trimmed : undefined
}

/**
 * Resolves the most specific static title from the active route matches.
 * Falls back to the provided string when no route title is available.
 */
export function useRouteTitle(fallback: string = DEFAULT_TITLE) {
  const { t } = useTranslation("common")
  const matches = useRouterState({
    select: (state) => state.matches,
  })
  const resolveKey = useCallback((key: string) => String(t(key as never)), [t])

  return useMemo(() => {
    for (let index = matches.length - 1; index >= 0; index -= 1) {
      const match = matches[index]
      const staticTitle = getStaticTitle(match.staticData, resolveKey)
      if (staticTitle) {
        return staticTitle
      }
    }

    return fallback
  }, [fallback, matches, resolveKey])
}
