/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { DisplayProfileEntry } from "@/lib/display-profiles"
import { cn } from "@/lib/utils"

import { displayProfileColorClass, findDisplayProfileIcon } from "./display-profile-icons"

type ProfileLook = Pick<DisplayProfileEntry, "icon" | "color" | "name">

const TILE_SIZES = {
  xs: { tile: "size-3.5 rounded-[3px]", icon: "size-3.5", letter: "text-[9px]" },
  sm: { tile: "size-5 rounded-[5px]", icon: "size-3.5", letter: "text-[11px]" },
  md: { tile: "size-8 rounded-md", icon: "size-[18px]", letter: "text-sm" },
  lg: { tile: "size-10 rounded-lg", icon: "size-5", letter: "text-base" },
} as const

interface DisplayProfileTileProps {
  profile: ProfileLook
  size?: keyof typeof TILE_SIZES
  className?: string
}

export function DisplayProfileTile({ profile, size = "md", className }: DisplayProfileTileProps) {
  const sizes = TILE_SIZES[size]
  const icon = findDisplayProfileIcon(profile.icon)
  return (
    <span
      aria-hidden="true"
      data-slot="display-profile-tile"
      className={cn("inline-grid shrink-0 place-items-center bg-current/12", sizes.tile, displayProfileColorClass(profile.color), className)}
    >
      {icon ? (
        <icon.Icon className={sizes.icon} />
      ) : (
        <span className={cn("font-semibold leading-none", sizes.letter)}>{[...profile.name.trim()][0]?.toLocaleUpperCase() ?? "?"}</span>
      )}
    </span>
  )
}

export function DisplayProfileGlyph({ profile, className }: { profile: ProfileLook; className?: string }) {
  const icon = findDisplayProfileIcon(profile.icon)
  if (!icon) return <DisplayProfileTile profile={profile} size="xs" className={className} />
  return <icon.Icon aria-hidden="true" className={cn("size-3.5 shrink-0", displayProfileColorClass(profile.color), className)} />
}
