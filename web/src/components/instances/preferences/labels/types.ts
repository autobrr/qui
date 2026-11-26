/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface FieldLabels {
  labelKey: string
  descriptionKey?: string
  placeholderKey?: string
  readOnlyNoteKey?: string
  unlimited?: number
  whenEnabled?: number
  options?: Record<string, { labelKey: string }>
}

export interface TabLabels {
  labelKey: string
  sections: Record<string, { labelKey: string; descriptionKey?: string }>
  fields: Record<string, FieldLabels>
}
