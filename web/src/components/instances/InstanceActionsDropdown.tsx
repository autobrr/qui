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
import { SpeedLimitsDialog } from "./preferences/SpeedLimitsDialog"
import { QueueManagementDialog } from "./preferences/QueueManagementDialog"
import { FileManagementDialog } from "./preferences/FileManagementDialog"
import { SeedingLimitsDialog } from "./preferences/SeedingLimitsDialog"
import { ConnectionSettingsDialog } from "./preferences/ConnectionSettingsDialog"
import { MoreVertical, Download, Clock, Folder, Upload, Wifi } from "lucide-react"

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
  const [speedLimitsOpen, setSpeedLimitsOpen] = useState(false)
  const [queueManagementOpen, setQueueManagementOpen] = useState(false)
  const [fileManagementOpen, setFileManagementOpen] = useState(false)
  const [seedingLimitsOpen, setSeedingLimitsOpen] = useState(false)
  const [connectionSettingsOpen, setConnectionSettingsOpen] = useState(false)

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
            onClick={(e) => handleMenuItemClick(e, () => setConnectionSettingsOpen(true))}
            className="flex items-center gap-2"
          >
            <Wifi className="h-4 w-4" />
            Connection Settings
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={(e) => handleMenuItemClick(e, () => setSpeedLimitsOpen(true))}
            className="flex items-center gap-2"
          >
            <Download className="h-4 w-4" />
            Speed Limits
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={(e) => handleMenuItemClick(e, () => setQueueManagementOpen(true))}
            className="flex items-center gap-2"
          >
            <Clock className="h-4 w-4" />
            Queue Management
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={(e) => handleMenuItemClick(e, () => setFileManagementOpen(true))}
            className="flex items-center gap-2"
          >
            <Folder className="h-4 w-4" />
            File Management
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={(e) => handleMenuItemClick(e, () => setSeedingLimitsOpen(true))}
            className="flex items-center gap-2"
          >
            <Upload className="h-4 w-4" />
            Seeding Limits
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <SpeedLimitsDialog
        open={speedLimitsOpen}
        onOpenChange={setSpeedLimitsOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />

      <QueueManagementDialog
        open={queueManagementOpen}
        onOpenChange={setQueueManagementOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />

      <FileManagementDialog
        open={fileManagementOpen}
        onOpenChange={setFileManagementOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />

      <SeedingLimitsDialog
        open={seedingLimitsOpen}
        onOpenChange={setSeedingLimitsOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />

      <ConnectionSettingsDialog
        open={connectionSettingsOpen}
        onOpenChange={setConnectionSettingsOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />
    </>
  )
}