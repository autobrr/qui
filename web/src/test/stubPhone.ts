/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { vi } from "vitest"

import { NARROW_PHONE_QUERY, SIDEWAYS_PHONE_QUERY } from "@/hooks/useMediaQuery"

const MATCHES = {
  upright: [NARROW_PHONE_QUERY, "(hover: none)"],
  sideways: [SIDEWAYS_PHONE_QUERY, "(hover: none)"],
  desktop: [],
} satisfies Record<string, string[]>

/**
 * Stubs `window.matchMedia` as `useIsPhone` and the `(hover: none)` checks read it on an upright phone, a phone
 * turned sideways, or a desktop. Undo it with `vi.unstubAllGlobals()`.
 */
export function stubPhone(kind: keyof typeof MATCHES): void {
  const matches: readonly string[] = MATCHES[kind]
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: matches.includes(query),
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
}
