/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { cn } from "@/lib/utils"
import { AlertTriangle } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { InstanceResponse } from "@/types"

interface PasswordIssuesBannerProps {
  instances: readonly InstanceResponse[]
  className?: string
}

export function PasswordIssuesBanner({ instances, className }: PasswordIssuesBannerProps) {
  const { t } = useTranslation("instances")
  const hasDecryptionErrors = instances.some(instance => instance.hasDecryptionError)

  if (!hasDecryptionErrors) {
    return null
  }

  return (
    <Alert className={cn("mb-6", className)}>
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>{t("passwordBanner.title")}</AlertTitle>
      <AlertDescription>
        {t("passwordBanner.description")}
      </AlertDescription>
    </Alert>
  )
}
