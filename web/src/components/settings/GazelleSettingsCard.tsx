/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SaveFooter } from "@/components/ui/save-footer"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useCrossSeedSettings, usePatchCrossSeedSettings } from "@/hooks/useCrossSeedSettings"
import { changedSecret } from "@/lib/cross-seed-utils"
import type { CrossSeedAutomationSettings } from "@/types"
import { useState } from "react"
import { useTranslation } from "react-i18next"

/** Gazelle (OPS/RED) keys live on the cross-seed settings; this card saves only those three fields. */
export function GazelleSettingsCard() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return <GazelleCard settings={settings} />
}

function GazelleCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("settings")
  const patchSettings = usePatchCrossSeedSettings()
  const [gazelleEnabled, setGazelleEnabled] = useState(settings.gazelleEnabled)
  const [redactedApiKey, setRedactedApiKey] = useState(settings.redactedApiKey)
  const [orpheusApiKey, setOrpheusApiKey] = useState(settings.orpheusApiKey)

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("gazelle.title")}</CardTitle>
        <CardDescription>{t("gazelle.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-1.5">
            <Label htmlFor="gazelle-enabled" className="font-medium">{t("gazelle.enableMatching")}</Label>
            <FieldHelp>{t("gazelle.enableDescription")}</FieldHelp>
          </div>
          <Switch
            id="gazelle-enabled"
            checked={gazelleEnabled}
            onCheckedChange={setGazelleEnabled}
          />
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="gazelle-red-api-key">{t("gazelle.redactedApiKey")}</Label>
              <FieldHelp>{t("gazelle.redDescription")}</FieldHelp>
            </div>
            <Input
              id="gazelle-red-api-key"
              type="password"
              value={redactedApiKey}
              data-1p-ignore="true"
              onChange={event => setRedactedApiKey(event.target.value)}
              placeholder={gazelleEnabled ? t("gazelle.pasteRedKey") : t("gazelle.enableToConfigure")}
              disabled={!gazelleEnabled}
              autoComplete="off"
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="gazelle-ops-api-key">{t("gazelle.orpheusApiKey")}</Label>
              <FieldHelp>{t("gazelle.opsDescription")}</FieldHelp>
            </div>
            <Input
              id="gazelle-ops-api-key"
              type="password"
              value={orpheusApiKey}
              data-1p-ignore="true"
              onChange={event => setOrpheusApiKey(event.target.value)}
              placeholder={gazelleEnabled ? t("gazelle.pasteOpsKey") : t("gazelle.enableToConfigure")}
              disabled={!gazelleEnabled}
              autoComplete="off"
            />
          </div>
        </div>
      </CardContent>
      <SaveFooter
        pending={patchSettings.isPending}
        onSave={() => patchSettings.mutate({
          gazelleEnabled,
          redactedApiKey: changedSecret(redactedApiKey, settings.redactedApiKey),
          orpheusApiKey: changedSecret(orpheusApiKey, settings.orpheusApiKey),
        }, {
          // Take the placeholders back, so the next save does not check a saved key again.
          onSuccess: (data) => {
            setRedactedApiKey(data.redactedApiKey ?? "")
            setOrpheusApiKey(data.orpheusApiKey ?? "")
          },
        })}
        label={t("gazelle.save")}
      />
    </Card>
  )
}
