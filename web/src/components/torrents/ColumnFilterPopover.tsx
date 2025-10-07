/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { type ColumnType, type FilterOperation } from "@/lib/column-constants"
import { getDefaultOperation, getOperations } from "@/lib/column-filter-utils"
import { Filter, X } from "lucide-react"
import { type KeyboardEvent, useState } from "react"

export type SizeUnit = "B" | "KiB" | "MiB" | "GiB" | "TiB"

export type DurationUnit = "seconds" | "minutes" | "hours" | "days"

export interface ColumnFilter {
  columnId: string
  operation: FilterOperation
  value: string
  value2?: string
  sizeUnit?: SizeUnit
  sizeUnit2?: SizeUnit
  durationUnit?: DurationUnit
  durationUnit2?: DurationUnit
}

interface ColumnFilterPopoverProps {
  columnId: string
  columnName: string
  columnType: ColumnType
  currentFilter?: ColumnFilter
  onApply: (filter: ColumnFilter | null) => void
}

const SIZE_UNITS: { value: SizeUnit; label: string }[] = [
  { value: "B", label: "B" },
  { value: "KiB", label: "KiB" },
  { value: "MiB", label: "MiB" },
  { value: "GiB", label: "GiB" },
  { value: "TiB", label: "TiB" },
]

const DURATION_UNITS: { value: DurationUnit; label: string }[] = [
  { value: "seconds", label: "Seconds" },
  { value: "minutes", label: "Minutes" },
  { value: "hours", label: "Hours" },
  { value: "days", label: "Days" },
]

interface ValueInputProps {
  columnType: ColumnType
  value: string
  onChange: (value: string) => void
  unit?: { value: SizeUnit | DurationUnit; onChange: (unit: SizeUnit | DurationUnit) => void }
  onKeyDown: (e: KeyboardEvent) => void
}

function ValueInput({ columnType, value, onChange, unit, onKeyDown }: ValueInputProps) {
  const isSizeColumn = columnType === "size"
  const isDurationColumn = columnType === "duration"
  const isBooleanColumn = columnType === "boolean"

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
  const [open, setOpen] = useState(false)
  const [operation, setOperation] = useState<FilterOperation>(
    currentFilter?.operation || getDefaultOperation(columnType)
  )
  const [value, setValue] = useState(currentFilter?.value || (columnType === "boolean" ? "true" : ""))
  const [value2, setValue2] = useState(currentFilter?.value2 || "")
  const [sizeUnit, setSizeUnit] = useState<SizeUnit>(
    currentFilter?.sizeUnit || "MiB"
  )
  const [sizeUnit2, setSizeUnit2] = useState<SizeUnit>(
    currentFilter?.sizeUnit2 || "MiB"
  )
  const [durationUnit, setDurationUnit] = useState<DurationUnit>(
    currentFilter?.durationUnit || "hours"
  )
  const [durationUnit2, setDurationUnit2] = useState<DurationUnit>(
    currentFilter?.durationUnit2 || "hours"
  )

  const isSizeColumn = columnType === "size"
  const isDurationColumn = columnType === "duration"
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

      if (isDurationColumn) {
        filter.durationUnit = durationUnit
        if (isBetweenOperation) {
          filter.durationUnit2 = durationUnit2
        }
      }

      onApply(filter)
    }
    setOpen(false)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      handleApply()
    }
    e.stopPropagation()
  }

  const handleClear = () => {
    setValue("")
    setValue2("")
    setSizeUnit("MiB")
    setDurationUnit("hours")
    setOperation(getDefaultOperation(columnType))
    onApply(null)
    setOpen(false)
  }

  const hasActiveFilter = currentFilter !== undefined && currentFilter !== null

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className={`h-6 w-6 p-0 ${hasActiveFilter ? "text-primary" : "text-muted-foreground"}`}
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
                } : isDurationColumn ? {
                  value: durationUnit,
                  onChange: (u) => setDurationUnit(u as DurationUnit),
                } : undefined
              }
              onKeyDown={handleKeyDown}
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
                  } : isDurationColumn ? {
                    value: durationUnit2,
                    onChange: (u) => setDurationUnit2(u as DurationUnit),
                  } : undefined
                }
                onKeyDown={handleKeyDown}
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
