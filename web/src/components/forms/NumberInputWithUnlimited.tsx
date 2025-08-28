/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

interface NumberInputWithUnlimitedProps {
  label: string
  value: number
  onChange: (value: number) => void
  min?: number
  max?: number
  step?: string | number
  description?: string
  allowUnlimited?: boolean
  placeholder?: string
  disabled?: boolean
}

export function NumberInputWithUnlimited({
  label,
  value,
  onChange,
  min = 0,
  max = 999999,
  step,
  description,
  allowUnlimited = false,
  placeholder,
  disabled = false,
}: NumberInputWithUnlimitedProps) {
  // Display value: show empty string for -1 when unlimited is allowed
  const displayValue = allowUnlimited && value === -1 ? "" : value.toString()
  
  // Default placeholder based on unlimited support
  const defaultPlaceholder = allowUnlimited ? "Unlimited" : undefined
  const actualPlaceholder = placeholder ?? defaultPlaceholder

  return (
    <div className="space-y-2">
      <div className="space-y-1">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">
            {description}
            {allowUnlimited && " (use -1 for unlimited)"}
          </p>
        )}
      </div>
      <Input
        type="number"
        value={displayValue}
        onChange={(e) => {
          const inputValue = e.target.value
          
          // Allow temporary empty or negative sign state
          if (inputValue === "" || inputValue === "-") {
            if (allowUnlimited) {
              // If unlimited is allowed and input is empty, treat as -1 (unlimited)
              if (inputValue === "") {
                onChange(-1)
              }
            }
            return
          }
          
          const num = parseFloat(inputValue)
          if (isNaN(num)) return
          
          // Allow -1 for unlimited if allowUnlimited is true
          if (allowUnlimited && num === -1) {
            onChange(-1)
            return
          }
          
          // Otherwise enforce min/max bounds
          onChange(Math.max(min, Math.min(max, num)))
        }}
        min={allowUnlimited ? -1 : min}
        max={max}
        step={step}
        placeholder={actualPlaceholder}
        disabled={disabled}
        className="w-full"
      />
    </div>
  )
}