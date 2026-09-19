/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check, Copy, Info } from "lucide-react"
import { Fragment, useEffect, useRef, useState, type PointerEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { copyTextToClipboard } from "@/lib/utils"

import type { PathTemplateEntry } from "./pathTemplateVariables"

const MOVE_PATH_TEMPLATE_EXAMPLE = "/data/{{ .Tracker }}/{{ sanitize .Name }}"

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

  // The snippet itself is the copy button, so each value takes one row.
  const renderSnippet = (snippet: string) => (
    <button
      type="button"
      onClick={() => void handleCopy(snippet)}
      className="inline-flex max-w-full items-center gap-1.5 rounded bg-muted px-1.5 py-0.5 text-left font-mono text-xs transition-colors hover:bg-accent"
      aria-label={t("preferences.workflowDialog.move.templateHelp.copySnippet", { snippet })}
    >
      <span className="break-all">{snippet}</span>
      {copiedSnippet === snippet
        ? <Check className="size-3 shrink-0" />
        : <Copy className="size-3 shrink-0 text-muted-foreground" />}
    </button>
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
        collisionPadding={16}
        className="max-h-(--radix-popover-content-available-height) w-[min(26rem,calc(100vw-2rem))] space-y-3 overflow-y-auto p-3 text-sm"
        onPointerEnter={handlePointerEnter}
        onPointerLeave={handlePointerLeave}
        onOpenAutoFocus={(event) => event.preventDefault()}
        // The portaled content sits outside the dialog's scroll lock, which would swallow these.
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
      >
        {description && <p className="text-xs">{description}</p>}
        <p className="font-medium">{t("preferences.workflowDialog.move.templateHelp.title")}</p>
        <dl className="grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1.5">
          {variables.map(entry => (
            <Fragment key={entry.snippet}>
              <dt>{renderSnippet(entry.snippet)}</dt>
              <dd className="text-xs text-muted-foreground">
                {t(`preferences.workflowDialog.move.templateHelp.${entry.descriptionKey}`)}
              </dd>
            </Fragment>
          ))}
        </dl>
        <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.move.templateHelp.trackerNote")}</p>
        <div className="space-y-1">
          <p className="text-xs font-medium">{t("preferences.workflowDialog.move.templateHelp.exampleLabel")}</p>
          {renderSnippet(MOVE_PATH_TEMPLATE_EXAMPLE)}
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
