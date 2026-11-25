/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferHint } from "@/lib/transfers-api"
import { Lightbulb } from "lucide-react"
import { useTranslation } from "react-i18next"
import { useInstanceName } from "./use-instance-name"

export function TransferHints({ hints }: { hints: TransferHint[] }) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  if (hints.length === 0) return null
  return (
    <ul className="space-y-1">
      {hints.map(hint => (
        <li key={`${hint.kind}-${hint.instanceId}`} className="flex gap-1.5 text-xs text-muted-foreground">
          <Lightbulb className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          <span>{t(`hints.${hint.kind}`, { instance: instanceName(hint.instanceId) })}</span>
        </li>
      ))}
    </ul>
  )
}
