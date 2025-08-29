/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Download, Upload, Clock } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { toast } from "sonner"

// Convert bytes/s to MiB/s for display
function bytesToMiB(bytes: number): number {
  return bytes === 0 ? 0 : bytes / (1024 * 1024)
}

// Convert MiB/s to bytes/s for API
function mibToBytes(mib: number): number {
  return mib === 0 ? 0 : Math.round(mib * 1024 * 1024)
}

// Day options for scheduler
const dayOptions = [
  { value: 0, label: "Every day" },
  { value: 1, label: "Every weekday" },
  { value: 2, label: "Every weekend" },
  { value: 3, label: "Monday" },
  { value: 4, label: "Tuesday" },
  { value: 5, label: "Wednesday" },
  { value: 6, label: "Thursday" },
  { value: 7, label: "Friday" },
  { value: 8, label: "Saturday" },
  { value: 9, label: "Sunday" },
]

function SpeedLimitInput({
  label,
  value,
  onChange,
  icon: Icon,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  icon: React.ComponentType<{ className?: string }>
}) {
  const displayValue = bytesToMiB(value)

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 text-muted-foreground" />
        <Label className="text-sm font-medium">{label}</Label>
      </div>
      <div className="flex items-center gap-2">
        <Input
          type="number"
          min="0"
          step="0.1"
          value={displayValue === 0 ? "" : displayValue.toFixed(1)}
          onChange={(e) => {
            const mibValue = e.target.value === "" ? 0 : parseFloat(e.target.value)
            if (!isNaN(mibValue) && mibValue >= 0) {
              onChange(mibToBytes(mibValue))
            }
          }}
          placeholder="0 (Unlimited)"
          className="flex-1"
        />
        <span className="text-sm text-muted-foreground min-w-12">MiB/s</span>
      </div>
    </div>
  )
}

function TimeInput({
  hour,
  minute,
  onHourChange,
  onMinuteChange,
  disabled = false,
}: {
  hour: number
  minute: number
  onHourChange: (hour: number) => void
  onMinuteChange: (minute: number) => void
  disabled?: boolean
}) {
  return (
    <div className="flex items-center gap-1">
      <Input
        type="number"
        min="0"
        max="23"
        value={hour.toString().padStart(2, "0")}
        onChange={(e) => {
          const value = parseInt(e.target.value, 10)
          if (!isNaN(value) && value >= 0 && value <= 23) {
            onHourChange(value)
          }
        }}
        disabled={disabled}
        className="w-16 text-center"
      />
      <span className="text-muted-foreground">:</span>
      <Input
        type="number"
        min="0"
        max="59"
        value={minute.toString().padStart(2, "0")}
        onChange={(e) => {
          const value = parseInt(e.target.value, 10)
          if (!isNaN(value) && value >= 0 && value <= 59) {
            onMinuteChange(value)
          }
        }}
        disabled={disabled}
        className="w-16 text-center"
      />
    </div>
  )
}

