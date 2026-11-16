/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// The entry type and the rules of a Display profile. The store and the key mapping live in client-settings.ts.

export const DEFAULT_DISPLAY_PROFILE_ID = "default"
export const DISPLAY_PROFILE_NAME_MAX_LENGTH = 32

export const DISPLAY_PROFILE_COLORS = ["sky", "emerald", "amber", "rose", "violet", "orange", "teal", "pink"] as const
export type DisplayProfileColor = typeof DISPLAY_PROFILE_COLORS[number]

export interface DisplayProfileEntry {
  name: string
  /** A lucide icon name, for example "app-window". */
  icon: string
  /** null is the color None: the icon uses the text color of the theme. */
  color: DisplayProfileColor | null
  /** "Same columns on every instance". null until the user sets it, so the app computes it. */
  sameColumns: boolean | null
  /** "Own sort and filters". Always false on Default. */
  ownSortAndFilters: boolean
}

export interface DisplayProfile extends DisplayProfileEntry {
  id: string
  isDefault: boolean
  /** The value of Same columns on every instance: the stored switch, or while it is unset, true when the profile has no Column setup of an instance. */
  usesSameColumns: boolean
}

export const DEFAULT_DISPLAY_PROFILE_ENTRY: DisplayProfileEntry = {
  name: "Default",
  icon: "app-window",
  color: null,
  sameColumns: null,
  ownSortAndFilters: false,
}

export function newDisplayProfileEntry(name: string): DisplayProfileEntry {
  return { name, icon: "bookmark", color: null, sameColumns: null, ownSortAndFilters: false }
}

/** Reads a stored entry. Returns null for a deleted entry ("") and for a value that is not an entry. */
export function parseDisplayProfileEntry(raw: string): DisplayProfileEntry | null {
  if (raw === "") return null
  try {
    const value = JSON.parse(raw) as Partial<DisplayProfileEntry> | null
    if (!value || typeof value !== "object" || typeof value.name !== "string") return null
    return {
      name: value.name,
      icon: typeof value.icon === "string" ? value.icon : "bookmark",
      color: DISPLAY_PROFILE_COLORS.includes(value.color as DisplayProfileColor) ? value.color as DisplayProfileColor : null,
      sameColumns: typeof value.sameColumns === "boolean" ? value.sameColumns : null,
      ownSortAndFilters: value.ownSortAndFilters === true,
    }
  } catch {
    return null
  }
}

export type DisplayProfileNameError = "empty" | "tooLong" | "duplicate"

/**
 * The name rule of every form that takes a profile name: 1 to 32 characters after trimming, and no other profile
 * with the same name, ignoring case. Pass the id of the profile that is renamed as ignoreId.
 */
export function validateDisplayProfileName(
  name: string,
  profiles: readonly DisplayProfile[],
  ignoreId?: string
): DisplayProfileNameError | null {
  const trimmed = name.trim()
  if (trimmed === "") return "empty"
  if ([...trimmed].length > DISPLAY_PROFILE_NAME_MAX_LENGTH) return "tooLong"
  const folded = trimmed.toLocaleLowerCase()
  if (profiles.some((profile) => profile.id !== ignoreId && profile.name.trim().toLocaleLowerCase() === folded)) return "duplicate"
  return null
}
