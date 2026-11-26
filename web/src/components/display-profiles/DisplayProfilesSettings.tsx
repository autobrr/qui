/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { Palette, Plus } from "lucide-react"
import { useState } from "react"
import { Trans, useTranslation } from "react-i18next"

import { Editor } from "@/components/kit/Editor"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { useIsMobile } from "@/hooks/useMediaQuery"

import { useNamedDisplayProfiles } from "./display-profile-names"
import { DisplayProfileEditor } from "./DisplayProfileEditor"
import { DisplayProfileList } from "./DisplayProfileList"
import { NewDisplayProfileDialog } from "./NewDisplayProfileDialog"

export function DisplayProfilesSettings() {
  const { t } = useTranslation("torrents")
  const { profiles, active, settled, pick } = useNamedDisplayProfiles()
  const narrow = useIsMobile()
  const [selectedId, setSelectedId] = useState(active.id)
  const [sheet, setSheet] = useState<{ profileId: string; open: boolean } | null>(null)
  const [newProfileOpen, setNewProfileOpen] = useState(false)
  const shown = profiles.find((profile) => profile.id === selectedId) ?? active
  const sheetProfile = profiles.find((profile) => profile.id === sheet?.profileId)

  return (
    <>
      <Section
        title={t("displayProfiles.settings.title")}
        description={t("displayProfiles.settings.holds")}
        help={t("displayProfiles.settings.holdsHelp")}
        actions={
          <Button size="sm" variant="outline" disabled={!settled} onClick={() => setNewProfileOpen(true)}>
            <Plus />
            {t("displayProfiles.newProfile")}
          </Button>
        }
        footer={
          <>
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
          </>
        }
      >
        <div className="grid items-start gap-4 md:grid-cols-[minmax(12rem,15rem)_minmax(0,1fr)]">
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
        </div>
      </Section>
      <NewDisplayProfileDialog open={newProfileOpen} onOpenChange={setNewProfileOpen} onCreated={setSelectedId} />
      <Editor
        open={narrow && sheet?.open === true && sheetProfile !== undefined}
        onOpenChange={(open) => !open && setSheet((current) => current && { ...current, open: false })}
        title={t("displayProfiles.settings.editTitle")}
      >
        {sheetProfile && <DisplayProfileEditor key={sheetProfile.id} profile={sheetProfile} />}
      </Editor>
    </>
  )
}
