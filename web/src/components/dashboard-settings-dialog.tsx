/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChevronDown, ChevronUp, Settings } from "lucide-react"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { DEFAULT_DASHBOARD_SETTINGS, useDashboardSettings, useUpdateDashboardSettings } from "@/hooks/useDashboardSettings"

const SECTION_IDS = ["server-stats", "global-stats", "instances"] as const

export function DashboardSettingsDialog({ iconOnly }: { iconOnly?: boolean }) {
  const { t } = useTranslation("dashboard")
  const { data: settings } = useDashboardSettings()
  const updateSettings = useUpdateDashboardSettings()

  const [open, setOpen] = useState(false)

  // Local state for editing - initialize from settings or defaults
  const [visibility, setVisibility] = useState<Record<string, boolean>>(
    () => settings?.sectionVisibility || DEFAULT_DASHBOARD_SETTINGS.sectionVisibility
  )
  const [order, setOrder] = useState<string[]>(
    () => settings?.sectionOrder || DEFAULT_DASHBOARD_SETTINGS.sectionOrder
  )

  // Sync local state only when dialog opens (not on every settings change to avoid overwriting user edits)
  useEffect(() => {
    if (open && settings) {
      setVisibility(settings.sectionVisibility || DEFAULT_DASHBOARD_SETTINGS.sectionVisibility)
      setOrder(settings.sectionOrder || DEFAULT_DASHBOARD_SETTINGS.sectionOrder)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const handleVisibilityChange = (sectionId: string, checked: boolean) => {
    const newVisibility = { ...visibility, [sectionId]: checked }
    setVisibility(newVisibility)
    updateSettings.mutate({ sectionVisibility: newVisibility })
  }

  const handleMoveUp = (index: number) => {
    if (index === 0) return
    const newOrder = [...order]
    ;[newOrder[index - 1], newOrder[index]] = [newOrder[index], newOrder[index - 1]]
    setOrder(newOrder)
    updateSettings.mutate({ sectionOrder: newOrder })
  }

  const handleMoveDown = (index: number) => {
    if (index === order.length - 1) return
    const newOrder = [...order]
    ;[newOrder[index], newOrder[index + 1]] = [newOrder[index + 1], newOrder[index]]
    setOrder(newOrder)
    updateSettings.mutate({ sectionOrder: newOrder })
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {iconOnly ? (
          <Button variant="outline" size="icon" className="size-11" aria-label={t("settingsDialog.button")}>
            <Settings className="h-4 w-4" />
          </Button>
        ) : (
          <Button variant="outline" size="sm">
            <Settings className="h-4 w-4 mr-2" />
            {t("settingsDialog.button")}
          </Button>
        )}
      </DialogTrigger>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("settingsDialog.title")}</DialogTitle>
          <DialogDescription>
            {t("settingsDialog.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {/* Section Visibility & Order */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">{t("settingsDialog.sections")}</Label>
            <div className="space-y-2">
              {order.map((sectionId, index) => (
                <div
                  key={sectionId}
                  className="flex items-center gap-3 p-2 rounded-md border bg-background"
                >
                  <Checkbox
                    id={`section-${sectionId}`}
                    checked={visibility[sectionId] !== false}
                    onCheckedChange={(checked) => handleVisibilityChange(sectionId, Boolean(checked))}
                  />
                  <Label
                    htmlFor={`section-${sectionId}`}
                    className="flex-1 text-sm cursor-pointer"
                  >
                    {SECTION_IDS.includes(sectionId as (typeof SECTION_IDS)[number])? t(`settingsDialog.sectionLabels.${sectionId}`): sectionId}
                  </Label>
                  <div className="flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0"
                      onClick={() => handleMoveUp(index)}
                      disabled={index === 0}
                    >
                      <ChevronUp className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0"
                      onClick={() => handleMoveDown(index)}
                      disabled={index === order.length - 1}
                    >
                      <ChevronDown className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
