/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

const DEFAULT_ITEMS = ["views", "status", "categories", "tags", "trackers"]
const VIEWS_SEEDED_KEY = "qui-accordion-views-seeded"

export function usePersistedAccordion() {
  const [expandedItems, setExpandedItems] = useState<string[]>(() => {
    try {
      const stored = localStorage.getItem("qui-accordion")
      if (!stored) return DEFAULT_ITEMS
      const items: string[] = JSON.parse(stored)

      // Existing users have a stored array predating "views", so the new section
      // would ship collapsed. Expand it once; after that their own toggling wins.
      // The seed marker is written in the effect below, so this stays pure.
      if (localStorage.getItem(VIEWS_SEEDED_KEY)) return items
      return items.includes("views") ? items : ["views", ...items]
    } catch {
      return DEFAULT_ITEMS
    }
  })

  useEffect(() => {
    localStorage.setItem("qui-accordion", JSON.stringify(expandedItems))
    localStorage.setItem(VIEWS_SEEDED_KEY, "1")
  }, [expandedItems])

  return [expandedItems, setExpandedItems] as const
}
