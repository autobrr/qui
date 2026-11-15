/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the profile editor of the Settings section, styled like
// the "In use" block of Themes. Its height does not change with its state:
// text that changes renders over invisible copies of every variant, so a slot
// is as tall as its longest variant at the current width. Text wraps; nothing
// is clamped.

import { Check, Pencil, X } from "lucide-react"
import { useId, useState, type ReactNode } from "react"

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
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import { DEFAULT_SHARES_HELP, HelpTip, OwnSortAndFiltersHelp } from "./HelpTip"
import { IconPicker } from "./IconPicker"
import { iconEntry, ProfileTile } from "./look"
import { ProfileName } from "./ProfileName"
import { ProfileThumbnail } from "./ProfileThumbnail"
import {
  actions,
  instanceLabel,
  NAME_MAX,
  PROTO_INSTANCES,
  sameColumns,
  useProfile,
  usePrototype,
  validateName,
  type DisplayProfile
} from "./store"

const instanceList = PROTO_INSTANCES.map((i) => (i.id === 0 ? "the Unified view" : i.name)).join(", ").replace(/, ([^,]*)$/, ", and $1")

/** Shows `shown`, in a box as tall as the tallest of `variants`. */
function StableText({ shown, variants, className }: { shown: ReactNode; variants: ReactNode[]; className?: string }) {
  return (
    <span className={cn("grid [&>*]:col-start-1 [&>*]:row-start-1", className)}>
      {variants.map((variant, i) => (
        <span key={i} aria-hidden="true" className="invisible">{variant}</span>
      ))}
      <span>{shown}</span>
    </span>
  )
}

function holdsLine(p: Pick<DisplayProfile, "rowDensity" | "sidebars">) {
  return `${p.rowDensity} rows · filter sidebar ${p.sidebars.filters} · details panel ${p.sidebars.details}`
}

function NameBlock({ profile }: { profile: DisplayProfile }) {
  const [renaming, setRenaming] = useState(false)
  const [draft, setDraft] = useState(profile.name)
  const longestOtherName = usePrototype((s) => s.profiles.filter((p) => p.id !== profile.id).reduce((a, p) => (p.name.length > a.length ? p.name : a), ""))
  const msgId = useId()

  const trimmed = draft.trim()
  const error = trimmed === profile.name ? null : validateName(draft, profile.id)
  const start = () => {
    setDraft(profile.name)
    setRenaming(true)
  }
  const save = () => {
    if (error) return
    if (trimmed !== profile.name) actions.rename(profile.id, draft)
    setRenaming(false)
  }

  const hint = <>Enter saves, Esc cancels.</>
  const view = <>{holdsLine(profile)}{profile.copiedFrom ? ` · copied from ${profile.copiedFrom}` : ""}</>

  return (
    <div className="min-w-0">
      <div className="flex h-12 items-center gap-3">
        <IconPicker
          icon={profile.icon}
          color={profile.color}
          onIconChange={(icon) => actions.setIcon(profile.id, icon)}
          onColorChange={(color) => actions.setColor(profile.id, color)}
        >
          <button
            type="button"
            aria-label={`Icon of ${profile.name}: ${iconEntry(profile.icon)?.label ?? "unknown"}. Change icon and color`}
            className="shrink-0 rounded-lg outline-none ring-1 ring-border hover:ring-foreground/40 focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <ProfileTile profile={profile} size="xl" />
          </button>
        </IconPicker>

        {renaming ? (
          <form
            className="flex min-w-0 flex-1 items-center gap-1"
            onSubmit={(e) => {
              e.preventDefault()
              save()
            }}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault()
                e.stopPropagation()
                setRenaming(false)
              }
            }}
          >
            <Input
              autoFocus
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onFocus={(e) => e.currentTarget.select()}
              aria-label={`Rename ${profile.name}`}
              aria-invalid={error ? true : undefined}
              aria-describedby={msgId}
              className="h-9 min-w-0 flex-1"
            />
            <Button type="submit" size="icon" variant="ghost" className="size-9 shrink-0" aria-label="Save name" disabled={!!error}>
              <Check className="size-4" />
            </Button>
            <Button type="button" size="icon" variant="ghost" className="size-9 shrink-0" aria-label="Cancel rename" onClick={() => setRenaming(false)}>
              <X className="size-4" />
            </Button>
          </form>
        ) : (
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <button
              type="button"
              onClick={start}
              title={profile.name}
              className="min-w-0 rounded text-left outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <ProfileName profile={profile} className="block text-lg font-semibold leading-tight" />
            </button>
            <Button size="icon" variant="ghost" className="size-8 shrink-0 text-muted-foreground" aria-label={`Rename ${profile.name}`} onClick={start}>
              <Pencil className="size-3.5" />
            </Button>
          </div>
        )}
      </div>

      <div id={msgId} className="mt-1.5 flex gap-2 text-xs leading-4">
        <StableText
          className={cn("min-w-0 flex-1 [overflow-wrap:anywhere]", renaming && error ? "text-destructive" : "text-muted-foreground")}
          shown={renaming ? (error ?? hint) : view}
          variants={[
            view,
            hint,
            `Use ${NAME_MAX} characters or fewer (now ${NAME_MAX + 10}).`,
            `${longestOtherName} already uses this name.`,
          ]}
        />
        <span className={cn("shrink-0 tabular-nums", !renaming && "invisible", trimmed.length > NAME_MAX ? "text-destructive" : "text-muted-foreground")} aria-hidden={!renaming}>
          {trimmed.length}/{NAME_MAX}
        </span>
      </div>
    </div>
  )
}

