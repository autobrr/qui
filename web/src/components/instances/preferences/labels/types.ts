/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// The labels of one preference. unlimited is the value that means "no limit": the
// number field shows it as an empty box.
export interface FieldLabels {
  labelKey: string
  descriptionKey?: string
  placeholderKey?: string
  unlimited?: number
  options?: Record<string, { labelKey: string }>
}

// The labels of one tab of the qBittorrent preferences, keyed by the section and the
// preference key of the catalog view.
export interface TabLabels {
  labelKey: string
  sections: Record<string, { labelKey: string; descriptionKey?: string }>
  fields: Record<string, FieldLabels>
}
