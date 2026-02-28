/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Link2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import {
  canRegisterProtocolHandler,
  dismissProtocolHandlerBanner,
  getMagnetHandlerRegistrationGuidanceVariant,
  isProtocolHandlerBannerDismissed,
  registerMagnetHandler,
} from "@/lib/protocol-handler"

export function MagnetHandlerBanner() {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const [dismissed, setDismissed] = useState(() => isProtocolHandlerBannerDismissed())

  // Don't show if browser doesn't support registerProtocolHandler or not HTTPS
  if (!canRegisterProtocolHandler()) {
    return null
  }

  // Don't show if user has dismissed
  if (dismissed) {
    return null
  }

  const handleRegister = () => {
    const success = registerMagnetHandler()
    if (success) {
      const guidanceVariant = getMagnetHandlerRegistrationGuidanceVariant()
      toast.success(tr("magnetHandlerBanner.toasts.registrationRequested"), {
        description: tr(`magnetHandlerBanner.guidance.${guidanceVariant}`),
      })
      dismissProtocolHandlerBanner()
      setDismissed(true)
    } else {
      toast.error(tr("magnetHandlerBanner.toasts.failedRegister"))
    }
  }

  const handleDismiss = () => {
    dismissProtocolHandlerBanner()
    setDismissed(true)
  }

  return (
    <div className="mb-4 flex items-center justify-between gap-4 rounded-md bg-blue-500/10 border border-blue-500/20 px-4 py-2.5 text-sm">
      <div className="flex items-center gap-2">
        <Link2 className="h-4 w-4 text-blue-500" />
        <span>{tr("magnetHandlerBanner.message")}</span>
      </div>
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={handleRegister}>
          {tr("magnetHandlerBanner.actions.register")}
        </Button>
        <Button variant="ghost" size="sm" onClick={handleDismiss}>
          {tr("magnetHandlerBanner.actions.dismiss")}
        </Button>
      </div>
    </div>
  )
}
