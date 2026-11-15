/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the inline name field behind "New profile from this setup…".

import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import { actions, summarize, useActiveProfile, validateName } from "./store"

interface NewProfileFormProps {
  onDone: () => void
  className?: string
}

export function NewProfileForm({ onDone, className }: NewProfileFormProps) {
  const active = useActiveProfile()
  const [name, setName] = useState("")
  const [touched, setTouched] = useState(false)
  const error = validateName(name)

  const submit = () => {
    setTouched(true)
    if (error) return
    actions.createFromActive(name)
    onDone()
  }

  return (
    <form
      className={cn("flex flex-col gap-1.5", className)}
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
      // Menus read letter keys for typeahead and arrows for focus; keep them in the field.
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault()
          onDone()
        }
        e.stopPropagation()
      }}
    >
      <div className="flex items-center gap-1.5">
        <Input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Profile name, e.g. Workstation"
          aria-label="New profile name"
          aria-invalid={touched && error ? true : undefined}
          className="h-8 text-sm"
        />
        <Button type="submit" size="sm" disabled={!name.trim()}>Create</Button>
      </div>
      {touched && error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : (
        <p className="text-xs text-muted-foreground">
          Copies {active.name}: {summarize(active.setup)}.
        </p>
      )}
    </form>
  )
}
