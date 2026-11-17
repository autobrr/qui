/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useId } from "react"
import { useTranslation } from "react-i18next"

interface PublicPeerAddressFieldProps {
  /** The address that the user set (publicPeerAddress), or "" when none is set. */
  value: string
  /** The address that qui works out from qBittorrent (resolvedPublicPeerAddress), if it can. */
  resolvedValue?: string
  /** Gets only what the user types; "" clears the user's value. */
  onChange: (value: string) => void
}

/**
 * The public peer address of an instance: the address and port at which other
 * BitTorrent clients reach its qBittorrent, for Transfers and Shares. The
 * worked-out address only shows as a placeholder, so a save never stores it
 * as the user's value.
 */
export function PublicPeerAddressField({ value, resolvedValue, onChange }: PublicPeerAddressFieldProps) {
  const { t } = useTranslation("instances")
  const id = useId()

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-1.5">
        <Label htmlFor={id}>{t("publicPeerAddress.label")}</Label>
        <FieldHelp>{t("publicPeerAddress.description")}</FieldHelp>
      </div>
      <Input
        id={id}
        value={value}
        placeholder={resolvedValue || t("publicPeerAddress.noResolvedAddress")}
        onChange={(event) => onChange(event.target.value)}
        autoComplete="off"
        spellCheck={false}
      />
    </div>
  )
}
