/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CSSProperties } from "react"

import { useClientSetting } from "@/lib/client-settings"
import { instanceInitials } from "@/lib/instances"

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

export interface InstanceMark {
  icon: string | null
  color: InstanceMarkColor | null
  letters: string
}

export const DEFAULT_INSTANCE_MARK: InstanceMark = { icon: null, color: null, letters: "" }

export const INSTANCE_MARK_LETTERS_MAX = 2

const KEY_PREFIX = "qui-instance-mark:"

function instanceMarkKey(instanceId: number): string {
  return `${KEY_PREFIX}${instanceId}`
}

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
    color: typeof color === "string" && Object.hasOwn(INSTANCE_MARK_HUES, color) ? color as InstanceMarkColor : null,
    letters: typeof letters === "string" ? normalizeInstanceMarkLetters(letters) : "",
  }
}

export function useInstanceMark(instanceId: number): [InstanceMark, (mark: InstanceMark) => void] {
  return useClientSetting(instanceMarkKey(instanceId), { defaultValue: DEFAULT_INSTANCE_MARK, parse: parseInstanceMark })
}

export function autoLettersFor(instances: readonly { id: number; name: string; isActive: boolean }[], instanceId: number): string {
  const listed = instances.filter((instance) => instance.isActive || instance.id === instanceId)
  return instanceInitials(listed.map((instance) => instance.name))[listed.findIndex((instance) => instance.id === instanceId)] ?? ""
}

export function instanceMarkColorProps(color: InstanceMarkColor | null): { className?: string; style?: CSSProperties } {
  if (!color) return {}
  return { className: "text-instance-mark", style: { "--mark-hue": INSTANCE_MARK_HUES[color] } as CSSProperties }
}
