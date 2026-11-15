/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway). The Settings › General › "Display profiles" section. The
// demo redirects /settings, so "Manage profiles…" opens it in a dialog instead.

import { Check, ChevronRight, Pencil, Plus, Trash2, X } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import { NewProfileForm } from "./NewProfileForm"
import { actions, summarize, usePrototype, validateName, type DisplayProfile } from "./store"

function RenameField({ profile, onDone }: { profile: DisplayProfile; onDone: () => void }) {
  const [name, setName] = useState(profile.name)
  const error = name.trim() === profile.name ? null : validateName(name, profile.id)
  return (
    <form
      className="flex min-w-0 flex-1 flex-col gap-1"
      onSubmit={(e) => {
        e.preventDefault()
        if (error) return
        if (name.trim() !== profile.name) actions.rename(profile.id, name)
        onDone()
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault()
          e.stopPropagation()
          onDone()
        }
      }}
    >
      <div className="flex items-center gap-1">
        <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} aria-label={`Rename ${profile.name}`} className="h-8" />
        <Button type="submit" size="icon" variant="ghost" className="h-8 w-8" aria-label="Save name" disabled={!!error}>
          <Check className="h-4 w-4" />
        </Button>
        <Button type="button" size="icon" variant="ghost" className="h-8 w-8" aria-label="Cancel" onClick={onDone}>
          <X className="h-4 w-4" />
        </Button>
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}
    </form>
  )
}

function ProfileRow({ profile, isActive }: { profile: DisplayProfile; isActive: boolean }) {
  const [mode, setMode] = useState<"view" | "rename" | "confirm-delete">("view")

  if (mode === "confirm-delete") {
    return (
      <li className="flex flex-wrap items-center gap-3 rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2.5">
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">Delete {profile.name}?</p>
          <p className="text-xs text-muted-foreground">
            {isActive ? "This browser moves to Default. " : ""}Other browsers that use it also move to Default.
          </p>
        </div>
        <div className="flex gap-1">
          <Button size="sm" variant="ghost" onClick={() => setMode("view")}>Cancel</Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => {
              actions.remove(profile.id)
              setMode("view")
            }}
          >
            Delete
          </Button>
        </div>
      </li>
    )
  }

  return (
    <li className={cn("flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border px-3 py-2.5", isActive && "border-primary/40 bg-primary/[0.04]")}>
      <div className="flex min-w-[12rem] flex-1 items-center gap-2">
        {mode === "rename" ? (
          <RenameField profile={profile} onDone={() => setMode("view")} />
        ) : (
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate text-sm font-medium">{profile.name}</span>
              {isActive && (
                <span className="shrink-0 rounded-full bg-primary/15 px-2 py-0.5 text-[11px] font-medium text-primary">This browser</span>
              )}
              {profile.isDefault && (
                <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">Default</span>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {summarize(profile.setup)}
              {profile.copiedFrom ? ` · copied from ${profile.copiedFrom}` : ""}
            </p>
          </div>
        )}
      </div>

      {mode === "view" && (
        <div className="flex items-center gap-3">
          {profile.isDefault ? (
            <span className="w-[11.5rem] text-right text-xs text-muted-foreground">Always shares sort and filters</span>
          ) : (
            <label className="flex w-[11.5rem] cursor-pointer items-center justify-end gap-2 text-xs">
              Own sort and filters
              <Switch checked={profile.ownSortAndFilters} onCheckedChange={(on) => actions.setOwnSortAndFilters(profile.id, on)} />
            </label>
          )}
          <div className="flex items-center gap-0.5">
            {isActive ? (
              <span className="w-[6.5rem]" />
            ) : (
              <Button size="sm" variant="outline" className="h-8 w-[6.5rem]" onClick={() => actions.pick(profile.id)}>
                Use here
              </Button>
            )}
            <Button size="icon" variant="ghost" className="h-8 w-8" aria-label={`Rename ${profile.name}`} onClick={() => setMode("rename")}>
              <Pencil className="h-4 w-4" />
            </Button>
            {profile.isDefault ? (
              <span className="w-8" />
            ) : (
              <Button
                size="icon"
                variant="ghost"
                className="h-8 w-8 text-muted-foreground hover:text-destructive"
                aria-label={`Delete ${profile.name}`}
                onClick={() => setMode("confirm-delete")}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            )}
          </div>
        </div>
      )}
    </li>
  )
}

export function DisplayProfilesSettingsCard() {
  const profiles = usePrototype((s) => s.profiles)
  const activeId = usePrototype((s) => s.activeProfileId)
  const [creating, setCreating] = useState(false)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Display profiles</CardTitle>
        <CardDescription>
          A profile holds the columns, their order and widths, row density, sidebars, and filter sections. Each browser picks
          one; a browser that never picked one uses Default.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <ul className="flex flex-col gap-2">
          {profiles.map((profile) => (
            <ProfileRow key={profile.id} profile={profile} isActive={profile.id === activeId} />
          ))}
        </ul>
        {creating ? (
          <div className="rounded-lg border border-dashed p-3">
            <p className="pb-1.5 text-sm font-medium">New profile from this setup</p>
            <NewProfileForm onDone={() => setCreating(false)} />
          </div>
        ) : (
          <Button variant="outline" size="sm" className="self-start" onClick={() => setCreating(true)}>
            <Plus className="h-4 w-4" />
            New profile from this setup
          </Button>
        )}
      </CardContent>
    </Card>
  )
}

export function ManageProfilesDialog() {
  const open = usePrototype((s) => s.manageOpen)
  return (
    <Dialog open={open} onOpenChange={actions.setManageOpen}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-1 text-sm font-normal text-muted-foreground">
            Settings <ChevronRight className="h-3.5 w-3.5" /> General <ChevronRight className="h-3.5 w-3.5" />
            <span className="text-foreground">Display profiles</span>
          </DialogTitle>
          <DialogDescription className="sr-only">Prototype of the Display profiles settings section.</DialogDescription>
        </DialogHeader>
        <DisplayProfilesSettingsCard />
      </DialogContent>
    </Dialog>
  )
}
