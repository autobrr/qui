/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DURATION_COLUMNS, SIZE_COLUMNS } from "@/lib/column-constants"
import { Filter, X } from "lucide-react"
import { useState } from "react"

export type FilterOperation =
  | "eq" // equals
  | "ne" // not equals
  | "gt" // greater than
  | "ge" // greater than or equal
  | "lt" // less than
  | "le" // less than or equal
  | "between"
  | "contains"
  | "notContains"
  | "startsWith"
  | "endsWith"

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
  columnType: "number" | "string" | "date"
  currentFilter?: ColumnFilter
  onApply: (filter: ColumnFilter | null) => void
}

const NUMERIC_OPERATIONS: { value: FilterOperation; label: string }[] = [
  { value: "eq", label: "Equal to" },
  { value: "ne", label: "Not equal to" },
  { value: "gt", label: "Greater than" },
  { value: "ge", label: "Greater than or equal" },
  { value: "lt", label: "Less than" },
  { value: "le", label: "Less than or equal" },
  { value: "between", label: "Between" },
]

const STRING_OPERATIONS: { value: FilterOperation; label: string }[] = [
  { value: "eq", label: "Equals" },
  { value: "ne", label: "Not equals" },
  { value: "contains", label: "Contains" },
  { value: "notContains", label: "Does not contain" },
  { value: "startsWith", label: "Starts with" },
  { value: "endsWith", label: "Ends with" },
]

const DATE_OPERATIONS: { value: FilterOperation; label: string }[] = [
  { value: "eq", label: "On" },
  { value: "gt", label: "After" },
  { value: "lt", label: "Before" },
  { value: "between", label: "Between" },
]

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

export function ColumnFilterPopover({
  columnId,
  columnName,
  columnType,
  currentFilter,
  onApply,
}: ColumnFilterPopoverProps) {
  const [open, setOpen] = useState(false)
  const [operation, setOperation] = useState<FilterOperation>(
    currentFilter?.operation || (columnType === "number" ? "gt" : columnType === "date" ? "gt" : "contains")
  )
  const [value, setValue] = useState(currentFilter?.value || "")
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

  const isSizeColumn = SIZE_COLUMNS.includes(columnId as typeof SIZE_COLUMNS[number])
  const isDurationColumn = DURATION_COLUMNS.includes(columnId as typeof DURATION_COLUMNS[number])
  const isBetweenOperation = operation === "between"

  const operations =
    columnType === "number" ? NUMERIC_OPERATIONS : columnType === "date" ? DATE_OPERATIONS : STRING_OPERATIONS

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

  const handleClear = () => {
    setValue("")
    setValue2("")
    setSizeUnit("MiB")
    setDurationUnit("hours")
    setOperation(columnType === "number" ? "gt" : "contains")
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
            {isSizeColumn ? (
              <div className="flex gap-2">
                <Input
                  id="value"
                  type="number"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  placeholder="Enter size..."
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      handleApply()
                    }
                    e.stopPropagation()
                  }}
                  className="flex-1"
                />
                <Select
                  value={sizeUnit}
                  onValueChange={(value) => setSizeUnit(value as SizeUnit)}
                >
                  <SelectTrigger className="w-24">
                    <SelectValue/>
                  </SelectTrigger>
                  <SelectContent>
                    {SIZE_UNITS.map((unit) => (
                      <SelectItem key={unit.value} value={unit.value}>
                        {unit.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ) : isDurationColumn ? (
              <div className="flex gap-2">
                <Input
                  id="value"
                  type="number"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  placeholder="Enter duration..."
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      handleApply()
                    }
                    e.stopPropagation()
                  }}
                  className="flex-1"
                />
                <Select
                  value={durationUnit}
                  onValueChange={(value) => setDurationUnit(value as DurationUnit)}
                >
                  <SelectTrigger className="w-28">
                    <SelectValue/>
                  </SelectTrigger>
                  <SelectContent>
                    {DURATION_UNITS.map((unit) => (
                      <SelectItem key={unit.value} value={unit.value}>
                        {unit.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ) : (
              <Input
                id="value"
                type={columnType === "number" ? "number" : columnType === "date" ? "date" : "text"}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder={`Enter ${columnType === "number" ? "number" : "value"}...`}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    handleApply()
                  }
                  e.stopPropagation()
                }}
              />
            )}
          </div>
          {isBetweenOperation && (
            <div className="grid gap-2">
              <Label htmlFor="value2">To</Label>
              {isSizeColumn ? (
                <div className="flex gap-2">
                  <Input
                    id="value2"
                    type="number"
                    value={value2}
                    onChange={(e) => setValue2(e.target.value)}
                    placeholder="Enter size..."
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        handleApply()
                      }
                      e.stopPropagation()
                    }}
                    className="flex-1"
                  />
                  <Select
                    value={sizeUnit2}
                    onValueChange={(value) => setSizeUnit2(value as SizeUnit)}
                  >
                    <SelectTrigger className="w-24">
                      <SelectValue/>
                    </SelectTrigger>
                    <SelectContent>
                      {SIZE_UNITS.map((unit) => (
                        <SelectItem key={unit.value} value={unit.value}>
                          {unit.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : isDurationColumn ? (
                <div className="flex gap-2">
                  <Input
                    id="value2"
                    type="number"
                    value={value2}
                    onChange={(e) => setValue2(e.target.value)}
                    placeholder="Enter duration..."
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        handleApply()
                      }
                      e.stopPropagation()
                    }}
                    className="flex-1"
                  />
                  <Select
                    value={durationUnit2}
                    onValueChange={(value) => setDurationUnit2(value as DurationUnit)}
                  >
                    <SelectTrigger className="w-28">
                      <SelectValue/>
                    </SelectTrigger>
                    <SelectContent>
                      {DURATION_UNITS.map((unit) => (
                        <SelectItem key={unit.value} value={unit.value}>
                          {unit.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : (
                <Input
                  id="value2"
                  type={columnType === "number" ? "number" : columnType === "date" ? "date" : "text"}
                  value={value2}
                  onChange={(e) => setValue2(e.target.value)}
                  placeholder={`Enter ${columnType === "number" ? "number" : "value"}...`}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      handleApply()
                    }
                    e.stopPropagation()
                  }}
                />
              )}
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
