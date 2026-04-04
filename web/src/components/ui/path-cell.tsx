/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Copy } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { cn, copyTextToClipboard } from "@/lib/utils"
import { TruncatedText } from "@/components/ui/truncated-text"

interface PathCellProps {
  path?: string | null
  className?: string
}

/**
 * A table cell component for displaying file paths with truncation and copy-to-clipboard.
 * Shows "-" when no path is provided.
 */
export function PathCell({ path, className }: PathCellProps) {
  const { t } = useTranslation()
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const hasPath = path != null && path !== ""
  let copyButtonToneClass = "text-muted-foreground/40 cursor-not-allowed"
  if (hasPath) {
    copyButtonToneClass = "text-muted-foreground hover:text-foreground cursor-pointer"
  }

  const handleCopy = async () => {
    if (!hasPath) return
    try {
      await copyTextToClipboard(path)
      toast.success(tr("pathCell.toasts.pathCopied"))
    } catch {
      toast.error(tr("pathCell.toasts.failedCopy"))
    }
  }

  return (
    <div className={cn("flex items-center gap-1.5 min-w-0", className)}>
      <TruncatedText className="flex-1 min-w-0 text-sm">
        {hasPath ? path : "-"}
      </TruncatedText>
      <button
        type="button"
        onClick={handleCopy}
        disabled={!hasPath}
        className={cn(
          "flex-shrink-0 p-0.5 rounded transition-colors",
          copyButtonToneClass
        )}
        aria-label={tr("pathCell.aria.copyPath")}
        title={hasPath ? tr("pathCell.tooltips.copyPath") : undefined}
      >
        <Copy className="size-3.5" />
      </button>
    </div>
  )
}
