/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { useTranslation } from "react-i18next"

import { Field } from "@/components/kit/Field"
import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Input } from "@/components/ui/input"
import { useCrossSeedSettings, usePatchCrossSeedSettings } from "@/hooks/useCrossSeedSettings"
import { changedSecret } from "@/lib/cross-seed-utils"
import type { CrossSeedAutomationSettings } from "@/types"

export function GazelleSettingsCard() {
  const { t } = useTranslation("settings")
  const query = useCrossSeedSettings()
  if (query.data) {
    return <GazelleSection settings={query.data} />
  }
  return <Section title={t("gazelle.title")} description={t("gazelle.description")} queries={[query]} />
}

function GazelleSection({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("settings")
  const patchSettings = usePatchCrossSeedSettings()
  const [gazelleEnabled, setGazelleEnabled] = useState(settings.gazelleEnabled)
  const [redactedApiKey, setRedactedApiKey] = useState(settings.redactedApiKey)
  const [orpheusApiKey, setOrpheusApiKey] = useState(settings.orpheusApiKey)

  const save = () => patchSettings.mutate({
    gazelleEnabled,
    redactedApiKey: changedSecret(redactedApiKey, settings.redactedApiKey),
    orpheusApiKey: changedSecret(orpheusApiKey, settings.orpheusApiKey),
  }, {
    onSuccess: (data) => {
      setRedactedApiKey(data.redactedApiKey ?? "")
      setOrpheusApiKey(data.orpheusApiKey ?? "")
    },
  })

  const keyInput = {
    type: "password",
    autoComplete: "off",
    disabled: !gazelleEnabled,
  }

  return (
    <Section
      title={t("gazelle.title")}
      description={t("gazelle.description")}
      submit={{ label: t("gazelle.save"), onSubmit: save, pending: patchSettings.isPending }}
    >
      <SwitchRow
        label={t("gazelle.enableMatching")}
        help={t("gazelle.enableDescription")}
        checked={gazelleEnabled}
        onCheckedChange={setGazelleEnabled}
      />
      <div className="grid gap-6 sm:grid-cols-2">
        <Field label={t("gazelle.redactedApiKey")} help={t("gazelle.redDescription")}>
          <Input
            {...keyInput}
            value={redactedApiKey}
            onChange={(event) => setRedactedApiKey(event.target.value)}
            placeholder={gazelleEnabled ? t("gazelle.pasteRedKey") : t("gazelle.enableToConfigure")}
          />
        </Field>
        <Field label={t("gazelle.orpheusApiKey")} help={t("gazelle.opsDescription")}>
          <Input
            {...keyInput}
            value={orpheusApiKey}
            onChange={(event) => setOrpheusApiKey(event.target.value)}
            placeholder={gazelleEnabled ? t("gazelle.pasteOpsKey") : t("gazelle.enableToConfigure")}
          />
        </Field>
      </div>
    </Section>
  )
}
