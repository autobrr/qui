/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  ResponsiveCommandPopover,
  ResponsiveCommand,
  ResponsiveCommandInput,
  ResponsiveCommandList,
  ResponsiveCommandEmpty,
  ResponsiveCommandGroup,
  ResponsiveCommandItem,
  useResponsiveMobile
} from "@/components/ui/responsive-command-popover"
import { cn } from "@/lib/utils"
import { Check, ChevronsUpDown, X } from "lucide-react"
import * as React from "react"

export interface Option {
  label: string
  value: string
  level?: number
  /** Optional icon element to display before the label */
  icon?: React.ReactNode
}

interface MultiSelectProps {
  options: Option[]
  selected: string[]
  onChange: (selected: string[]) => void
  placeholder?: string
  className?: string
  creatable?: boolean
  onCreateOption?: (inputValue: string) => void
  disabled?: boolean
  /** Hide the check icon in dropdown items (useful when options have icons) */
  hideCheckIcon?: boolean
  title?: string
}

export function MultiSelect({
  options,
  selected,
  onChange,
  placeholder = "Select items...",
  className,
  creatable = false,
  onCreateOption,
  disabled = false,
  hideCheckIcon = false,
  title = "Select",
}: MultiSelectProps) {
  const [open, setOpen] = React.useState(false)
  const [inputValue, setInputValue] = React.useState("")

  const selectedSet = React.useMemo(() => new Set(selected), [selected])

  const handleUnselect = (item: string) => {
    onChange(selected.filter((i) => i !== item))
  }

  const handleSelect = (item: string) => {
    if (selectedSet.has(item)) {
      handleUnselect(item)
    } else {
      onChange([...selected, item])
    }
    setInputValue("")
  }

  const handleCreate = () => {
    if (inputValue.trim() && onCreateOption) {
      onCreateOption(inputValue.trim())
      setInputValue("")
    } else if (inputValue.trim()) {
      handleSelect(inputValue.trim())
    }
  }

  const triggerButton = (
    <Button
      type="button"
      variant="outline"
      role="combobox"
      aria-expanded={open}
      disabled={disabled}
      className={cn("w-full justify-between h-auto min-h-10 hover:bg-background", className)}
    >
      <div className="flex flex-wrap gap-1 flex-1 text-left">
        {selected.length > 0 ? (
          selected.map((item) => {
            const option = options.find((o) => o.value === item)
            return (
              <Badge
                variant="secondary"
                key={item}
                className="shrink-0"
                onClick={(e) => {
                  e.stopPropagation()
                  handleUnselect(item)
                }}
              >
                {option?.icon && <span className="mr-1 shrink-0">{option.icon}</span>}
                {option?.label || item}
                <span
                  role="button"
                  tabIndex={0}
                  className="ml-1 ring-offset-background rounded-full outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 cursor-pointer"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault()
                      handleUnselect(item)
                    }
                  }}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                  }}
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    handleUnselect(item)
                  }}
                >
                  <X className="h-3 w-3 text-muted-foreground hover:text-foreground" />
                </span>
              </Badge>
            )
          })
        ) : (
          <span className="text-muted-foreground font-normal">{placeholder}</span>
        )}
      </div>
      <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
    </Button>
  )

  return (
    <ResponsiveCommandPopover
      open={open}
      onOpenChange={setOpen}
      trigger={triggerButton}
      title={title}
      popoverWidth="100%"
      popoverAlign="start"
    >
      <MultiSelectContent
        options={options}
        selectedSet={selectedSet}
        inputValue={inputValue}
        setInputValue={setInputValue}
        handleSelect={handleSelect}
        handleCreate={handleCreate}
        creatable={creatable}
        hideCheckIcon={hideCheckIcon}
      />
    </ResponsiveCommandPopover>
  )
}

function MultiSelectContent({
  options,
  selectedSet,
  inputValue,
  setInputValue,
  handleSelect,
  handleCreate,
  creatable,
  hideCheckIcon,
}: {
  options: Option[]
  selectedSet: Set<string>
  inputValue: string
  setInputValue: (value: string) => void
  handleSelect: (item: string) => void
  handleCreate: () => void
  creatable: boolean
  hideCheckIcon: boolean
}) {
  const isMobile = useResponsiveMobile()

  const displayOptions = isMobile ? options : options.filter((option) => !selectedSet.has(option.value))

  return (
    <ResponsiveCommand>
      <ResponsiveCommandInput
        placeholder="Search..."
        value={inputValue}
        onValueChange={setInputValue}
      />
      <ResponsiveCommandList>
        <ResponsiveCommandEmpty>
          {creatable && inputValue.trim() ? (
            <div
              className={cn(
                "cursor-pointer hover:bg-accent hover:text-accent-foreground rounded-lg",
                isMobile ? "py-4 px-4 text-base" : "py-2 px-4 text-sm"
              )}
              onClick={handleCreate}
            >
              Create "{inputValue}"
            </div>
          ) : (
            "No results found."
          )}

        </ResponsiveCommandEmpty>
        <ResponsiveCommandGroup className="overflow-auto w-full md:max-h-64">
          {displayOptions.map((option) => {
            const isSelected = selectedSet.has(option.value)
            return (
              <ResponsiveCommandItem
                key={option.value}
                value={option.label} // Use label for search matching
                disableHighlight={isMobile}
                onSelect={() => {
                  handleSelect(option.value)
                  // Keep open for multi-select convenience
                }}
                className={isSelected ? "text-primary" : undefined}
              >
                {!hideCheckIcon && (
                  <Check
                    className={cn(
                      "shrink-0",
                      isMobile ? "mr-3 h-5 w-5" : "mr-2 h-4 w-4",
                      isSelected ? "opacity-100 text-primary" : "opacity-0"
                    )}
                  />
                )}
                {option.icon && <span className={cn("shrink-0", isMobile ? "mr-2.5" : "mr-1.5")}>{option.icon}</span>}
                <span
                  className="truncate"
                  style={option.level ? { paddingLeft: option.level * 12 } : undefined}
                >
                  {option.label}
                </span>
              </ResponsiveCommandItem>
            )
          })}
        </ResponsiveCommandGroup>
      </ResponsiveCommandList>
    </ResponsiveCommand>
  )
}
