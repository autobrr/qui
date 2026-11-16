/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId } from "react"
import { Trans, useTranslation } from "react-i18next"

import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useDisplayProfiles } from "@/lib/client-settings"
import type { DisplayProfile } from "@/lib/display-profiles"

/** What "Own sort and filters" covers, as a list. The funnel in the profile list of Settings shows it too. */
export function OwnSortAndFiltersHelp() {
  return (
    <Trans
      ns="torrents"
      i18nKey="displayProfiles.ownSortAndFilters.help"
      components={{
        lead: <p />,
        list: <ul className="my-1 list-disc pl-4" />,
        item: <li />,
        note: <p />,
      }}
    />
  )
}

/** The "Own sort and filters" switch of a profile. Default always shares, so it shows a line in place of the switch. */
export function OwnSortAndFiltersSetting({ profile }: { profile: DisplayProfile }) {
  const { t } = useTranslation("torrents")
  const { setOwnSortAndFilters } = useDisplayProfiles()
  const switchId = useId()

  if (profile.isDefault) {
    return (
      <div className="flex items-center gap-1.5 text-sm text-muted-foreground">
        <span>{t("displayProfiles.ownSortAndFilters.defaultShares")}</span>
        <FieldHelp>{t("displayProfiles.ownSortAndFilters.defaultSharesHelp")}</FieldHelp>
      </div>
    )
  }

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-1.5">
          <Label htmlFor={switchId} className="cursor-pointer">{t("displayProfiles.ownSortAndFilters.label")}</Label>
          <FieldHelp><OwnSortAndFiltersHelp /></FieldHelp>
        </div>
        <Switch
          id={switchId}
          checked={profile.ownSortAndFilters}
          onCheckedChange={(on) => setOwnSortAndFilters(profile.id, on)}
        />
      </div>
      {profile.ownSortAndFilters && (
        <p className="text-xs text-muted-foreground">
          {t("displayProfiles.ownSortAndFilters.offWarning", { name: profile.name })}
        </p>
      )}
    </div>
  )
}
