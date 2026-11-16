/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId, useRef, useState, type FormEvent, type RefObject } from "react"
import { Trans, useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useDisplayProfiles } from "@/lib/client-settings"
import {
  DISPLAY_PROFILE_NAME_MAX_LENGTH,
  guessDisplayProfileIcon,
  validateDisplayProfileName,
  type DisplayProfileColor,
  type DisplayProfileNameError
} from "@/lib/display-profiles"
import { cn } from "@/lib/utils"

import { findDisplayProfileIcon } from "./display-profile-icons"
import { DisplayProfileIconPicker } from "./DisplayProfileIconPicker"
import { DisplayProfileTile } from "./DisplayProfileTile"

interface NewDisplayProfileDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Called with the id of the new profile, after this browser starts to use it. */
  onCreated?: (profileId: string) => void
}

/** Makes a profile from the setup of this browser, and this browser then uses it. */
export function NewDisplayProfileDialog({ open, onOpenChange, onCreated }: NewDisplayProfileDialogProps) {
  const nameRef = useRef<HTMLInputElement>(null)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="sm:max-w-md"
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          nameRef.current?.focus()
        }}
      >
        {/* The content unmounts on close, so a cancelled draft does not come back. */}
        <NewDisplayProfileForm
          nameRef={nameRef}
          onCreated={(profileId) => {
            onOpenChange(false)
            onCreated?.(profileId)
          }}
          onCancel={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  )
}

interface NewDisplayProfileFormProps {
  nameRef: RefObject<HTMLInputElement | null>
  onCreated: (profileId: string) => void
  onCancel: () => void
}

function NewDisplayProfileForm({ nameRef, onCreated, onCancel }: NewDisplayProfileFormProps) {
  const { t } = useTranslation("torrents")
  const { profiles, active, create } = useDisplayProfiles()
  const [name, setName] = useState("")
  const [submitted, setSubmitted] = useState(false)
  // null while the icon still follows the name.
  const [pickedIcon, setPickedIcon] = useState<string | null>(null)
  const [color, setColor] = useState<DisplayProfileColor | null>(null)
  const nameId = useId()
  const messageId = useId()

  const icon = pickedIcon ?? guessDisplayProfileIcon(name)
  const iconLabelKey = findDisplayProfileIcon(icon)?.labelKey
  const error = validateDisplayProfileName(name, profiles)
  const shownError = name.trim() !== "" || submitted ? error : null
  const length = [...name.trim()].length

  const nameErrorText: Record<DisplayProfileNameError, string> = {
    empty: t("displayProfiles.nameErrors.empty"),
    tooLong: t("displayProfiles.nameErrors.tooLong", { max: DISPLAY_PROFILE_NAME_MAX_LENGTH }),
    duplicate: t("displayProfiles.nameErrors.duplicate"),
  }

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault()
    setSubmitted(true)
    if (error) return
    onCreated(create(name, { icon, color }))
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t("displayProfiles.newDialog.title")}</DialogTitle>
        <DialogDescription>
          <Trans
            t={t}
            i18nKey="displayProfiles.newDialog.copies"
            values={{ name: active.name }}
            components={{ profile: <span className="font-medium text-foreground" /> }}
          />
        </DialogDescription>
      </DialogHeader>

      <div className="flex items-start gap-2">
        <DisplayProfileIconPicker icon={icon} color={color} onIconChange={setPickedIcon} onColorChange={setColor}>
          <button
            type="button"
            aria-label={t("displayProfiles.iconPicker.trigger", { icon: iconLabelKey ? t(iconLabelKey) : icon })}
            className="mt-6 shrink-0 rounded-lg outline-none ring-1 ring-border hover:ring-foreground/40 focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <DisplayProfileTile profile={{ icon, color, name }} size="lg" />
          </button>
        </DisplayProfileIconPicker>
        <div className="min-w-0 flex-1 space-y-1.5">
          <Label htmlFor={nameId}>{t("displayProfiles.newDialog.name")}</Label>
          <Input
            ref={nameRef}
            id={nameId}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={t("displayProfiles.newDialog.namePlaceholder")}
            aria-invalid={shownError ? true : undefined}
            aria-describedby={messageId}
            className="h-10"
          />
          <div id={messageId} className="flex gap-2 text-xs leading-4">
            <span className={cn("min-w-0 flex-1", shownError ? "text-destructive" : "text-muted-foreground")}>
              {shownError && nameErrorText[shownError]}
            </span>
            <span className={cn("shrink-0 tabular-nums", length > DISPLAY_PROFILE_NAME_MAX_LENGTH ? "text-destructive" : "text-muted-foreground")}>
              {t("displayProfiles.newDialog.nameCount", { length, max: DISPLAY_PROFILE_NAME_MAX_LENGTH })}
            </span>
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>{t("displayProfiles.newDialog.cancel")}</Button>
        <Button type="submit">{t("displayProfiles.newDialog.create")}</Button>
      </DialogFooter>
    </form>
  )
}
