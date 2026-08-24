/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { Lock, Unlock } from "lucide-react"
import { useTranslation } from "react-i18next"

type DeleteFilesPreferenceProps = {
  id: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  isLocked: boolean
  onToggleLock: () => void
  className?: string
}

export function DeleteFilesPreference({
  id,
  checked,
  onCheckedChange,
  isLocked,
  onToggleLock,
  className,
}: DeleteFilesPreferenceProps) {
  const { t } = useTranslation("torrents")
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(value === true)}
      />
      <div className="flex items-center gap-2">
        <label htmlFor={id} className="text-sm font-medium">
          {t("deleteFilesPreference.label")}
        </label>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={onToggleLock}
              aria-pressed={isLocked}
              aria-label={
                isLocked? t("deleteFilesPreference.lockAriaLabel.stop"): t("deleteFilesPreference.lockAriaLabel.remember")
              }
              className={cn(
                "h-8 w-8 text-muted-foreground hover:text-accent-foreground",
                isLocked && "text-accent-foreground"
              )}
            >
              {isLocked ? (
                <Lock className="h-4 w-4" />
              ) : (
                <Unlock className="h-4 w-4" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {isLocked ? t("deleteFilesPreference.lockTooltip.stop") : t("deleteFilesPreference.lockTooltip.remember")}
          </TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
}