interface SpeedLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SpeedLimitsForm({ instanceId, onSuccess }: SpeedLimitsFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)


  // Track if form is being actively edited
  const [isFormDirty, setIsFormDirty] = React.useState(false)

  // Memoize preferences to prevent unnecessary form resets
  const memoizedPreferences = React.useMemo(() => preferences, [
    preferences,
  ])

  const form = useForm({
    defaultValues: {
      dl_limit: 0,
      up_limit: 0,
      alt_dl_limit: 0,
      alt_up_limit: 0,
      scheduler_enabled: false,
      schedule_from_hour: 16,
      schedule_from_min: 0,
      schedule_to_hour: 23,
      schedule_to_min: 0,
      scheduler_days: 0,
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        setIsFormDirty(false) // Reset dirty flag after successful save
        toast.success("Speed limits updated successfully")
        onSuccess?.()
      } catch {
        toast.error("Failed to update speed limits")
      }
    },
  })


  // Update form when preferences change (but only if form is not being actively edited)
  React.useEffect(() => {
    if (memoizedPreferences && !isFormDirty) {
      form.setFieldValue("dl_limit", memoizedPreferences.dl_limit)
      form.setFieldValue("up_limit", memoizedPreferences.up_limit)
      form.setFieldValue("alt_dl_limit", memoizedPreferences.alt_dl_limit)
      form.setFieldValue("alt_up_limit", memoizedPreferences.alt_up_limit)
      form.setFieldValue("scheduler_enabled", memoizedPreferences.scheduler_enabled)
      form.setFieldValue("schedule_from_hour", memoizedPreferences.schedule_from_hour)
      form.setFieldValue("schedule_from_min", memoizedPreferences.schedule_from_min)
      form.setFieldValue("schedule_to_hour", memoizedPreferences.schedule_to_hour)
      form.setFieldValue("schedule_to_min", memoizedPreferences.schedule_to_min)
      form.setFieldValue("scheduler_days", memoizedPreferences.scheduler_days)
    }
  }, [memoizedPreferences, form, isFormDirty])

  if (isLoading) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">Loading speed limits...</p>
      </div>
    )
  }

  if (!memoizedPreferences) {
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
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <form.Field name="dl_limit">
          {(field) => (
            <SpeedLimitInput
              label="Download Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
              icon={Download}
            />
          )}
        </form.Field>

        <form.Field name="up_limit">
          {(field) => (
            <SpeedLimitInput
              label="Upload Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
              icon={Upload}
            />
          )}
        </form.Field>

        <form.Field name="alt_dl_limit">
          {(field) => (
            <SpeedLimitInput
              label="Alternative Download Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
              icon={Download}
            />
          )}
        </form.Field>

        <form.Field name="alt_up_limit">
          {(field) => (
            <SpeedLimitInput
              label="Alternative Upload Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
              icon={Upload}
            />
          )}
        </form.Field>
      </div>

      {/* Scheduler Section */}
      <div className="space-y-4 pt-6 border-t border-border">
        <form.Field name="scheduler_enabled">
          {(field) => (
            <div className="flex items-center gap-3">
              <Switch
                checked={field.state.value as boolean}
                onCheckedChange={(checked) => {
                  setIsFormDirty(true)
                  field.handleChange(checked)
                }}
              />
              <div className="flex items-center gap-2">
                <Clock className="h-4 w-4 text-muted-foreground" />
                <Label className="text-sm font-medium">
                  Schedule the use of alternative rate limits
                </Label>
              </div>
            </div>
          )}
        </form.Field>

        <form.Subscribe selector={(state) => state.values.scheduler_enabled}>
          {(schedulerEnabled) => (
            schedulerEnabled && (
              <div className="space-y-4">
                <div className="grid grid-cols-2 gap-6">
                  <div className="space-y-2">
                    <Label className="text-sm font-medium">From:</Label>
                    <div className="flex items-center gap-4">
                      <form.Field name="schedule_from_hour">
                        {(hourField) => (
                          <form.Field name="schedule_from_min">
                            {(minField) => (
                              <TimeInput
                                hour={(hourField.state.value as number) ?? 16}
                                minute={(minField.state.value as number) ?? 0}
                                onHourChange={(hour) => {
                                  setIsFormDirty(true)
                                  hourField.handleChange(hour)
                                }}
                                onMinuteChange={(minute) => {
                                  setIsFormDirty(true)
                                  minField.handleChange(minute)
                                }}
                              />
                            )}
                          </form.Field>
                        )}
                      </form.Field>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <Label className="text-sm font-medium">To:</Label>
                    <div className="flex items-center gap-4">
                      <form.Field name="schedule_to_hour">
                        {(hourField) => (
                          <form.Field name="schedule_to_min">
                            {(minField) => (
                              <TimeInput
                                hour={(hourField.state.value as number) ?? 23}
                                minute={(minField.state.value as number) ?? 0}
                                onHourChange={(hour) => {
                                  setIsFormDirty(true)
                                  hourField.handleChange(hour)
                                }}
                                onMinuteChange={(minute) => {
                                  setIsFormDirty(true)
                                  minField.handleChange(minute)
                                }}
                              />
                            )}
                          </form.Field>
                        )}
                      </form.Field>
                    </div>
                  </div>
                </div>

                <div className="space-y-2">
                  <Label className="text-sm font-medium">When:</Label>
                  <form.Field name="scheduler_days">
                    {(field) => (
                      <Select
                        value={(field.state.value as number).toString()}
                        onValueChange={(value) => {
                          setIsFormDirty(true)
                          field.handleChange(parseInt(value, 10))
                        }}
                      >
                        <SelectTrigger className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {dayOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value.toString()}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    )}
                  </form.Field>
                </div>
              </div>
            )
          )}
        </form.Subscribe>
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