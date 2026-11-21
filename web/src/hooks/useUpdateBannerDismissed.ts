/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

let updateBannerDismissed = false
const dismissListeners = new Set<() => void>()

export function useUpdateBannerDismissed(): [boolean, () => void] {
  const dismissed = useSyncExternalStore(
    (listener) => {
      dismissListeners.add(listener)
      return () => dismissListeners.delete(listener)
    },
    () => updateBannerDismissed
  )
  const dismiss = () => {
    updateBannerDismissed = true
    dismissListeners.forEach((listener) => listener())
  }
  return [dismissed, dismiss]
}
