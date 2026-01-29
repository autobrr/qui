/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRouterState } from "@tanstack/react-router"
import { useMemo } from "react"

const DEFAULT_TITLE = "qui"

type RouteStaticData = {
  title?: string
}

/**
 * Resolves the most specific static title from the active route matches.
 * Falls back to the provided string when no route title is available.
 */
export function useRouteTitle(fallback: string = DEFAULT_TITLE) {
  const matches = useRouterState({
    select: (state: { matches: unknown[] }) => state.matches,
  })

  return useMemo(() => {
    for (let index = matches.length - 1; index >= 0; index -= 1) {
      const match = matches[index]
      const routeOptions = (match as { route?: { options?: { staticData?: RouteStaticData } } }).route?.options
      const staticTitle = routeOptions?.staticData?.title
      if (typeof staticTitle === "string" && staticTitle.trim().length > 0) {
        return staticTitle
      }
    }

    return fallback
  }, [fallback, matches])
}
