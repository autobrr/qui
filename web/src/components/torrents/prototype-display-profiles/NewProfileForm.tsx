/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the inline name field behind "New profile from this setup…".

import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import { actions, sameColumns, useActiveProfile, validateName } from "./store"

interface NewProfileFormProps {
  onDone: () => void
  className?: string
}

export function NewProfileForm({ onDone, className }: NewProfileFormProps) {
  const active = useActiveProfile()
  const [name, setName] = useState("")
  const [touched, setTouched] = useState(false)
  const error = validateName(name)
  // Check a typed name live; an empty field shows its error only after Create.
  const showError = name.trim() || touched ? error : null

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
          aria-invalid={showError ? true : undefined}
          className="h-8 text-sm"
        />
        <Button type="submit" size="sm" disabled={!name.trim()}>Create</Button>
      </div>
      {showError ? (
        <p className="text-xs text-destructive">{showError}</p>
      ) : (
        <p className="text-xs text-muted-foreground">
          Copies {active.name}: {sameColumns(active) ? "the same columns on every instance" : "the columns of each instance"},{" "}
          {active.rowDensity.toLowerCase()} rows, the sidebars, and {active.ownSortAndFilters ? "its own sort and filters" : "shared sort and filters"}.
          This browser then uses the copy.
        </p>
      )}
    </form>
  )
}
