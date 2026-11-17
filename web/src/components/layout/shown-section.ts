/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext } from "react"

export const ShownSectionContext = createContext<string | null>(null)

/**
 * The title of the section that a phone shows as the title of the page, or null. A card with the same title can leave
 * it out, so that the phone does not show the same words twice.
 */
export function useShownSectionTitle(): string | null {
  return useContext(ShownSectionContext)
}
