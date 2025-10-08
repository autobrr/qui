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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { usePersistedStartPaused } from "@/hooks/usePersistedStartPaused"
import { toast } from "sonner"
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

interface FileManagementFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function FileManagementForm({ instanceId, onSuccess }: FileManagementFormProps) {
  const { t } = useTranslation()
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  const [startPausedEnabled, setStartPausedEnabled] = usePersistedStartPaused(instanceId, false)

  const form = useForm({
    defaultValues: {
      auto_tmm_enabled: false,
      start_paused_enabled: false,
      save_path: "",
      torrent_content_layout: "Original",
    },
    onSubmit: async ({ value }) => {
      try {
        // NOTE: Save start_paused_enabled to localStorage instead of qBittorrent
        // This is a workaround because qBittorrent's API rejects this preference
        setStartPausedEnabled(value.start_paused_enabled)

        // Update other preferences to qBittorrent (excluding start_paused_enabled)
        const qbittorrentPrefs = {
          auto_tmm_enabled: value.auto_tmm_enabled,
          save_path: value.save_path,
          torrent_content_layout: value.torrent_content_layout ?? "Original",
        }
        updatePreferences(qbittorrentPrefs)
        toast.success(t("instancePreferences.content.fileManagement.notifications.saveSuccess"))
        onSuccess?.()
      } catch {
        toast.error(t("instancePreferences.content.fileManagement.notifications.saveError"))
      }
    },
  })

  // Update form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("auto_tmm_enabled", preferences.auto_tmm_enabled)
      form.setFieldValue("save_path", preferences.save_path)
      form.setFieldValue("torrent_content_layout", preferences.torrent_content_layout ?? "Original")
    }
  }, [preferences, form])

  // Update form when localStorage start_paused_enabled changes
  React.useEffect(() => {
    form.setFieldValue("start_paused_enabled", startPausedEnabled)
  }, [startPausedEnabled, form])

  if (isLoading) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">{t("instancePreferences.content.fileManagement.loading")}</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">{t("instancePreferences.content.fileManagement.failed")}</p>
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
        <form.Field name="auto_tmm_enabled">
          {(field) => (
            <SwitchSetting
              label={t("instancePreferences.content.fileManagement.autoTMM")}
              checked={field.state.value as boolean}
              onCheckedChange={field.handleChange}
              description={t("instancePreferences.content.fileManagement.autoTMMDescription")}
            />
          )}
        </form.Field>

        <form.Field name="start_paused_enabled">
          {(field) => (
            <SwitchSetting
              label={t("instancePreferences.content.fileManagement.startPaused")}
              checked={field.state.value as boolean}
              onCheckedChange={field.handleChange}
              description={t("instancePreferences.content.fileManagement.startPausedDescription")}
            />
          )}
        </form.Field>

        <form.Field name="save_path">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("instancePreferences.content.fileManagement.defaultSavePath")}</Label>
              <p className="text-xs text-muted-foreground">
                {t("instancePreferences.content.fileManagement.defaultSavePathDescription")}
              </p>
              <Input
                value={field.state.value as string}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder={t("instancePreferences.content.fileManagement.placeholderPath")}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="torrent_content_layout">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("instancePreferences.content.fileManagement.defaultContentLayout")}</Label>
              <p className="text-xs text-muted-foreground">
                {t("instancePreferences.content.fileManagement.defaultContentLayoutDescription")}
              </p>
              <Select
                value={field.state.value as string}
                onValueChange={field.handleChange}
              >
                <SelectTrigger>
                  <SelectValue placeholder={t("instancePreferences.content.fileManagement.selectContentLayout")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="Original">{t("instancePreferences.content.fileManagement.layoutOptions.original")}</SelectItem>
                  <SelectItem value="Subfolder">{t("instancePreferences.content.fileManagement.layoutOptions.subfolder")}</SelectItem>
                  <SelectItem value="NoSubfolder">{t("instancePreferences.content.fileManagement.layoutOptions.noSubfolder")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
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
              {isSubmitting || isUpdating ? t("instancePreferences.content.fileManagement.savingButton") : t("instancePreferences.content.fileManagement.saveButton")}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
