/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { NavigateFn } from "@tanstack/react-router"

const ADD_INTENT_KEY = "qui-add-intent"

export interface AddIntent {
  magnet?: string
  hasFiles?: boolean
  openAdd?: boolean
}

export function storeAddIntent(intent: AddIntent): void {
  try {
    sessionStorage.setItem(ADD_INTENT_KEY, JSON.stringify(intent))
  } catch (err) {
    console.error("[add-intent] Failed to store intent:", err)
  }
}

export function getAndClearAddIntent(): AddIntent | null {
  let stored: string | null = null
  try {
    stored = sessionStorage.getItem(ADD_INTENT_KEY)
    if (!stored) return null
    sessionStorage.removeItem(ADD_INTENT_KEY)
  } catch (err) {
    console.error("[add-intent] Failed to read intent:", err)
    return null
  }
  try {
    return JSON.parse(stored)
  } catch (err) {
    console.error("[add-intent] Failed to parse stored intent:", err)
    return null
  }
}

export function clearAddIntent(): void {
  try {
    sessionStorage.removeItem(ADD_INTENT_KEY)
  } catch (err) {
    console.error("[add-intent] Failed to clear stored intent:", err)
  }
}

/**
 * Navigate to the appropriate route after successful authentication.
 * Checks for stored add intent (from PWA protocol/file handler) and routes accordingly.
 */
export function navigateAfterAuth(navigate: NavigateFn, defaultRoute: string = "/dashboard"): void {
  const addIntent = getAndClearAddIntent()
  if (addIntent?.magnet) {
    navigate({ to: "/add", search: { magnet: addIntent.magnet } })
  } else if (addIntent?.hasFiles) {
    // Pass expectingFiles flag so /add can show appropriate error if launchQueue doesn't fire
    navigate({ to: "/add", search: { expectingFiles: "true" } })
  } else if (addIntent?.openAdd) {
    navigate({ to: "/add" })
  } else {
    navigate({ to: defaultRoute })
  }
}
