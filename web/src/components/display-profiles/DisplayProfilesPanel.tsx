/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Pencil, Trash2 } from "lucide-react"
import { useState, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { useInstances } from "@/hooks/useInstances"
import { useDisplayProfiles } from "@/lib/client-settings"
import {
  DISPLAY_PROFILE_NAME_MAX_LENGTH,
  validateDisplayProfileName,
  type DisplayProfile,
  type DisplayProfileNameError
} from "@/lib/display-profiles"

import { DisplayProfileIconPicker } from "./DisplayProfileIconPicker"
import { DisplayProfileTile } from "./DisplayProfileTile"
import { SameColumnsSwitch } from "./SameColumnsSwitch"
import { OwnSortAndFiltersSetting } from "./OwnSortAndFiltersSetting"

interface DisplayProfilesPanelProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onNewProfile: () => void
}

type NameErrorText = Record<DisplayProfileNameError, string>

/** Lists the Display profiles, picks, renames, restyles, and deletes one, and opens the New profile dialog. */
export function DisplayProfilesPanel({ open, onOpenChange, onNewProfile }: DisplayProfilesPanelProps) {
  const { t } = useTranslation("torrents")
  const { profiles, active, pick, rename, delete: deleteProfile, setIcon, setColor } = useDisplayProfiles()
  const { instances } = useInstances()
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<DisplayProfile | null>(null)

  const nameErrorText: NameErrorText = {
    empty: t("displayProfiles.nameErrors.empty"),
    tooLong: t("displayProfiles.nameErrors.tooLong", { max: DISPLAY_PROFILE_NAME_MAX_LENGTH }),
    duplicate: t("displayProfiles.nameErrors.duplicate"),
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="sm:max-w-md"
        onEscapeKeyDown={(event) => {
          // Esc in an open rename cancels the rename and keeps the panel open.
          if (renamingId === null) return
          event.preventDefault()
          setRenamingId(null)
        }}
      >
        <DialogHeader>
          <DialogTitle>{t("displayProfiles.panel.title")}</DialogTitle>
          <DialogDescription>{t("displayProfiles.panel.description")}</DialogDescription>
        </DialogHeader>
        <ul className="divide-y rounded-md border">
          {profiles.map((profile) => (
            <li key={profile.id} className="flex min-h-11 items-center justify-between gap-2 px-3 py-2 text-sm">
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
              {renamingId === profile.id ? (
                <RenameField
                  profile={profile}
                  profiles={profiles}
                  nameErrorText={nameErrorText}
                  onSave={(newName) => {
                    rename(profile.id, newName)
                    setRenamingId(null)
                  }}
                  onCancel={() => setRenamingId(null)}
                />
              ) : (
                <>
                  <span className="min-w-0 flex-1 truncate" title={profile.name}>{profile.name}</span>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7"
                      aria-label={t("displayProfiles.panel.renameAriaLabel", { name: profile.name })}
                      onClick={() => setRenamingId(profile.id)}
                    >
                      <Pencil className="size-3.5" />
                    </Button>
                    {!profile.isDefault && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-7"
                        aria-label={t("displayProfiles.panel.deleteAriaLabel", { name: profile.name })}
                        onClick={() => setDeleting(profile)}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                    {profile.id === active.id ? (
                      <span className="text-xs text-muted-foreground">{t("displayProfiles.panel.thisBrowser")}</span>
                    ) : (
                      <Button variant="outline" size="sm" className="h-7" onClick={() => pick(profile.id)}>
                        {t("displayProfiles.panel.use")}
                      </Button>
                    )}
                  </div>
                </>
              )}
            </li>
          ))}
        </ul>
        <SameColumnsSwitch profileId={active.id} instances={instances ?? []} />
        <OwnSortAndFiltersSetting profile={active} />
        <Button variant="outline" onClick={onNewProfile}>{t("displayProfiles.newProfile")}</Button>
      </DialogContent>
      <AlertDialog open={deleting !== null} onOpenChange={(isOpen) => !isOpen && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("displayProfiles.panel.deleteTitle", { name: deleting?.name })}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("displayProfiles.panel.deleteDescription", { defaultName: profiles[0].name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common:actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => {
                if (deleting) deleteProfile(deleting.id)
                setDeleting(null)
              }}
            >
              {t("displayProfiles.panel.deleteConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  )
}

interface RenameFieldProps {
  profile: DisplayProfile
  profiles: readonly DisplayProfile[]
  nameErrorText: NameErrorText
  onSave: (name: string) => void
  onCancel: () => void
}

/** Inline rename: Enter saves. Esc (handled by the panel) and a click elsewhere cancel. */
function RenameField({ profile, profiles, nameErrorText, onSave, onCancel }: RenameFieldProps) {
  const { t } = useTranslation("torrents")
  const [value, setValue] = useState(profile.name)
  const [error, setError] = useState<DisplayProfileNameError | null>(null)

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter") return
    event.preventDefault()
    const nextError = validateDisplayProfileName(value, profiles, profile.id)
    setError(nextError)
    if (!nextError) onSave(value)
  }

  return (
    <div className="min-w-0 flex-1 space-y-1">
      <Input
        autoFocus
        className="h-7"
        value={value}
        aria-label={t("displayProfiles.panel.renameAriaLabel", { name: profile.name })}
        aria-invalid={error !== null}
        onChange={(event) => {
          setValue(event.target.value)
          setError(null)
        }}
        onKeyDown={handleKeyDown}
        onBlur={onCancel}
      />
      {error && <p className="text-xs text-destructive">{nameErrorText[error]}</p>}
    </div>
  )
}
