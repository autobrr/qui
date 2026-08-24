/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useMemo, useRef, type SetStateAction } from "react"

import { useClientSetting } from "@/lib/client-settings"

const DEFAULT_ITEMS = ["views", "status", "categories", "tags", "trackers"]
const VIEWS_SEEDED_KEY = "qui-accordion-views-seeded"

const parseAccordionItems = (raw: string): string[] => {
  const parsed: unknown = JSON.parse(raw)
  if (!Array.isArray(parsed) || parsed.some((item) => typeof item !== "string")) {
    throw new Error("invalid accordion state")
  }
  return parsed
}

const parseRawString = (raw: string): string => raw

export function usePersistedAccordion() {
  const [storedItems, setStoredItems] = useClientSetting<string[]>("qui-accordion", {
    defaultValue: DEFAULT_ITEMS,
    parse: parseAccordionItems,
  })
  const [viewsSeeded, setViewsSeeded] = useClientSetting<string>(VIEWS_SEEDED_KEY, {
    defaultValue: "",
    parse: parseRawString,
    serialize: parseRawString,
  })

  // Existing users have a stored array predating "views", so the new section
  // would ship collapsed. Expand it in-memory until the seed marker is set;
  // the marker is written on the user's first toggle, after which their own
  // choice wins.
  const expandedItems = useMemo(() => {
    if (viewsSeeded || storedItems.includes("views")) return storedItems
    return ["views", ...storedItems]
  }, [storedItems, viewsSeeded])

  const expandedRef = useRef(expandedItems)
  expandedRef.current = expandedItems

  const setExpandedItems = useCallback(
    (next: SetStateAction<string[]>) => {
      const resolved = typeof next === "function" ? next(expandedRef.current) : next
      setStoredItems(resolved)
      setViewsSeeded("1")
    },
    [setStoredItems, setViewsSeeded]
  )

  return [expandedItems, setExpandedItems] as const
}
