/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): a profile name. A renamed Default reads "Workstation · Default".

import { cn } from "@/lib/utils"
import type { DisplayProfile } from "./store"

export function ProfileName({ profile, className }: { profile: DisplayProfile; className?: string }) {
  return (
    <span className={cn("min-w-0 truncate", className)}>
      {profile.name}
      {profile.isDefault && profile.name !== "Default" && (
        <span className="font-normal text-muted-foreground"> · Default</span>
      )}
    </span>
  )
}
