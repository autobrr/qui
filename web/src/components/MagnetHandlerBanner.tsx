/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Link2 } from "lucide-react"
import { toast } from "sonner"
import {
  canRegisterProtocolHandler,
  dismissProtocolHandlerBanner,
  getMagnetHandlerRegistrationGuidance,
  isProtocolHandlerBannerDismissed,
  registerMagnetHandler,
} from "@/lib/protocol-handler"

export function MagnetHandlerBanner() {
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
      toast.success("Magnet handler registration requested", {
        description: getMagnetHandlerRegistrationGuidance(),
      })
      dismissProtocolHandlerBanner()
      setDismissed(true)
    } else {
      toast.error("Failed to register magnet handler")
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
        <span>Register qui as your magnet link handler</span>
      </div>
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={handleRegister}>
          Register
        </Button>
        <Button variant="ghost" size="sm" onClick={handleDismiss}>
          Dismiss
        </Button>
      </div>
    </div>
  )
}
