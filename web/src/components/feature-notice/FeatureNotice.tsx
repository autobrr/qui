/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as PopperPrimitive from "@radix-ui/react-popper"
import { X } from "lucide-react"
import { useEffect, useId, useState, useSyncExternalStore, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { subscribeClientSettings, useClientSetting, useClientSettingsLoaded } from "@/lib/client-settings"
import { isDemo } from "@/lib/demo"
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
 * notice counts as seen after Got it, the X, the primary button, or once the
 * feature is in use while the control is in the page.
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

  return (
    <PopperPrimitive.Root>
      <PopperPrimitive.Anchor
        asChild
        ref={setControl}
        className={open ? "motion-safe:animate-feature-notice-pulse" : undefined}
      >
        {children}
      </PopperPrimitive.Anchor>
      {open && (
        <NoticeBubble
          notice={notice}
          onDismiss={() => setSeen(true)}
          onAction={() => {
            setSeen(true)
            onAction()
          }}
        />
      )}
    </PopperPrimitive.Root>
  )
}

interface NoticeBubbleProps {
  notice: FeatureNoticeEntry
  onDismiss: () => void
  onAction: () => void
}

function NoticeBubble({ notice, onDismiss, onAction }: NoticeBubbleProps) {
  const { t } = useTranslation()
  const titleId = useId()

  // Popper, not Popover: the bubble takes no focus, keeps no Esc key or outside click, and is not a dialog.
  // No portal: the bubble follows its control in the tab order.
  return (
    <PopperPrimitive.Content
      side="bottom"
      sideOffset={8}
      collisionPadding={8}
      role="region"
      aria-labelledby={titleId}
      className="z-50 w-72 rounded-md border border-primary/60 bg-popover p-4 text-popover-foreground shadow-md outline-none"
    >
      <div className="flex items-start justify-between gap-2">
        <p id={titleId} className="text-sm font-medium">{t(notice.titleKey)}</p>
        <button
          type="button"
          aria-label={t("common:actions.close")}
          className="-mr-1 -mt-1 rounded-sm p-1 text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onClick={onDismiss}
        >
          <X className="h-4 w-4" />
        </button>
      </div>
      <p className="mt-1 text-sm text-muted-foreground">{t(notice.descriptionKey)}</p>
      <div className="mt-3 flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={onDismiss}>{t("common:featureNotice.gotIt")}</Button>
        <Button size="sm" onClick={onAction}>{t(notice.actionLabelKey)}</Button>
      </div>
      <PopperPrimitive.Arrow className="fill-primary" width={12} height={6} />
    </PopperPrimitive.Content>
  )
}
