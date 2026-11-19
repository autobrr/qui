/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CSSProperties } from "react"

import { useClientSetting } from "@/lib/client-settings"
import { instanceInitials } from "@/lib/instances"

// The hue of each color. The theme sets the lightness and the chroma, see .instance-mark-color in index.css.
// No hue sits near red, because a red mark means that the instance has a problem.
export const INSTANCE_MARK_HUES = {
  sky: 237,
  violet: 293,
  pink: 350,
  orange: 50,
  amber: 80,
  lime: 130,
  emerald: 163,
  teal: 185,
} as const

export type InstanceMarkColor = keyof typeof INSTANCE_MARK_HUES

export const INSTANCE_MARK_COLORS = Object.keys(INSTANCE_MARK_HUES) as InstanceMarkColor[]

/** How an instance shows in the sidebar and the instance menus. Unified has one too, under ALL_INSTANCES_ID. */
export interface InstanceMark {
  /** A lucide name from INSTANCE_MARK_ICON_GROUPS, or null for letters. */
  icon: string | null
  color: InstanceMarkColor | null
  /** Up to 2 characters that the icon-only sidebar shows in place of an icon. "" shows the automatic initials. */
  letters: string
}

export const DEFAULT_INSTANCE_MARK: InstanceMark = { icon: null, color: null, letters: "" }

export const INSTANCE_MARK_LETTERS_MAX = 2

const KEY_PREFIX = "qui-instance-mark:"

function instanceMarkKey(instanceId: number): string {
  return `${KEY_PREFIX}${instanceId}`
}

/**
 * The first INSTANCE_MARK_LETTERS_MAX characters, upper case, without spaces. A character is a grapheme, so an emoji
 * counts as one.
 */
export function normalizeInstanceMarkLetters(value: string): string {
  const graphemes = Array.from(new Intl.Segmenter().segment(value.replace(/\s/gu, "").toLocaleUpperCase()), (part) => part.segment)
  return graphemes.slice(0, INSTANCE_MARK_LETTERS_MAX).join("")
}

export function parseInstanceMark(raw: string): InstanceMark {
  const value: unknown = JSON.parse(raw)
  if (typeof value !== "object" || value === null) throw new Error("not an object")
  const { icon, color, letters } = value as Record<string, unknown>
  return {
    icon: typeof icon === "string" && icon !== "" ? icon : null,
    color: typeof color === "string" && color in INSTANCE_MARK_HUES ? color as InstanceMarkColor : null,
    letters: typeof letters === "string" ? normalizeInstanceMarkLetters(letters) : "",
  }
}

/** The mark of an instance, or of Unified under ALL_INSTANCES_ID. A change saves at once. */
export function useInstanceMark(instanceId: number): [InstanceMark, (mark: InstanceMark) => void] {
  return useClientSetting(instanceMarkKey(instanceId), { defaultValue: DEFAULT_INSTANCE_MARK, parse: parseInstanceMark })
}

/**
 * The automatic letters that the collapsed sidebar shows for an instance. The sidebar lists only the active instances,
 * and its place among them decides its initials. A disabled instance gets the letters it would have if it were active.
 */
export function autoLettersFor(instances: readonly { id: number; name: string; isActive: boolean }[], instanceId: number): string {
  const listed = instances.filter((instance) => instance.isActive || instance.id === instanceId)
  return instanceInitials(listed.map((instance) => instance.name))[listed.findIndex((instance) => instance.id === instanceId)] ?? ""
}

/** The class and style that give a mark its color. Pair the class with bg-current for a swatch. */
export function instanceMarkColorProps(color: InstanceMarkColor | null): { className?: string; style?: CSSProperties } {
  if (!color) return {}
  return { className: "instance-mark-color", style: { "--mark-hue": INSTANCE_MARK_HUES[color] } as CSSProperties }
}
