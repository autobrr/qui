/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext } from "react"

export const ShownSectionContext = createContext<string | null>(null)

export function useShownSectionTitle(): string | null {
  return useContext(ShownSectionContext)
}
