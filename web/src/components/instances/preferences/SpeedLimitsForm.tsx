/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useForm } from "@tanstack/react-form"
import { Clock, Download, Upload } from "lucide-react"
import React from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// Convert bytes/s to MiB/s for display
function bytesToMiB(bytes: number): number {
  return bytes === 0 ? 0 : bytes / (1024 * 1024)
}

// Convert MiB/s to bytes/s for API
function mibToBytes(mib: number): number {
  return mib === 0 ? 0 : Math.round(mib * 1024 * 1024)
}

function SpeedLimitInput({
  label,
  value,
  onChange,
  icon: Icon,
  placeholder,
  unitLabel,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  icon: React.ComponentType<{ className?: string }>
  placeholder: string
  unitLabel: string
}) {
  const inputId = React.useId()
  const [localValue, setLocalValue] = React.useState("")
  const [isFocused, setIsFocused] = React.useState(false)

  // Sync local value from props when not focused
  React.useEffect(() => {
    if (!isFocused) {
      const displayValue = bytesToMiB(value)
      setLocalValue(displayValue === 0 ? "" : displayValue.toFixed(1))
    }
  }, [value, isFocused])

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
        <Label htmlFor={inputId} className="text-sm font-medium">{label}</Label>
      </div>
      <div className="flex items-center gap-2">
        <Input
          id={inputId}
          type="number"
          min="0"
          step="0.1"
          value={localValue}
          onFocus={() => setIsFocused(true)}
          onChange={(e) => {
            setLocalValue(e.target.value)
            const mibValue = e.target.value === "" ? 0 : parseFloat(e.target.value)
            if (!isNaN(mibValue) && mibValue >= 0) {
              onChange(mibToBytes(mibValue))
            }
          }}
          onBlur={() => setIsFocused(false)}
          placeholder={placeholder}
          className="flex-1"
          aria-describedby={`${inputId}-unit`}
        />
        <span id={`${inputId}-unit`} className="text-sm text-muted-foreground min-w-12">{unitLabel}</span>
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
  groupAriaLabel,
  hourAriaLabel,
  minuteAriaLabel,
}: {
  hour: number
  minute: number
  onHourChange: (hour: number) => void
  onMinuteChange: (minute: number) => void
  disabled?: boolean
  groupAriaLabel: string
  hourAriaLabel: string
  minuteAriaLabel: string
}) {
  return (
    <div className="flex items-center gap-1" role="group" aria-label={groupAriaLabel}>
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
        aria-label={hourAriaLabel}
      />
      <span className="text-muted-foreground" aria-hidden="true">:</span>
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
        aria-label={minuteAriaLabel}
      />
    </div>
  )
}

