/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { AlertTriangle } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { InstanceResponse } from "@/types"

interface PasswordIssuesBannerProps {
  instances: InstanceResponse[]
}

export function PasswordIssuesBanner({ instances }: PasswordIssuesBannerProps) {
  const { t } = useTranslation()
  const hasDecryptionErrors = instances.some(instance => instance.hasDecryptionError)

  if (!hasDecryptionErrors) {
    return null
  }

  return (
    <Alert className="mb-6">
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>{t("instances.passwordIssues.title")}</AlertTitle>
      <AlertDescription>
        {t("instances.passwordIssues.description")}
      </AlertDescription>
    </Alert>
  )
}