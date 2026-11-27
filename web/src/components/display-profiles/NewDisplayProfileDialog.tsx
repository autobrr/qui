/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId, useRef, useState, type FormEvent, type RefObject } from "react"
import { Trans, useTranslation } from "react-i18next"

import { useReturnFocus } from "@/components/kit/use-return-focus"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { guessDisplayProfileIcon, type DisplayProfileColor } from "@/lib/display-profiles"
import { cn } from "@/lib/utils"

import { findDisplayProfileIcon } from "./display-profile-icons"
import { useDisplayProfileNameCheck, useNamedDisplayProfiles } from "./display-profile-names"
import { DisplayProfileIconPicker } from "./DisplayProfileIconPicker"
import { DisplayProfileTile } from "./DisplayProfileTile"

interface NewDisplayProfileDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: (profileId: string) => void
  returnFocusRef?: RefObject<HTMLElement | null>
}

export function NewDisplayProfileDialog({ open, onOpenChange, onCreated, returnFocusRef }: NewDisplayProfileDialogProps) {
  const nameRef = useRef<HTMLInputElement>(null)
  const returnFocus = useReturnFocus(returnFocusRef)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="sm:max-w-md"
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          returnFocus.remember()
          nameRef.current?.focus()
        }}
        onCloseAutoFocus={returnFocus.restore}
      >
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
  const { active, settled, create } = useNamedDisplayProfiles()
  const [name, setName] = useState("")
  const [submitted, setSubmitted] = useState(false)
  const [pickedIcon, setPickedIcon] = useState<string | null>(null)
  const [color, setColor] = useState<DisplayProfileColor | null>(null)
  const nameId = useId()
  const messageId = useId()

  const icon = pickedIcon ?? guessDisplayProfileIcon(name)
  const iconLabelKey = findDisplayProfileIcon(icon)!.labelKey
  const { error, shownError, errorTexts, count } = useDisplayProfileNameCheck(name, submitted)

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault()
    setSubmitted(true)
    if (error || !settled) return
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
            aria-label={t("displayProfiles.iconPicker.trigger", { icon: t(iconLabelKey) })}
            className="mt-6 shrink-0 rounded-lg outline-none ring-1 ring-border hover:ring-foreground/40 focus-visible:ring-3 focus-visible:ring-ring/50"
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
              {shownError && errorTexts[shownError]}
            </span>
            <span className={cn("shrink-0 tabular-nums", error === "tooLong" ? "text-destructive" : "text-muted-foreground")}>
              {count}
            </span>
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>{t("displayProfiles.newDialog.cancel")}</Button>
        <Button type="submit" disabled={!settled}>{t("displayProfiles.newDialog.create")}</Button>
      </DialogFooter>
    </form>
  )
}
