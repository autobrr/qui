/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check, Funnel, Plus } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDisplayProfiles } from "@/lib/client-settings"
import type { DisplayProfile } from "@/lib/display-profiles"
import { cn } from "@/lib/utils"

import { DisplayProfileTile } from "./DisplayProfileTile"
import { OwnSortAndFiltersHelp } from "./OwnSortAndFiltersSetting"

interface DisplayProfileListProps {
  selectedId: string
  onSelect: (profile: DisplayProfile) => void
  onNewProfile: () => void
}

/** The profiles in rows of 56 px. While only Default exists, a dashed tile below it opens the New profile dialog. */
export function DisplayProfileList({ selectedId, onSelect, onNewProfile }: DisplayProfileListProps) {
  const { t } = useTranslation("torrents")
  const { profiles, active } = useDisplayProfiles()

  return (
    <ul aria-label={t("displayProfiles.settings.title")} className="flex flex-col gap-1.5">
      {profiles.map((profile) => {
        const selected = profile.id === selectedId
        return (
          <li
            key={profile.id}
            className={cn(
              "flex min-h-14 items-center rounded-lg border transition-colors",
              selected ? "border-foreground/20 bg-accent" : "hover:bg-accent/50"
            )}
          >
            <button
              type="button"
              aria-current={selected || undefined}
              onClick={() => onSelect(profile)}
              className="flex min-w-0 flex-1 items-center gap-3 self-stretch rounded-lg px-3 py-2 text-left outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <DisplayProfileTile profile={profile} size="md" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium" title={profile.name}>{profile.name}</span>
                {profile.id === active.id && (
                  <span className="flex items-center gap-1 text-xs font-medium text-primary">
                    <Check aria-hidden="true" className="size-3 shrink-0" />
                    {t("displayProfiles.settings.thisBrowser")}
                  </span>
                )}
              </span>
            </button>
            {profile.ownSortAndFilters && (
              <Tooltip>
                <TooltipTrigger
                  aria-label={t("displayProfiles.ownSortAndFilters.label")}
                  className="mr-2 grid size-8 shrink-0 place-items-center rounded-md text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
                >
                  <Funnel className="size-4" />
                </TooltipTrigger>
                <TooltipContent className="max-w-xs"><OwnSortAndFiltersHelp /></TooltipContent>
              </Tooltip>
            )}
          </li>
        )
      })}
      {profiles.length === 1 && (
        <li>
          <button
            type="button"
            onClick={onNewProfile}
            className="flex min-h-14 w-full items-center gap-3 rounded-lg border border-dashed px-3 py-2 text-left text-sm text-muted-foreground outline-none hover:bg-accent/50 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-md border border-dashed">
              <Plus className="size-4" />
            </span>
            {t("displayProfiles.settings.makeProfile")}
          </button>
        </li>
      )}
    </ul>
  )
}
