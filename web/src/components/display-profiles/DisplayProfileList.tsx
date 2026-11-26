/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check, ChevronRight, Funnel, Plus } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import type { DisplayProfile } from "@/lib/display-profiles"
import { cn } from "@/lib/utils"

import { useNamedDisplayProfiles } from "./display-profile-names"
import { DisplayProfileTile } from "./DisplayProfileTile"
import { OwnSortAndFiltersHelp } from "./OwnSortAndFiltersSetting"

interface DisplayProfileListProps {
  selectedId: string
  onSelect: (profile: DisplayProfile) => void
  onNewProfile: () => void
  onEdit?: (profile: DisplayProfile) => void
  rowsDisabled?: boolean
}

export function DisplayProfileList({ selectedId, onSelect, onNewProfile, onEdit, rowsDisabled = false }: DisplayProfileListProps) {
  const { t } = useTranslation("torrents")
  const { profiles, active, settled } = useNamedDisplayProfiles()

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
              disabled={rowsDisabled}
              onClick={() => onSelect(profile)}
              className="flex min-w-0 flex-1 items-center gap-3 self-stretch rounded-lg px-3 py-2 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"
            >
              <DisplayProfileTile profile={profile} size="md" />
              <span className="min-w-0 flex-1">
                <TruncatedText wrap openOnTap={false} className="block text-sm font-medium [overflow-wrap:anywhere] pointer-fine:truncate">{profile.name}</TruncatedText>
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
                  className={cn(
                    "grid size-8 shrink-0 place-items-center rounded-md text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50",
                    !onEdit && "mr-2"
                  )}
                >
                  <Funnel className="size-4" />
                </TooltipTrigger>
                <TooltipContent className="max-w-xs"><OwnSortAndFiltersHelp /></TooltipContent>
              </Tooltip>
            )}
            {onEdit && (
              <button
                type="button"
                aria-label={t("displayProfiles.settings.editAriaLabel", { name: profile.name })}
                onClick={() => onEdit(profile)}
                className="mr-1 grid size-11 shrink-0 place-items-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
              >
                <ChevronRight aria-hidden="true" className="size-5" />
              </button>
            )}
          </li>
        )
      })}
      {profiles.length === 1 && (
        <li>
          <button
            type="button"
            disabled={!settled}
            onClick={onNewProfile}
            className="flex min-h-14 w-full items-center gap-3 rounded-lg border border-dashed px-3 py-1.5 text-left text-sm text-muted-foreground outline-none hover:bg-accent/50 hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50"
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
