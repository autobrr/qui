/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { Palette, Plus } from "lucide-react"
import { useState } from "react"
import { Trans, useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { useDisplayProfiles } from "@/lib/client-settings"

import { DisplayProfileEditor } from "./DisplayProfileEditor"
import { DisplayProfileList } from "./DisplayProfileList"
import { NewDisplayProfileDialog } from "./NewDisplayProfileDialog"

/** Settings > General > Display profiles: the profiles in a list next to an editor. */
export function DisplayProfilesSettings() {
  const { t } = useTranslation("torrents")
  const { profiles, active } = useDisplayProfiles()
  const [selectedId, setSelectedId] = useState(active.id)
  const [newProfileOpen, setNewProfileOpen] = useState(false)
  // A deleted profile leaves the editor on the profile of this browser.
  const shown = profiles.find((profile) => profile.id === selectedId) ?? active

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("displayProfiles.settings.title")}</CardTitle>
        <CardDescription className="flex items-center gap-1.5">
          <span>{t("displayProfiles.settings.holds")}</span>
          <FieldHelp>{t("displayProfiles.settings.holdsHelp")}</FieldHelp>
        </CardDescription>
        <CardAction>
          <Button size="sm" variant="outline" onClick={() => setNewProfileOpen(true)}>
            <Plus />
            {t("displayProfiles.newProfile")}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="grid items-start gap-4 md:grid-cols-[minmax(12rem,15rem)_minmax(0,1fr)]">
        <DisplayProfileList selectedId={shown.id} onSelect={(profile) => setSelectedId(profile.id)} onNewProfile={() => setNewProfileOpen(true)} />
        <DisplayProfileEditor key={shown.id} profile={shown} />
      </CardContent>
      <CardFooter className="gap-1.5 text-xs text-muted-foreground">
        <Palette aria-hidden="true" className="size-3.5 shrink-0" />
        <span>
          <Trans
            t={t}
            i18nKey="displayProfiles.settings.themesSync"
            components={{
              sync: <Link to="/settings" search={{ tab: "themes" }} hash="theme-sync" className="font-medium text-foreground underline underline-offset-2 hover:text-primary" />,
            }}
          />
        </span>
      </CardFooter>
      <NewDisplayProfileDialog open={newProfileOpen} onOpenChange={setNewProfileOpen} onCreated={setSelectedId} />
    </Card>
  )
}
