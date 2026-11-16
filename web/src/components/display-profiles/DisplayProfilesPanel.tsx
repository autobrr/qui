/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useDisplayProfiles } from "@/lib/client-settings"

import { DisplayProfileIconPicker } from "./DisplayProfileIconPicker"
import { DisplayProfileTile } from "./DisplayProfileTile"

interface DisplayProfilesPanelProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onNewProfile: () => void
}

/** Lists the Display profiles, picks one for this browser, and creates one from the current setup. */
export function DisplayProfilesPanel({ open, onOpenChange, onNewProfile }: DisplayProfilesPanelProps) {
  const { t } = useTranslation("torrents")
  const { profiles, active, pick, setIcon, setColor } = useDisplayProfiles()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("displayProfiles.panel.title")}</DialogTitle>
          <DialogDescription>{t("displayProfiles.panel.description")}</DialogDescription>
        </DialogHeader>
        <ul className="divide-y rounded-md border">
          {profiles.map((profile) => (
            <li key={profile.id} className="flex items-center justify-between gap-2 px-3 py-2 text-sm">
              <DisplayProfileIconPicker
                icon={profile.icon}
                color={profile.color}
                onIconChange={(icon) => setIcon(profile.id, icon)}
                onColorChange={(color) => setColor(profile.id, color)}
              >
                <button type="button" aria-label={t("displayProfiles.panel.changeIcon", { name: profile.name })} className="shrink-0 rounded-md">
                  <DisplayProfileTile profile={profile} size="sm" />
                </button>
              </DisplayProfileIconPicker>
              <span className="min-w-0 flex-1 truncate" title={profile.name}>{profile.name}</span>
              {profile.id === active.id ? (
                <span className="shrink-0 text-xs text-muted-foreground">{t("displayProfiles.panel.thisBrowser")}</span>
              ) : (
                <Button variant="outline" size="sm" className="h-7 shrink-0" onClick={() => pick(profile.id)}>
                  {t("displayProfiles.panel.use")}
                </Button>
              )}
            </li>
          ))}
        </ul>
        <Button variant="outline" onClick={onNewProfile}>{t("displayProfiles.newProfile")}</Button>
      </DialogContent>
    </Dialog>
  )
}
