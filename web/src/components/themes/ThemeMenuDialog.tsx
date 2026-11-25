/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Lock, Search } from "lucide-react"
import { useEffect, useId, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { ThemeThumbnail } from "@/components/themes/ThemeThumbnail"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import type { Theme } from "@/config/themes"
import { THEME_GROUPS, isThemeShown, themeGroup, themeMatchesSearch, type ThemeGroup, type ThemeVisibility } from "@/lib/theme-catalog"
import type { ThemeThumbnailPalette } from "@/lib/theme-thumbnail"
import { cn } from "@/lib/utils"

interface ThemeMenuDialogProps {
  trigger: ReactNode
  catalog: readonly Theme[]
  currentThemeId: string
  visibility: ThemeVisibility
  paletteOf: (theme: Theme) => ThemeThumbnailPalette
  onShownChange: (themes: readonly Theme[], shown: boolean) => void
  onReplace: (next: ThemeVisibility) => void
  openRequested?: boolean
  onOpenRequestHandled?: () => void
}

export function ThemeMenuDialog({ trigger, catalog, currentThemeId, visibility, paletteOf, onShownChange, onReplace, openRequested = false, onOpenRequestHandled }: ThemeMenuDialogProps) {
  const { t } = useTranslation("settings")
  const baseId = useId()
  const [open, setOpenState] = useState(false)
  const [query, setQuery] = useState("")
  const [beforeReset, setBeforeReset] = useState<ThemeVisibility | null>(null)
  const searching = query.trim() !== ""
  const matches = catalog.filter((theme) => themeMatchesSearch(theme, query))
  const inMenu = (theme: Theme) => theme.id === currentThemeId || isThemeShown(theme, visibility)
  const shownCount = catalog.filter(inMenu).length
  const hasChoices = Object.keys(visibility).length > 0
  const groupLabels: Record<ThemeGroup, string> = {
    free: t("themes.groups.free"),
    licensed: t("themes.groups.licensed"),
    custom: t("themes.groups.custom"),
  }
  useEffect(() => {
    if (!openRequested) return
    setOpenState(true)
    onOpenRequestHandled?.()
  }, [openRequested, onOpenRequestHandled])

  const setOpen = (next: boolean) => {
    if (!next) {
      setQuery("")
      setBeforeReset(null)
    }
    setOpenState(next)
  }
  const setShown = (themes: readonly Theme[], shown: boolean) => {
    setBeforeReset(null)
    onShownChange(themes, shown)
  }
  const resetOrUndo = () => {
    if (beforeReset) {
      onReplace(beforeReset)
      setBeforeReset(null)
    } else {
      setBeforeReset(visibility)
      onReplace({})
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent
        className="flex max-h-[min(40rem,90dvh)] flex-col gap-4 sm:max-w-lg"
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          const content = event.currentTarget as HTMLElement | null
          content?.focus()
        }}
      >
        <DialogHeader>
          <DialogTitle>{t("themes.menu.title")}</DialogTitle>
          <DialogDescription>{t("themes.menu.description")}</DialogDescription>
        </DialogHeader>

        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("themes.selector.search")}
            aria-label={t("themes.selector.search")}
            className="pl-8"
          />
        </div>
        {searching && <p className="-mt-2 text-xs text-muted-foreground">{t("themes.menu.groupsLocked")}</p>}

        <div className="-mx-6 min-h-0 flex-1 overflow-y-auto px-6">
          {matches.length === 0 && (
            <p className="py-6 text-center text-sm text-muted-foreground">{t("themes.selector.noMatch", { query: query.trim() })}</p>
          )}
          {THEME_GROUPS.map((group) => {
            const themes = matches.filter((theme) => themeGroup(theme) === group)
            if (themes.length === 0) return null
            const groupThemes = catalog.filter((theme) => themeGroup(theme) === group)
            const shown = groupThemes.filter(inMenu).length
            const toggleable = groupThemes.filter((theme) => theme.id !== currentThemeId)
            const chosen = toggleable.filter((theme) => isThemeShown(theme, visibility)).length
            const groupId = `${baseId}-group-${group}`
            const groupState = chosen === toggleable.length ? true : chosen === 0 ? false : "indeterminate"
            return (
              <section key={group} aria-labelledby={groupId} className="pb-3">
                <div className="sticky top-0 z-10 flex items-stretch gap-3 border-b bg-background">
                  <Checkbox
                    className="self-center"
                    id={`${groupId}-all`}
                    checked={groupState}
                    disabled={searching || toggleable.length === 0}
                    onCheckedChange={() => setShown(toggleable, groupState !== true)}
                  />
                  <label id={groupId} htmlFor={`${groupId}-all`} className="flex min-h-10 flex-1 cursor-pointer items-center justify-between gap-2 py-2 text-sm font-medium max-sm:min-h-11">
                    {groupLabels[group]}{" "}
                    <span className="text-xs font-normal text-muted-foreground tabular-nums">
                      {t("themes.menu.groupCount", { shown, total: groupThemes.length })}
                    </span>
                  </label>
                </div>
                <ul className="pt-1">
                  {themes.map((theme) => {
                    const rowId = `${baseId}-theme-${theme.id}`
                    const inUse = theme.id === currentThemeId
                    return (
                      <li key={theme.id}>
                        <label htmlFor={rowId} className={cn("flex items-center gap-3 rounded-md py-1.5 max-sm:min-h-11", inUse ? "cursor-default" : "cursor-pointer hover:bg-muted/50")}>
                          <Checkbox
                            id={rowId}
                            checked={inMenu(theme)}
                            disabled={inUse}
                            onCheckedChange={(checked) => setShown([theme], checked === true)}
                          />
                          <ThemeThumbnail palette={paletteOf(theme)} detail="compact" className="w-10 shrink-0 rounded-sm" />
                          <span className="min-w-0 flex-1 truncate text-sm">{theme.name}</span>{" "}
                          {theme.locked && (
                            <>
                              <Lock aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
                              <span className="sr-only">{t("themes.selector.needsLicense")}</span>
                            </>
                          )}
                          {inUse && (
                            <span className="shrink-0 text-xs text-muted-foreground">{t("themes.menu.inUse")}</span>
                          )}
                        </label>
                      </li>
                    )
                  })}
                </ul>
              </section>
            )
          })}
        </div>

        <DialogFooter className="flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <p role="status" className="text-xs text-muted-foreground tabular-nums">
            {t("themes.menu.summary", { shown: shownCount, total: catalog.length })}
          </p>
          <div className="flex w-full gap-2 sm:w-auto">
            <Button variant="ghost" className="flex-1 sm:flex-none" disabled={!beforeReset && !hasChoices} onClick={resetOrUndo}>
              {beforeReset ? t("themes.menu.undo") : t("themes.menu.reset")}
            </Button>
            <Button className="flex-1 sm:flex-none" onClick={() => setOpen(false)}>
              {t("themes.menu.done")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
