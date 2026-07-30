/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

const DEFAULT_ITEMS = ["views", "status", "categories", "tags", "trackers"]
const VIEWS_SEEDED_KEY = "qui-accordion-views-seeded"

export function usePersistedAccordion() {
  const [expandedItems, setExpandedItems] = useState<string[]>(() => {
    const stored = localStorage.getItem("qui-accordion")
    if (!stored) return DEFAULT_ITEMS
    const items: string[] = JSON.parse(stored)

    // Existing users have a stored array predating "views", so the new section
    // would ship collapsed. Expand it once; after that their own toggling wins.
    if (localStorage.getItem(VIEWS_SEEDED_KEY)) return items
    localStorage.setItem(VIEWS_SEEDED_KEY, "1")
    return items.includes("views") ? items : ["views", ...items]
  })

  useEffect(() => {
    localStorage.setItem("qui-accordion", JSON.stringify(expandedItems))
  }, [expandedItems])

  return [expandedItems, setExpandedItems] as const
}
