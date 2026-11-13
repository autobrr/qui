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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { PERIOD_PRESETS, type PeriodPreset } from "@/lib/statistics"
import { ChevronDown } from "lucide-react"
import { useState } from "react"
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
    <div className="flex max-w-full flex-wrap items-center gap-2">
      <Tabs value={value} onValueChange={next => next !== "custom" && onPreset(next as PeriodPreset)} className="max-w-full overflow-x-auto">
        <TabsList>
          {PERIOD_PRESETS.map(preset => (
            <TabsTrigger key={preset} value={preset} className="px-3">{t(`periods.${preset}`)}</TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button variant={value === "custom" ? "secondary" : "outline"} size="sm" className="h-9">
            {value === "custom" && customLabel ? customLabel : t("periods.custom")}
            <ChevronDown className="size-4 opacity-60" />
          </Button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-72">
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
            <div className="space-y-1.5">
              <Label htmlFor="statistics-first-day">{t("customRange.from")}</Label>
              <Input id="statistics-first-day" type="date" max={today} value={firstDay} onChange={event => setFirstDay(event.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="statistics-last-day">{t("customRange.to")}</Label>
              <Input id="statistics-last-day" type="date" value={lastDay} onChange={event => setLastDay(event.target.value)} />
            </div>
            <Button type="submit" size="sm" className="w-full" disabled={!valid}>{t("customRange.apply")}</Button>
          </form>
        </PopoverContent>
      </Popover>
    </div>
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
        <Button variant="outline" size="sm" className="h-9 max-w-[14rem]">
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