interface SpeedLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SpeedLimitsForm({ instanceId, onSuccess }: SpeedLimitsFormProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const dayOptions = React.useMemo(() => ([
    { value: 0, label: tr("speedLimitsForm.dayOptions.everyDay") },
    { value: 1, label: tr("speedLimitsForm.dayOptions.everyWeekday") },
    { value: 2, label: tr("speedLimitsForm.dayOptions.everyWeekend") },
    { value: 3, label: tr("speedLimitsForm.dayOptions.monday") },
    { value: 4, label: tr("speedLimitsForm.dayOptions.tuesday") },
    { value: 5, label: tr("speedLimitsForm.dayOptions.wednesday") },
    { value: 6, label: tr("speedLimitsForm.dayOptions.thursday") },
    { value: 7, label: tr("speedLimitsForm.dayOptions.friday") },
    { value: 8, label: tr("speedLimitsForm.dayOptions.saturday") },
    { value: 9, label: tr("speedLimitsForm.dayOptions.sunday") },
  ]), [tr])

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
        await updatePreferences(value)
        setIsFormDirty(false) // Reset dirty flag after successful save
        toast.success(tr("speedLimitsForm.toasts.updated"))
        onSuccess?.()
      } catch {
        toast.error(tr("speedLimitsForm.toasts.failedUpdate"))
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
  // eslint-disable-next-line react-hooks/exhaustive-deps -- form reference is stable, only sync on preferences change
  }, [memoizedPreferences, isFormDirty])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">{tr("speedLimitsForm.states.loading")}</p>
      </div>
    )
  }

  if (!memoizedPreferences) {
    return (
      <div className="flex items-center justify-center py-8" role="alert">
        <p className="text-sm text-muted-foreground">{tr("speedLimitsForm.states.failedLoadPreferences")}</p>
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
        <form.Field
          name="dl_limit"
          validators={{
            onChange: ({ value }) => {
              if (value < 0) {
                return tr("speedLimitsForm.errors.globalDownloadRate")
              }
              return undefined
            },
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <SpeedLimitInput
                label={tr("speedLimitsForm.fields.downloadLimit")}
                value={(field.state.value as number) ?? 0}
                onChange={(value) => {
                  setIsFormDirty(true)
                  field.handleChange(value)
                }}
                icon={Download}
                placeholder={tr("speedLimitsForm.fields.limitPlaceholder")}
                unitLabel={tr("speedLimitsForm.units.mibPerSecond")}
              />
              {field.state.meta.errors.length > 0 && (
                <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field
          name="up_limit"
          validators={{
            onChange: ({ value }) => {
              if (value < 0) {
                return tr("speedLimitsForm.errors.globalUploadRate")
              }
              return undefined
            },
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <SpeedLimitInput
                label={tr("speedLimitsForm.fields.uploadLimit")}
                value={(field.state.value as number) ?? 0}
                onChange={(value) => {
                  setIsFormDirty(true)
                  field.handleChange(value)
                }}
                icon={Upload}
                placeholder={tr("speedLimitsForm.fields.limitPlaceholder")}
                unitLabel={tr("speedLimitsForm.units.mibPerSecond")}
              />
              {field.state.meta.errors.length > 0 && (
                <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field
          name="alt_dl_limit"
          validators={{
            onChange: ({ value }) => {
              if (value < 0) {
                return tr("speedLimitsForm.errors.alternativeDownloadRate")
              }
              return undefined
            },
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <SpeedLimitInput
                label={tr("speedLimitsForm.fields.alternativeDownloadLimit")}
                value={(field.state.value as number) ?? 0}
                onChange={(value) => {
                  setIsFormDirty(true)
                  field.handleChange(value)
                }}
                icon={Download}
                placeholder={tr("speedLimitsForm.fields.limitPlaceholder")}
                unitLabel={tr("speedLimitsForm.units.mibPerSecond")}
              />
              {field.state.meta.errors.length > 0 && (
                <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field
          name="alt_up_limit"
          validators={{
            onChange: ({ value }) => {
              if (value < 0) {
                return tr("speedLimitsForm.errors.alternativeUploadRate")
              }
              return undefined
            },
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <SpeedLimitInput
                label={tr("speedLimitsForm.fields.alternativeUploadLimit")}
                value={(field.state.value as number) ?? 0}
                onChange={(value) => {
                  setIsFormDirty(true)
                  field.handleChange(value)
                }}
                icon={Upload}
                placeholder={tr("speedLimitsForm.fields.limitPlaceholder")}
                unitLabel={tr("speedLimitsForm.units.mibPerSecond")}
              />
              {field.state.meta.errors.length > 0 && (
                <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
              )}
            </div>
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
                  {tr("speedLimitsForm.fields.scheduleAlternativeLimits")}
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
                    <Label className="text-sm font-medium">{tr("speedLimitsForm.labels.from")}</Label>
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
                                groupAriaLabel={tr("speedLimitsForm.aria.startTimeGroup")}
                                hourAriaLabel={tr("speedLimitsForm.aria.startHour")}
                                minuteAriaLabel={tr("speedLimitsForm.aria.startMinute")}
                              />
                            )}
                          </form.Field>
                        )}
                      </form.Field>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <Label className="text-sm font-medium">{tr("speedLimitsForm.labels.to")}</Label>
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
                                groupAriaLabel={tr("speedLimitsForm.aria.endTimeGroup")}
                                hourAriaLabel={tr("speedLimitsForm.aria.endHour")}
                                minuteAriaLabel={tr("speedLimitsForm.aria.endMinute")}
                              />
                            )}
                          </form.Field>
                        )}
                      </form.Field>
                    </div>
                  </div>
                </div>

                <div className="space-y-2">
                  <Label className="text-sm font-medium">{tr("speedLimitsForm.labels.when")}</Label>
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
              {isSubmitting || isUpdating
                ? tr("speedLimitsForm.actions.saving")
                : tr("speedLimitsForm.actions.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
