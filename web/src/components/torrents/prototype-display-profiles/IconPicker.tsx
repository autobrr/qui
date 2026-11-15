/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): the icon and color picker behind the icon tile of the
// profile editor and the New profile dialog. A pick calls back at once; the
// popover stays open so the person can pick a color too.

import { useId, useState, type KeyboardEvent, type ReactNode } from "react"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import { ALL_ICONS, ICON_GROUPS, PROFILE_COLORS, type ProfileColor } from "./look"

const COLUMNS = 8

interface IconPickerProps {
  icon: string
  color: ProfileColor
  onIconChange: (icon: string) => void
  onColorChange: (color: ProfileColor) => void
  /** The trigger, usually a ProfileTile in a button. */
  children: ReactNode
}

export function IconPicker({ icon, color, onIconChange, onColorChange, children }: IconPickerProps) {
  const [open, setOpen] = useState(false)
  const colorLabelId = useId()
  const selectedIndex = ALL_ICONS.findIndex((entry) => entry.name === icon)
  // Roving tab stop: the picked icon, or the first one when the stored name is unknown.
  const tabStop = selectedIndex === -1 ? 0 : selectedIndex

  const moveFocus = (event: KeyboardEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement
    const from = Number(target.dataset.iconIndex)
    if (Number.isNaN(from)) return
    const steps: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: COLUMNS, ArrowUp: -COLUMNS }
    let to: number | null = null
    if (event.key in steps) to = from + steps[event.key]
    else if (event.key === "Home") to = from - (from % COLUMNS)
    else if (event.key === "End") to = from - (from % COLUMNS) + COLUMNS - 1
    if (to === null) return
    event.preventDefault()
    if (to < 0 || to >= ALL_ICONS.length) return
    event.currentTarget.querySelector<HTMLButtonElement>(`[data-icon-index="${to}"]`)?.focus()
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent align="start" className="w-auto max-w-[calc(100vw-1rem)] p-3" data-prototype-icon-picker="">
        <div onKeyDown={moveFocus} className="flex flex-col gap-2.5">
          {ICON_GROUPS.map((group, groupIndex) => (
            <div key={group.label} role="group" aria-label={group.label}>
              <p className="pb-1 text-xs text-muted-foreground" aria-hidden="true">{group.label}</p>
              <div className="grid grid-cols-[repeat(8,2.25rem)] gap-0.5">
                {group.icons.map((entry, i) => {
                  const index = groupIndex * COLUMNS + i
                  const pressed = entry.name === icon
                  return (
                    <button
                      key={entry.name}
                      type="button"
                      data-icon-index={index}
                      tabIndex={index === tabStop ? 0 : -1}
                      aria-label={entry.label}
                      aria-pressed={pressed}
                      title={entry.label}
                      onClick={() => onIconChange(entry.name)}
                      className={cn(
                        "grid size-9 place-items-center rounded-md outline-none hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50",
                        pressed && "bg-accent ring-1 ring-foreground/40"
                      )}
                    >
                      <entry.Icon className="size-[18px]" />
                    </button>
                  )
                })}
              </div>
            </div>
          ))}

          <div className="border-t pt-2.5">
            <p className="pb-1 text-xs text-muted-foreground" id={colorLabelId}>Color</p>
            <div role="group" aria-labelledby={colorLabelId} className="flex flex-wrap items-center gap-1">
              {PROFILE_COLORS.map((option) => (
                <button
                  key={option.id}
                  type="button"
                  aria-pressed={color === option.id}
                  aria-label={option.id === "none" ? "None (follows the text color)" : option.label}
                  title={option.id === "none" ? "None: follows the text color" : option.label}
                  onClick={() => onColorChange(option.id)}
                  className={cn("grid size-7 place-items-center rounded-full outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50", option.className)}
                >
                  <span
                    className={cn(
                      "size-5 rounded-full bg-current ring-offset-2 ring-offset-popover",
                      color === option.id ? "ring-2 ring-foreground" : "ring-1 ring-border"
                    )}
                  />
                </button>
              ))}
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
