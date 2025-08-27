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

function NumberInput({
  label,
  value,
  onChange,
  min = 0,
  max = 999999,
  description,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  min?: number
  max?: number
  description?: string
}) {
  return (
    <div className="space-y-2">
      <div className="space-y-1">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Input
        type="number"
        value={value}
        onChange={(e) => {
          const num = parseInt(e.target.value) || 0
          onChange(Math.max(min, Math.min(max, num)))
        }}
        min={min}
        max={max}
        className="w-full"
      />
    </div>
  )
}

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
    <div className="flex items-center justify-between space-x-2">
      <div className="space-y-1">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}

interface QueueManagementFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function QueueManagementForm({ instanceId, onSuccess }: QueueManagementFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const form = useForm({
    defaultValues: {
      queueing_enabled: false,
      max_active_downloads: 3,
      max_active_uploads: 3,
      max_active_torrents: 5,
      max_active_checking_torrents: 1,
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        toast.success("Queue settings updated successfully")
        onSuccess?.()
      } catch (error) {
        toast.error("Failed to update queue settings")
      }
    },
  })

  // Update form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("queueing_enabled", preferences.queueing_enabled ?? false)
      form.setFieldValue("max_active_downloads", preferences.max_active_downloads ?? 3)
      form.setFieldValue("max_active_uploads", preferences.max_active_uploads ?? 3)
      form.setFieldValue("max_active_torrents", preferences.max_active_torrents ?? 5)
      form.setFieldValue("max_active_checking_torrents", preferences.max_active_checking_torrents ?? 1)
    }
  }, [preferences, form])

  if (isLoading) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">Loading queue settings...</p>
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
        <form.Field name="queueing_enabled">
          {(field) => (
            <SwitchSetting
              label="Enable Queueing"
              checked={(field.state.value as boolean) ?? false}
              onCheckedChange={field.handleChange}
              description="Limit the number of active torrents"
            />
          )}
        </form.Field>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field name="max_active_downloads">
            {(field) => (
              <NumberInput
                label="Max Active Downloads"
                value={(field.state.value as number) ?? 3}
                onChange={field.handleChange}
                max={999}
                description="Maximum number of downloading torrents"
              />
            )}
          </form.Field>

          <form.Field name="max_active_uploads">
            {(field) => (
              <NumberInput
                label="Max Active Uploads"
                value={(field.state.value as number) ?? 3}
                onChange={field.handleChange}
                max={999}
                description="Maximum number of uploading torrents"
              />
            )}
          </form.Field>

          <form.Field name="max_active_torrents">
            {(field) => (
              <NumberInput
                label="Max Active Torrents"
                value={(field.state.value as number) ?? 5}
                onChange={field.handleChange}
                max={999}
                description="Total maximum active torrents"
              />
            )}
          </form.Field>

          <form.Field name="max_active_checking_torrents">
            {(field) => (
              <NumberInput
                label="Max Checking Torrents"
                value={(field.state.value as number) ?? 1}
                onChange={field.handleChange}
                max={999}
                description="Maximum torrents checking simultaneously"
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
              {isSubmitting || isUpdating ? "Saving..." : "Save Changes"}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}