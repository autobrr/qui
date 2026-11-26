/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  Anchor,
  AppWindow,
  Archive,
  Bird,
  BookHeadphones,
  BookOpen,
  Box,
  Bug,
  Building2,
  Cat,
  Cloud,
  CloudDownload,
  Container,
  Cpu,
  Crown,
  Database,
  Disc,
  Dog,
  Film,
  Fish,
  Flag,
  Flame,
  Gamepad2,
  Ghost,
  Globe,
  HardDrive,
  HardDriveDownload,
  Heart,
  House,
  HouseWifi,
  Image,
  Laptop,
  Layers,
  Leaf,
  Magnet,
  MapPin,
  MemoryStick,
  Monitor,
  Mountain,
  Music,
  PcCase,
  Podcast,
  Rabbit,
  Rocket,
  Router,
  Satellite,
  Server,
  ServerNas,
  Shield,
  Sprout,
  Star,
  Tent,
  Turtle,
  Tv,
  Warehouse,
  Zap,
  type LucideIcon
} from "lucide-react"

// i18n-namespace: instances

export interface InstanceMarkIconEntry {
  name: string
  labelKey: string
  Icon: LucideIcon
}

export interface InstanceMarkIconGroup {
  labelKey: string
  icons: readonly InstanceMarkIconEntry[]
}

export const INSTANCE_MARK_ICON_GROUPS: readonly InstanceMarkIconGroup[] = [
  {
    labelKey: "mark.iconGroups.servers",
    icons: [
      { name: "server", labelKey: "mark.icons.server", Icon: Server },
      { name: "server-nas", labelKey: "mark.icons.nas", Icon: ServerNas },
      { name: "pc-case", labelKey: "mark.icons.tower", Icon: PcCase },
      { name: "house-wifi", labelKey: "mark.icons.homeServer", Icon: HouseWifi },
      { name: "sprout", labelKey: "mark.icons.seedbox", Icon: Sprout },
      { name: "container", labelKey: "mark.icons.container", Icon: Container },
      { name: "cloud", labelKey: "mark.icons.cloud", Icon: Cloud },
      { name: "cloud-download", labelKey: "mark.icons.cloudDownload", Icon: CloudDownload },
    ],
  },
  {
    labelKey: "mark.iconGroups.hardware",
    icons: [
      { name: "hard-drive", labelKey: "mark.icons.hardDrive", Icon: HardDrive },
      { name: "hard-drive-download", labelKey: "mark.icons.driveDownload", Icon: HardDriveDownload },
      { name: "database", labelKey: "mark.icons.database", Icon: Database },
      { name: "cpu", labelKey: "mark.icons.cpu", Icon: Cpu },
      { name: "memory-stick", labelKey: "mark.icons.memory", Icon: MemoryStick },
      { name: "router", labelKey: "mark.icons.router", Icon: Router },
      { name: "monitor", labelKey: "mark.icons.monitor", Icon: Monitor },
      { name: "laptop", labelKey: "mark.icons.laptop", Icon: Laptop },
    ],
  },
  {
    labelKey: "mark.iconGroups.places",
    icons: [
      { name: "house", labelKey: "mark.icons.house", Icon: House },
      { name: "building-2", labelKey: "mark.icons.building", Icon: Building2 },
      { name: "warehouse", labelKey: "mark.icons.warehouse", Icon: Warehouse },
      { name: "globe", labelKey: "mark.icons.globe", Icon: Globe },
      { name: "map-pin", labelKey: "mark.icons.mapPin", Icon: MapPin },
      { name: "flag", labelKey: "mark.icons.flag", Icon: Flag },
      { name: "mountain", labelKey: "mark.icons.mountain", Icon: Mountain },
      { name: "tent", labelKey: "mark.icons.tent", Icon: Tent },
    ],
  },
  {
    labelKey: "mark.iconGroups.media",
    icons: [
      { name: "film", labelKey: "mark.icons.film", Icon: Film },
      { name: "tv", labelKey: "mark.icons.tv", Icon: Tv },
      { name: "music", labelKey: "mark.icons.music", Icon: Music },
      { name: "podcast", labelKey: "mark.icons.podcast", Icon: Podcast },
      { name: "book-open", labelKey: "mark.icons.book", Icon: BookOpen },
      { name: "book-headphones", labelKey: "mark.icons.audiobook", Icon: BookHeadphones },
      { name: "gamepad-2", labelKey: "mark.icons.gamepad", Icon: Gamepad2 },
      { name: "image", labelKey: "mark.icons.image", Icon: Image },
    ],
  },
  {
    labelKey: "mark.iconGroups.nature",
    icons: [
      { name: "cat", labelKey: "mark.icons.cat", Icon: Cat },
      { name: "dog", labelKey: "mark.icons.dog", Icon: Dog },
      { name: "bird", labelKey: "mark.icons.bird", Icon: Bird },
      { name: "fish", labelKey: "mark.icons.fish", Icon: Fish },
      { name: "rabbit", labelKey: "mark.icons.rabbit", Icon: Rabbit },
      { name: "turtle", labelKey: "mark.icons.turtle", Icon: Turtle },
      { name: "bug", labelKey: "mark.icons.bug", Icon: Bug },
      { name: "leaf", labelKey: "mark.icons.leaf", Icon: Leaf },
    ],
  },
  {
    labelKey: "mark.iconGroups.symbols",
    icons: [
      { name: "star", labelKey: "mark.icons.star", Icon: Star },
      { name: "heart", labelKey: "mark.icons.heart", Icon: Heart },
      { name: "flame", labelKey: "mark.icons.flame", Icon: Flame },
      { name: "zap", labelKey: "mark.icons.zap", Icon: Zap },
      { name: "rocket", labelKey: "mark.icons.rocket", Icon: Rocket },
      { name: "crown", labelKey: "mark.icons.crown", Icon: Crown },
      { name: "shield", labelKey: "mark.icons.shield", Icon: Shield },
      { name: "ghost", labelKey: "mark.icons.ghost", Icon: Ghost },
    ],
  },
  {
    labelKey: "mark.iconGroups.objects",
    icons: [
      { name: "box", labelKey: "mark.icons.box", Icon: Box },
      { name: "archive", labelKey: "mark.icons.archive", Icon: Archive },
      { name: "app-window", labelKey: "mark.icons.software", Icon: AppWindow },
      { name: "disc", labelKey: "mark.icons.disc", Icon: Disc },
      { name: "layers", labelKey: "mark.icons.layers", Icon: Layers },
      { name: "anchor", labelKey: "mark.icons.anchor", Icon: Anchor },
      { name: "magnet", labelKey: "mark.icons.magnet", Icon: Magnet },
      { name: "satellite", labelKey: "mark.icons.satellite", Icon: Satellite },
    ],
  },
]

export const INSTANCE_MARK_ICONS: readonly InstanceMarkIconEntry[] = INSTANCE_MARK_ICON_GROUPS.flatMap((group) => group.icons)

const ICONS_BY_NAME = new Map(INSTANCE_MARK_ICONS.map((icon) => [icon.name, icon.Icon]))

export function findInstanceMarkIcon(name: string | null): LucideIcon | undefined {
  return name === null ? undefined : ICONS_BY_NAME.get(name)
}
