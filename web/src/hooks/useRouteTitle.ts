/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRouterState } from "@tanstack/react-router"
import { useMemo } from "react"

const DEFAULT_TITLE = "qui"

function getStaticTitle(staticData: unknown): string | undefined {
  if (!staticData || typeof staticData !== "object") {
    return undefined
  }

  if (!("title" in staticData)) {
    return undefined
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
  const matches = useRouterState({
    select: (state) => state.matches,
  })

  return useMemo(() => {
    for (let index = matches.length - 1; index >= 0; index -= 1) {
      const match = matches[index]
      const staticTitle = getStaticTitle(match.staticData)
      if (staticTitle) {
        return staticTitle
      }
    }

    return fallback
  }, [fallback, matches])
}
