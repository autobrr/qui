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
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
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

interface WebhooksFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function WebhooksForm({ instanceId, onSuccess }: WebhooksFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const form = useForm({
    defaultValues: {
      autorun_on_torrent_added_enabled: false,
      autorun_enabled: false,
      qui_url: "",
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        toast.success("Webhooks updated successfully")
        onSuccess?.()
      } catch {
        toast.error("Failed to update webhooks")
      }
    },
  })

  // Update form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("autorun_on_torrent_added_enabled", preferences.autorun_on_torrent_added_enabled)
      form.setFieldValue("autorun_enabled", preferences.autorun_enabled)
      form.setFieldValue("qui_url", preferences.qui_url)
    }
  }, [preferences, form])

  if (isLoading || !preferences) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">Loading webhooks...</p>
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
        <form.Field name="autorun_on_torrent_added_enabled">
          {(field) => (
            <SwitchSetting
              label="Enable webhooks for new torrents"
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description="Enables sending webhooks to qui when a new torrent is added"
            />
          )}
        </form.Field>

        <form.Field name="autorun_enabled">
          {(field) => (
            <SwitchSetting
              label="Enable webhooks for completed torrents"
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description="Enables sending webhooks to qui when a torrent is completed"
            />
          )}
        </form.Field>

        <form.Field name="qui_url">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">qui URL</Label>
              <p className="text-xs text-muted-foreground">
                URL of your qui server - It must be reachable by the qBittorrent instance
              </p>
              <Input
                value={field.state.value as string}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="http://localhost:7476"
              />
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