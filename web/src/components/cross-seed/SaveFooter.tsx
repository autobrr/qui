/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { CardFooter } from "@/components/ui/card"
import { Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"

interface SaveFooterProps {
  pending: boolean
  onSave: () => void
  label?: string
}

export function SaveFooter({ pending, onSave, label }: SaveFooterProps) {
  const { t } = useTranslation("crossseed")
  return (
    <CardFooter className="flex justify-end">
      <Button className="min-h-11 md:min-h-9" onClick={onSave} disabled={pending}>
        {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
        {label ?? t("rules.saveChanges")}
      </Button>
    </CardFooter>
  )
}
