/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { InstancePreferencesDialog } from "./preferences/InstancePreferencesDialog"
import { Cog } from "lucide-react"

interface InstanceSettingsButtonProps {
  instanceId: number
  instanceName: string
  onClick?: (e: React.MouseEvent) => void
}

export function InstanceSettingsButton({
  instanceId,
  instanceName,
  onClick,
}: InstanceSettingsButtonProps) {
  const [preferencesOpen, setPreferencesOpen] = useState(false)

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    onClick?.(e)
    setPreferencesOpen(true)
  }

  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6 p-0"
        onClick={handleClick}
        title="Instance Settings"
      >
        <Cog className="h-4 w-4" />
      </Button>

      <InstancePreferencesDialog
        open={preferencesOpen}
        onOpenChange={setPreferencesOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />
    </>
  )
}