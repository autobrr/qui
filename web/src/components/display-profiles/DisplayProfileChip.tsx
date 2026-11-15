/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AppWindow, ChevronDown } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { useDisplayProfiles } from "@/lib/client-settings"

import { DisplayProfilesPanel } from "./DisplayProfilesPanel"

/** The Display profile of this browser in the status bar of the torrent table, with a picker. */
export function DisplayProfileChip() {
  const { t } = useTranslation("torrents")
  const { profiles, active, pick } = useDisplayProfiles()
  const [panelOpen, setPanelOpen] = useState(false)

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            aria-label={t("displayProfiles.chipAriaLabel", { name: active.name })}
            className="h-6 px-2 text-xs text-muted-foreground hover:text-accent-foreground"
          >
            <AppWindow className="h-3 w-3" />
            <span className="max-w-[8rem] truncate">{active.name}</span>
            <ChevronDown className="h-3 w-3" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{t("displayProfiles.menuLabel")}</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={active.id} onValueChange={pick}>
            {profiles.map((profile) => (
              <DropdownMenuRadioItem key={profile.id} value={profile.id}>
                <span className="truncate" title={profile.name}>{profile.name}</span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setPanelOpen(true)}>{t("displayProfiles.manage")}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <DisplayProfilesPanel open={panelOpen} onOpenChange={setPanelOpen} />
    </>
  )
}