interface SwitchRowProps {
  id: string
  label: string
  /** Field help next to the label. */
  labelHelp?: ReactNode
  help: string
  helpVariants: string[]
  children: ReactNode
}

function SwitchRow({ id, label, labelHelp, help, helpVariants, children }: SwitchRowProps) {
  return (
    <div className="flex items-start gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <label htmlFor={id} className="text-sm leading-5">{label}</label>
          {labelHelp}
        </div>
        <StableText className="mt-0.5 text-xs leading-4 text-muted-foreground [overflow-wrap:anywhere]" shown={help} variants={helpVariants} />
      </div>
      {children}
    </div>
  )
}

function ownHelp(on: boolean, name: string) {
  return on ? `Turning this off deletes the own sort and filters of ${name}.` : "Off: shares sort and filters with every profile."
}

function sameHelp(on: boolean, lastInstanceId: number) {
  return on
    ? `One Column setup for ${instanceList}.`
    : `Each instance keeps its own. Turning this on gives every instance the ${instanceLabel(lastInstanceId)} columns.`
}

interface ProfileEditorProps {
  profileId: string
  /** Called after the profile is deleted, so the host can show another one. */
  onDeleted?: () => void
  className?: string
}

export function ProfileEditor({ profileId, onDeleted, className }: ProfileEditorProps) {
  const profile = useProfile(profileId)
  const isActive = usePrototype((s) => s.activeProfileId === profile.id)
  const lastInstanceId = usePrototype((s) => s.lastInstanceId)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const titleId = useId()
  const same = sameColumns(profile)
  const own = !!profile.ownSortAndFilters
  const switchId = (key: string) => `profile-${profile.id}-${key}`

  const useSlot = "h-8 w-[10.5rem]"

  return (
    <section aria-labelledby={titleId} data-prototype-editor={profile.name} className={cn("@container min-w-0 rounded-lg border", className)}>
      <h3 id={titleId} className="sr-only">{profile.name}</h3>
      <div
        className={cn(
          "grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-4 gap-y-3 p-4",
          "[grid-template-areas:'thumb_head'_'thumb_actions'] @max-md:[grid-template-areas:'thumb_head'_'actions_actions']"
        )}
      >
        <ProfileThumbnail profile={profile} className="w-48 shadow-sm [grid-area:thumb] @max-xl:w-40 @max-lg:w-32 @max-md:w-24" />
        <div className="min-w-0 self-end [grid-area:head] @max-md:self-center">
          <NameBlock key={profile.id} profile={profile} />
        </div>
        <div className="flex flex-wrap items-center gap-2 self-start [grid-area:actions]">
          {isActive ? (
            <span className={cn(useSlot, "inline-flex items-center justify-center gap-1.5 rounded-md bg-primary/10 text-sm font-medium text-primary")}>
              <Check aria-hidden="true" className="size-4" />
              This browser
            </span>
          ) : (
            <Button size="sm" variant="outline" className={useSlot} onClick={() => actions.pick(profile.id)}>
              Use in this browser
            </Button>
          )}
          {profile.isDefault ? (
            <span aria-hidden="true" className="invisible h-8 px-3 text-sm">Delete…</span>
          ) : (
            <Button size="sm" variant="ghost" className="h-8 text-muted-foreground hover:text-destructive" onClick={() => setConfirmDelete(true)}>
              Delete…
            </Button>
          )}
        </div>
      </div>

      <div className="divide-y border-t">
        <SwitchRow
          id={switchId("same")}
          label="Same columns on every instance"
          help={sameHelp(same, lastInstanceId)}
          helpVariants={[sameHelp(true, lastInstanceId), sameHelp(false, lastInstanceId)]}
        >
          <Switch id={switchId("same")} checked={same} onCheckedChange={(on) => actions.setSameColumns(profile.id, on, null)} className="mt-0.5" />
        </SwitchRow>

        {profile.isDefault ? (
          // One muted line in place of the switch, in a slot as tall as the switch row of other profiles.
          <div className="grid px-4 py-3 [&>*]:col-start-1 [&>*]:row-start-1">
            <div aria-hidden="true" className="invisible flex gap-3">
              <div className="min-w-0 flex-1">
                <p className="text-sm leading-5">Own sort and filters</p>
                <StableText className="mt-0.5 text-xs leading-4" shown="" variants={[ownHelp(true, profile.name), ownHelp(false, profile.name)]} />
              </div>
              <span className="h-5 w-8" />
            </div>
            <div className="flex items-center gap-1.5 self-center text-sm text-muted-foreground">
              Always shares sort and filters
              <HelpTip label="About Always shares sort and filters" className="size-5">{DEFAULT_SHARES_HELP}</HelpTip>
            </div>
          </div>
        ) : (
          <SwitchRow
            id={switchId("own")}
            label="Own sort and filters"
            labelHelp={
              <HelpTip label="About Own sort and filters" className="size-5">
                <OwnSortAndFiltersHelp />
              </HelpTip>
            }
            help={ownHelp(own, profile.name)}
            helpVariants={[ownHelp(true, profile.name), ownHelp(false, profile.name)]}
          >
            <Switch id={switchId("own")} checked={own} onCheckedChange={(on) => actions.setOwnSortAndFilters(profile.id, on)} className="mt-0.5" />
          </SwitchRow>
        )}
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="[overflow-wrap:anywhere]">Delete {profile.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              {isActive ? "This browser and other browsers that use it move to Default." : "Other browsers that use it move to Default."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => {
                actions.remove(profile.id)
                onDeleted?.()
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
