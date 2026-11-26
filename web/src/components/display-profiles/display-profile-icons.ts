/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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

import type { DisplayProfileColor } from "@/lib/display-profiles"

// i18n-namespace: torrents

export interface DisplayProfileIcon {
  name: string
  labelKey: string
  Icon: LucideIcon
}

export interface DisplayProfileIconGroup {
  labelKey: string
  icons: readonly DisplayProfileIcon[]
}

export const DISPLAY_PROFILE_ICON_GROUPS: readonly DisplayProfileIconGroup[] = [
  {
    labelKey: "displayProfiles.iconGroups.devices",
    icons: [
      { name: "app-window", labelKey: "displayProfiles.icons.appWindow", Icon: AppWindow },
      { name: "monitor", labelKey: "displayProfiles.icons.monitor", Icon: Monitor },
      { name: "laptop", labelKey: "displayProfiles.icons.laptop", Icon: Laptop },
      { name: "tablet", labelKey: "displayProfiles.icons.tablet", Icon: Tablet },
      { name: "smartphone", labelKey: "displayProfiles.icons.smartphone", Icon: Smartphone },
      { name: "tv", labelKey: "displayProfiles.icons.tv", Icon: Tv },
      { name: "server", labelKey: "displayProfiles.icons.server", Icon: Server },
      { name: "gamepad-2", labelKey: "displayProfiles.icons.gamepad", Icon: Gamepad2 },
    ],
  },
  {
    labelKey: "displayProfiles.iconGroups.places",
    icons: [
      { name: "house", labelKey: "displayProfiles.icons.house", Icon: House },
      { name: "briefcase", labelKey: "displayProfiles.icons.briefcase", Icon: Briefcase },
      { name: "building-2", labelKey: "displayProfiles.icons.building", Icon: Building2 },
      { name: "sofa", labelKey: "displayProfiles.icons.sofa", Icon: Sofa },
      { name: "bed", labelKey: "displayProfiles.icons.bed", Icon: Bed },
      { name: "coffee", labelKey: "displayProfiles.icons.coffee", Icon: Coffee },
      { name: "plane", labelKey: "displayProfiles.icons.plane", Icon: Plane },
      { name: "train-front", labelKey: "displayProfiles.icons.train", Icon: TrainFront },
    ],
  },
  {
    labelKey: "displayProfiles.iconGroups.use",
    icons: [
      { name: "rows-3", labelKey: "displayProfiles.icons.rows", Icon: Rows3 },
      { name: "table-2", labelKey: "displayProfiles.icons.table", Icon: Table2 },
      { name: "layout-list", labelKey: "displayProfiles.icons.list", Icon: LayoutList },
      { name: "eye", labelKey: "displayProfiles.icons.eye", Icon: Eye },
      { name: "gauge", labelKey: "displayProfiles.icons.gauge", Icon: Gauge },
      { name: "zap", labelKey: "displayProfiles.icons.zap", Icon: Zap },
      { name: "download", labelKey: "displayProfiles.icons.download", Icon: Download },
      { name: "upload", labelKey: "displayProfiles.icons.upload", Icon: Upload },
    ],
  },
  {
    labelKey: "displayProfiles.iconGroups.mood",
    icons: [
      { name: "sprout", labelKey: "displayProfiles.icons.sprout", Icon: Sprout },
      { name: "bookmark", labelKey: "displayProfiles.icons.bookmark", Icon: Bookmark },
      { name: "moon", labelKey: "displayProfiles.icons.moon", Icon: Moon },
      { name: "sun", labelKey: "displayProfiles.icons.sun", Icon: Sun },
      { name: "star", labelKey: "displayProfiles.icons.star", Icon: Star },
      { name: "heart", labelKey: "displayProfiles.icons.heart", Icon: Heart },
      { name: "flame", labelKey: "displayProfiles.icons.flame", Icon: Flame },
      { name: "rocket", labelKey: "displayProfiles.icons.rocket", Icon: Rocket },
    ],
  },
]

export const DISPLAY_PROFILE_ICONS: readonly DisplayProfileIcon[] = DISPLAY_PROFILE_ICON_GROUPS.flatMap((group) => group.icons)

const ICONS_BY_NAME = new Map(DISPLAY_PROFILE_ICONS.map((icon) => [icon.name, icon]))

export function findDisplayProfileIcon(name: string): DisplayProfileIcon | undefined {
  return ICONS_BY_NAME.get(name)
}

export interface DisplayProfileColorOption {
  color: DisplayProfileColor | null
  labelKey: string
  className: string
}

export const DISPLAY_PROFILE_COLOR_OPTIONS: readonly DisplayProfileColorOption[] = [
  { color: null, labelKey: "displayProfiles.colors.none", className: "" },
  { color: "sky", labelKey: "displayProfiles.colors.sky", className: "text-sky-600 dark:text-sky-400" },
  { color: "emerald", labelKey: "displayProfiles.colors.emerald", className: "text-emerald-600 dark:text-emerald-400" },
  { color: "amber", labelKey: "displayProfiles.colors.amber", className: "text-amber-600 dark:text-amber-400" },
  { color: "rose", labelKey: "displayProfiles.colors.rose", className: "text-rose-600 dark:text-rose-400" },
  { color: "violet", labelKey: "displayProfiles.colors.violet", className: "text-violet-600 dark:text-violet-400" },
  { color: "orange", labelKey: "displayProfiles.colors.orange", className: "text-orange-600 dark:text-orange-400" },
  { color: "teal", labelKey: "displayProfiles.colors.teal", className: "text-teal-600 dark:text-teal-400" },
  { color: "pink", labelKey: "displayProfiles.colors.pink", className: "text-pink-600 dark:text-pink-400" },
]

export function displayProfileColorClass(color: DisplayProfileColor | null): string {
  return DISPLAY_PROFILE_COLOR_OPTIONS.find((option) => option.color === color)?.className ?? ""
}
