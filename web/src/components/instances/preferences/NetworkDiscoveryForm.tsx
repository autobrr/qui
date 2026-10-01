/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { Button } from "@/components/ui/button"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Radar, Users, Shield } from "lucide-react"
import { usePreferencesForm } from "@/hooks/usePreferencesForm"
import type { AppPreferences } from "@/types"
import { useTranslation } from "react-i18next"

import { PreferencesFormShell } from "./PreferencesFormShell"
import { PreferencesSection } from "./PreferencesSection"

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

  return (
    <label
      htmlFor={switchId}
      className="flex items-center gap-3 cursor-pointer"
    >
      <Switch
        id={switchId}
        checked={checked}
        onCheckedChange={onChange}
      />
      <span className="text-sm font-medium">{label}</span>
      {description && <FieldHelp>{description}</FieldHelp>}
    </label>
  )
}

export function NetworkDiscoveryForm({ instanceId, onSuccess }: NetworkDiscoveryFormProps) {
  return (
    <PreferencesSection instanceId={instanceId} i18nPrefix="preferences.networkDiscovery">
      {(preferences) => <NetworkDiscoveryFields instanceId={instanceId} preferences={preferences} onSuccess={onSuccess} />}
    </PreferencesSection>
  )
}

function NetworkDiscoveryFields({ instanceId, preferences, onSuccess }: NetworkDiscoveryFormProps & { preferences: AppPreferences }) {
  const { t } = useTranslation("instances")
  const { form, isUpdating } = usePreferencesForm({
    instanceId,
    preferences,
    toForm: (p) => ({
      dht: p.dht,
      pex: p.pex,
      lsd: p.lsd,
      encryption: p.encryption,
      anonymous_mode: p.anonymous_mode,
      announce_to_all_tiers: p.announce_to_all_tiers,
      announce_to_all_trackers: p.announce_to_all_trackers,
      resolve_peer_countries: p.resolve_peer_countries,
    }),
    i18nPrefix: "preferences.networkDiscovery",
    onSaved: onSuccess,
  })

  const getEncryptionLabel = (value: number) => {
    switch (value) {
      case 1: return t("preferences.networkDiscovery.requireEncryption")
      case 2: return t("preferences.networkDiscovery.disableEncryption")
      default: return t("preferences.networkDiscovery.preferEncryption")
    }
  }

  return (
    <PreferencesFormShell
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      footer={(
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              disabled={!canSubmit || isSubmitting || isUpdating}
              className="min-w-32"
            >
              {isSubmitting || isUpdating ? t("preferences.common.saving") : t("preferences.common.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      )}
    >
      <div className="space-y-6">
        {/* Peer Discovery Section */}
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Radar className="h-4 w-4" />
            <h3 className="text-lg font-medium">{t("preferences.networkDiscovery.peerDiscovery")}</h3>
          </div>

          <div className="space-y-4">
            <form.Field name="dht">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.networkDiscovery.enableDht")}
                  description={t("preferences.networkDiscovery.enableDhtDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="pex">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.networkDiscovery.enablePex")}
                  description={t("preferences.networkDiscovery.enablePexDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="lsd">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.networkDiscovery.enableLsd")}
                  description={t("preferences.networkDiscovery.enableLsdDescription")}
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
            <h3 className="text-lg font-medium">{t("preferences.networkDiscovery.trackerSettings")}</h3>
          </div>

          <div className="space-y-4">
            <form.Field name="announce_to_all_tiers">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.networkDiscovery.announceToAllTiers")}
                  description={t("preferences.networkDiscovery.announceToAllTiersDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="announce_to_all_trackers">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.networkDiscovery.announceToAllTrackers")}
                  description={t("preferences.networkDiscovery.announceToAllTrackersDescription")}
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
            <h3 className="text-lg font-medium">{t("preferences.networkDiscovery.securityPrivacy")}</h3>
          </div>

          <div className="space-y-4">
            <form.Field name="encryption">
              {(field) => (
                <div className="space-y-2">
                  <Label className="flex items-center gap-2 text-sm font-medium">
                    {t("preferences.networkDiscovery.protocolEncryption")}
                    <FieldHelp>{t("preferences.networkDiscovery.encryptionDescription")}</FieldHelp>
                  </Label>
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
                </div>
              )}
            </form.Field>

            <form.Field name="anonymous_mode">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.networkDiscovery.enableAnonymousMode")}
                  description={t("preferences.networkDiscovery.enableAnonymousModeDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="resolve_peer_countries">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.networkDiscovery.resolvePeerCountries")}
                  description={t("preferences.networkDiscovery.resolvePeerCountriesDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>
          </div>
        </div>
      </div>
    </PreferencesFormShell>
  )
}
