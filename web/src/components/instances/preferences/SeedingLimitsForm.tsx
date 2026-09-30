/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { usePreferencesForm } from "@/hooks/usePreferencesForm"
import type { AppPreferences } from "@/types"
import { useTranslation } from "react-i18next"
import { NumberInputWithUnlimited } from "@/components/forms/NumberInputWithUnlimited"

import { PreferencesFormShell } from "./PreferencesFormShell"
import { PreferencesSection } from "./PreferencesSection"


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
      <Label className="text-sm font-medium">{label}</Label>
      {description && <FieldHelp>{description}</FieldHelp>}
    </div>
  )
}

interface SeedingLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SeedingLimitsForm({ instanceId, onSuccess }: SeedingLimitsFormProps) {
  return (
    <PreferencesSection instanceId={instanceId} i18nPrefix="preferences.seedingLimits">
      {(preferences) => <SeedingLimitsFields instanceId={instanceId} preferences={preferences} onSuccess={onSuccess} />}
    </PreferencesSection>
  )
}

function SeedingLimitsFields({ instanceId, preferences, onSuccess }: SeedingLimitsFormProps & { preferences: AppPreferences }) {
  const { t } = useTranslation("instances")
  const { form, isUpdating } = usePreferencesForm({
    instanceId,
    preferences,
    toForm: (p) => ({
      max_ratio_enabled: p.max_ratio_enabled,
      max_ratio: p.max_ratio,
      max_seeding_time_enabled: p.max_seeding_time_enabled,
      max_seeding_time: p.max_seeding_time,
    }),
    i18nPrefix: "preferences.seedingLimits",
    onSaved: onSuccess,
  })

  return (
    <PreferencesFormShell
      // step sets the arrow increment; qBittorrent can store a ratio off that grid, which must still save
      noValidate
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
        <div className="space-y-6">
          <form.Field name="max_ratio_enabled">
            {(field) => (
              <SwitchSetting
                label={t("preferences.seedingLimits.enableShareRatioLimit")}
                checked={(field.state.value as boolean) ?? false}
                onCheckedChange={field.handleChange}
                description={t("preferences.seedingLimits.enableShareRatioLimitDescription")}
              />
            )}
          </form.Field>

          <form.Field name="max_ratio_enabled">
            {(enabledField) => (
              <form.Field name="max_ratio">
                {(field) => (
                  <NumberInputWithUnlimited
                    label={t("preferences.seedingLimits.maxShareRatio")}
                    value={(field.state.value as number) ?? 2.0}
                    onChange={field.handleChange}
                    min={-1}
                    max={Number.MAX_SAFE_INTEGER} // qBittorrent has no upper limit; the input defaults to 999999
                    step="0.05"
                    description={t("preferences.seedingLimits.maxShareRatioDescription")}
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
                label={t("preferences.seedingLimits.enableSeedingTimeLimit")}
                checked={(field.state.value as boolean) ?? false}
                onCheckedChange={field.handleChange}
                description={t("preferences.seedingLimits.enableSeedingTimeLimitDescription")}
              />
            )}
          </form.Field>

          <form.Field name="max_seeding_time_enabled">
            {(enabledField) => (
              <form.Field name="max_seeding_time">
                {(field) => (
                  <NumberInputWithUnlimited
                    label={t("preferences.seedingLimits.maxSeedingTime")}
                    value={(field.state.value as number) ?? 1440}
                    onChange={field.handleChange}
                    min={-1}
                    max={525600} // 1 year in minutes
                    description={t("preferences.seedingLimits.maxSeedingTimeDescription")}
                    allowUnlimited={true}
                    disabled={!(enabledField.state.value as boolean)}
                  />
                )}
              </form.Field>
            )}
          </form.Field>
        </div>
      </div>
    </PreferencesFormShell>
  )
}
