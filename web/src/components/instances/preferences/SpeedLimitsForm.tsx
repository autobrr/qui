/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Slider } from "@/components/ui/slider"
import { Download, Upload } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { toast } from "sonner"
import { formatSpeed } from "@/lib/utils"

function formatSpeedLimit(kbps: number) {
  return kbps === 0 ? "Unlimited" : formatSpeed(kbps * 1024) // Convert KB/s to B/s for formatSpeed
}

function SpeedLimitSlider({
  label,
  value,
  onChange,
  icon: Icon,
  max = 100000,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  icon: React.ComponentType<{ className?: string }>
  max?: number
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Icon className="h-4 w-4 text-muted-foreground" />
          <Label className="text-sm font-medium">{label}</Label>
        </div>
        <span className="text-sm text-muted-foreground">
          {formatSpeedLimit(value)}
        </span>
      </div>
      <Slider
        value={[value]}
        onValueChange={(values) => onChange(values[0])}
        max={max}
        step={value < 1000 ? 50 : 1000}
        className="w-full"
      />
    </div>
  )
}

interface SpeedLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SpeedLimitsForm({ instanceId, onSuccess }: SpeedLimitsFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const form = useForm({
    defaultValues: {
      dl_limit: 0,
      up_limit: 0,
      alt_dl_limit: 0,
      alt_up_limit: 0,
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        toast.success("Speed limits updated successfully")
        onSuccess?.()
      } catch (error) {
        toast.error("Failed to update speed limits")
      }
    },
  })

  // Reset form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.reset({
        dl_limit: preferences.dl_limit || 0,
        up_limit: preferences.up_limit || 0,
        alt_dl_limit: preferences.alt_dl_limit || 0,
        alt_up_limit: preferences.alt_up_limit || 0,
      })
    }
  }, [preferences, form])

  if (isLoading) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">Loading speed limits...</p>
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
        <form.Field name="dl_limit">
          {(field) => (
            <SpeedLimitSlider
              label="Download Limit"
              value={(field.state.value as number) ?? 0}
              onChange={field.handleChange}
              icon={Download}
            />
          )}
        </form.Field>
        
        <form.Field name="up_limit">
          {(field) => (
            <SpeedLimitSlider
              label="Upload Limit"
              value={(field.state.value as number) ?? 0}
              onChange={field.handleChange}
              icon={Upload}
            />
          )}
        </form.Field>
        
        <form.Field name="alt_dl_limit">
          {(field) => (
            <SpeedLimitSlider
              label="Alternative Download Limit"
              value={(field.state.value as number) ?? 0}
              onChange={field.handleChange}
              icon={Download}
            />
          )}
        </form.Field>
        
        <form.Field name="alt_up_limit">
          {(field) => (
            <SpeedLimitSlider
              label="Alternative Upload Limit"
              value={(field.state.value as number) ?? 0}
              onChange={field.handleChange}
              icon={Upload}
            />
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