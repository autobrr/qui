/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Field } from "@/components/kit/Field"
import { Input } from "@/components/ui/input"
import { useTranslation } from "react-i18next"

interface PublicPeerAddressFieldProps {
  value: string
  resolvedValue?: string
  onChange: (value: string) => void
}

export function PublicPeerAddressField({ value, resolvedValue, onChange }: PublicPeerAddressFieldProps) {
  const { t } = useTranslation("instances")

  return (
    <Field label={t("publicPeerAddress.label")} help={t("publicPeerAddress.description")}>
      <Input
        value={value}
        placeholder={resolvedValue || t("publicPeerAddress.noResolvedAddress")}
        onChange={(event) => onChange(event.target.value)}
        autoComplete="off"
        spellCheck={false}
      />
    </Field>
  )
}
