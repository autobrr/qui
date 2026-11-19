/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useIsPhone, useMediaQuery } from "@/hooks/useMediaQuery"
import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { SERIES_COLOR_SETS, useSeriesColorSet, type SeriesColorSet } from "@/hooks/useSeriesColors"
import { formatCalendarDay } from "@/lib/dateTimeUtils"
import { PERIOD_PRESETS, type PeriodPreset } from "@/lib/statistics"
import { cn } from "@/lib/utils"
import { CalendarDays, CalendarRange, ChevronDown, Palette } from "lucide-react"
import { useRef, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

export type PeriodChoice = PeriodPreset | "custom"

interface PeriodPickerProps {
  value: PeriodChoice
  customLabel?: string
  onPreset: (preset: PeriodPreset) => void
  onCustom: (firstDay: Date, lastDay: Date) => void
}

function parseDay(value: string): Date | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  return match ? new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3])) : undefined
}

export function PeriodPicker({ value, customLabel, onPreset, onCustom }: PeriodPickerProps) {
  const { t } = useTranslation("statistics")
  const narrow = useIsPhone()
  const custom = value === "custom"

  if (narrow) {
    return (
      <>
        <Select value={value} onValueChange={next => onPreset(next as PeriodPreset)}>
          <SelectTrigger className="h-9 min-w-0 max-w-[50vw] shrink-0 gap-1 px-2.5">
            <SelectValue>{custom ? customLabel ?? t("periods.custom") : t(`periods.${value}`)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {PERIOD_PRESETS.map(preset => (
              <SelectItem key={preset} value={preset}>{t(`periods.${preset}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <CustomRange onCustom={onCustom}>
          <Button variant={custom ? "secondary" : "outline"} size="icon" className="size-9 shrink-0" aria-label={t("periods.custom")}>
            <CalendarRange />
          </Button>
        </CustomRange>
      </>
    )
  }

  return (
    <div className="flex max-w-full flex-wrap items-center gap-2">
      <Tabs value={value} onValueChange={next => next !== "custom" && onPreset(next as PeriodPreset)} className="max-w-full overflow-x-auto">
        <TabsList>
          {PERIOD_PRESETS.map(preset => (
            <TabsTrigger key={preset} value={preset} className="px-3">{t(`periods.${preset}`)}</TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <CustomRange onCustom={onCustom}>
        <Button variant={custom ? "secondary" : "outline"} size="sm" className="h-9">
          {custom && customLabel ? customLabel : t("periods.custom")}
          <ChevronDown className="size-4 opacity-60" />
        </Button>
      </CustomRange>
    </div>
  )
}

interface DayFieldProps {
  id: string
  label: string
  // A day as YYYY-MM-DD, or an empty string.
  value: string
  max?: string
  onChange: (value: string) => void
}

/**
 * A date field that shows its day in the date format of the user. The date field of the browser lies on top of the
 * text and cannot be seen, because a browser shows a date only in its own format.
 */
function DayField({ id, label, value, max, onChange }: DayFieldProps) {
  const { t } = useTranslation("statistics")
  const { preferences } = usePersistedDateTimePreferences()
  const day = parseDay(value)
  // iOS Safari reports a tap as a mouse click, so the type comes from pointer down.
  const press = useRef<string>(undefined)

  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="relative flex h-9 items-center justify-between gap-2 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs transition-[color,box-shadow] has-focus-visible:border-ring has-focus-visible:ring-[3px] has-focus-visible:ring-ring/50 dark:bg-input/30">
        <span className={cn("tabular-nums", !day && "text-muted-foreground")}>{day ? formatCalendarDay(day, preferences) : t("customRange.pick")}</span>
        <CalendarDays className="size-4 text-muted-foreground" />
        <Input
          id={id}
          type="date"
          className="absolute inset-0 size-full cursor-pointer opacity-0 pointer-coarse:appearance-none [&::-webkit-calendar-picker-indicator]:absolute [&::-webkit-calendar-picker-indicator]:inset-0 [&::-webkit-calendar-picker-indicator]:size-full [&::-webkit-calendar-picker-indicator]:cursor-pointer"
          max={max}
          value={value}
          onChange={event => onChange(event.target.value)}
          // Firefox opens its calendar only on a click on its own calendar button, and closes it again without preventDefault.
          // A touch keeps the picker of the device, because Safari on iOS has no showPicker for a date.
          onPointerDown={event => {
            press.current = event.pointerType
          }}
          onClick={event => {
            if (press.current === "mouse") {
              event.preventDefault()
              event.currentTarget.showPicker()
            }
          }}
          onKeyDown={event => {
            if (event.key === " ") {
              event.preventDefault()
              event.currentTarget.showPicker()
            }
          }}
        />
      </div>
    </div>
  )
}

function CustomRange({ onCustom, children }: { onCustom: (firstDay: Date, lastDay: Date) => void; children: ReactNode }) {
  const { t } = useTranslation("statistics")
  const touch = useMediaQuery("(hover: none)")
  const [open, setOpen] = useState(false)
  const [firstDay, setFirstDay] = useState("")
  const [lastDay, setLastDay] = useState("")
  const first = parseDay(firstDay)
  const last = parseDay(lastDay)
  const now = new Date()
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`
  // A range that starts after today has no past to show.
  const valid = first !== undefined && last !== undefined && first <= last && firstDay <= today

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      {/* iOS opens its date picker over the form when the first field gets focus, so a touch screen leaves the focus where it is. */}
      <PopoverContent align="end" collisionPadding={16} className="w-72" onOpenAutoFocus={touch ? event => event.preventDefault() : undefined}>
        <form
          className="space-y-3"
          onSubmit={event => {
            event.preventDefault()
            if (valid) {
              onCustom(first, last)
              setOpen(false)
            }
          }}
        >
          <DayField id="statistics-first-day" label={t("customRange.from")} max={today} value={firstDay} onChange={setFirstDay} />
          <DayField id="statistics-last-day" label={t("customRange.to")} value={lastDay} onChange={setLastDay} />
          <Button type="submit" size="sm" className="w-full" disabled={!valid}>{t("customRange.apply")}</Button>
        </form>
      </PopoverContent>
    </Popover>
  )
}

export interface InstanceOption {
  id: number
  name: string
  removed: boolean
}

interface InstanceFilterProps {
  options: InstanceOption[]
  selected: number[]
  onChange: (selected: number[]) => void
}

/** An empty selection means all instances. */
export function InstanceFilter({ options, selected, onChange }: InstanceFilterProps) {
  const { t } = useTranslation("statistics")
  const label = selected.length === 0 ? t("instances.all") : selected.length === 1 ? options.find(option => option.id === selected[0])?.name : t("instances.count", { count: selected.length })

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="h-9 min-w-0 max-w-[14rem] max-sm:ml-auto max-sm:shrink max-sm:gap-1 max-sm:px-2.5">
          <span className="truncate">{label}</span>
          <ChevronDown className="size-4 opacity-60" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuCheckboxItem checked={selected.length === 0} onCheckedChange={() => onChange([])} onSelect={event => event.preventDefault()}>
          {t("instances.all")}
        </DropdownMenuCheckboxItem>
        <DropdownMenuSeparator />
        {options.map(option => (
          <DropdownMenuCheckboxItem
            key={option.id}
            checked={selected.includes(option.id)}
            onCheckedChange={checked => onChange(checked ? [...selected, option.id] : selected.filter(id => id !== option.id))}
            onSelect={event => event.preventDefault()}
          >
            <span className="truncate">{option.name}</span>
            {option.removed && <span className="ml-auto pl-2 text-xs text-muted-foreground">{t("instances.removed")}</span>}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** Selects the series colors of the charts: the distinct set or the chart colors of the theme. */
export function SeriesColorPicker() {
  const { t } = useTranslation("statistics")
  const [set, setSet] = useSeriesColorSet()
  const narrow = useIsPhone()

  return (
    <Select value={set} onValueChange={value => setSet(value as SeriesColorSet)}>
      <SelectTrigger aria-label={t("seriesColors.label")} className={narrow ? "size-9 shrink-0 justify-center px-0 [&>svg:last-child]:hidden" : "h-9"}>
        {narrow ? <><Palette /><span className="sr-only"><SelectValue /></span></> : <SelectValue />}
      </SelectTrigger>
      <SelectContent align="end">
        {SERIES_COLOR_SETS.map(value => (
          <SelectItem key={value} value={value}>{t(`seriesColors.${value}`)}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
