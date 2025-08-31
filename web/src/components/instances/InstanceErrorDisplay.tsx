/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger
} from "@/components/ui/collapsible"
import { XCircle, Edit, AlertCircle, ChevronDown } from "lucide-react"
import type { InstanceResponse } from "@/types"
import { formatErrorMessage } from "@/lib/utils"

interface InstanceErrorDisplayProps {
  instance: InstanceResponse
  onEdit?: () => void
  showEditButton?: boolean
  compact?: boolean
}

export function InstanceErrorDisplay({ instance, onEdit, showEditButton = false, compact = false }: InstanceErrorDisplayProps) {
  const [isDecryptionOpen, setIsDecryptionOpen] = useState(false)
  const [isConnectionOpen, setIsConnectionOpen] = useState(false)

  // Helper to check if connection error is decryption-related
  const isDecryptionError = (error: string) => {
    const errorLower = error.toLowerCase()
    return errorLower.includes("decrypt") &&
           (errorLower.includes("password") || errorLower.includes("cipher"))
  }

  // Compact mode shows expandable error cards
  if (compact) {
    return (
      <>
        {instance.hasDecryptionError && (
          <Collapsible open={isDecryptionOpen} onOpenChange={setIsDecryptionOpen} className="mt-2">
            <div className="rounded-lg border border-destructive/20 bg-destructive/10">
              <CollapsibleTrigger className="flex w-full items-center justify-between p-3 text-left hover:bg-destructive/20 transition-colors">
                <div className="flex items-center gap-2">
                  <XCircle className="h-4 w-4 text-destructive flex-shrink-0" />
                  <span className="text-sm font-medium text-destructive">Password Required</span>
                </div>
                <ChevronDown className={`h-4 w-4 text-destructive transition-transform duration-200 ${isDecryptionOpen ? "rotate-180" : ""}`} />
              </CollapsibleTrigger>

              <CollapsibleContent className="px-3 pb-3">
                <div className="text-sm text-destructive/90 mt-2 mb-3">
                  Unable to decrypt saved password. This usually happens when the session secret has changed.
                </div>
                {showEditButton && onEdit && (
                  <Button
                    onClick={onEdit}
                    size="sm"
                    variant="outline"
                    className="h-7 px-3 text-xs"
                  >
                    <Edit className="mr-1 h-3 w-3" />
                    Re-enter Password
                  </Button>
                )}
              </CollapsibleContent>
            </div>
          </Collapsible>
        )}

        {instance.connectionError && !(instance.hasDecryptionError && isDecryptionError(instance.connectionError)) && (
          <Collapsible open={isConnectionOpen} onOpenChange={setIsConnectionOpen} className="mt-2">
            <div className="rounded-lg border border-destructive/20 bg-destructive/10">
              <CollapsibleTrigger className="flex w-full items-center justify-between p-3 text-left hover:bg-destructive/20 transition-colors">
                <div className="flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 text-destructive flex-shrink-0" />
                  <span className="text-sm font-medium text-destructive">Connection Error</span>
                </div>
                <ChevronDown className={`h-4 w-4 text-destructive transition-transform duration-200 ${isConnectionOpen ? "rotate-180" : ""}`} />
              </CollapsibleTrigger>

              <CollapsibleContent className="px-3 pb-3">
                <div className="text-sm text-destructive/90 mt-2 font-mono leading-relaxed">
                  {formatErrorMessage(instance.connectionError)}
                </div>
              </CollapsibleContent>
            </div>
          </Collapsible>
        )}
      </>
    )
  }

  // Full mode shows expanded error messages (for dedicated pages)
  return (
    <>
      {instance.hasDecryptionError && (
        <div className="mt-4 p-3 rounded-lg bg-muted border border-border">
          <div className="flex items-start gap-2 text-sm text-foreground">
            <XCircle className="h-4 w-4 mt-0.5 flex-shrink-0 text-destructive" />
            <div className="flex-1">
              <div className="font-medium mb-1 text-destructive">Password Required</div>
              <div className="text-muted-foreground mb-2">
                Unable to decrypt saved password. This usually happens when the session secret has changed.
              </div>
              {showEditButton && onEdit && (
                <Button
                  onClick={onEdit}
                  size="sm"
                  variant="outline"
                >
                  <Edit className="mr-2 h-3 w-3" />
                  Re-enter Password
                </Button>
              )}
            </div>
          </div>
        </div>
      )}

      {instance.connectionError && !(instance.hasDecryptionError && isDecryptionError(instance.connectionError)) && (
        <div className="mt-4 p-3 rounded-lg bg-destructive/10 border border-destructive/20">
          <div className="flex items-start gap-2 text-sm text-destructive">
            <XCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
            <div className="flex-1">
              <div className="font-medium mb-1">Connection Error</div>
              <div className="text-destructive/90">
                {formatErrorMessage(instance.connectionError)}
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  )
}