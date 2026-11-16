/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId } from "react"
import { useTranslation } from "react-i18next"

import { Switch } from "@/components/ui/switch"
import { useDisplayProfiles, useLastColumnSetupInstance } from "@/lib/client-settings"
import { isAllInstancesScope } from "@/lib/instances"

interface SameColumnsSwitchProps {
  profileId: string
  instances: readonly { id: number; name: string }[]
}

/** Same columns on every instance for one profile, in a place with no instance on screen, such as Settings. */
export function SameColumnsSwitch({ profileId, instances }: SameColumnsSwitchProps) {
  const { t, i18n } = useTranslation("torrents")
  const { profiles, setSameColumns } = useDisplayProfiles()
  const lastInstanceId = useLastColumnSetupInstance()
  const switchId = useId()
  const profile = profiles.find((candidate) => candidate.id === profileId)
  if (!profile) return null

  const instanceLabel = (id: number) => isAllInstancesScope(id)? t("displayProfiles.columns.unifiedView"): instances.find((instance) => instance.id === id)?.name
  const lastInstance = lastInstanceId === null ? undefined : instanceLabel(lastInstanceId)
  const everyInstance = new Intl.ListFormat(i18n.language, { type: "conjunction" }).format([
    ...instances.map((instance) => instance.name),
    t("displayProfiles.sameColumns.unifiedViewInList"),
  ])
  const help = profile.usesSameColumns? t("displayProfiles.sameColumns.helpOn", { instances: everyInstance }): lastInstance === undefined? t("displayProfiles.sameColumns.helpOffNoInstance"): t("displayProfiles.sameColumns.helpOff", { instance: lastInstance })

  return (
    <div className="flex items-start gap-3">
      <div className="min-w-0 flex-1">
        <label htmlFor={switchId} className="text-sm leading-5">{t("displayProfiles.sameColumns.label")}</label>
        <p data-testid="same-columns-help" className="mt-0.5 text-xs leading-4 text-muted-foreground [overflow-wrap:anywhere]">{help}</p>
      </div>
      <Switch
        id={switchId}
        checked={profile.usesSameColumns}
        onCheckedChange={(on) => setSameColumns(profile.id, on)}
        className="mt-0.5"
      />
    </div>
  )
}
