/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the profile list of the Settings section and of the
// phone sheet. Rows are 56 px at every width; a long name is cut off.

import { Check, ChevronRight, Funnel, Plus } from "lucide-react"

import { cn } from "@/lib/utils"
import { HelpTip, OwnSortAndFiltersHelp } from "./HelpTip"
import { ProfileTile } from "./look"
import { ProfileName } from "./ProfileName"
import { openNewProfileDialog } from "./NewProfileDialog"
import { usePrototype, type DisplayProfile } from "./store"

interface ProfileListProps {
  /** select: a click shows the profile in the editor. pick: a click makes it this browser's profile. */
  mode: "select" | "pick"
  selectedId?: string
  onActivate: (profile: DisplayProfile) => void
  /** Shows a › button on each row that opens the editor. */
  onEdit?: (profile: DisplayProfile) => void
  /** Marks a profile that keeps its own sort and filters (Settings only). */
  showOwnMark?: boolean
  className?: string
}

export function ProfileList({ mode, selectedId, onActivate, onEdit, showOwnMark = false, className }: ProfileListProps) {
  const profiles = usePrototype((s) => s.profiles)
  const activeId = usePrototype((s) => s.activeProfileId)

  return (
    <ul className={cn("flex flex-col gap-1.5", className)} aria-label="Display profiles" data-prototype-profile-list={mode}>
      {profiles.map((profile) => {
        const isActive = profile.id === activeId
        const isSelected = mode === "select" && profile.id === selectedId
        const own = showOwnMark && !profile.isDefault && !!profile.ownSortAndFilters
        return (
          <li
            key={profile.id}
            data-profile-row={profile.name}
            className={cn(
              "flex h-14 items-center rounded-lg border transition-colors",
              isSelected ? "border-foreground/20 bg-accent" : "hover:bg-accent/50"
            )}
          >
            <button
              type="button"
              onClick={() => onActivate(profile)}
              aria-current={isSelected ? "true" : undefined}
              aria-pressed={mode === "pick" ? isActive : undefined}
              className="flex h-full min-w-0 flex-1 items-center gap-3 rounded-lg px-3 text-left outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <ProfileTile profile={profile} size="md" />
              <span className="min-w-0 flex-1">
                <ProfileName profile={profile} className="block text-sm font-medium" />
                {isActive && (
                  <span className="flex items-center gap-1 text-xs font-medium text-primary">
                    <Check aria-hidden="true" className="size-3" />
                    This browser
                  </span>
                )}
              </span>
            </button>
            {own && (
              <HelpTip label="Own sort and filters" icon={Funnel} iconClassName="size-4" className={cn("size-8 rounded-md", !onEdit && "mr-2")}>
                <OwnSortAndFiltersHelp />
              </HelpTip>
            )}
            {onEdit && (
              <button
                type="button"
                onClick={() => onEdit(profile)}
                aria-label={`Edit ${profile.name}`}
                className="mr-1 grid size-11 shrink-0 place-items-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
              >
                <ChevronRight className="size-5" />
              </button>
            )}
          </li>
        )
      })}
      {profiles.length === 1 && (
        <li>
          <button
            type="button"
            onClick={(e) => openNewProfileDialog(e.currentTarget)}
            className="flex h-14 w-full items-center gap-3 rounded-lg border border-dashed px-3 text-left text-sm text-muted-foreground outline-none hover:bg-accent/50 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-md border border-dashed">
              <Plus className="size-4" />
            </span>
            Make a profile for a laptop or phone
          </button>
        </li>
      )}
    </ul>
  )
}
