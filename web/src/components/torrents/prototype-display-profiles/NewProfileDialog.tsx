/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the New profile dialog. Mounted once by the overlay;
// the picker, Settings, and the phone sheet open it through the store.

import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import { IconPicker } from "./IconPicker"
import { guessIcon, iconEntry, ProfileTile, type ProfileColor } from "./look"
import { actions, NAME_MAX, useActiveProfile, usePrototype, validateName } from "./store"

let returnFocusTo: HTMLElement | null = null

/** Opens the dialog; on close, focus goes back to the given element (the chip, for example). */
export function openNewProfileDialog(from?: HTMLElement | null) {
  returnFocusTo = from ?? null
  actions.setNewProfileOpen(true)
}

function NewProfileBody() {
  const active = useActiveProfile()
  const [name, setName] = useState("")
  const [touched, setTouched] = useState(false)
  // null: the icon still follows the name.
  const [pickedIcon, setPickedIcon] = useState<string | null>(null)
  const [color, setColor] = useState<ProfileColor>("none")

  const icon = pickedIcon ?? guessIcon(name)
  const error = validateName(name)
  const shownError = name.trim() || touched ? error : null
  const length = name.trim().length

  const submit = () => {
    setTouched(true)
    if (error) return
    actions.createFromActive(name, { icon, color })
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
      className="flex flex-col gap-4"
    >
      <DialogHeader>
        <DialogTitle>New profile</DialogTitle>
        <DialogDescription>
          Copies <span className="font-medium text-foreground">{active.name}</span>: columns, density, sidebars, and filters.
          This browser then uses it.
        </DialogDescription>
      </DialogHeader>

      <div className="flex items-start gap-2">
        <IconPicker icon={icon} color={color} onIconChange={setPickedIcon} onColorChange={setColor}>
          <button
            type="button"
            aria-label={`Icon: ${iconEntry(icon)?.label ?? icon}. Change icon and color`}
            className="shrink-0 rounded-lg outline-none ring-1 ring-border hover:ring-foreground/40 focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <ProfileTile profile={{ icon, color, name: name || "?" }} size="lg" />
          </button>
        </IconPicker>
        <div className="min-w-0 flex-1">
          <Input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Profile name, e.g. Workstation"
            aria-label="Profile name"
            aria-invalid={shownError ? true : undefined}
            aria-describedby="new-profile-name-msg"
            className="h-10"
          />
          <div id="new-profile-name-msg" className="mt-1 flex gap-2 text-xs leading-4">
            <span className={cn("min-w-0 flex-1", shownError ? "text-destructive" : "text-muted-foreground")}>
              {shownError}
            </span>
            <span className={cn("shrink-0 tabular-nums", length > NAME_MAX ? "text-destructive" : "text-muted-foreground")}>
              {length}/{NAME_MAX}
            </span>
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={() => actions.setNewProfileOpen(false)}>Cancel</Button>
        <Button type="submit" disabled={!name.trim()}>Create</Button>
      </DialogFooter>
    </form>
  )
}

export function NewProfileDialog() {
  const open = usePrototype((s) => s.newProfileOpen)
  return (
    <Dialog open={open} onOpenChange={actions.setNewProfileOpen}>
      <DialogContent
        className="sm:max-w-md"
        data-prototype-new-profile=""
        onCloseAutoFocus={(e) => {
          if (returnFocusTo?.isConnected) {
            e.preventDefault()
            returnFocusTo.focus()
          }
          returnFocusTo = null
        }}
      >
        {/* Radix unmounts the content once it is closed, so a cancelled draft does not come back. */}
        <NewProfileBody />
      </DialogContent>
    </Dialog>
  )
}
