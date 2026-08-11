/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { useNavigate } from "@tanstack/react-router"

// The concrete navigate function returned by useNavigate().
type NavigateFn = ReturnType<typeof useNavigate>

// Loose, structurally-correct shape for dynamically-built search params.
// Router search params come from useSearch({ strict: false }) and are spread
// into new objects, so values are effectively unknown at the call site.
type DynamicSearch = Record<string, unknown>

interface NavigateSearchOptions {
  navigate: NavigateFn
  to?: string
  search: DynamicSearch
  replace?: boolean
}

/**
 * Navigate while replacing search params with a dynamically-built object.
 *
 * TanStack Router's navigate() requires `search` to match the destination
 * route's generated SearchSchema. We build search objects dynamically
 * (spread current params + set/delete one key), which the compiler cannot
 * narrow to a specific route schema. The single cast below is the ONLY place
 * that bridge happens; every call site passes a DynamicSearch and stays
 * cast-free. Each route validates at runtime via its Zod validateSearch
 * (with .catch(undefined)), so the typed boundary is centralized, not weakened.
 */
export function navigateWithSearch({ navigate, to, search, replace }: NavigateSearchOptions): void {
  navigate({
    ...(to !== undefined ? { to } : {}),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- router navigate() expects a per-route SearchSchema; dynamic search objects are validated by each route's Zod validateSearch at runtime, so this single cast centralizes the unavoidable TanStack typing boundary.
    search: search as any,
    ...(replace !== undefined ? { replace } : {}),
  })
}
