/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { useNavigate } from "@tanstack/react-router"

type NavigateFn = ReturnType<typeof useNavigate>

type DynamicSearch = Record<string, unknown>

interface NavigateSearchOptions {
  navigate: NavigateFn
  to?: string
  search: DynamicSearch
  replace?: boolean
}

export function navigateWithSearch({ navigate, to, search, replace }: NavigateSearchOptions): void {
  navigate({
    ...(to !== undefined ? { to } : {}),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- router navigate() expects a per-route SearchSchema; dynamic search objects are validated by each route's Zod validateSearch at runtime, so this single cast centralizes the unavoidable TanStack typing boundary.
    search: search as any,
    ...(replace !== undefined ? { replace } : {}),
  })
}
