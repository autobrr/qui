/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// The labels of one preference. unlimited is the value that means "no limit": the
// number field shows it as an empty box. whenEnabled is the value that an unlimited field
// gets when its switch turns on. readOnlyNoteKey is the note under a read-only field. A
// read-only field without it shows no note.
export interface FieldLabels {
  labelKey: string
  descriptionKey?: string
  placeholderKey?: string
  readOnlyNoteKey?: string
  unlimited?: number
  whenEnabled?: number
  options?: Record<string, { labelKey: string }>
}

// The labels of one tab of the qBittorrent preferences, keyed by the section and the
// preference key of the catalog view.
export interface TabLabels {
  labelKey: string
  sections: Record<string, { labelKey: string; descriptionKey?: string }>
  fields: Record<string, FieldLabels>
}
