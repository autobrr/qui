/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
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
}: MultiSelectProps) {
  const [open, setOpen] = React.useState(false)
  const [inputValue, setInputValue] = React.useState("")

  const handleUnselect = (item: string) => {
    onChange(selected.filter((i) => i !== item))
  }

  const handleSelect = (item: string) => {
    if (selected.includes(item)) {
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

  // Filter options that are not already selected
  const availableOptions = options.filter((option) => !selected.includes(option.value))

  return (
    <Popover open={open} onOpenChange={setOpen} modal={true}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className={cn("w-full justify-between h-auto min-h-10 hover:bg-background", className)}
        >
          <div className="flex flex-wrap gap-1">
            {selected.length > 0 ? (
              selected.map((item) => {
                const option = options.find((o) => o.value === item)
                return (
                <Badge
                  variant="secondary"
                  key={item}
                  className="mr-1 mb-1"
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
              )})
            ) : (
              <span className="text-muted-foreground font-normal">{placeholder}</span>
            )}
          </div>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-full p-0" align="start">
        <Command>
          <CommandInput
            placeholder="Search..."
            value={inputValue}
            onValueChange={setInputValue}
          />
          <CommandList>
            <CommandEmpty>
                {creatable && inputValue.trim() ? (
                     <div
                     className="py-2 px-4 text-sm cursor-pointer hover:bg-accent hover:text-accent-foreground"
                     onClick={handleCreate}
                   >
                     Create "{inputValue}"
                   </div>
                ) : (
                    "No results found."
                )}

            </CommandEmpty>
            <CommandGroup className="max-h-64 overflow-auto w-full">
              {availableOptions.map((option) => (
                <CommandItem
                  key={option.value}
                  value={option.label} // Use label for search matching
                  onSelect={() => {
                    handleSelect(option.value)
                    // Keep open for multi-select convenience
                  }}
                  className="truncate"
                >
                  {!hideCheckIcon && (
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4 shrink-0",
                        selected.includes(option.value) ? "opacity-100" : "opacity-0"
                      )}
                    />
                  )}
                  {option.icon && <span className="mr-1.5 shrink-0">{option.icon}</span>}
                  <span
                    className="truncate"
                    style={option.level ? { paddingLeft: option.level * 12 } : undefined}
                  >
                    {option.label}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

