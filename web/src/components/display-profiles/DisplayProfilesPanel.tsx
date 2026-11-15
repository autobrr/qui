/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useDisplayProfiles } from "@/lib/client-settings"
import { DISPLAY_PROFILE_NAME_MAX_LENGTH, validateDisplayProfileName, type DisplayProfileNameError } from "@/lib/display-profiles"

interface DisplayProfilesPanelProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Lists the Display profiles, picks one for this browser, and creates one from the current setup. */
export function DisplayProfilesPanel({ open, onOpenChange }: DisplayProfilesPanelProps) {
  const { t } = useTranslation("torrents")
  const { profiles, active, pick, create } = useDisplayProfiles()
  const [name, setName] = useState("")
  const [nameError, setNameError] = useState<DisplayProfileNameError | null>(null)
  const nameId = useId()

  const nameErrorText: Record<DisplayProfileNameError, string> = {
    empty: t("displayProfiles.nameErrors.empty"),
    tooLong: t("displayProfiles.nameErrors.tooLong", { max: DISPLAY_PROFILE_NAME_MAX_LENGTH }),
    duplicate: t("displayProfiles.nameErrors.duplicate"),
  }

  const handleCreate = (event: FormEvent) => {
    event.preventDefault()
    const error = validateDisplayProfileName(name, profiles)
    setNameError(error)
    if (error) return
    create(name)
    setName("")
  }

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
              <span className="truncate" title={profile.name}>{profile.name}</span>
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
        <form onSubmit={handleCreate} className="space-y-2">
          <Label htmlFor={nameId}>{t("displayProfiles.panel.newName")}</Label>
          <div className="flex gap-2">
            <Input
              id={nameId}
              value={name}
              aria-invalid={nameError !== null}
              onChange={(event) => {
                setName(event.target.value)
                setNameError(null)
              }}
            />
            <Button type="submit">{t("displayProfiles.panel.create")}</Button>
          </div>
          {nameError && <p className="text-sm text-destructive">{nameErrorText[nameError]}</p>}
        </form>
      </DialogContent>
    </Dialog>
  )
}
