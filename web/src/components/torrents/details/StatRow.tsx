/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
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
  highlight?: "green" | "blue" | "yellow" | "red"
}

export const StatRow = memo(function StatRow({
  label,
  value,
  monospace,
  className,
  valueClassName,
  valueStyle,
  tooltip,
  highlight,
}: StatRowProps) {
  const highlightClass = highlight? {
    green: "text-green-500",
    blue: "text-blue-500",
    yellow: "text-yellow-500",
    red: "text-red-500",
  }[highlight]: ""

  const content = (
    <div className={cn("flex items-center gap-1.5 text-xs", className)}>
      <span className="text-muted-foreground shrink-0">{label}:</span>
      <span
        className={cn(
          "font-medium truncate",
          monospace && "font-mono",
          highlightClass,
          valueClassName
        )}
        style={valueStyle}
        title={typeof value === "string" ? value : undefined}
      >
        {value}
      </span>
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
