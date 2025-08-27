/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Download, Upload } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { toast } from "sonner"

// Convert bytes/s to MiB/s for display
function bytesToMiB(bytes: number): number {
  return bytes === 0 ? 0 : bytes / (1024 * 1024)
}

// Convert MiB/s to bytes/s for API
function mibToBytes(mib: number): number {
  return mib === 0 ? 0 : Math.round(mib * 1024 * 1024)
}

function SpeedLimitInput({
  label,
  value,
  onChange,
  icon: Icon,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  icon: React.ComponentType<{ className?: string }>
}) {
  const displayValue = bytesToMiB(value)
  
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 text-muted-foreground" />
        <Label className="text-sm font-medium">{label}</Label>
      </div>
      <div className="flex items-center gap-2">
        <Input
          type="number"
          min="0"
          step="0.1"
          value={displayValue === 0 ? "" : displayValue.toFixed(1)}
          onChange={(e) => {
            const mibValue = e.target.value === "" ? 0 : parseFloat(e.target.value)
            if (!isNaN(mibValue) && mibValue >= 0) {
              onChange(mibToBytes(mibValue))
            }
          }}
          placeholder="0 (Unlimited)"
          className="flex-1"
        />
        <span className="text-sm text-muted-foreground min-w-12">MiB/s</span>
      </div>
    </div>
  )
}

interface SpeedLimitsFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function SpeedLimitsForm({ instanceId, onSuccess }: SpeedLimitsFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  
  
  // Track if form is being actively edited
  const [isFormDirty, setIsFormDirty] = React.useState(false)
  
  // Memoize preferences to prevent unnecessary form resets
  const memoizedPreferences = React.useMemo(() => preferences, [
    preferences,
  ])

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
        setIsFormDirty(false) // Reset dirty flag after successful save
        toast.success("Speed limits updated successfully")
        onSuccess?.()
      } catch {
        toast.error("Failed to update speed limits")
      }
    },
  })


  // Update form when preferences change (but only if form is not being actively edited)
  React.useEffect(() => {
    if (memoizedPreferences && !isFormDirty) {
      form.setFieldValue("dl_limit", memoizedPreferences.dl_limit ?? 0)
      form.setFieldValue("up_limit", memoizedPreferences.up_limit ?? 0)
      form.setFieldValue("alt_dl_limit", memoizedPreferences.alt_dl_limit ?? 0)
      form.setFieldValue("alt_up_limit", memoizedPreferences.alt_up_limit ?? 0)
    }
  }, [memoizedPreferences, form, isFormDirty])

  if (isLoading) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">Loading speed limits...</p>
      </div>
    )
  }

  if (!memoizedPreferences) {
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
            <SpeedLimitInput
              label="Download Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
              icon={Download}
            />
          )}
        </form.Field>
        
        <form.Field name="up_limit">
          {(field) => (
            <SpeedLimitInput
              label="Upload Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
              icon={Upload}
            />
          )}
        </form.Field>
        
        <form.Field name="alt_dl_limit">
          {(field) => (
            <SpeedLimitInput
              label="Alternative Download Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
              icon={Download}
            />
          )}
        </form.Field>
        
        <form.Field name="alt_up_limit">
          {(field) => (
            <SpeedLimitInput
              label="Alternative Upload Limit"
              value={(field.state.value as number) ?? 0}
              onChange={(value) => {
                setIsFormDirty(true)
                field.handleChange(value)
              }}
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