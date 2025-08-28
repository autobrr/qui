/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { InstancePreferencesDialog } from "./preferences/InstancePreferencesDialog"
import { MoreVertical, Cog } from "lucide-react"

interface InstanceActionsDropdownProps {
  instanceId: number
  instanceName: string
  onClick?: (e: React.MouseEvent) => void
}

export function InstanceActionsDropdown({
  instanceId,
  instanceName,
  onClick,
}: InstanceActionsDropdownProps) {
  const [preferencesOpen, setPreferencesOpen] = useState(false)

  const handleMenuItemClick = (e: React.MouseEvent, action: () => void) => {
    e.preventDefault()
    e.stopPropagation()
    action()
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 p-0"
            onClick={onClick}
            title="Instance Settings"
          >
            <MoreVertical className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={(e) => handleMenuItemClick(e, () => setPreferencesOpen(true))}
            className="flex items-center gap-2"
          >
            <Cog className="h-4 w-4" />
            Preferences
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <InstancePreferencesDialog
        open={preferencesOpen}
        onOpenChange={setPreferencesOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />
    </>
  )
}