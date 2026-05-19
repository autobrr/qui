/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

export function usePersistedAccordion() {
  const [expandedItems, setExpandedItems] = useState<string[]>(() => {
    const stored = localStorage.getItem("qui-accordion")
    return stored ? JSON.parse(stored) : ["status", "categories", "tags", "trackers"]
  })

  useEffect(() => {
    localStorage.setItem("qui-accordion", JSON.stringify(expandedItems))
  }, [expandedItems])

  return [expandedItems, setExpandedItems] as const
}