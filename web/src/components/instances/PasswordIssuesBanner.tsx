/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { AlertTriangle } from "lucide-react"
import type { InstanceResponse } from "@/types"
import { useTranslation } from "react-i18next"

interface PasswordIssuesBannerProps {
  instances: InstanceResponse[]
}

export function PasswordIssuesBanner({ instances }: PasswordIssuesBannerProps) {
  const { t } = useTranslation()
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const hasDecryptionErrors = instances.some(instance => instance.hasDecryptionError)

  if (!hasDecryptionErrors) {
    return null
  }

  return (
    <Alert className="mb-6">
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>{tr("passwordIssuesBanner.title")}</AlertTitle>
      <AlertDescription>
        {tr("passwordIssuesBanner.description")}
      </AlertDescription>
    </Alert>
  )
}
