/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Trans, useTranslation } from "react-i18next"

import { SwitchRow } from "@/components/kit/SwitchRow"
import { FieldHelp } from "@/components/ui/field-help"
import { useDisplayProfiles } from "@/lib/client-settings"
import type { DisplayProfile } from "@/lib/display-profiles"

import { StableText } from "./StableText"

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

/**
 * The "Own sort and filters" switch of a profile. Default always shares, so it shows a line in place of the switch.
 * The row keeps one height for every profile and in both states of the switch.
 */
export function OwnSortAndFiltersSetting({ profile }: { profile: DisplayProfile }) {
  const { t } = useTranslation("torrents")
  const { settled, setOwnSortAndFilters } = useDisplayProfiles()
  const label = t("displayProfiles.ownSortAndFilters.label")
  const offWarning = t("displayProfiles.ownSortAndFilters.offWarning", { name: profile.name })

  if (profile.isDefault) {
    return (
      <div className="grid [&>*]:col-start-1 [&>*]:row-start-1">
        <div aria-hidden="true" className="invisible flex items-start gap-4">
          <div className="min-w-0 flex-1">
            <StableText reserve={[label]} className="text-sm leading-5" />
            <StableText reserve={[offWarning]} className="mt-0.5 text-xs leading-4 [overflow-wrap:anywhere]" />
          </div>
          <span className="mt-0.5 h-[1.15rem] w-8 shrink-0" />
        </div>
        <div className="flex items-center gap-1.5 self-center text-sm text-muted-foreground">
          <span>{t("displayProfiles.ownSortAndFilters.defaultShares")}</span>
          <FieldHelp>{t("displayProfiles.ownSortAndFilters.defaultSharesHelp")}</FieldHelp>
        </div>
      </div>
    )
  }

  return (
    <SwitchRow
      label={label}
      help={<OwnSortAndFiltersHelp />}
      description={<StableText reserve={[offWarning]}>{profile.ownSortAndFilters && offWarning}</StableText>}
      checked={profile.ownSortAndFilters}
      disabled={!settled}
      onCheckedChange={(on) => setOwnSortAndFilters(profile.id, on)}
    />
  )
}
