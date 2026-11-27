/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { cn } from "@/lib/utils"
import { memo } from "react"

interface StatRowProps {
  label: string
  value: string | React.ReactNode
  monospace?: boolean
  className?: string
  valueClassName?: string
  valueStyle?: React.CSSProperties
  tooltip?: string
}

export const StatRow = memo(function StatRow({
  label,
  value,
  monospace,
  className,
  valueClassName,
  valueStyle,
  tooltip,
}: StatRowProps) {
  const content = (
    <div className={cn("flex items-center gap-1.5 text-xs", className)}>
      <span className="text-muted-foreground shrink-0">{label}:</span>
      <TruncatedText
        className={cn(
          "font-medium",
          monospace && "font-mono",
          valueClassName
        )}
        style={valueStyle}
      >
        {value}
      </TruncatedText>
    </div>
  )

  if (tooltip) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        <TooltipContent>
          <p className="text-xs">{tooltip}</p>
        </TooltipContent>
      </Tooltip>
    )
  }

  return content
})
