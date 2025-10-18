/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { type ColumnType, type FilterOperation } from "@/lib/column-constants"
import { getDefaultOperation, getOperations } from "@/lib/column-filter-utils"
import { CaseSensitive, Filter, X } from "lucide-react"
import { type KeyboardEvent, useEffect, useRef, useState } from "react"

export type SizeUnit = "B" | "KiB" | "MiB" | "GiB" | "TiB"

export type SpeedUnit = "B/s" | "KiB/s" | "MiB/s" | "GiB/s" | "TiB/s"

export type DurationUnit = "seconds" | "minutes" | "hours" | "days"

export interface ColumnFilter {
  columnId: string
  operation: FilterOperation
  value: string
  value2?: string
  sizeUnit?: SizeUnit
  sizeUnit2?: SizeUnit
  speedUnit?: SpeedUnit
  speedUnit2?: SpeedUnit
  durationUnit?: DurationUnit
  durationUnit2?: DurationUnit
  caseSensitive?: boolean
}

interface ColumnFilterPopoverProps {
  columnId: string
  columnName: string
  columnType: ColumnType
  currentFilter?: ColumnFilter
  onApply: (filter: ColumnFilter | null) => void
}

function getScrollableParent(element: HTMLElement | null): HTMLElement | null {
  if (typeof window === "undefined" || !element) {
    return null
  }

  let current = element.parentElement

  while (current) {
    const style = window.getComputedStyle(current)
    const overflowX = style.overflowX
    const overflowY = style.overflowY

    if (/(auto|scroll|overlay)/.test(overflowX) || /(auto|scroll|overlay)/.test(overflowY)) {
      return current
    }

    current = current.parentElement
  }

  return (document.scrollingElement as HTMLElement | null) ?? document.documentElement
}

const SIZE_UNITS: { value: SizeUnit; label: string }[] = [
  { value: "B", label: "B" },
  { value: "KiB", label: "KiB" },
  { value: "MiB", label: "MiB" },
  { value: "GiB", label: "GiB" },
  { value: "TiB", label: "TiB" },
]

const SPEED_UNITS: { value: SpeedUnit; label: string }[] = [
  { value: "B/s", label: "B/s" },
  { value: "KiB/s", label: "KiB/s" },
  { value: "MiB/s", label: "MiB/s" },
  { value: "GiB/s", label: "GiB/s" },
  { value: "TiB/s", label: "TiB/s" },
]

const DURATION_UNITS: { value: DurationUnit; label: string }[] = [
  { value: "seconds", label: "Seconds" },
  { value: "minutes", label: "Minutes" },
  { value: "hours", label: "Hours" },
  { value: "days", label: "Days" },
]

const TORRENT_STATES: { value: string; label: string }[] = [
  { value: "downloading", label: "Downloading" },
  { value: "uploading", label: "Seeding" },
  { value: "forcedUP", label: "Forced (UP)" },
  { value: "forcedDL", label: "Forced (DL)" },
  { value: "pausedUP", label: "Paused (UP)" },
  { value: "pausedDL", label: "Paused (DL)" },
  { value: "stoppedUP", label: "Stopped (UP)" },
  { value: "stoppedDL", label: "Stopped (DL)" },
  { value: "queuedUP", label: "Queued (UP)" },
  { value: "queuedDL", label: "Queued (DL)" },
  { value: "stalledUP", label: "Stalled (UP)" },
  { value: "stalledDL", label: "Stalled (DL)" },
  { value: "error", label: "Error" },
  { value: "missingFiles", label: "Missing Files" },
  { value: "checkingUP", label: "Checking (UP)" },
  { value: "checkingDL", label: "Checking (DL)" },
  { value: "moving", label: "Moving" },
  { value: "checkingResumeData", label: "Checking Resume Data" },
  { value: "allocating", label: "Allocating" },
  { value: "metaDL", label: "Fetching Metadata" },
  { value: "unknown", label: "Unknown" },
]

