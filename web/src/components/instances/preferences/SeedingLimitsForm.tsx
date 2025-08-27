/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { toast } from "sonner"

function NumberInput({
  label,
  value,
  onChange,
  min = 0,
  max = 999999,
  description,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  min?: number
  max?: number
  description?: string
}) {
  return (
    <div className="space-y-2">
      <div className="space-y-1">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Input
        type="number"
        value={value}
        onChange={(e) => {
          const num = parseInt(e.target.value) || 0
          onChange(Math.max(min, Math.min(max, num)))
        }}
        min={min}
        max={max}
        className="w-full"
      />
    </div>
  )
}

function SwitchSetting({
  label,
  checked,
  onCheckedChange,
  description,
}: {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  description?: string
}) {
  return (
    <div className="flex items-center justify-between space-x-2">
      <div className="space-y-1">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}

interface SeedingLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SeedingLimitsForm({ instanceId, onSuccess }: SeedingLimitsFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const form = useForm({
    defaultValues: {
      max_ratio_enabled: false,
      max_ratio: 2.0,
      max_seeding_time_enabled: false,
      max_seeding_time: 1440,
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        toast.success("Seeding limits updated successfully")
        onSuccess?.()
      } catch (error) {
        toast.error("Failed to update seeding limits")
      }
    },
  })

  // Update form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("max_ratio_enabled", preferences.max_ratio_enabled ?? false)
      form.setFieldValue("max_ratio", preferences.max_ratio ?? 2.0)
      form.setFieldValue("max_seeding_time_enabled", preferences.max_seeding_time_enabled ?? false)
      form.setFieldValue("max_seeding_time", preferences.max_seeding_time ?? 1440)
    }
  }, [preferences, form])

  if (isLoading) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">Loading seeding limits...</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">Failed to load preferences</p>
      </div>
    )
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-6"
    >
      <div className="space-y-6">
        <form.Field name="max_ratio_enabled">
          {(field) => (
            <SwitchSetting
              label="Enable Share Ratio Limit"
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description="Stop seeding when ratio is reached"
            />
          )}
        </form.Field>

        <form.Field name="max_ratio">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Maximum Share Ratio</Label>
              <p className="text-xs text-muted-foreground">
                Stop seeding at this upload/download ratio
              </p>
              <Input
                type="number"
                step="0.1"
                min="0"
                max="10"
                value={(field.state.value as number) ?? 2.0}
                onChange={(e) => field.handleChange(parseFloat(e.target.value) || 2.0)}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="max_seeding_time_enabled">
          {(field) => (
            <SwitchSetting
              label="Enable Seeding Time Limit"
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description="Stop seeding after specified time"
            />
          )}
        </form.Field>

        <form.Field name="max_seeding_time">
          {(field) => (
            <NumberInput
              label="Maximum Seeding Time (minutes)"
              value={(field.state.value as number) ?? 1440}
              onChange={field.handleChange}
              min={1}
              max={525600} // 1 year in minutes
              description="Stop seeding after this many minutes"
            />
          )}
        </form.Field>
      </div>

      <div className="flex justify-end pt-4">
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              disabled={!canSubmit || isSubmitting || isUpdating}
              className="min-w-32"
            >
              {isSubmitting || isUpdating ? "Saving..." : "Save Changes"}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}