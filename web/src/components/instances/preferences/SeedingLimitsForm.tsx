/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { toast } from "sonner"
import { NumberInputWithUnlimited } from "@/components/forms/NumberInputWithUnlimited"
import { useTranslation } from "react-i18next"


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
    <div className="flex items-center gap-3">
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
      <div className="space-y-0.5">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
    </div>
  )
}

interface SeedingLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SeedingLimitsForm({ instanceId, onSuccess }: SeedingLimitsFormProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const form = useForm({
    defaultValues: {
      max_ratio_enabled: false,
      max_ratio: 0,
      max_seeding_time_enabled: false,
      max_seeding_time: 0,
    },
    onSubmit: async ({ value }) => {
      try {
        await updatePreferences(value)
        toast.success(tr("seedingLimitsForm.toasts.updated"))
        onSuccess?.()
      } catch {
        toast.error(tr("seedingLimitsForm.toasts.failedUpdate"))
      }
    },
  })

  // Update form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("max_ratio_enabled", preferences.max_ratio_enabled)
      form.setFieldValue("max_ratio", preferences.max_ratio)
      form.setFieldValue("max_seeding_time_enabled", preferences.max_seeding_time_enabled)
      form.setFieldValue("max_seeding_time", preferences.max_seeding_time)
    }
  }, [preferences, form])

  if (isLoading) {
    return (
      <div className="text-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">{tr("seedingLimitsForm.loading")}</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="text-center py-8" role="alert">
        <p className="text-sm text-muted-foreground">{tr("seedingLimitsForm.loadFailed")}</p>
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
              label={tr("seedingLimitsForm.fields.enableShareRatioLimitLabel")}
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description={tr("seedingLimitsForm.fields.enableShareRatioLimitDescription")}
            />
          )}
        </form.Field>

        <form.Field name="max_ratio_enabled">
          {(enabledField) => (
            <form.Field name="max_ratio">
              {(field) => (
                <NumberInputWithUnlimited
                  label={tr("seedingLimitsForm.fields.maximumShareRatioLabel")}
                  value={(field.state.value as number) ?? 2.0}
                  onChange={field.handleChange}
                  min={-1}
                  max={10}
                  step="0.05"
                  description={tr("seedingLimitsForm.fields.maximumShareRatioDescription")}
                  allowUnlimited={true}
                  disabled={!(enabledField.state.value as boolean)}
                />
              )}
            </form.Field>
          )}
        </form.Field>

        <form.Field name="max_seeding_time_enabled">
          {(field) => (
            <SwitchSetting
              label={tr("seedingLimitsForm.fields.enableSeedingTimeLimitLabel")}
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description={tr("seedingLimitsForm.fields.enableSeedingTimeLimitDescription")}
            />
          )}
        </form.Field>

        <form.Field name="max_seeding_time_enabled">
          {(enabledField) => (
            <form.Field name="max_seeding_time">
              {(field) => (
                <NumberInputWithUnlimited
                  label={tr("seedingLimitsForm.fields.maximumSeedingTimeLabel")}
                  value={(field.state.value as number) ?? 1440}
                  onChange={field.handleChange}
                  min={-1}
                  max={525600} // 1 year in minutes
                  description={tr("seedingLimitsForm.fields.maximumSeedingTimeDescription")}
                  allowUnlimited={true}
                  disabled={!(enabledField.state.value as boolean)}
                />
              )}
            </form.Field>
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
              {isSubmitting || isUpdating ? tr("seedingLimitsForm.actions.saving") : tr("seedingLimitsForm.actions.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
