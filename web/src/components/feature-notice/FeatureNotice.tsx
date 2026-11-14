/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as PopperPrimitive from "@radix-ui/react-popper"
import { useEffect, useId, useState, useSyncExternalStore, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { subscribeClientSettings, useClientSetting, useClientSettingsLoaded } from "@/lib/client-settings"
import { isDemo } from "@/lib/demo"
import { cn } from "@/lib/utils"
import { getDueNoticeId, SEEN_SETTING, seenKey } from "./due-notice"
import { FEATURE_NOTICES, type FeatureNoticeEntry, type FeatureNoticeId } from "./notices"

interface FeatureNoticeProps {
  id: FeatureNoticeId
  /** Turns the feature on. Called from the primary button of the bubble. */
  onAction: () => void
  /** The control the bubble points at. It must take a ref and a className. */
  children: ReactNode
}

/**
 * Points a one-time bubble at the wrapped control while its notice is due. The
 * notice counts as seen after Got it, the primary button, or once the feature
 * is in use while the control is in the page.
 */
export function FeatureNotice(props: FeatureNoticeProps) {
  if (isDemo) return props.children
  return <FeatureNoticeBubble {...props} />
}

function FeatureNoticeBubble({ id, onAction, children }: FeatureNoticeProps) {
  const notice: FeatureNoticeEntry = FEATURE_NOTICES.find((entry) => entry.id === id)!

  const loaded = useClientSettingsLoaded()
  const [seen, setSeen] = useClientSetting<boolean>(seenKey(id), SEEN_SETTING)
  const inUse = useSyncExternalStore(subscribeClientSettings, notice.isInUse)
  const [control, setControl] = useState<HTMLElement | null>(null)
  const present = control !== null

  useEffect(() => {
    if (loaded && present && inUse && !seen) setSeen(true)
  }, [loaded, present, inUse, seen, setSeen])

  const open = loaded && present && !seen && !inUse && getDueNoticeId() === id

  // The bubble goes away with its buttons. A button that had focus hands it to the control.
  const close = (button: HTMLElement) => {
    if (button === document.activeElement) control?.focus()
    setSeen(true)
  }

  return (
    <PopperPrimitive.Root>
      <PopperPrimitive.Anchor
        asChild
        ref={setControl}
        className={open ? "motion-safe:animate-feature-notice-pulse motion-reduce:ring-2 motion-reduce:ring-primary/30" : undefined}
      >
        {children}
      </PopperPrimitive.Anchor>
      {open && (
        <NoticeBubble
          notice={notice}
          onDismiss={close}
          onAction={(button) => {
            close(button)
            onAction()
          }}
        />
      )}
    </PopperPrimitive.Root>
  )
}

interface NoticeBubbleProps {
  notice: FeatureNoticeEntry
  onDismiss: (button: HTMLElement) => void
  onAction: (button: HTMLElement) => void
}

function NoticeBubble({ notice, onDismiss, onAction }: NoticeBubbleProps) {
  const { t } = useTranslation()
  const titleId = useId()

  // Popper, not Popover: the bubble takes no focus, keeps no Esc key or outside click, and is not a dialog.
  // No portal: the bubble follows its control in the tab order.
  return (
    <PopperPrimitive.Content
      side="bottom"
      sideOffset={5}
      collisionPadding={8}
      role="region"
      aria-labelledby={titleId}
      className="z-50 w-80 origin-(--radix-popper-transform-origin) rounded-xl border border-foreground/15 bg-popover p-3 text-popover-foreground shadow-xl outline-none motion-safe:animate-in motion-safe:fade-in-0 motion-safe:zoom-in-95 motion-safe:slide-in-from-top-1"
    >
      {notice.Picture && <notice.Picture />}
      <p id={titleId} className={cn("text-sm font-semibold leading-5", notice.Picture && "pt-3")}>{t(notice.titleKey)}</p>
      <p className="mt-1 text-sm leading-5 text-muted-foreground text-balance">{t(notice.descriptionKey)}</p>
      <div className="mt-3 flex justify-end gap-1">
        <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={(event) => onDismiss(event.currentTarget)}>{t("common:featureNotice.gotIt")}</Button>
        <Button size="sm" onClick={(event) => onAction(event.currentTarget)}>{t(notice.actionLabelKey)}</Button>
      </div>
      <PopperPrimitive.Arrow asChild>
        <svg width="20" height="10" viewBox="0 0 20 10" className="block overflow-visible">
          <path d="M0 -2 L0 0 L10 9 L20 0 L20 -2 Z" className="fill-popover" />
          <path d="M0 -0.5 L10 8.5 L20 -0.5" fill="none" className="stroke-foreground/15" />
        </svg>
      </PopperPrimitive.Arrow>
    </PopperPrimitive.Content>
  )
}
