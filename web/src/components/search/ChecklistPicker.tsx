/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Drawer, DrawerContent, DrawerFooter, DrawerTitle, DrawerTrigger } from "@/components/ui/drawer"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { SearchInput } from "@/components/ui/SearchInput"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"
import { useId, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

export interface ChecklistOption {
  value: string
  label: string
  detail?: string
  count?: number
}

interface ChecklistPickerProps {
  trigger: ReactNode
  title: string
  options: ChecklistOption[]
  selected: ReadonlySet<string>
  onChange: (selected: Set<string>) => void
}

const FILTER_FROM = 12

export function ChecklistPicker({ trigger, title, options, selected, onChange }: ChecklistPickerProps) {
  const { t } = useTranslation("search")
  const isMobile = useIsMobile()
  const [open, setOpen] = useState(false)

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={setOpen} repositionInputs={false}>
        <DrawerTrigger asChild>{trigger}</DrawerTrigger>
        <DrawerContent className="max-h-[85dvh]" aria-describedby={undefined}>
          <Checklist title={<DrawerTitle className="text-base">{title}</DrawerTitle>} options={options} selected={selected} onChange={onChange} touch />
          <DrawerFooter className="border-t pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
            <Button type="button" size="lg" onClick={() => setOpen(false)}>{t("picker.done")}</Button>
          </DrawerFooter>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent align="start" className="flex max-h-[min(26rem,var(--radix-popover-content-available-height))] w-60 flex-col p-0">
        <Checklist title={<p className="text-sm font-medium">{title}</p>} options={options} selected={selected} onChange={onChange} />
      </PopoverContent>
    </Popover>
  )
}

interface ChecklistProps {
  title: ReactNode
  options: ChecklistOption[]
  selected: ReadonlySet<string>
  onChange: (selected: Set<string>) => void
  touch?: boolean
}

function Checklist({ title, options, selected, onChange, touch = false }: ChecklistProps) {
  const { t } = useTranslation("search")
  const id = useId()
  const [filter, setFilter] = useState("")
  const needle = filter.trim().toLowerCase()
  const shown = needle ? options.filter(option => option.label.toLowerCase().includes(needle)) : options
  const allSelected = options.every(option => selected.has(option.value))

  const toggle = (value: string) => {
    const next = new Set(selected)
    if (next.has(value)) next.delete(value)
    else next.add(value)
    onChange(next)
  }

  return (
    <>
      <div className={cn("flex items-center gap-2 border-b", touch ? "px-4 pt-1 pb-2" : "py-1 pr-1 pl-3")}>
        <div className="min-w-0 flex-1 truncate">{title}</div>
        <Button
          type="button"
          variant="ghost"
          size={touch ? "sm" : "xs"}
          className="shrink-0 text-muted-foreground"
          onClick={() => onChange(allSelected ? new Set() : new Set(options.map(option => option.value)))}
        >
          {allSelected ? t("picker.none") : t("picker.all")}
        </Button>
      </div>
      {options.length >= FILTER_FROM && (
        <div className={touch ? "px-4 pt-2" : "px-2 pt-2"}>
          <SearchInput
            value={filter}
            onChange={event => setFilter(event.target.value)}
            onClear={() => setFilter("")}
            placeholder={t("picker.filter")}
            aria-label={t("picker.filter")}
            className={touch ? "h-10" : "h-8"}
          />
        </div>
      )}
      <ul className={cn("min-h-0 flex-1 overflow-y-auto overscroll-contain", touch ? "px-2 py-1" : "p-1")}>
        {shown.map(option => {
          const optionId = `${id}-${option.value}`
          return (
            <li key={option.value}>
              <label
                htmlFor={optionId}
                className={cn("flex cursor-pointer items-center gap-2.5 rounded-sm px-2 text-sm hover:bg-accent/60", touch ? "min-h-11" : "h-8")}
              >
                <Checkbox id={optionId} checked={selected.has(option.value)} onCheckedChange={() => toggle(option.value)} />
                <span className="min-w-0 flex-1 truncate">{option.label}</span>
                {option.detail && <span className="shrink-0 text-xs text-muted-foreground">{option.detail}</span>}
                {option.count !== undefined && <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{option.count}</span>}
              </label>
            </li>
          )
        })}
        {shown.length === 0 && <li className="px-2 py-6 text-center text-sm text-muted-foreground">{t("picker.noMatch")}</li>}
      </ul>
    </>
  )
}
