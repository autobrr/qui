/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check, Copy, Info } from "lucide-react"
import { useEffect, useRef, useState, type PointerEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { copyTextToClipboard } from "@/lib/utils"

import type { PathTemplateEntry } from "./pathTemplateVariables"

const MOVE_PATH_TEMPLATE_EXAMPLES: PathTemplateEntry[] = [
  { snippet: "/data/{{ .Category }}", descriptionKey: "examples.byCategory" },
  { snippet: "/data/{{ .Tracker }}/{{ sanitize .Name }}", descriptionKey: "examples.byTrackerAndName" },
]

const MOVE_PATH_TEMPLATE_DOCS_URL = "https://getqui.com/docs/features/automations/#move-path-templates"

// Long enough to cross the gap between the trigger and the popover.
const HOVER_CLOSE_DELAY_MS = 200
const COPIED_RESET_MS = 1500

interface PathTemplateHelpProps {
  variables: PathTemplateEntry[]
  // Field help shown above the template list, so the label carries one help icon.
  description?: ReactNode
}

// A popover rather than FieldHelp's tooltip: a tooltip closes before the
// pointer reaches the copy buttons. Mouse hover previews it; a click or tap
// pins it open until the user clicks outside or presses Escape.
export function PathTemplateHelp({ variables, description }: PathTemplateHelpProps) {
  const { t } = useTranslation("instances")
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [copiedSnippet, setCopiedSnippet] = useState<string | null>(null)
  const closeTimer = useRef<number | undefined>(undefined)
  const copiedTimer = useRef<number | undefined>(undefined)

  useEffect(() => () => {
    window.clearTimeout(closeTimer.current)
    window.clearTimeout(copiedTimer.current)
  }, [])

  const handlePointerEnter = (event: PointerEvent) => {
    if (event.pointerType !== "mouse") return
    window.clearTimeout(closeTimer.current)
    setOpen(true)
  }

  const handlePointerLeave = (event: PointerEvent) => {
    if (event.pointerType !== "mouse" || pinned) return
    closeTimer.current = window.setTimeout(() => setOpen(false), HOVER_CLOSE_DELAY_MS)
  }

  const handleOpenChange = (next: boolean) => {
    setOpen(next)
    if (!next) setPinned(false)
  }

  const handleCopy = async (snippet: string) => {
    try {
      await copyTextToClipboard(snippet)
      setCopiedSnippet(snippet)
      window.clearTimeout(copiedTimer.current)
      copiedTimer.current = window.setTimeout(() => setCopiedSnippet(null), COPIED_RESET_MS)
    } catch {
      toast.error(t("preferences.workflowDialog.move.templateHelp.copyFailed"))
    }
  }

  const renderEntry = (entry: PathTemplateEntry) => (
    <li key={entry.snippet} className="space-y-0.5">
      <div className="flex items-center gap-1">
        <code className="min-w-0 flex-1 break-all rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
          {entry.snippet}
        </code>
        <button
          type="button"
          onClick={() => void handleCopy(entry.snippet)}
          className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          aria-label={t("preferences.workflowDialog.move.templateHelp.copySnippet", { snippet: entry.snippet })}
        >
          {copiedSnippet === entry.snippet ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
        </button>
      </div>
      <p className="text-xs text-muted-foreground">
        {t(`preferences.workflowDialog.move.templateHelp.${entry.descriptionKey}`)}
      </p>
    </li>
  )

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="inline-flex shrink-0 items-center text-muted-foreground hover:text-foreground"
          aria-label={t("preferences.workflowDialog.move.templateHelp.trigger")}
          onPointerEnter={handlePointerEnter}
          onPointerLeave={handlePointerLeave}
          onClick={(event) => {
            // Radix would toggle, closing a popover that hover just opened.
            event.preventDefault()
            if (pinned) {
              handleOpenChange(false)
            } else {
              setPinned(true)
              setOpen(true)
            }
          }}
        >
          <Info className="size-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="max-h-(--radix-popover-content-available-height) w-[min(24rem,calc(100vw-2rem))] space-y-3 overflow-y-auto p-3 text-sm"
        onPointerEnter={handlePointerEnter}
        onPointerLeave={handlePointerLeave}
        onOpenAutoFocus={(event) => event.preventDefault()}
        // The portaled content sits outside the dialog's scroll lock, which would swallow these.
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
      >
        {description && <p className="text-xs">{description}</p>}
        <div className="space-y-1">
          <p className="font-medium">{t("preferences.workflowDialog.move.templateHelp.title")}</p>
          <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.move.templateHelp.intro")}</p>
        </div>
        <ul className="space-y-2">{variables.map(renderEntry)}</ul>
        <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.move.templateHelp.trackerNote")}</p>
        <div className="space-y-2">
          <p className="text-xs font-medium">{t("preferences.workflowDialog.move.templateHelp.examplesHeading")}</p>
          <ul className="space-y-2">{MOVE_PATH_TEMPLATE_EXAMPLES.map(renderEntry)}</ul>
        </div>
        <a
          href={MOVE_PATH_TEMPLATE_DOCS_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-block text-xs text-primary underline underline-offset-4"
        >
          {t("preferences.workflowDialog.move.templateHelp.learnMore")}
        </a>
      </PopoverContent>
    </Popover>
  )
}
