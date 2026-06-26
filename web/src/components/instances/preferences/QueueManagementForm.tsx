/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { NumberInputWithUnlimited } from "@/components/forms/NumberInputWithUnlimited"

import { PreferencesFormShell } from "./PreferencesFormShell"


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
  const switchId = React.useId()
  const descriptionId = description ? `${switchId}-desc` : undefined

  return (
    <label
      htmlFor={switchId}
      className="flex items-center gap-3 cursor-pointer"
    >
      <Switch
        id={switchId}
        checked={checked}
        onCheckedChange={onCheckedChange}
        aria-describedby={descriptionId}
      />
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{label}</span>
        {description && (
          <p id={descriptionId} className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
    </label>
  )
}

interface QueueManagementFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function QueueManagementForm({ instanceId, onSuccess }: QueueManagementFormProps) {
  const { t } = useTranslation("instances")
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const form = useForm({
    defaultValues: {
      queueing_enabled: false,
      max_active_downloads: 0,
      max_active_uploads: 0,
      max_active_torrents: 0,
      max_active_checking_torrents: 0,
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        toast.success(t("preferences.queueManagement.toast.success"))
        onSuccess?.()
      } catch {
        toast.error(t("preferences.queueManagement.toast.error"))
      }
    },
  })

  // Update form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("queueing_enabled", preferences.queueing_enabled)
      form.setFieldValue("max_active_downloads", preferences.max_active_downloads)
      form.setFieldValue("max_active_uploads", preferences.max_active_uploads)
      form.setFieldValue("max_active_torrents", preferences.max_active_torrents)
      form.setFieldValue("max_active_checking_torrents", preferences.max_active_checking_torrents)
    }
  }, [preferences, form])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">{t("preferences.queueManagement.loading")}</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="flex items-center justify-center py-8" role="alert">
        <p className="text-sm text-muted-foreground">{t("preferences.queueManagement.loadFailed")}</p>
      </div>
    )
  }

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
        <div className="space-y-6">
          <form.Field name="queueing_enabled">
            {(field) => (
              <SwitchSetting
                label={t("preferences.queueManagement.enableQueueing")}
                checked={(field.state.value as boolean) ?? false}
                onCheckedChange={field.handleChange}
                description={t("preferences.queueManagement.enableQueueingDescription")}
              />
            )}
          </form.Field>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <form.Field
              name="max_active_downloads"
              validators={{
                onChange: ({ value }) => {
                  if (value < -1) {
                    return t("preferences.queueManagement.validation.maxActiveDownloads")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInputWithUnlimited
                    label={t("preferences.queueManagement.maxActiveDownloads")}
                    value={(field.state.value as number) ?? 3}
                    onChange={field.handleChange}
                    max={99999}
                    description={t("preferences.queueManagement.maxActiveDownloadsDescription")}
                    allowUnlimited={true}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="max_active_uploads"
              validators={{
                onChange: ({ value }) => {
                  if (value < -1) {
                    return t("preferences.queueManagement.validation.maxActiveUploads")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInputWithUnlimited
                    label={t("preferences.queueManagement.maxActiveUploads")}
                    value={(field.state.value as number) ?? 3}
                    onChange={field.handleChange}
                    max={99999}
                    description={t("preferences.queueManagement.maxActiveUploadsDescription")}
                    allowUnlimited={true}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="max_active_torrents"
              validators={{
                onChange: ({ value }) => {
                  if (value < -1) {
                    return t("preferences.queueManagement.validation.maxActiveTorrents")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInputWithUnlimited
                    label={t("preferences.queueManagement.maxActiveTorrents")}
                    value={(field.state.value as number) ?? 5}
                    onChange={field.handleChange}
                    max={99999}
                    description={t("preferences.queueManagement.maxActiveTorrentsDescription")}
                    allowUnlimited={true}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field name="max_active_checking_torrents">
              {(field) => (
                <NumberInputWithUnlimited
                  label={t("preferences.queueManagement.maxCheckingTorrents")}
                  value={(field.state.value as number) ?? 1}
                  onChange={field.handleChange}
                  max={99999}
                  description={t("preferences.queueManagement.maxCheckingTorrentsDescription")}
                  allowUnlimited={true}
                />
              )}
            </form.Field>
          </div>
        </div>
      </div>
    </PreferencesFormShell>
  )
}
