/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { XCircle, Edit } from "lucide-react"
import type { InstanceResponse } from "@/types"
import { formatErrorMessage } from "@/lib/utils"

interface InstanceErrorDisplayProps {
  instance: InstanceResponse
  onEdit?: () => void
  showEditButton?: boolean
}

export function InstanceErrorDisplay({ instance, onEdit, showEditButton = false }: InstanceErrorDisplayProps) {
  // Helper to check if connection error is decryption-related
  const isDecryptionError = (error: string) => {
    const errorLower = error.toLowerCase()
    return errorLower.includes("decrypt") &&
           (errorLower.includes("password") || errorLower.includes("cipher"))
  }

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