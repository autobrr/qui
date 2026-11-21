/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check, Pencil, X } from "lucide-react"
import { useId, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { flushSync } from "react-dom"
import { useTranslation } from "react-i18next"

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { TruncatedText } from "@/components/ui/truncated-text"
import { VIEW_MODE_SETTINGS } from "@/hooks/usePersistedCompactViewState"
import { useInstances } from "@/hooks/useInstances"
import { FILTER_SIDEBAR_COLLAPSED_KEY, parseJsonBoolean, useDisplayProfileSetting } from "@/lib/client-settings"
import { DISPLAY_PROFILE_NAME_MAX_LENGTH, type DisplayProfile } from "@/lib/display-profiles"
import { cn } from "@/lib/utils"

import { findDisplayProfileIcon } from "./display-profile-icons"
import { useDisplayProfileNameCheck, useNamedDisplayProfiles } from "./display-profile-names"
import { DisplayProfileIconPicker } from "./DisplayProfileIconPicker"
import { DisplayProfileThumbnail, type DisplayProfileDensity } from "./DisplayProfileThumbnail"
import { DisplayProfileTile } from "./DisplayProfileTile"
import { OwnSortAndFiltersSetting } from "./OwnSortAndFiltersSetting"
import { SameColumnsSwitch } from "./SameColumnsSwitch"
import { StableText } from "./StableText"

const DENSITY_OF_VIEW_MODE: Record<string, DisplayProfileDensity> = { normal: "table", dense: "dense", compact: "stacked" }
const DENSITY_LABEL_KEYS = {
  table: "displayProfiles.settings.densities.table",
  dense: "displayProfiles.settings.densities.dense",
  stacked: "displayProfiles.settings.densities.stacked",
} as const satisfies Record<DisplayProfileDensity, string>

/** The row density and the filter sidebar of the desktop torrent table in one profile. */
function useTableView(profileId: string): { density: DisplayProfileDensity; filterSidebarOpen: boolean } {
  const desktop = VIEW_MODE_SETTINGS.desktop
  const viewMode = useDisplayProfileSetting(profileId, desktop.storageKey, { defaultValue: desktop.defaultMode, parse: desktop.parse })
  const collapsed = useDisplayProfileSetting(profileId, FILTER_SIDEBAR_COLLAPSED_KEY, { defaultValue: false, parse: parseJsonBoolean })
  return { density: DENSITY_OF_VIEW_MODE[viewMode] ?? "table", filterSidebarOpen: !collapsed }
}

interface DisplayProfileEditorProps {
  profile: DisplayProfile
}

/** One profile in Settings, in the style of the "In use" block of Themes. */
export function DisplayProfileEditor({ profile }: DisplayProfileEditorProps) {
  const { t } = useTranslation("torrents")
  const { profiles, active, settled, pick, setIcon, setColor, delete: deleteProfile } = useNamedDisplayProfiles()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const { instances } = useInstances()
  const view = useTableView(profile.id)
  const titleId = useId()
  const isActive = profile.id === active.id
  const iconLabelKey = findDisplayProfileIcon(profile.icon)?.labelKey
  const lookLine = t("displayProfiles.settings.look", {
    density: t(DENSITY_LABEL_KEYS[view.density]),
    filterSidebar: view.filterSidebarOpen ? t("displayProfiles.settings.filterSidebarOpen") : t("displayProfiles.settings.filterSidebarClosed"),
  })

  return (
    <section aria-labelledby={titleId} className="@container min-w-0 rounded-lg border">
      <div className="flex items-center gap-4 p-4">
        <DisplayProfileThumbnail {...view} className="w-40 shrink-0 shadow-sm @max-lg:w-28 @max-md:w-24" />
        <div className="min-w-0 flex-1 space-y-3">
          <div className="flex items-start gap-3">
            <DisplayProfileIconPicker
              icon={profile.icon}
              color={profile.color}
              onIconChange={(icon) => setIcon(profile.id, icon)}
              onColorChange={(color) => setColor(profile.id, color)}
            >
              <button
                type="button"
                disabled={!settled}
                aria-label={t("displayProfiles.iconPicker.trigger", { icon: iconLabelKey ? t(iconLabelKey) : t("displayProfiles.iconPicker.letter") })}
                className="shrink-0 rounded-lg outline-none ring-1 ring-border hover:ring-foreground/40 focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:opacity-50"
              >
                <DisplayProfileTile profile={profile} size="lg" />
              </button>
            </DisplayProfileIconPicker>
            <NameField profile={profile} titleId={titleId} lookLine={lookLine} />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {/* Both labels take the same place, so the button keeps its width when the pick changes. */}
            <Button
              size="sm"
              variant="outline"
              disabled={!settled}
              aria-disabled={isActive || undefined}
              onClick={() => !isActive && pick(profile.id)}
              className={cn(
                "grid [&>*]:col-start-1 [&>*]:row-start-1",
                isActive && "cursor-default border-transparent bg-primary/10 text-primary shadow-none hover:bg-primary/10 hover:text-primary dark:bg-primary/10 dark:hover:bg-primary/10"
              )}
            >
              <span aria-hidden={isActive || undefined} className={cn(isActive && "invisible")}>{t("displayProfiles.settings.use")}</span>
              <span aria-hidden={!isActive || undefined} className={cn("inline-flex items-center justify-center gap-1.5", !isActive && "invisible")}>
                <Check aria-hidden="true" />
                {t("displayProfiles.settings.thisBrowser")}
              </span>
            </Button>
            {!profile.isDefault && (
              <Button size="sm" variant="ghost" className="text-muted-foreground hover:text-destructive" disabled={!settled} onClick={() => setConfirmDelete(true)}>
                {t("displayProfiles.settings.delete")}
              </Button>
            )}
          </div>
        </div>
      </div>
      <div className="divide-y border-t">
        <div className="px-4 py-3"><SameColumnsSwitch profileId={profile.id} instances={instances ?? []} /></div>
        <div className="px-4 py-3"><OwnSortAndFiltersSetting profile={profile} /></div>
      </div>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t("displayProfiles.settings.deleteTitle", { name: profile.name })}
        description={t("displayProfiles.settings.deleteDescription", { defaultName: profiles[0].name })}
        confirmLabel={t("displayProfiles.settings.deleteConfirm")}
        onConfirm={() => deleteProfile(profile.id)}
      />
    </section>
  )
}

