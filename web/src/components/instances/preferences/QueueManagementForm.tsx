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

interface QueueManagementFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function QueueManagementForm({ instanceId, onSuccess }: QueueManagementFormProps) {
  const { t } = useTranslation()
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
        toast.success(t("instancePreferences.content.queueManagement.notifications.saveSuccess"))
        onSuccess?.()
      } catch {
        toast.error(t("instancePreferences.content.queueManagement.notifications.saveError"))
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

  if (isLoading || !preferences) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">{t("instancePreferences.content.queueManagement.loading")}</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">{t("instancePreferences.content.queueManagement.failed")}</p>
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
        <form.Field name="queueing_enabled">
          {(field) => (
            <SwitchSetting
              label={t("instancePreferences.content.queueManagement.enableQueueing")}
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description={t("instancePreferences.content.queueManagement.enableQueueingDescription")}
            />
          )}
        </form.Field>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field
            name="max_active_downloads"
            validators={{
              onChange: ({ value }) => {
                if (value < -1) {
                  return t("instancePreferences.content.queueManagement.validation.maxActiveDownloads")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.queueManagement.maxActiveDownloads")}
                  value={(field.state.value as number) ?? 3}
                  onChange={field.handleChange}
                  max={999}
                  description={t("instancePreferences.content.queueManagement.maxActiveDownloadsDescription")}
                  allowUnlimited={true}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="max_active_uploads"
            validators={{
              onChange: ({ value }) => {
                if (value < -1) {
                  return t("instancePreferences.content.queueManagement.validation.maxActiveUploads")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.queueManagement.maxActiveUploads")}
                  value={(field.state.value as number) ?? 3}
                  onChange={field.handleChange}
                  max={999}
                  description={t("instancePreferences.content.queueManagement.maxActiveUploadsDescription")}
                  allowUnlimited={true}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="max_active_torrents"
            validators={{
              onChange: ({ value }) => {
                if (value < -1) {
                  return t("instancePreferences.content.queueManagement.validation.maxActiveTorrents")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.queueManagement.maxActiveTorrents")}
                  value={(field.state.value as number) ?? 5}
                  onChange={field.handleChange}
                  max={999}
                  description={t("instancePreferences.content.queueManagement.maxActiveTorrentsDescription")}
                  allowUnlimited={true}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field name="max_active_checking_torrents">
            {(field) => (
              <NumberInputWithUnlimited
                label={t("instancePreferences.content.queueManagement.maxCheckingTorrents")}
                value={(field.state.value as number) ?? 1}
                onChange={field.handleChange}
                max={999}
                description={t("instancePreferences.content.queueManagement.maxCheckingTorrentsDescription")}
                allowUnlimited={true}
              />
            )}
          </form.Field>
        </div>
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
              {isSubmitting || isUpdating ? t("instancePreferences.content.queueManagement.savingButton") : t("instancePreferences.content.queueManagement.saveButton")}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
