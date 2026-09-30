/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { usePreferencesForm } from "@/hooks/usePreferencesForm"
import type { AppPreferences } from "@/types"
import { Clock, Download, Upload } from "lucide-react"
import React from "react"
import { useTranslation } from "react-i18next"

import { PreferencesFormShell } from "./PreferencesFormShell"
import { PreferencesSection } from "./PreferencesSection"

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
  groupLabel,
  hourLabel,
  minuteLabel,
}: {
  hour: number
  minute: number
  onHourChange: (hour: number) => void
  onMinuteChange: (minute: number) => void
  disabled?: boolean
  groupLabel: string
  hourLabel: string
  minuteLabel: string
}) {
  return (
    <div className="flex items-center gap-1" role="group" aria-label={groupLabel}>
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
        aria-label={hourLabel}
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
        aria-label={minuteLabel}
      />
    </div>
  )
}

interface SpeedLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SpeedLimitsForm({ instanceId, onSuccess }: SpeedLimitsFormProps) {
  return (
    <PreferencesSection instanceId={instanceId} i18nPrefix="preferences.speedLimits">
      {(preferences) => <SpeedLimitsFields instanceId={instanceId} preferences={preferences} onSuccess={onSuccess} />}
    </PreferencesSection>
  )
}

function SpeedLimitsFields({ instanceId, preferences, onSuccess }: SpeedLimitsFormProps & { preferences: AppPreferences }) {
  const { t } = useTranslation("instances")
  const dayOptions = [
    { value: 0, label: t("preferences.speedLimits.everyDay") },
    { value: 1, label: t("preferences.speedLimits.everyWeekday") },
    { value: 2, label: t("preferences.speedLimits.everyWeekend") },
    { value: 3, label: t("preferences.speedLimits.monday") },
    { value: 4, label: t("preferences.speedLimits.tuesday") },
    { value: 5, label: t("preferences.speedLimits.wednesday") },
    { value: 6, label: t("preferences.speedLimits.thursday") },
    { value: 7, label: t("preferences.speedLimits.friday") },
    { value: 8, label: t("preferences.speedLimits.saturday") },
    { value: 9, label: t("preferences.speedLimits.sunday") },
  ]

  const { form, isUpdating } = usePreferencesForm({
    instanceId,
    preferences,
    toForm: (p) => ({
      dl_limit: p.dl_limit,
      up_limit: p.up_limit,
      alt_dl_limit: p.alt_dl_limit,
      alt_up_limit: p.alt_up_limit,
      scheduler_enabled: p.scheduler_enabled,
      schedule_from_hour: p.schedule_from_hour,
      schedule_from_min: p.schedule_from_min,
      schedule_to_hour: p.schedule_to_hour,
      schedule_to_min: p.schedule_to_min,
      scheduler_days: p.scheduler_days,
    }),
    i18nPrefix: "preferences.speedLimits",
    onSaved: onSuccess,
  })

  return (
    <PreferencesFormShell
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      footer={(
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              disabled={!canSubmit || isSubmitting || isUpdating}
              className="min-w-32"
            >
              {isSubmitting || isUpdating ? t("preferences.common.saving") : t("preferences.common.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      )}
    >
      <div className="space-y-6">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <form.Field
            name="dl_limit"
            validators={{
              onChange: ({ value }) => {
                if (value < 0) {
                  return t("preferences.speedLimits.validation.downloadLimit")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <SpeedLimitInput
                  label={t("preferences.speedLimits.downloadLimit")}
                  value={(field.state.value as number) ?? 0}
                  onChange={field.handleChange}
                  icon={Download}
                  placeholder={t("preferences.speedLimits.unlimitedPlaceholder")}
                  unitLabel={t("preferences.speedLimits.rateUnit")}
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
                  return t("preferences.speedLimits.validation.uploadLimit")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <SpeedLimitInput
                  label={t("preferences.speedLimits.uploadLimit")}
                  value={(field.state.value as number) ?? 0}
                  onChange={field.handleChange}
                  icon={Upload}
                  placeholder={t("preferences.speedLimits.unlimitedPlaceholder")}
                  unitLabel={t("preferences.speedLimits.rateUnit")}
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
                  return t("preferences.speedLimits.validation.altDownloadLimit")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <SpeedLimitInput
                  label={t("preferences.speedLimits.altDownloadLimit")}
                  value={(field.state.value as number) ?? 0}
                  onChange={field.handleChange}
                  icon={Download}
                  placeholder={t("preferences.speedLimits.unlimitedPlaceholder")}
                  unitLabel={t("preferences.speedLimits.rateUnit")}
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
                  return t("preferences.speedLimits.validation.altUploadLimit")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <SpeedLimitInput
                  label={t("preferences.speedLimits.altUploadLimit")}
                  value={(field.state.value as number) ?? 0}
                  onChange={field.handleChange}
                  icon={Upload}
                  placeholder={t("preferences.speedLimits.unlimitedPlaceholder")}
                  unitLabel={t("preferences.speedLimits.rateUnit")}
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
                  onCheckedChange={field.handleChange}
                />
                <div className="flex items-center gap-2">
                  <Clock className="h-4 w-4 text-muted-foreground" />
                  <Label className="text-sm font-medium">
                    {t("preferences.speedLimits.scheduleAltLimits")}
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
                      <Label className="text-sm font-medium">{t("preferences.speedLimits.from")}</Label>
                      <div className="flex items-center gap-4">
                        <form.Field name="schedule_from_hour">
                          {(hourField) => (
                            <form.Field name="schedule_from_min">
                              {(minField) => (
                                <TimeInput
                                  hour={(hourField.state.value as number) ?? 16}
                                  minute={(minField.state.value as number) ?? 0}
                                  onHourChange={hourField.handleChange}
                                  onMinuteChange={minField.handleChange}
                                  groupLabel={t("preferences.speedLimits.timeLabel", { label: t("preferences.speedLimits.start") })}
                                  hourLabel={t("preferences.speedLimits.hourLabel", { label: t("preferences.speedLimits.start") })}
                                  minuteLabel={t("preferences.speedLimits.minuteLabel", { label: t("preferences.speedLimits.start") })}
                                />
                              )}
                            </form.Field>
                          )}
                        </form.Field>
                      </div>
                    </div>

                    <div className="space-y-2">
                      <Label className="text-sm font-medium">{t("preferences.speedLimits.to")}</Label>
                      <div className="flex items-center gap-4">
                        <form.Field name="schedule_to_hour">
                          {(hourField) => (
                            <form.Field name="schedule_to_min">
                              {(minField) => (
                                <TimeInput
                                  hour={(hourField.state.value as number) ?? 23}
                                  minute={(minField.state.value as number) ?? 0}
                                  onHourChange={hourField.handleChange}
                                  onMinuteChange={minField.handleChange}
                                  groupLabel={t("preferences.speedLimits.timeLabel", { label: t("preferences.speedLimits.end") })}
                                  hourLabel={t("preferences.speedLimits.hourLabel", { label: t("preferences.speedLimits.end") })}
                                  minuteLabel={t("preferences.speedLimits.minuteLabel", { label: t("preferences.speedLimits.end") })}
                                />
                              )}
                            </form.Field>
                          )}
                        </form.Field>
                      </div>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <Label className="text-sm font-medium">{t("preferences.speedLimits.when")}</Label>
                    <form.Field name="scheduler_days">
                      {(field) => (
                        <Select
                          value={(field.state.value as number).toString()}
                          onValueChange={(value) => field.handleChange(parseInt(value, 10))}
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
      </div>
    </PreferencesFormShell>
  )
}
