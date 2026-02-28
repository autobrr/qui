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
import { useTranslation } from "react-i18next"
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
        onCheckedChange={onChange}
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

export function NetworkDiscoveryForm({ instanceId, onSuccess }: NetworkDiscoveryFormProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
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
    onSubmit: ({ value }) => {
      updatePreferences(value, {
        onSuccess: () => {
          toast.success(tr("networkDiscoveryForm.toasts.updated"))
          onSuccess?.()
        },
        onError: (error) => {
          toast.error(tr("networkDiscoveryForm.toasts.failedUpdate"))
          console.error("Failed to update network discovery settings:", error)
        },
      })
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
  // eslint-disable-next-line react-hooks/exhaustive-deps -- form reference is stable, only sync on preferences change
  }, [preferences])

  if (isLoading || !preferences) {
    return (
      <div className="flex items-center justify-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">{tr("networkDiscoveryForm.loading")}</p>
      </div>
    )
  }

  const getEncryptionLabel = (value: number) => {
    switch (value) {
      case 0: return tr("networkDiscoveryForm.fields.encryptionOptions.prefer")
      case 1: return tr("networkDiscoveryForm.fields.encryptionOptions.require")
      case 2: return tr("networkDiscoveryForm.fields.encryptionOptions.disable")
      default: return tr("networkDiscoveryForm.fields.encryptionOptions.prefer")
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
          <h3 className="text-lg font-medium">{tr("networkDiscoveryForm.sections.peerDiscovery")}</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="dht">
            {(field) => (
              <SwitchSetting
                label={tr("networkDiscoveryForm.fields.dhtLabel")}
                description={tr("networkDiscoveryForm.fields.dhtDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="pex">
            {(field) => (
              <SwitchSetting
                label={tr("networkDiscoveryForm.fields.pexLabel")}
                description={tr("networkDiscoveryForm.fields.pexDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="lsd">
            {(field) => (
              <SwitchSetting
                label={tr("networkDiscoveryForm.fields.lsdLabel")}
                description={tr("networkDiscoveryForm.fields.lsdDescription")}
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
          <h3 className="text-lg font-medium">{tr("networkDiscoveryForm.sections.trackerSettings")}</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="announce_to_all_tiers">
            {(field) => (
              <SwitchSetting
                label={tr("networkDiscoveryForm.fields.announceAllTiersLabel")}
                description={tr("networkDiscoveryForm.fields.announceAllTiersDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="announce_to_all_trackers">
            {(field) => (
              <SwitchSetting
                label={tr("networkDiscoveryForm.fields.announceAllTrackersLabel")}
                description={tr("networkDiscoveryForm.fields.announceAllTrackersDescription")}
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
          <h3 className="text-lg font-medium">{tr("networkDiscoveryForm.sections.securityPrivacy")}</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="encryption">
            {(field) => (
              <div className="space-y-2">
                <Label className="text-sm font-medium">{tr("networkDiscoveryForm.fields.encryptionLabel")}</Label>
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
                  {tr("networkDiscoveryForm.fields.encryptionDescription")}
                </p>
              </div>
            )}
          </form.Field>

          <form.Field name="anonymous_mode">
            {(field) => (
              <SwitchSetting
                label={tr("networkDiscoveryForm.fields.anonymousModeLabel")}
                description={tr("networkDiscoveryForm.fields.anonymousModeDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="resolve_peer_countries">
            {(field) => (
              <SwitchSetting
                label={tr("networkDiscoveryForm.fields.resolvePeerCountriesLabel")}
                description={tr("networkDiscoveryForm.fields.resolvePeerCountriesDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
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
              {isSubmitting || isUpdating
                ? tr("networkDiscoveryForm.actions.saving")
                : tr("networkDiscoveryForm.actions.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
