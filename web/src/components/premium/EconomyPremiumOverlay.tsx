/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Lock, ShoppingCart } from "lucide-react"

interface EconomyPremiumOverlayProps {
  className?: string
}

export function EconomyPremiumOverlay({ className = "" }: EconomyPremiumOverlayProps) {
  return (
    <div className={`absolute inset-0 bg-background/25 backdrop-blur-sm flex flex-col items-center justify-center z-10 ${className}`}>
      <div className="text-center space-y-6 max-w-md mx-auto px-6">
        <div className="flex justify-center">
          <div className="relative">
            <Lock className="h-16 w-16 text-muted-foreground" />
          </div>
        </div>

        <div className="space-y-2">
          <h2 className="text-2xl font-bold">Premium Feature</h2>
          <p className="text-muted-foreground">
            Unlock the Economy Analysis feature to optimize your torrent storage and analyze retention value.
          </p>
        </div>

        <div className="space-y-4">
          <Button asChild className="w-full">
            <a
              href="https://buy.polar.sh/polar_cl_yyXJesVM9pFVfAPIplspbfCukgVgXzXjXIc2N0I8WcL"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2"
            >
              <ShoppingCart className="h-4 w-4" />
              Unlock Premium Features
            </a>
          </Button>

          <p className="text-sm text-muted-foreground">
            One-time purchase • $9.99 • Lifetime access
          </p>
        </div>
      </div>
    </div>
  )
}