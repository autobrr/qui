/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): Settings › General › Display profiles. Each row has a
// fixed height at each width: rename, its error, the delete confirm, and the
// switch warnings replace content in place, so the section never changes height.

import { Link } from "@tanstack/react-router"
import { Check, Globe, Palette, Pencil, Plus, Trash2, X } from "lucide-react"
import { useState, type ReactNode } from "react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import { ProfileName } from "./ProfileName"
import {
  actions,
  instanceLabel,
  NAME_MAX,
  PROTO_INSTANCES,
  sameColumns,
  summarize,
  useActiveProfile,
  usePrototype,
  validateName,
  type DisplayProfile
} from "./store"

const instanceList = PROTO_INSTANCES.map((i) => (i.id === 0 ? "the Unified view" : i.name)).join(", ").replace(/, ([^,]*)$/, ", and $1")

function SwitchCell({ id, label, help, children }: { id: string; label: ReactNode; help: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex h-[5.75rem] items-start gap-3 px-3 py-3 sm:h-[4.75rem]">
      <div className="min-w-0 flex-1">
        <label htmlFor={id} className="block h-5 truncate text-sm leading-5">{label}</label>
        <p className="mt-0.5 line-clamp-3 h-12 text-xs leading-4 text-muted-foreground [overflow-wrap:anywhere] sm:line-clamp-2 sm:h-8">{help}</p>
      </div>
      {children}
    </div>
  )
}

function UseHereSlot({ profile, isActive, className }: { profile: DisplayProfile; isActive: boolean; className?: string }) {
  if (isActive) {
    return (
      <span className={cn("inline-flex items-center justify-center gap-1 rounded-md bg-primary/10 text-xs font-medium text-primary", className)}>
        <Check className="h-3.5 w-3.5" />
        This browser
      </span>
    )
  }
  return (
    <Button size="sm" variant="outline" className={className} onClick={() => actions.pick(profile.id)}>
      Use here
    </Button>
  )
}

function DeleteConfirm({ profile, isActive, onCancel }: { profile: DisplayProfile; isActive: boolean; onCancel: () => void }) {
  return (
    <div className="flex h-full flex-col justify-center gap-3 bg-destructive/5 px-4 py-3 sm:flex-row sm:items-center">
      <div className="min-w-0 sm:flex-1">
        <p className="truncate text-sm font-medium">Delete {profile.name}?</p>
        <p className="mt-0.5 text-xs leading-4 text-muted-foreground">
          {isActive ? "This browser moves to Default. " : ""}Other browsers that use it also move to Default.
        </p>
      </div>
      <div className="flex shrink-0 gap-1.5">
        <Button size="sm" variant="ghost" className="flex-1 sm:flex-none" onClick={onCancel} autoFocus>Cancel</Button>
        <Button size="sm" variant="destructive" className="flex-1 sm:flex-none" onClick={() => actions.remove(profile.id)}>
          Delete
        </Button>
      </div>
    </div>
  )
}