interface NameFieldProps {
  profile: DisplayProfile
  titleId: string
  lookLine: string
}

/** The name of the profile with inline rename, and below it the look line, or the count and the errors while it renames. */
function NameField({ profile, titleId, lookLine }: NameFieldProps) {
  const { t } = useTranslation("torrents")
  const { settled, rename } = useNamedDisplayProfiles()
  const [draft, setDraft] = useState<string | null>(null)
  const [submitted, setSubmitted] = useState(false)
  const pencilRef = useRef<HTMLButtonElement>(null)
  const lineId = useId()

  const renaming = draft !== null
  const check = useDisplayProfileNameCheck(draft ?? "", submitted, profile.id)
  const shownError = renaming ? check.shownError : null

  const finish = () => {
    // The pencil comes back in place of the field, so focus can go back to it.
    flushSync(() => {
      setDraft(null)
      setSubmitted(false)
    })
    pencilRef.current?.focus()
  }

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault()
    setSubmitted(true)
    if (draft === null || check.error) return
    // An unchanged name stays unsaved, so Default keeps following the language of the app.
    if (draft.trim() !== profile.name) rename(profile.id, draft)
    finish()
  }

  const handleKeyDown = (event: KeyboardEvent) => {
    if (event.key !== "Escape") return
    event.preventDefault()
    finish()
  }

  return (
    <div className="min-w-0 flex-1">
      <div className="flex min-h-10 items-center gap-1">
        <h3
          id={titleId}
          className={cn(renaming ? "sr-only" : "min-w-0 text-lg font-semibold leading-tight [overflow-wrap:anywhere]")}
        >
          <TruncatedText wrap className="block pointer-fine:truncate">{profile.name}</TruncatedText>
        </h3>
        {renaming ? (
          <form onSubmit={handleSubmit} onKeyDown={handleKeyDown} className="flex min-w-0 flex-1 items-center gap-1">
            <Input
              autoFocus
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              aria-label={t("displayProfiles.settings.renameAriaLabel", { name: profile.name })}
              aria-invalid={shownError ? true : undefined}
              aria-describedby={lineId}
              className="h-9 min-w-0 flex-1"
            />
            <Button type="submit" size="icon" variant="ghost" className="size-9 shrink-0" aria-label={t("displayProfiles.settings.saveName")}>
              <Check />
            </Button>
            <Button type="button" size="icon" variant="ghost" className="size-9 shrink-0" aria-label={t("displayProfiles.settings.cancelRename")} onClick={finish}>
              <X />
            </Button>
          </form>
        ) : (
          <Button
            ref={pencilRef}
            size="icon"
            variant="ghost"
            className="size-8 shrink-0 text-muted-foreground"
            disabled={!settled}
            aria-label={t("displayProfiles.settings.renameAriaLabel", { name: profile.name })}
            onClick={() => setDraft(profile.name)}
          >
            <Pencil className="size-3.5" />
          </Button>
        )}
      </div>
      <div id={lineId} className="mt-0.5 flex gap-2 text-xs leading-4">
        <StableText
          reserve={[lookLine, ...Object.values(check.errorTexts)]}
          className={cn("min-w-0 flex-1 [overflow-wrap:anywhere]", shownError ? "text-destructive" : "text-muted-foreground")}
        >
          {shownError ? check.errorTexts[shownError] : lookLine}
        </StableText>
        <StableText
          reserve={[t("displayProfiles.newDialog.nameCount", { length: DISPLAY_PROFILE_NAME_MAX_LENGTH, max: DISPLAY_PROFILE_NAME_MAX_LENGTH })]}
          className={cn("shrink-0 tabular-nums", renaming && check.error === "tooLong" ? "text-destructive" : "text-muted-foreground")}
        >
          {renaming && check.count}
        </StableText>
      </div>
    </div>
  )
}
