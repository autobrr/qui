/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Radar, Users, Shield } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { toast } from "sonner"

interface NetworkDiscoveryFormProps {
  instanceId: number
  onSuccess?: () => void
}

function SwitchSetting({
  label,
  description,
  checked,
  onChange,
}: {
  label: string
  description?: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <div className="flex items-center gap-3">
      <Switch checked={checked} onCheckedChange={onChange} />
      <div className="space-y-0.5">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
    </div>
  )
}

export function NetworkDiscoveryForm({ instanceId, onSuccess }: NetworkDiscoveryFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

  const form = useForm({
    defaultValues: {
      dht: false,
      pex: false,
      lsd: false,
      encryption: 0,
      anonymous_mode: false,
      announce_to_all_tiers: false,
      announce_to_all_trackers: false,
      resolve_peer_countries: false,
    },
    onSubmit: async ({ value }) => {
      try {
        await updatePreferences(value)
        toast.success("Network discovery settings updated successfully")
        onSuccess?.()
      } catch (error) {
        toast.error("Failed to update network discovery settings")
        console.error("Failed to update network discovery settings:", error)
      }
    },
  })

  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("dht", preferences.dht)
      form.setFieldValue("pex", preferences.pex)
      form.setFieldValue("lsd", preferences.lsd)
      form.setFieldValue("encryption", preferences.encryption)
      form.setFieldValue("anonymous_mode", preferences.anonymous_mode)
      form.setFieldValue("announce_to_all_tiers", preferences.announce_to_all_tiers)
      form.setFieldValue("announce_to_all_trackers", preferences.announce_to_all_trackers)
      form.setFieldValue("resolve_peer_countries", preferences.resolve_peer_countries)
    }
  }, [preferences, form])

  if (isLoading || !preferences) {
    return <div className="flex items-center justify-center py-8">Loading network discovery settings...</div>
  }

  const getEncryptionLabel = (value: number) => {
    switch (value) {
      case 0: return "Prefer encryption"
      case 1: return "Require encryption"
      case 2: return "Disable encryption"
      default: return "Prefer encryption"
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-6"
    >
      {/* Peer Discovery Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Radar className="h-4 w-4" />
          <h3 className="text-lg font-medium">Peer Discovery</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="dht">
            {(field) => (
              <SwitchSetting
                label="Enable DHT (decentralized network)"
                description="Distributed Hash Table for finding peers without trackers"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="pex">
            {(field) => (
              <SwitchSetting
                label="Enable PeX (Peer Exchange)"
                description="Exchange peer lists with other peers"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="lsd">
            {(field) => (
              <SwitchSetting
                label="Enable LSD (Local Service Discovery)"
                description="Find peers on your local network"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>
        </div>
      </div>

      {/* Tracker Settings Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4" />
          <h3 className="text-lg font-medium">Tracker Settings</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="announce_to_all_tiers">
            {(field) => (
              <SwitchSetting
                label="Always announce to all tiers"
                description="Announce to all tracker tiers simultaneously"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="announce_to_all_trackers">
            {(field) => (
              <SwitchSetting
                label="Always announce to all trackers in a tier"
                description="Announce to all trackers in a tier simultaneously"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>
        </div>
      </div>

      {/* Security & Privacy Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Shield className="h-4 w-4" />
          <h3 className="text-lg font-medium">Security & Privacy</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="encryption">
            {(field) => (
              <div className="space-y-2">
                <Label className="text-sm font-medium">Protocol Encryption</Label>
                <Select
                  value={field.state.value.toString()}
                  onValueChange={(value) => field.handleChange(parseInt(value))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">{getEncryptionLabel(0)}</SelectItem>
                    <SelectItem value="1">{getEncryptionLabel(1)}</SelectItem>
                    <SelectItem value="2">{getEncryptionLabel(2)}</SelectItem>
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">
                  Protocol encryption helps bypass ISP traffic shaping
                </p>
              </div>
            )}
          </form.Field>

          <form.Field name="anonymous_mode">
            {(field) => (
              <SwitchSetting
                label="Enable anonymous mode"
                description="Hide your client identification and version"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="resolve_peer_countries">
            {(field) => (
              <SwitchSetting
                label="Resolve peer countries"
                description="Show country flags for peers (requires additional bandwidth)"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>
        </div>
      </div>

      <form.Subscribe
        selector={(state) => [state.canSubmit, state.isSubmitting]}
      >
        {([canSubmit, isSubmitting]) => (
          <Button
            type="submit"
            disabled={!canSubmit || isSubmitting || isUpdating}
            className="w-full"
          >
            {isSubmitting || isUpdating ? "Updating..." : "Update Network Discovery Settings"}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}