/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
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
  unlimitedPlaceholder?: string
  unlimitedHint?: string
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
  unlimitedPlaceholder,
  unlimitedHint,
  disabled = false,
}: NumberInputWithUnlimitedProps) {
  // Display value: show empty string for -1 when unlimited is allowed
  const displayValue = allowUnlimited && value === -1 ? "" : value.toString()

  // Default placeholder based on unlimited support
  const defaultPlaceholder = allowUnlimited ? (unlimitedPlaceholder ?? "Unlimited") : undefined
  const actualPlaceholder = placeholder ?? defaultPlaceholder

  // Track previous value to detect when we hit 0 and should transition to unlimited
  const prevValueRef = React.useRef(value)

  React.useEffect(() => {
    if (!allowUnlimited) return

    const prevValue = prevValueRef.current
    const currentValue = value

    // If we stepped down to exactly 0 from a positive value, transition to unlimited
    if (prevValue > 0 && currentValue === 0) {
      onChange(-1)
    }

    prevValueRef.current = value
  }, [value, allowUnlimited, onChange])

  // Handle stepping up from unlimited (-1) back to a valid positive value
  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!allowUnlimited) return

    if (e.key === "ArrowUp" && value === -1) {
      e.preventDefault()
      const stepValue = typeof step === "string" ? parseFloat(step) : (step || 1)
      const minPositive = stepValue // Use the step value as the starting point
      onChange(minPositive)
    }
  }

  return (
    <div className="space-y-2">
      <div className="space-y-1">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">
            {description}
            {allowUnlimited && (unlimitedHint ?? " (use -1 for unlimited)")}
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

          // Handle unlimited values when allowUnlimited is true
          if (allowUnlimited) {
            // Allow exactly -1 for unlimited
            if (num === -1) {
              onChange(-1)
              return
            }

            // Prevent invalid negative values between -1 and 0
            if (num < 0 && num > -1) {
              // Don't update the value, effectively blocking invalid negative values
              return
            }
          }

          // Otherwise enforce min/max bounds
          onChange(Math.max(min, Math.min(max, num)))
        }}
        onKeyDown={handleKeyDown}
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
