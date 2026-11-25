/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Switch } from "@/components/ui/switch"
import { isAllInstancesScope } from "@/lib/instances"

import { useNamedDisplayProfiles } from "./display-profile-names"

interface ColumnsMenuHeaderProps {
  instanceId: number
  instanceName?: string
}

export function ColumnsMenuHeader({ instanceId, instanceName }: ColumnsMenuHeaderProps) {
  const { t } = useTranslation("torrents")
  const { active, settled, setSameColumns } = useNamedDisplayProfiles()
  const same = active.usesSameColumns
  const instance = isAllInstancesScope(instanceId)? t("displayProfiles.columns.unifiedView"): instanceName ?? t("displayProfiles.columns.thisInstance")

  return (
    <>
      <div data-testid="columns-menu-heading" className="px-2 pb-1 pt-1.5 text-xs font-medium text-muted-foreground [overflow-wrap:anywhere]">
        {same? t("displayProfiles.columns.headingEveryInstance", { profile: active.name }): t("displayProfiles.columns.headingOneInstance", { profile: active.name, instance })}
      </div>
      <DropdownMenuItem
        role="menuitemcheckbox"
        aria-checked={same}
        disabled={!settled}
        className="items-start gap-3"
        onSelect={(event) => {
          event.preventDefault()
          setSameColumns(active.id, !same, instanceId)
        }}
      >
        <span className="min-w-0 flex-1">
          <span className="block">{t("displayProfiles.sameColumns.label")}</span>
          <span className="block text-xs text-muted-foreground">{t("displayProfiles.sameColumns.menuHelp")}</span>
        </span>
        <Switch checked={same} tabIndex={-1} aria-hidden="true" className="pointer-events-none mt-0.5" />
      </DropdownMenuItem>
    </>
  )
}
