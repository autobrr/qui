/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface PageTitle {
  detail?: string
  speeds?: string
}

let pageTitle: PageTitle = {}
const listeners = new Set<() => void>()

export function publishPageTitle(next: PageTitle) {
  pageTitle = next
  listeners.forEach(listener => listener())
}

export function subscribePageTitle(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function readPageTitle(): PageTitle {
  return pageTitle
}
