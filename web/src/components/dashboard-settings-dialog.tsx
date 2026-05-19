/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChevronDown, ChevronUp, Settings } from "lucide-react"
import { useEffect, useState } from "react"

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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { DEFAULT_DASHBOARD_SETTINGS, useDashboardSettings, useUpdateDashboardSettings } from "@/hooks/useDashboardSettings"

const SECTION_LABELS: Record<string, string> = {
  "server-stats": "Server Statistics",
  "tracker-breakdown": "Tracker Breakdown",
  "global-stats": "Global Stats Cards",
  "instances": "Instance Cards",
}

const SORT_COLUMN_LABELS: Record<string, string> = {
  "tracker": "Tracker Name",
  "uploaded": "Uploaded",
  "downloaded": "Downloaded",
  "ratio": "Ratio",
  "buffer": "Buffer",
  "count": "Torrents",
  "size": "Size",
  "performance": "Seeded",
}

export function DashboardSettingsDialog() {
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
  const [sortColumn, setSortColumn] = useState(
    () => settings?.trackerBreakdownSortColumn || "uploaded"
  )
  const [sortDirection, setSortDirection] = useState(
    () => settings?.trackerBreakdownSortDirection || "desc"
  )
  const [itemsPerPage, setItemsPerPage] = useState(
    () => settings?.trackerBreakdownItemsPerPage || 15
  )

  // Sync local state only when dialog opens (not on every settings change to avoid overwriting user edits)
  useEffect(() => {
    if (open && settings) {
      setVisibility(settings.sectionVisibility || DEFAULT_DASHBOARD_SETTINGS.sectionVisibility)
      setOrder(settings.sectionOrder || DEFAULT_DASHBOARD_SETTINGS.sectionOrder)
      setSortColumn(settings.trackerBreakdownSortColumn || "uploaded")
      setSortDirection(settings.trackerBreakdownSortDirection || "desc")
      setItemsPerPage(settings.trackerBreakdownItemsPerPage || 15)
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

  const handleSortColumnChange = (value: string) => {
    setSortColumn(value)
    updateSettings.mutate({ trackerBreakdownSortColumn: value })
  }

  const handleSortDirectionChange = (value: string) => {
    setSortDirection(value)
    updateSettings.mutate({ trackerBreakdownSortDirection: value })
  }

  const handleItemsPerPageChange = (value: string) => {
    const num = parseInt(value, 10)
    setItemsPerPage(num)
    updateSettings.mutate({ trackerBreakdownItemsPerPage: num })
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" className="w-full sm:w-auto">
          <Settings className="h-4 w-4 mr-2" />
          Layout Settings
        </Button>
      </DialogTrigger>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Dashboard Settings</DialogTitle>
          <DialogDescription>
            Customize which sections are visible and their order.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {/* Section Visibility & Order */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">Sections</Label>
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
                    {SECTION_LABELS[sectionId] || sectionId}
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

          <Separator />

          {/* Tracker Breakdown Settings */}
          <div className="space-y-4">
            <Label className="text-sm font-medium">Tracker Breakdown Defaults</Label>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="sort-column" className="text-xs text-muted-foreground">
                  Default Sort
                </Label>
                <Select value={sortColumn} onValueChange={handleSortColumnChange}>
                  <SelectTrigger id="sort-column">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {Object.entries(SORT_COLUMN_LABELS).map(([value, label]) => (
                      <SelectItem key={value} value={value}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label htmlFor="sort-direction" className="text-xs text-muted-foreground">
                  Direction
                </Label>
                <Select value={sortDirection} onValueChange={handleSortDirectionChange}>
                  <SelectTrigger id="sort-direction">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="desc">Descending</SelectItem>
                    <SelectItem value="asc">Ascending</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="items-per-page" className="text-xs text-muted-foreground">
                Items Per Page
              </Label>
              <Select value={String(itemsPerPage)} onValueChange={handleItemsPerPageChange}>
                <SelectTrigger id="items-per-page" className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="10">10</SelectItem>
                  <SelectItem value="15">15</SelectItem>
                  <SelectItem value="25">25</SelectItem>
                  <SelectItem value="50">50</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
