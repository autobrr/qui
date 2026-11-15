/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import { CopyButton } from "@/components/ui/copy-button"
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
  const { t } = useTranslation("common")

  return (
    <div className={cn("flex items-center gap-1.5 min-w-0", className)}>
      <TruncatedText className="flex-1 min-w-0 text-sm">
        {path || "-"}
      </TruncatedText>
      {path && <CopyButton value={path} label={t("labels.path")} className="size-5 text-muted-foreground hover:text-foreground" />}
    </div>
  )
}