function ProfileRow({ profile, isActive }: { profile: DisplayProfile; isActive: boolean }) {
  const [mode, setMode] = useState<"view" | "rename" | "confirm-delete">("view")
  const [draft, setDraft] = useState(profile.name)
  const lastInstanceId = usePrototype((s) => s.lastInstanceId)
  const same = sameColumns(profile)
  const own = !!profile.ownSortAndFilters

  const trimmed = draft.trim()
  const renameError = trimmed === profile.name ? null : validateName(draft, profile.id)
  const startRename = () => {
    setDraft(profile.name)
    setMode("rename")
  }
  const saveRename = () => {
    if (renameError) return
    if (trimmed !== profile.name) actions.rename(profile.id, draft)
    setMode("view")
  }

  const switchId = (key: string) => `profile-${profile.id}-${key}`

  return (
    <li
      data-profile-row={profile.name}
      className={cn(
        "flex h-[21.5rem] flex-col overflow-hidden rounded-lg border sm:h-[10rem]",
        isActive && "border-primary/40",
        mode === "confirm-delete" && "border-destructive/40"
      )}
    >
      {mode === "confirm-delete" ? (
        <DeleteConfirm profile={profile} isActive={isActive} onCancel={() => setMode("view")} />
      ) : (
        <>
          {/* Name, summary, and actions: 2 lines on desktop, the summary may take 2 on phones. */}
          <div className={cn("flex min-h-0 flex-1 flex-col justify-center gap-1 px-3 py-3", isActive && "bg-primary/[0.04]")}>
            <div className="flex h-8 items-center gap-2">
              {mode === "rename" ? (
                <form
                  className="flex min-w-0 flex-1 items-center gap-1"
                  onSubmit={(e) => {
                    e.preventDefault()
                    saveRename()
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") {
                      e.preventDefault()
                      e.stopPropagation()
                      setMode("view")
                    }
                  }}
                >
                  <Input
                    autoFocus
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onFocus={(e) => e.currentTarget.select()}
                    aria-label={`Rename ${profile.name}`}
                    aria-invalid={renameError ? true : undefined}
                    aria-describedby={`${switchId("rename")}-msg`}
                    className="h-8 min-w-0 flex-1"
                  />
                  <Button type="submit" size="icon" variant="ghost" className="h-8 w-8 shrink-0" aria-label="Save name" disabled={!!renameError}>
                    <Check className="h-4 w-4" />
                  </Button>
                  <Button type="button" size="icon" variant="ghost" className="h-8 w-8 shrink-0" aria-label="Cancel rename" onClick={() => setMode("view")}>
                    <X className="h-4 w-4" />
                  </Button>
                </form>
              ) : (
                <>
                  <button
                    type="button"
                    onClick={startRename}
                    className="flex min-w-0 items-center gap-1.5 rounded text-left text-sm font-medium outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring/50"
                    title="Rename"
                  >
                    <ProfileName profile={profile} />
                  </button>
                  <div className="ml-auto flex shrink-0 items-center gap-0.5">
                    <UseHereSlot profile={profile} isActive={isActive} className="mr-1 hidden h-8 w-[7rem] sm:inline-flex" />
                    <Button size="icon" variant="ghost" className="h-8 w-8" aria-label={`Rename ${profile.name}`} onClick={startRename}>
                      <Pencil className="h-4 w-4" />
                    </Button>
                    {profile.isDefault ? (
                      <span className="w-8" aria-hidden="true" />
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
                </>
              )}
            </div>

            <p
              id={`${switchId("rename")}-msg`}
              className={cn(
                "line-clamp-2 h-8 text-xs leading-4 [overflow-wrap:anywhere] sm:line-clamp-1 sm:h-4",
                mode === "rename" && renameError ? "text-destructive" : "text-muted-foreground"
              )}
            >
              {mode === "rename" ? (
                renameError ?? (
                  <>
                    Enter saves, Esc cancels.{" "}
                    <span className={cn("tabular-nums", trimmed.length > NAME_MAX - 6 && "text-foreground")}>{trimmed.length}/{NAME_MAX}</span>
                  </>
                )
              ) : (
                <>
                  {summarize(profile)}
                  {profile.copiedFrom ? ` · copied from ${profile.copiedFrom}` : ""}
                </>
              )}
            </p>
          </div>

          <div className="grid shrink-0 divide-y border-t sm:grid-cols-2 sm:divide-x sm:divide-y-0">
            <SwitchCell
              id={switchId("same")}
              label="Same columns on every instance"
              help={same
                ? `One Column setup for ${instanceList}.`
                : `Each instance keeps its own. Turning this on gives every instance the ${instanceLabel(lastInstanceId)} columns.`}
            >
              <Switch
                id={switchId("same")}
                checked={same}
                onCheckedChange={(on) => actions.setSameColumns(profile.id, on, null)}
                className="mt-0.5"
              />
            </SwitchCell>
            {profile.isDefault ? (
              <SwitchCell
                id={switchId("own")}
                label={<span className="text-muted-foreground">Always shares sort and filters</span>}
                help="Default cannot keep its own. Make another profile for that."
              />
            ) : (
              <SwitchCell
                id={switchId("own")}
                label="Own sort and filters"
                help={own
                  ? `Turning this off deletes the own sort and filters of ${profile.name}.`
                  : "Off: shares sort and filters with every profile."}
              >
                <Switch
                  id={switchId("own")}
                  checked={own}
                  onCheckedChange={(on) => actions.setOwnSortAndFilters(profile.id, on)}
                  className="mt-0.5"
                />
              </SwitchCell>
            )}
          </div>

          {/* Phones have no table toolbar: they pick their profile here. */}
          <div className="flex h-[3.75rem] shrink-0 items-center border-t px-3 sm:hidden">
            <UseHereSlot profile={profile} isActive={isActive} className="h-9 w-full" />
          </div>
        </>
      )}
    </li>
  )
}

function NewProfileFooter() {
  const active = useActiveProfile()
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState("")
  const [touched, setTouched] = useState(false)
  const error = validateName(name)
  const shownError = name.trim() || touched ? error : null

  const close = () => {
    setCreating(false)
    setName("")
    setTouched(false)
  }

  return (
    <div className="flex h-[5.5rem] flex-col justify-center gap-1 rounded-lg border border-dashed px-3 sm:h-[4.5rem]">
      {creating ? (
        <form
          className="flex items-center gap-1.5"
          onSubmit={(e) => {
            e.preventDefault()
            setTouched(true)
            if (error) return
            actions.createFromActive(name)
            close()
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault()
              close()
            }
          }}
        >
          <Input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Profile name, e.g. Workstation"
            aria-label="New profile name"
            aria-invalid={shownError ? true : undefined}
            className="h-8 min-w-0 flex-1 text-sm"
          />
          <Button type="submit" size="sm" className="h-8" disabled={!name.trim()}>Create</Button>
          <Button type="button" size="icon" variant="ghost" className="h-8 w-8 shrink-0" aria-label="Cancel" onClick={close}>
            <X className="h-4 w-4" />
          </Button>
        </form>
      ) : (
        <Button variant="ghost" size="sm" className="h-8 self-start px-2" onClick={() => setCreating(true)}>
          <Plus className="h-4 w-4" />
          New profile from this setup
        </Button>
      )}
      <p className={cn("line-clamp-2 h-8 px-0.5 text-xs leading-4 [overflow-wrap:anywhere] sm:line-clamp-1 sm:h-4", shownError ? "text-destructive" : "text-muted-foreground")}>
        {shownError ?? `Copies ${active.name}, the profile this browser uses, and switches this browser to the copy.`}
      </p>
    </div>
  )
}

