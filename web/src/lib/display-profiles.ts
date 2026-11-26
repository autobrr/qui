/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const DEFAULT_DISPLAY_PROFILE_ID = "default"
export const DISPLAY_PROFILE_NAME_MAX_LENGTH = 32

export const DISPLAY_PROFILE_COLORS = ["sky", "emerald", "amber", "rose", "violet", "orange", "teal", "pink"] as const
export type DisplayProfileColor = typeof DISPLAY_PROFILE_COLORS[number]

export interface DisplayProfileEntry {
  name: string
  icon: string
  color: DisplayProfileColor | null
  sameColumns: boolean | null
  ownSortAndFilters: boolean
}

export interface DisplayProfile extends DisplayProfileEntry {
  id: string
  isDefault: boolean
  usesSameColumns: boolean
}

export const DEFAULT_DISPLAY_PROFILE_ENTRY: DisplayProfileEntry = {
  name: "",
  icon: "app-window",
  color: null,
  sameColumns: null,
  ownSortAndFilters: false,
}

export function displayProfileName(profile: Pick<DisplayProfile, "name" | "isDefault">, defaultName: string): string {
  return profile.isDefault && profile.name === "" ? defaultName : profile.name
}

export const DISPLAY_PROFILE_FALLBACK_ICON = "bookmark"

const ICON_GUESSES: readonly [RegExp, string][] = [
  [/laptop/, "laptop"],
  [/phone|mobile/, "smartphone"],
  [/tablet|ipad/, "tablet"],
  [/\btv\b/, "tv"],
  [/desktop|\bwork/, "monitor"],
]

export function guessDisplayProfileIcon(name: string): string {
  const folded = name.toLocaleLowerCase()
  return ICON_GUESSES.find(([pattern]) => pattern.test(folded))?.[1] ?? DISPLAY_PROFILE_FALLBACK_ICON
}

export function newDisplayProfileEntry(name: string): DisplayProfileEntry {
  return { name, icon: DISPLAY_PROFILE_FALLBACK_ICON, color: null, sameColumns: null, ownSortAndFilters: false }
}

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