interface ValueInputProps {
  columnType: ColumnType
  value: string
  onChange: (value: string) => void
  unit?: { value: SizeUnit | SpeedUnit | DurationUnit; onChange: (unit: SizeUnit | SpeedUnit | DurationUnit) => void }
  onKeyDown: (e: KeyboardEvent) => void
  caseSensitive?: boolean
  onCaseSensitiveChange?: (value: boolean) => void
}

function ValueInput({
  columnType,
  value,
  onChange,
  unit,
  onKeyDown,
  caseSensitive,
  onCaseSensitiveChange,
}: ValueInputProps) {
  const isSizeColumn = columnType === "size"
  const isSpeedColumn = columnType === "speed"
  const isDurationColumn = columnType === "duration"
  const isPercentageColumn = columnType === "percentage"
  const isBooleanColumn = columnType === "boolean"
  const isEnumColumn = columnType === "enum"
  const isStringColumn = columnType === "string"

  if (isSizeColumn && unit) {
    return (
      <div className="flex gap-2">
        <Input
          type="number"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Enter size..."
          onKeyDown={onKeyDown}
          className="flex-1"
        />
        <Select
          value={unit.value as string}
          onValueChange={(v) => unit.onChange(v as SizeUnit)}
        >
          <SelectTrigger className="w-24">
            <SelectValue/>
          </SelectTrigger>
          <SelectContent>
            {SIZE_UNITS.map((u) => (
              <SelectItem key={u.value} value={u.value}>
                {u.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    )
  }

  if (isSpeedColumn && unit) {
    return (
      <div className="flex gap-2">
        <Input
          type="number"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Enter speed..."
          onKeyDown={onKeyDown}
          className="flex-1"
        />
        <Select
          value={unit.value as string}
          onValueChange={(v) => unit.onChange(v as SizeUnit)}
        >
          <SelectTrigger className="w-24">
            <SelectValue/>
          </SelectTrigger>
          <SelectContent>
            {SPEED_UNITS.map((u) => (
              <SelectItem key={u.value} value={u.value}>
                {u.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    )
  }

  if (isDurationColumn && unit) {
    return (
      <div className="flex gap-2">
        <Input
          type="number"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Enter duration..."
          onKeyDown={onKeyDown}
          className="flex-1"
        />
        <Select
          value={unit.value as string}
          onValueChange={(v) => unit.onChange(v as DurationUnit)}
        >
          <SelectTrigger className="w-28">
            <SelectValue/>
          </SelectTrigger>
          <SelectContent>
            {DURATION_UNITS.map((u) => (
              <SelectItem key={u.value} value={u.value}>
                {u.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    )
  }

  if (isPercentageColumn) {
    return (
      <div className="relative">
        <Input
          type="number"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Enter percentage..."
          onKeyDown={onKeyDown}
          className="pr-8"
        />
        <div className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-3 text-muted-foreground">
          %
        </div>
      </div>
    )
  }

  if (isBooleanColumn) {
    return (
      <Select
        value={value}
        onValueChange={onChange}
      >
        <SelectTrigger>
          <SelectValue placeholder="Select value"/>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="true">True</SelectItem>
          <SelectItem value="false">False</SelectItem>
        </SelectContent>
      </Select>
    )
  }

  if (isEnumColumn) {
    return (
      <Select
        value={value}
        onValueChange={onChange}
      >
        <SelectTrigger>
          <SelectValue placeholder="Select status"/>
        </SelectTrigger>
        <SelectContent>
          {TORRENT_STATES.map((state) => (
            <SelectItem key={state.value} value={state.value}>
              {state.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  }

  if (isStringColumn && onCaseSensitiveChange) {
    return (
      <div className="flex gap-2">
        <Input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Enter value..."
          onKeyDown={onKeyDown}
          className="flex-1"
        />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="outline"
              size="icon"
              className={`${caseSensitive ? "text-primary hover:text-primary/80" : "text-muted-foreground"}`}
              onClick={() => onCaseSensitiveChange(!caseSensitive)}
            >
              <CaseSensitive className="size-4"/>
            </Button>
          </TooltipTrigger>
          <TooltipContent>{caseSensitive ? "Match case (click to ignore)" : "Ignore case (click to match)"}</TooltipContent>
        </Tooltip>
      </div>
    )
  }

  return (
    <Input
      type={columnType === "number" ? "number" : columnType === "date" ? "date" : "text"}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={`Enter ${columnType === "number" ? "number" : "value"}...`}
      onKeyDown={onKeyDown}
    />
  )
}

export function ColumnFilterPopover({
  columnId,
  columnName,
  columnType,
  currentFilter,
  onApply,
}: ColumnFilterPopoverProps) {
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const lastScrollPosition = useRef({ left: 0, top: 0 })

  const [open, setOpen] = useState(false)
  const [operation, setOperation] = useState<FilterOperation>(
    currentFilter?.operation || getDefaultOperation(columnType)
  )
  const [value, setValue] = useState(
    currentFilter?.value ||
    (columnType === "boolean" ? "true" : columnType === "enum" ? "downloading" : "")
  )
  const [value2, setValue2] = useState(currentFilter?.value2 || "")
  const [sizeUnit, setSizeUnit] = useState<SizeUnit>(
    currentFilter?.sizeUnit || "MiB"
  )
  const [sizeUnit2, setSizeUnit2] = useState<SizeUnit>(
    currentFilter?.sizeUnit2 || "MiB"
  )
  const [speedUnit, setSpeedUnit] = useState<SpeedUnit>(
    currentFilter?.speedUnit || "MiB/s"
  )
  const [speedUnit2, setSpeedUnit2] = useState<SpeedUnit>(
    currentFilter?.speedUnit2 || "MiB/s"
  )
  const [durationUnit, setDurationUnit] = useState<DurationUnit>(
    currentFilter?.durationUnit || "hours"
  )
  const [durationUnit2, setDurationUnit2] = useState<DurationUnit>(
    currentFilter?.durationUnit2 || "hours"
  )
  const [caseSensitive, setCaseSensitive] = useState<boolean>(
    currentFilter?.caseSensitive ?? true
  )

  const isSizeColumn = columnType === "size"
  const isSpeedColumn = columnType === "speed"
  const isDurationColumn = columnType === "duration"
  const isStringColumn = columnType === "string"
  const isBetweenOperation = operation === "between"

  const operations = getOperations(columnType)

  const handleApply = () => {
    if (value.trim() === "" || (isBetweenOperation && value2.trim() === "")) {
      onApply(null)
    } else {
      const filter: ColumnFilter = {
        columnId,
        operation,
        value: value.trim(),
      }

      if (isBetweenOperation) {
        filter.value2 = value2.trim()
      }

      if (isSizeColumn) {
        filter.sizeUnit = sizeUnit
        if (isBetweenOperation) {
          filter.sizeUnit2 = sizeUnit2
        }
      }

      if (isSpeedColumn) {
        filter.speedUnit = speedUnit
        if (isBetweenOperation) {
          filter.speedUnit2 = speedUnit2
        }
      }

      if (isDurationColumn) {
        filter.durationUnit = durationUnit
        if (isBetweenOperation) {
          filter.durationUnit2 = durationUnit2
        }
      }

      if (isStringColumn) {
        filter.caseSensitive = caseSensitive
      }

      onApply(filter)
    }
    setOpen(false)
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter") {
      handleApply()
    }
    e.stopPropagation()
  }

  const handleClear = () => {
    setValue("")
    setValue2("")
    setSizeUnit("MiB")
    setSizeUnit2("MiB")
    setSpeedUnit("MiB/s")
    setSpeedUnit2("MiB/s")
    setDurationUnit("hours")
    setDurationUnit2("hours")
    setCaseSensitive(true)
    setOperation(getDefaultOperation(columnType))
    onApply(null)
    setOpen(false)
  }

  useEffect(() => {
    if (!open) {
      return
    }

    const triggerEl = triggerRef.current
    const scrollParent = getScrollableParent(triggerEl)

    if (!scrollParent) {
      return
    }

    lastScrollPosition.current = {
      left: scrollParent.scrollLeft,
      top: scrollParent.scrollTop,
    }

    const handleScroll = () => {
      const left = scrollParent.scrollLeft
      const top = scrollParent.scrollTop
      const hasHorizontalScroll = Math.abs(left - lastScrollPosition.current.left) > 0

      lastScrollPosition.current = { left, top }

      if (hasHorizontalScroll) {
        setOpen((prev) => (prev ? false : prev))
      }
    }

    scrollParent.addEventListener("scroll", handleScroll, { passive: true })

    return () => {
      scrollParent.removeEventListener("scroll", handleScroll)
    }
  }, [open])

  const hasActiveFilter = currentFilter !== undefined && currentFilter !== null

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          ref={triggerRef}
          className={`h-6 w-6 p-0 transition-opacity ${
            hasActiveFilter || open ? "opacity-100 text-primary" : "opacity-10 group-hover:opacity-100 focus:opacity-100 focus-visible:opacity-100 active:opacity-100 text-muted-foreground"
          }`}
          onClick={(e) => {
            e.stopPropagation()
            setOpen(true)
          }}
        >
          <Filter className={`h-3.5 w-3.5 ${hasActiveFilter ? "fill-current" : ""}`}/>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-80"
        align="center"
        onClick={(e) => e.stopPropagation()}
        onPointerDown={(e) => e.stopPropagation()}
        onMouseDown={(e) => e.stopPropagation()}
        onDragStart={(e) => e.preventDefault()}
      >
        <div className="grid gap-4">
          <div className="space-y-2">
            <h4 className="font-medium leading-none">Filter {columnName}</h4>
            <p className="text-sm text-muted-foreground">
              Set conditions to filter this column
            </p>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="operation">Operation</Label>
            <Select
              value={operation}
              onValueChange={(value) => setOperation(value as FilterOperation)}
            >
              <SelectTrigger id="operation">
                <SelectValue placeholder="Select operation"/>
              </SelectTrigger>
              <SelectContent>
                {operations.map((op) => (
                  <SelectItem key={op.value} value={op.value}>
                    {op.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="value">{isBetweenOperation ? "From" : "Value"}</Label>
            <ValueInput
              columnType={columnType}
              value={value}
              onChange={setValue}
              unit={
                isSizeColumn ? {
                  value: sizeUnit,
                  onChange: (u) => setSizeUnit(u as SizeUnit),
                } : isSpeedColumn ? {
                  value: speedUnit,
                  onChange: (u) => setSpeedUnit(u as SpeedUnit),
                } : isDurationColumn ? {
                  value: durationUnit,
                  onChange: (u) => setDurationUnit(u as DurationUnit),
                } : undefined
              }
              onKeyDown={handleKeyDown}
              caseSensitive={caseSensitive}
              onCaseSensitiveChange={setCaseSensitive}
            />
          </div>
          {isBetweenOperation && (
            <div className="grid gap-2">
              <Label htmlFor="value2">To</Label>
              <ValueInput
                columnType={columnType}
                value={value2}
                onChange={setValue2}
                unit={
                  isSizeColumn ? {
                    value: sizeUnit2,
                    onChange: (u) => setSizeUnit2(u as SizeUnit),
                  } : isSpeedColumn ? {
                    value: speedUnit2,
                    onChange: (u) => setSpeedUnit2(u as SpeedUnit),
                  } : isDurationColumn ? {
                    value: durationUnit2,
                    onChange: (u) => setDurationUnit2(u as DurationUnit),
                  } : undefined
                }
                onKeyDown={handleKeyDown}
                caseSensitive={caseSensitive}
                onCaseSensitiveChange={setCaseSensitive}
              />
            </div>
          )}
          <div className="flex gap-2">
            <Button onClick={handleApply} className="flex-1">
              Apply Filter
            </Button>
            {hasActiveFilter && (
              <Button onClick={handleClear} variant="outline" size="icon">
                <X className="h-4 w-4"/>
              </Button>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
