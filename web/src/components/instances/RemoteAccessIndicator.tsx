/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { remoteAccessLimits, type RemoteAccessLimit } from "@/lib/remote-access"
import { cn } from "@/lib/utils"
import type { Instance } from "@/types"
import { HardDrive, KeyRound } from "lucide-react"
import { useTranslation } from "react-i18next"

interface RemoteAccessIndicatorProps {
  instance: Pick<Instance, "filesystemMode" | "sshTier" | "sshHardlink">
  // value: icon and label, for the instance card row. icon: the icon alone,
  // for the dashboard. inline: the list of limits, for the SSH tab.
  variant: "value" | "icon" | "inline"
  className?: string
}

// How qui reaches the instance's files, and on a remote instance what that
// leaves off: the degraded mode the design doc wants visible, not silent.
export function RemoteAccessIndicator({ instance, variant, className }: RemoteAccessIndicatorProps) {
  const { t } = useTranslation("instances")
  const limits = remoteAccessLimits(instance)

  if (variant === "inline") {
    if (limits.length === 0) {
      return null
    }
    return <LimitList limits={limits} className={cn("text-sm text-muted-foreground", className)} />
  }

  const mode = instance.filesystemMode
  // The dashboard shows nothing for an instance without file access.
  if (mode === "none" && variant === "icon") {
    return null
  }
  const Icon = mode === "remote" ? KeyRound : HardDrive
  const label = mode === "local" ? t("access.local") : mode === "remote" ? t("access.remote") : t("access.none")
  const content = (
    <>
      <Icon className={variant === "icon" ? "h-4 w-4" : "h-3 w-3"} aria-label={variant === "icon" ? label : undefined} />
      {variant === "value" && label}
    </>
  )
  const classes = cn("flex items-center gap-1", mode === "none" ? "text-muted-foreground" : "text-primary", className)
  if (mode === "none") {
    return <span className={classes}>{content}</span>
  }
  // A button, so the keyboard reaches the tooltip with the limits.
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" className={cn(classes, "cursor-help rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring")}>{content}</button>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        {variant === "icon" && <p className="font-medium">{label}</p>}
        {mode === "local" && <p>{t("access.localFull")}</p>}
        <LimitList limits={limits} />
      </TooltipContent>
    </Tooltip>
  )
}

function LimitList({ limits, className }: { limits: RemoteAccessLimit[]; className?: string }) {
  const { t } = useTranslation("instances")
  if (limits.length === 0) {
    return null
  }
  return (
    <ul className={cn("list-disc space-y-1 pl-4", className)}>
      {limits.map((limit) => (
        <li key={limit}>{limitText(limit, t)}</li>
      ))}
    </ul>
  )
}

function limitText(limit: RemoteAccessLimit, t: (key: string) => string): string {
  switch (limit) {
    case "noIdentity":
      return t("access.limits.noIdentity")
    case "noReflinks":
      return t("access.limits.noReflinks")
    case "noHardlinkExtension":
      return t("access.limits.noHardlinkExtension")
  }
}
