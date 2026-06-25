/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import {
  FILE_PRIORITY_LABEL_KEYS,
  FILE_PRIORITY_OPTIONS,
  type FilePriorityValue,
  type FolderPriority
} from "@/lib/file-priority"
import { cn } from "@/lib/utils"
import type { MouseEvent as ReactMouseEvent } from "react"
import { useTranslation } from "react-i18next"

interface FilePrioritySelectProps {
  value: FolderPriority
  disabled?: boolean
  onChange: (priority: FilePriorityValue) => void
  className?: string
}

// qBittorrent-style download-priority dropdown shared by the file table and tree views.
// "Mixed" is a display-only state for folders whose files differ; it is never selectable.
export function FilePrioritySelect({ value, disabled, onChange, className }: FilePrioritySelectProps) {
  const { t } = useTranslation("torrents")
  const isMixed = value === "mixed"

  return (
    <Select
      value={String(value)}
      disabled={disabled}
      onValueChange={(next) => {
        if (next === "mixed") return
        onChange(Number(next) as FilePriorityValue)
      }}
    >
      {/*
        h-6! is intentional: the shared SelectTrigger sets its height via a data-[size]:h-*
        variant whose attribute selector outspecifies a plain h-* utility, so a non-important
        height would be ignored. Callers pass width only; height is owned here.
      */}
      <SelectTrigger
        className={cn("h-6! gap-1 px-2 text-xs", className)}
        onClick={(e: ReactMouseEvent) => e.stopPropagation()}
        aria-label={t("filePriority.header")}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {isMixed && (
          <SelectItem value="mixed" disabled>
            {t("filePriority.mixed")}
          </SelectItem>
        )}
        {FILE_PRIORITY_OPTIONS.map((option) => (
          <SelectItem key={option} value={String(option)}>
            {t(FILE_PRIORITY_LABEL_KEYS[option])}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
