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
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useIsMobile } from "@/hooks/useMediaQuery"

import { useNamedDisplayProfiles } from "./display-profile-names"
import { DisplayProfileEditor } from "./DisplayProfileEditor"
import { DisplayProfileList } from "./DisplayProfileList"
import { NewDisplayProfileDialog } from "./NewDisplayProfileDialog"
import { useReturnFocus } from "./use-return-focus"

/**
 * Settings > General > Display profiles: the profiles in a list next to an editor. Below 768 px only the list shows:
 * a tap on a row picks that profile for this browser, and a › opens the editor in a sheet.
 */
export function DisplayProfilesSettings() {
  const { t } = useTranslation("torrents")
  const { profiles, active, settled, pick } = useNamedDisplayProfiles()
  const narrow = useIsMobile()
  const [selectedId, setSelectedId] = useState(active.id)
  // The sheet keeps its profile after it closes, so the editor stays in the sheet while it slides out.
  const [sheet, setSheet] = useState<{ profileId: string; open: boolean } | null>(null)
  const editReturnFocus = useReturnFocus()
  const [newProfileOpen, setNewProfileOpen] = useState(false)
  // A deleted profile leaves the editor on the profile of this browser, and closes the sheet.
  const shown = profiles.find((profile) => profile.id === selectedId) ?? active
  const sheetProfile = profiles.find((profile) => profile.id === sheet?.profileId)

  return (
    <Card>
      <CardHeader className="max-sm:has-data-[slot=card-action]:grid-cols-1">
        <CardTitle>{t("displayProfiles.settings.title")}</CardTitle>
        <CardDescription className="flex items-center gap-1.5">
          <span>{t("displayProfiles.settings.holds")}</span>
          <FieldHelp>{t("displayProfiles.settings.holdsHelp")}</FieldHelp>
        </CardDescription>
        <CardAction className="max-sm:col-start-1 max-sm:row-span-1 max-sm:row-start-3 max-sm:mt-2 max-sm:justify-self-stretch">
          <Button size="sm" variant="outline" className="max-sm:h-10 max-sm:w-full" disabled={!settled} onClick={() => setNewProfileOpen(true)}>
            <Plus />
            {t("displayProfiles.newProfile")}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="grid items-start gap-4 md:grid-cols-[minmax(12rem,15rem)_minmax(0,1fr)]">
        {narrow ? (
          <DisplayProfileList
            selectedId={active.id}
            rowsDisabled={!settled}
            onSelect={(profile) => pick(profile.id)}
            onEdit={(profile) => setSheet({ profileId: profile.id, open: true })}
            onNewProfile={() => setNewProfileOpen(true)}
          />
        ) : (
          <>
            <DisplayProfileList selectedId={shown.id} onSelect={(profile) => setSelectedId(profile.id)} onNewProfile={() => setNewProfileOpen(true)} />
            <DisplayProfileEditor key={shown.id} profile={shown} />
          </>
        )}
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
      <Sheet
        open={narrow && sheet?.open === true && sheetProfile !== undefined}
        onOpenChange={(open) => !open && setSheet((current) => current && { ...current, open: false })}
      >
        {/* Until the first read of the server ends, the first control that takes focus is a FieldHelp, whose tooltip
            would open on each open. Focus the sheet itself so the focus trap keeps an anchor. */}
        <SheetContent
          side="bottom"
          aria-describedby={undefined}
          className="max-h-[90dvh] gap-0 overflow-y-auto pb-[calc(1rem+env(safe-area-inset-bottom))]"
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            editReturnFocus.remember()
            const sheet = event.currentTarget as HTMLElement
            sheet.focus()
          }}
          onCloseAutoFocus={editReturnFocus.restore}
        >
          <SheetHeader className="pb-2">
            <SheetTitle>{t("displayProfiles.settings.editTitle")}</SheetTitle>
          </SheetHeader>
          {sheetProfile && <div className="px-4"><DisplayProfileEditor key={sheetProfile.id} profile={sheetProfile} /></div>}
        </SheetContent>
      </Sheet>
    </Card>
  )
}
