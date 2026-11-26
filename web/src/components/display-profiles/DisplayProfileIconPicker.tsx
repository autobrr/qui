/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId, type KeyboardEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { DisplayProfileColor } from "@/lib/display-profiles"
import { cn } from "@/lib/utils"

import { DISPLAY_PROFILE_COLOR_OPTIONS, DISPLAY_PROFILE_ICON_GROUPS, DISPLAY_PROFILE_ICONS } from "./display-profile-icons"

const COLUMNS = 8

const GRID_STEPS: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: COLUMNS, ArrowUp: -COLUMNS }

interface DisplayProfileIconPickerProps {
  icon: string
  color: DisplayProfileColor | null
  onIconChange: (icon: string) => void
  onColorChange: (color: DisplayProfileColor | null) => void
  children: ReactNode
}

export function DisplayProfileIconPicker({ icon, color, onIconChange, onColorChange, children }: DisplayProfileIconPickerProps) {
  const { t } = useTranslation("torrents")
  const colorLabelId = useId()
  const selectedIndex = DISPLAY_PROFILE_ICONS.findIndex((entry) => entry.name === icon)
  const tabStop = Math.max(selectedIndex, 0)

  const moveFocus = (event: KeyboardEvent<HTMLDivElement>) => {
    const from = Number((event.target as HTMLElement).dataset.iconIndex)
    if (Number.isNaN(from)) return
    let to: number
    if (event.key in GRID_STEPS) to = from + GRID_STEPS[event.key]
    else if (event.key === "Home") to = from - (from % COLUMNS)
    else if (event.key === "End") to = from - (from % COLUMNS) + COLUMNS - 1
    else return
    event.preventDefault()
    if (to < 0 || to >= DISPLAY_PROFILE_ICONS.length) return
    event.currentTarget.querySelector<HTMLButtonElement>(`[data-icon-index="${to}"]`)?.focus()
  }

  return (
    <Popover>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent align="start" className="w-auto max-w-[calc(100vw-1rem)] p-3">
        <div onKeyDown={moveFocus} className="flex flex-col gap-2.5">
          {DISPLAY_PROFILE_ICON_GROUPS.map((group, groupIndex) => (
            <div key={group.labelKey} role="group" aria-label={t(group.labelKey)}>
              <p aria-hidden="true" className="pb-1 text-xs text-muted-foreground">{t(group.labelKey)}</p>
              <div className="grid grid-cols-[repeat(8,2.25rem)] gap-0.5">
                {group.icons.map((entry, i) => {
                  const index = groupIndex * COLUMNS + i
                  const pressed = entry.name === icon
                  const label = t(entry.labelKey)
                  return (
                    <Tooltip key={entry.name}>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          data-icon-index={index}
                          tabIndex={index === tabStop ? 0 : -1}
                          aria-label={label}
                          aria-pressed={pressed}
                          onClick={() => onIconChange(entry.name)}
                          className={cn(
                            "grid size-9 place-items-center rounded-md outline-none hover:bg-accent focus-visible:ring-3 focus-visible:ring-ring/50",
                            pressed && "bg-accent ring-1 ring-foreground/40"
                          )}
                        >
                          <entry.Icon className="size-[18px]" />
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>{label}</TooltipContent>
                    </Tooltip>
                  )
                })}
              </div>
            </div>
          ))}

          <div className="border-t pt-2.5">
            <p id={colorLabelId} className="pb-1 text-xs text-muted-foreground">{t("displayProfiles.iconPicker.color")}</p>
            <div role="group" aria-labelledby={colorLabelId} className="flex flex-wrap items-center gap-1">
              {DISPLAY_PROFILE_COLOR_OPTIONS.map((option) => {
                const pressed = option.color === color
                const label = t(option.labelKey)
                return (
                  <Tooltip key={option.labelKey}>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        aria-pressed={pressed}
                        aria-label={label}
                        onClick={() => onColorChange(option.color)}
                        className={cn("grid size-7 place-items-center rounded-full outline-none focus-visible:ring-3 focus-visible:ring-ring/50", option.className)}
                      >
                        <span
                          className={cn(
                            "relative size-5 overflow-hidden rounded-full ring-offset-2 ring-offset-popover",
                            option.color && "bg-current",
                            pressed ? "ring-2 ring-foreground" : "ring-1 ring-border"
                          )}
                        >
                          {option.color === null && (
                            <span className="absolute inset-x-0 top-1/2 h-px -translate-y-1/2 -rotate-45 bg-muted-foreground" />
                          )}
                        </span>
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>{label}</TooltipContent>
                  </Tooltip>
                )
              })}
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
