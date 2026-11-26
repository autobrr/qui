/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn, copyTextToClipboard } from "@/lib/utils"
import { Check, Copy } from "lucide-react"
import { type MouseEvent, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const CHECK_MS = 1500

interface CopyButtonProps {
  value: string
  label: string
  className?: string
  iconClassName?: string
}

export function CopyButton({ value, label, className, iconClassName = "size-3.5" }: CopyButtonProps) {
  const { t } = useTranslation("common")
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])

  const handleClick = async (e: MouseEvent) => {
    e.stopPropagation()
    try {
      await copyTextToClipboard(value)
    } catch {
      toast.error(t("feedback.copyFailed"))
      return
    }
    toast.success(t("feedback.copied", { label }))
    setCopied(true)
    clearTimeout(timer.current)
    timer.current = setTimeout(() => setCopied(false), CHECK_MS)
  }

  const name = t("actions.copyValue", { label })
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className={cn("shrink-0", className)}
          onClick={handleClick}
          aria-label={name}
          data-state={copied ? "copied" : "idle"}
        >
          <span className="relative inline-flex">
            <Copy className={cn(iconClassName, "transition-all duration-200", copied && "scale-50 opacity-0")} />
            <Check className={cn(iconClassName, "absolute inset-0 text-green-500 transition-all duration-200", !copied && "scale-50 opacity-0")} />
          </span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{name}</TooltipContent>
    </Tooltip>
  )
}