export function DisplayProfilesSettings() {
  const profiles = usePrototype((s) => s.profiles)
  const activeId = usePrototype((s) => s.activeProfileId)

  return (
    <Card data-prototype-settings-section="">
      <CardHeader className="px-4 sm:px-6">
        <CardTitle>Display profiles</CardTitle>
        <CardDescription>
          A profile holds how the torrent list looks: which columns show, their order and widths, Name stretch, row
          density, open sidebars, open filter sections, collapsed category trees, and whether empty filters show. Each
          browser picks one profile, and only that browser remembers the pick. A browser that never picked one, or a
          private window, uses Default.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 px-4 sm:px-6">
        <div className="flex flex-col gap-1.5 rounded-lg bg-muted/50 px-3 py-2.5 text-xs leading-4 text-muted-foreground">
          <p className="flex gap-2">
            <Globe className="mt-px h-3.5 w-3.5 shrink-0" />
            <span>
              Every profile shares the rest: language, units, date and time format, delete defaults, and the other
              settings on these pages.
            </span>
          </p>
          <p className="flex gap-2">
            <Palette className="mt-px h-3.5 w-3.5 shrink-0" />
            <span>
              Themes sync on their own, in{" "}
              <Link to="/settings" search={{ tab: "themes" }} className="font-medium text-foreground underline underline-offset-2 hover:text-primary">
                Theme sync
              </Link>
              .
            </span>
          </p>
        </div>

        <ul className="flex flex-col gap-2" aria-label="Display profiles">
          {profiles.map((profile) => (
            <ProfileRow key={profile.id} profile={profile} isActive={profile.id === activeId} />
          ))}
        </ul>

        <NewProfileFooter />
      </CardContent>
    </Card>
  )
}
