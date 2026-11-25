/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { SwitchRow } from "@/components/kit/SwitchRow"
import { useDisplayProfiles, useLastColumnSetupInstance } from "@/lib/client-settings"
import { isAllInstancesScope } from "@/lib/instances"

import { StableText } from "./StableText"

interface SameColumnsSwitchProps {
  profileId: string
  instances: readonly { id: number; name: string }[]
}

export function SameColumnsSwitch({ profileId, instances }: SameColumnsSwitchProps) {
  const { t, i18n } = useTranslation("torrents")
  const { profiles, settled, setSameColumns } = useDisplayProfiles()
  const lastInstanceId = useLastColumnSetupInstance()
  const profile = profiles.find((candidate) => candidate.id === profileId)
  if (!profile) return null

  const instanceLabel = (id: number) => isAllInstancesScope(id)? t("displayProfiles.columns.unifiedView"): instances.find((instance) => instance.id === id)?.name
  const lastInstance = lastInstanceId === null ? undefined : instanceLabel(lastInstanceId)
  const everyInstance = new Intl.ListFormat(i18n.language, { type: "conjunction" }).format([
    ...instances.map((instance) => instance.name),
    t("displayProfiles.sameColumns.unifiedViewInList"),
  ])
  const helpOn = t("displayProfiles.sameColumns.helpOn", { instances: everyInstance })
  const helpOff = lastInstance === undefined? t("displayProfiles.sameColumns.helpOffNoInstance"): t("displayProfiles.sameColumns.helpOff", { instance: lastInstance })

  return (
    <SwitchRow
      label={t("displayProfiles.sameColumns.label")}
      description={<StableText reserve={[helpOn, helpOff]}>{profile.usesSameColumns ? helpOn : helpOff}</StableText>}
      checked={profile.usesSameColumns}
      disabled={!settled}
      onCheckedChange={(on) => setSameColumns(profile.id, on)}
    />
  )
}
