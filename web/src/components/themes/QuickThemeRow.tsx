/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check, Lock } from "lucide-react"
import type { MouseEvent, PointerEvent } from "react"
import { useTranslation } from "react-i18next"

import { ThemeThumbnail } from "@/components/themes/ThemeThumbnail"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { Theme } from "@/config/themes"
import type { ThemeThumbnailPalette } from "@/lib/theme-thumbnail"
import { cn } from "@/lib/utils"

type Variation = { id: string; color: string }

interface QuickThemeRowProps {
  theme: Theme
  palette: ThemeThumbnailPalette
  variations: Variation[]
  isCurrent: boolean
}

/** One theme in a quick theme picker: its thumbnail, its name, and a mark for the theme in use, a lock, or its color variations. */
export function QuickThemeRow({ theme, palette, variations, isCurrent }: QuickThemeRowProps) {
  const { t } = useTranslation("settings")
  return (
    <span className="flex min-w-0 items-center gap-2.5">
      <ThemeThumbnail palette={palette} detail="compact" className={cn("w-10 shrink-0 rounded-sm", theme.locked && "opacity-60")} />
      <span className="min-w-0 flex-1 whitespace-normal break-words">{theme.name}</span>
      {!isCurrent && !theme.locked && variations.length > 1 && (
        <span className="flex shrink-0 -space-x-1">
          {variations.slice(0, 3).map((v) => (
            <span key={v.id} aria-hidden="true" className="size-2.5 rounded-full ring-2 ring-popover" style={{ backgroundColor: v.color }} />
          ))}
          <span className="sr-only">{t("themes.selector.variationCount", { count: variations.length })}</span>
        </span>
      )}
      {theme.locked && (
        <>
          <Lock aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="sr-only">{t("themes.selector.needsLicense")}</span>
        </>
      )}
      {isCurrent && <Check aria-hidden="true" className="size-4 shrink-0" />}
    </span>
  )
}

interface QuickThemeVariationsProps {
  variations: Variation[]
  selected: string | null
  onSelect: (variationId: string) => void
  className?: string
}

const keepInParent = (event: MouseEvent | PointerEvent) => event.stopPropagation()

/** The color variations of the theme in use, as buttons. A click picks the variation and does not reach the row around it. */
export function QuickThemeVariations({ variations, selected, onSelect, className }: QuickThemeVariationsProps) {
  const { t } = useTranslation("settings")
  return (
    <span role="group" data-slot="theme-variations" aria-label={t("themes.current.colors")} className={cn("flex flex-wrap items-center gap-1", className)}>
      {variations.map((v) => (
        <Tooltip key={v.id}>
          <TooltipTrigger asChild>
            <button
              type="button"
              aria-pressed={selected === v.id}
              aria-label={v.id}
              onPointerDown={keepInParent}
              onClick={(event) => {
                keepInParent(event)
                onSelect(v.id)
              }}
              className="grid size-7 place-items-center rounded-full outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <span
                className={cn(
                  "size-5 rounded-full ring-offset-2 ring-offset-popover",
                  selected === v.id ? "ring-2 ring-foreground" : "ring-1 ring-border"
                )}
                style={{ backgroundColor: v.color }}
              />
            </button>
          </TooltipTrigger>
          <TooltipContent>{v.id}</TooltipContent>
        </Tooltip>
      ))}
    </span>
  )
}
