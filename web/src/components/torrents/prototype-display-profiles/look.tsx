/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the icon and color of a Display profile. Every icon is a
// static import, so the bundle holds these 32 and no others.

import {
  AppWindow,
  Bed,
  Bookmark,
  Briefcase,
  Building2,
  Coffee,
  Download,
  Eye,
  Flame,
  Gamepad2,
  Gauge,
  Heart,
  House,
  Laptop,
  LayoutList,
  Monitor,
  Moon,
  Plane,
  Rocket,
  Rows3,
  Server,
  Smartphone,
  Sofa,
  Sprout,
  Star,
  Sun,
  Table2,
  Tablet,
  TrainFront,
  Tv,
  Upload,
  Zap,
  type LucideIcon
} from "lucide-react"

import { cn } from "@/lib/utils"
import type { DisplayProfile } from "./store"

export interface ProfileIconEntry {
  name: string
  label: string
  Icon: LucideIcon
}

export const ICON_GROUPS: { label: string; icons: ProfileIconEntry[] }[] = [
  {
    label: "Devices",
    icons: [
      { name: "app-window", label: "App window", Icon: AppWindow },
      { name: "monitor", label: "Monitor", Icon: Monitor },
      { name: "laptop", label: "Laptop", Icon: Laptop },
      { name: "tablet", label: "Tablet", Icon: Tablet },
      { name: "smartphone", label: "Smartphone", Icon: Smartphone },
      { name: "tv", label: "TV", Icon: Tv },
      { name: "server", label: "Server", Icon: Server },
      { name: "gamepad-2", label: "Gamepad", Icon: Gamepad2 },
    ],
  },
  {
    label: "Places",
    icons: [
      { name: "house", label: "House", Icon: House },
      { name: "briefcase", label: "Briefcase", Icon: Briefcase },
      { name: "building-2", label: "Building", Icon: Building2 },
      { name: "sofa", label: "Sofa", Icon: Sofa },
      { name: "bed", label: "Bed", Icon: Bed },
      { name: "coffee", label: "Coffee", Icon: Coffee },
      { name: "plane", label: "Plane", Icon: Plane },
      { name: "train-front", label: "Train", Icon: TrainFront },
    ],
  },
  {
    label: "Use",
    icons: [
      { name: "rows-3", label: "Rows", Icon: Rows3 },
      { name: "table-2", label: "Table", Icon: Table2 },
      { name: "layout-list", label: "List", Icon: LayoutList },
      { name: "eye", label: "Eye", Icon: Eye },
      { name: "gauge", label: "Gauge", Icon: Gauge },
      { name: "zap", label: "Zap", Icon: Zap },
      { name: "download", label: "Download", Icon: Download },
      { name: "upload", label: "Upload", Icon: Upload },
    ],
  },
  {
    label: "Mood",
    icons: [
      { name: "sprout", label: "Sprout", Icon: Sprout },
      { name: "bookmark", label: "Bookmark", Icon: Bookmark },
      { name: "moon", label: "Moon", Icon: Moon },
      { name: "sun", label: "Sun", Icon: Sun },
      { name: "star", label: "Star", Icon: Star },
      { name: "heart", label: "Heart", Icon: Heart },
      { name: "flame", label: "Flame", Icon: Flame },
      { name: "rocket", label: "Rocket", Icon: Rocket },
    ],
  },
]

export const ALL_ICONS: ProfileIconEntry[] = ICON_GROUPS.flatMap((g) => g.icons)
const ICONS_BY_NAME = new Map(ALL_ICONS.map((entry) => [entry.name, entry]))

export function iconEntry(name: string): ProfileIconEntry | undefined {
  return ICONS_BY_NAME.get(name)
}

export const FALLBACK_ICON = "bookmark"

const GUESSES: [RegExp, string][] = [
  [/laptop/, "laptop"],
  [/phone|mobile/, "smartphone"],
  [/tablet|ipad/, "tablet"],
  [/\btv\b/, "tv"],
  [/desktop|\bwork/, "monitor"],
]

/** The icon a new profile gets from its name until the person picks one. */
export function guessIcon(name: string): string {
  const lower = name.toLowerCase()
  return GUESSES.find(([pattern]) => pattern.test(lower))?.[1] ?? FALLBACK_ICON
}

// Fixed classes, so Tailwind sees each one. "none" sets no class and follows the text color.
export const PROFILE_COLORS = [
  { id: "none", label: "None", className: "" },
  { id: "sky", label: "Sky", className: "text-sky-600 dark:text-sky-400" },
  { id: "emerald", label: "Emerald", className: "text-emerald-600 dark:text-emerald-400" },
  { id: "amber", label: "Amber", className: "text-amber-600 dark:text-amber-400" },
  { id: "rose", label: "Rose", className: "text-rose-600 dark:text-rose-400" },
  { id: "violet", label: "Violet", className: "text-violet-600 dark:text-violet-400" },
  { id: "orange", label: "Orange", className: "text-orange-600 dark:text-orange-400" },
  { id: "teal", label: "Teal", className: "text-teal-600 dark:text-teal-400" },
  { id: "pink", label: "Pink", className: "text-pink-600 dark:text-pink-400" },
] as const

export type ProfileColor = (typeof PROFILE_COLORS)[number]["id"]

export function colorClass(color: ProfileColor): string {
  return PROFILE_COLORS.find((c) => c.id === color)?.className ?? ""
}

type Look = Pick<DisplayProfile, "icon" | "color" | "name">

const TILE_SIZES = {
  xs: { tile: "size-3.5 rounded-[3px]", icon: "size-3.5", letter: "text-[9px]" },
  sm: { tile: "size-5 rounded-[5px]", icon: "size-3.5", letter: "text-[11px]" },
  md: { tile: "size-8 rounded-md", icon: "size-[18px]", letter: "text-sm" },
  lg: { tile: "size-10 rounded-lg", icon: "size-5", letter: "text-base" },
  xl: { tile: "size-12 rounded-lg", icon: "size-6", letter: "text-lg" },
} as const

/** The profile icon on a tile tinted in the profile color. An unknown icon name shows the first letter of the name. */
export function ProfileTile({ profile, size = "md", className }: { profile: Look; size?: keyof typeof TILE_SIZES; className?: string }) {
  const s = TILE_SIZES[size]
  const entry = iconEntry(profile.icon)
  return (
    <span aria-hidden="true" className={cn("inline-grid shrink-0 place-items-center bg-current/12", s.tile, colorClass(profile.color), className)}>
      {entry ? <entry.Icon className={s.icon} /> : <span className={cn("font-semibold leading-none", s.letter)}>{profile.name.trim().charAt(0).toUpperCase() || "?"}</span>}
    </span>
  )
}

/** The bare profile icon in the profile color, for tight places such as the status bar chip. */
export function ProfileGlyph({ profile, className }: { profile: Look; className?: string }) {
  const entry = iconEntry(profile.icon)
  if (!entry) return <ProfileTile profile={profile} size="xs" className={className} />
  return <entry.Icon aria-hidden="true" className={cn("size-3.5 shrink-0", colorClass(profile.color), className)} />
}
