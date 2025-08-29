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
        toast.success("File management settings updated successfully")
        onSuccess?.()
      } catch {
        toast.error("Failed to update file management settings")
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
        <p className="text-sm text-muted-foreground">Loading file management settings...</p>
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
        <form.Field name="auto_tmm_enabled">
          {(field) => (
            <SwitchSetting
              label="Automatic Torrent Management"
              checked={field.state.value as boolean}
              onCheckedChange={field.handleChange}
              description="Use category-based paths for downloads"
            />
          )}
        </form.Field>

        <form.Field name="start_paused_enabled">
          {(field) => (
            <SwitchSetting
              label="Start Torrents Paused"
              checked={field.state.value as boolean}
              onCheckedChange={field.handleChange}
              description="New torrents start in paused state"
            />
          )}
        </form.Field>

        <form.Field name="save_path">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Default Save Path</Label>
              <p className="text-xs text-muted-foreground">
                Default directory for downloading files
              </p>
              <Input
                value={field.state.value as string}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="/downloads"
              />
            </div>
          )}
        </form.Field>

        <form.Field name="torrent_content_layout">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Default Content Layout</Label>
              <p className="text-xs text-muted-foreground">
                How torrent files are organized within the save directory
              </p>
              <Select
                value={field.state.value as string}
                onValueChange={field.handleChange}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select content layout" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="Original">Original</SelectItem>
                  <SelectItem value="Subfolder">Create subfolder</SelectItem>
                  <SelectItem value="NoSubfolder">Don't create subfolder</SelectItem>
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
              {isSubmitting || isUpdating ? "Saving..." : "Save Changes"}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}