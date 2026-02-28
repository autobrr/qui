/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn, copyTextToClipboard } from "@/lib/utils"
import { Copy } from "lucide-react"
import { memo } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface StatRowProps {
  label: string
  value: string | React.ReactNode
  copyable?: boolean
  copyValue?: string
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
  copyable,
  copyValue,
  monospace,
  className,
  valueClassName,
  valueStyle,
  tooltip,
  highlight,
}: StatRowProps) {
  const { t } = useTranslation()
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))

  const handleCopy = () => {
    const textToCopy = copyValue || (typeof value === "string" ? value : "")
    if (textToCopy) {
      copyTextToClipboard(textToCopy)
      toast.success(tr("torrentGeneral.toasts.copied", { label }))
    }
  }

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
      {copyable && (
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 shrink-0 opacity-50 hover:opacity-100"
          onClick={handleCopy}
          aria-label={tr("statRow.copyAria", { label })}
        >
          <Copy className="h-3 w-3" />
        </Button>
      )}
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
