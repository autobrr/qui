/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React, { useState } from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Slider } from "@/components/ui/slider"
import { Separator } from "@/components/ui/separator"
import { AlertCircle, Download, Upload, Clock, Folder } from "lucide-react"
import { useInstances } from "@/hooks/useInstances"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { toast } from "sonner"
import { formatSpeed } from "@/lib/utils"
import type { InstanceResponse } from "@/types"

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

export function InstancePreferencesForm() {
  const [selectedInstanceId, setSelectedInstanceId] = useState<number | undefined>(undefined)
  const { instances } = useInstances()
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(selectedInstanceId)

  const connectedInstances = instances?.filter((instance: InstanceResponse) => instance.connected) || []

  const form = useForm({
    defaultValues: {
      dl_limit: 0,
      up_limit: 0,
      alt_dl_limit: 0,
      alt_up_limit: 0,
      queueing_enabled: false,
      max_active_downloads: 3,
      max_active_uploads: 3,
      max_active_torrents: 5,
      max_active_checking_torrents: 1,
      auto_tmm_enabled: false,
      start_paused_enabled: false,
      save_path: "",
      max_ratio_enabled: false,
      max_ratio: 2.0,
      max_seeding_time_enabled: false,
      max_seeding_time: 1440,
    },
    onSubmit: async ({ value }) => {
      if (!selectedInstanceId) return
      try {
        updatePreferences(value)
        toast.success("Preferences updated successfully")
      } catch (error) {
        toast.error("Failed to update preferences")
      }
    },
  })

  // Reset form when preferences change (instance selection or data load)
  React.useEffect(() => {
    if (preferences) {
      form.reset(preferences)
    }
  }, [preferences, form])

  if (connectedInstances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Instance Preferences</CardTitle>
          <CardDescription>
            Configure qBittorrent instance preferences
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-3 p-4 rounded-lg bg-muted">
            <AlertCircle className="h-5 w-5 text-muted-foreground" />
            <div>
              <p className="text-sm font-medium">No connected instances</p>
              <p className="text-xs text-muted-foreground">
                You need at least one connected qBittorrent instance to manage preferences.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Instance Preferences</CardTitle>
        <CardDescription>
          Configure qBittorrent instance preferences for speed limits, queue management, and more
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Instance Selector */}
        <div className="space-y-2">
          <Label className="text-sm font-medium">qBittorrent Instance</Label>
          <Select
            value={selectedInstanceId?.toString()}
            onValueChange={(value) => setSelectedInstanceId(parseInt(value))}
          >
            <SelectTrigger>
              <SelectValue placeholder="Select an instance to configure" />
            </SelectTrigger>
            <SelectContent>
              {connectedInstances.map((instance: InstanceResponse) => (
                <SelectItem key={instance.id} value={instance.id.toString()}>
                  {instance.name} ({instance.host})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {selectedInstanceId && (
          <>
            {isLoading ? (
              <div className="text-center py-8">
                <p className="text-sm text-muted-foreground">Loading preferences...</p>
              </div>
            ) : preferences && (
              <form
                onSubmit={(e) => {
                  e.preventDefault()
                  form.handleSubmit()
                }}
                className="space-y-8"
              >
                {/* Speed Limits Section */}
                <div className="space-y-4">
                  <div className="flex items-center gap-2">
                    <Download className="h-5 w-5" />
                    <h3 className="text-lg font-semibold">Speed Limits</h3>
                  </div>
                  <div className="grid gap-6">
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
                </div>

                <Separator />

                {/* Queue Management Section */}
                <div className="space-y-4">
                  <div className="flex items-center gap-2">
                    <Clock className="h-5 w-5" />
                    <h3 className="text-lg font-semibold">Queue Management</h3>
                  </div>
                  
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

                <Separator />

                {/* File Management Section */}
                <div className="space-y-4">
                  <div className="flex items-center gap-2">
                    <Folder className="h-5 w-5" />
                    <h3 className="text-lg font-semibold">File Management</h3>
                  </div>
                  
                  <form.Field name="auto_tmm_enabled">
                    {(field) => (
                      <SwitchSetting
                        label="Automatic Torrent Management"
                        checked={(field.state.value as boolean) ?? false}
                        onCheckedChange={field.handleChange}
                        description="Use category-based paths for downloads"
                      />
                    )}
                  </form.Field>

                  <form.Field name="start_paused_enabled">
                    {(field) => (
                      <SwitchSetting
                        label="Start Torrents Paused"
                        checked={(field.state.value as boolean) ?? false}
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
                          value={(field.state.value as string) ?? ""}
                          onChange={(e) => field.handleChange(e.target.value)}
                          placeholder="/downloads"
                        />
                      </div>
                    )}
                  </form.Field>
                </div>

                <Separator />

                {/* Seeding Limits Section */}
                <div className="space-y-4">
                  <h3 className="text-lg font-semibold">Seeding Limits</h3>
                  
                  <form.Field name="max_ratio_enabled">
                    {(field) => (
                      <SwitchSetting
                        label="Enable Share Ratio Limit"
                        checked={(field.state.value as boolean) ?? false}
                        onCheckedChange={field.handleChange}
                        description="Stop seeding when ratio is reached"
                      />
                    )}
                  </form.Field>

                  <form.Field name="max_ratio">
                    {(field) => (
                      <div className="space-y-2">
                        <Label className="text-sm font-medium">Maximum Share Ratio</Label>
                        <p className="text-xs text-muted-foreground">
                          Stop seeding at this upload/download ratio
                        </p>
                        <Input
                          type="number"
                          step="0.1"
                          min="0"
                          max="10"
                          value={(field.state.value as number) ?? 2.0}
                          onChange={(e) => field.handleChange(parseFloat(e.target.value) || 2.0)}
                        />
                      </div>
                    )}
                  </form.Field>

                  <form.Field name="max_seeding_time_enabled">
                    {(field) => (
                      <SwitchSetting
                        label="Enable Seeding Time Limit"
                        checked={(field.state.value as boolean) ?? false}
                        onCheckedChange={field.handleChange}
                        description="Stop seeding after specified time"
                      />
                    )}
                  </form.Field>

                  <form.Field name="max_seeding_time">
                    {(field) => (
                      <NumberInput
                        label="Maximum Seeding Time (minutes)"
                        value={(field.state.value as number) ?? 1440}
                        onChange={field.handleChange}
                        min={1}
                        max={525600} // 1 year in minutes
                        description="Stop seeding after this many minutes"
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
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}