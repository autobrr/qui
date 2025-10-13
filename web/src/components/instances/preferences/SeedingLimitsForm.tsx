/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
  const { t } = useTranslation()
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
        updatePreferences(value)
        toast.success(t("instancePreferences.content.seedingLimits.notifications.saveSuccess"))
        onSuccess?.()
      } catch {
        toast.error(t("instancePreferences.content.seedingLimits.notifications.saveError"))
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

  if (isLoading || !preferences) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">{t("instancePreferences.content.seedingLimits.loading")}</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">{t("instancePreferences.content.seedingLimits.failed")}</p>
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
              label={t("instancePreferences.content.seedingLimits.enableRatioLimit")}
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description={t("instancePreferences.content.seedingLimits.enableRatioLimitDescription")}
            />
          )}
        </form.Field>

        <form.Field name="max_ratio_enabled">
          {(enabledField) => (
            <form.Field name="max_ratio">
              {(field) => (
                  <NumberInputWithUnlimited
                    label={t("instancePreferences.content.seedingLimits.maxRatio")}
                    value={(field.state.value as number) ?? 2.0}
                    onChange={field.handleChange}
                    min={-1}
                    max={10}
                    step="0.05"
                    description={t("instancePreferences.content.seedingLimits.maxRatioDescription")}
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
              label={t("instancePreferences.content.seedingLimits.enableTimeLimit")}
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description={t("instancePreferences.content.seedingLimits.enableTimeLimitDescription")}
            />
          )}
        </form.Field>

        <form.Field name="max_seeding_time_enabled">
          {(enabledField) => (
            <form.Field name="max_seeding_time">
              {(field) => (
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.seedingLimits.maxTime")}
                  value={(field.state.value as number) ?? 1440}
                  onChange={field.handleChange}
                  min={-1}
                  max={525600} // 1 year in minutes
                  description={t("instancePreferences.content.seedingLimits.maxTimeDescription")}
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
              {isSubmitting || isUpdating ? t("instancePreferences.content.seedingLimits.savingButton") : t("instancePreferences.content.seedingLimits.saveButton")}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
