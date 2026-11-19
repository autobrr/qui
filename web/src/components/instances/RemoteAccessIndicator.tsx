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
  // fact: an icon and a few words, for the row of the instance in Settings, with the limits in a tooltip on hover. It
  // takes no focus, because the row is a button. inline: the list of limits, for the SSH tab.
  variant: "fact" | "inline"
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
  if (mode !== "local" && mode !== "remote") {
    return <span className={className}>{t("access.none")}</span>
  }
  const Icon = mode === "remote" ? KeyRound : HardDrive
  const fact = (
    <span className={cn(limits.length > 0 && "cursor-help", className)}>
      <Icon aria-hidden="true" className="mr-1 inline size-3 align-[-0.125em]" />
      {mode === "local" ? t("access.local") : t("access.remote")}
    </span>
  )
  if (limits.length === 0) {
    return fact
  }
  return (
    <Tooltip>
      {/* A tap opens the tooltip, not the row: the row opens the editor, which would cover the tooltip. A mouse click still opens the row. */}
      <TooltipTrigger asChild openOnTap="only">
        {fact}
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
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
