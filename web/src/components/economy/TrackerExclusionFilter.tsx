/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import { Filter, X } from "lucide-react"
import { useMemo, useState } from "react"

interface TrackerExclusionFilterProps {
  availableTrackers: string[]
  excludedTrackers: string[]
  onExcludedTrackersChange: (trackers: string[]) => void
  disabled?: boolean
}

export function TrackerExclusionFilter({
  availableTrackers,
  excludedTrackers,
  onExcludedTrackersChange,
  disabled = false,
}: TrackerExclusionFilterProps) {
  const [search, setSearch] = useState("")

  const filteredTrackers = useMemo(() => {
    if (!search) return availableTrackers
    const searchLower = search.toLowerCase()
    return availableTrackers.filter((tracker) =>
      tracker.toLowerCase().includes(searchLower)
    )
  }, [availableTrackers, search])

  const handleToggleTracker = (tracker: string) => {
    const newExcluded = excludedTrackers.includes(tracker)
      ? excludedTrackers.filter((t) => t !== tracker)
      : [...excludedTrackers, tracker]
    onExcludedTrackersChange(newExcluded)
  }

  const handleClearAll = () => {
    onExcludedTrackersChange([])
  }

  const handleRemoveTracker = (tracker: string, e: React.MouseEvent) => {
    e.stopPropagation()
    onExcludedTrackersChange(excludedTrackers.filter((t) => t !== tracker))
  }

  return (
    <div className="flex items-center gap-2">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="outline"
            size="sm"
            className={cn("h-9 border-dashed", excludedTrackers.length > 0 && "border-primary")}
            disabled={disabled}
          >
            <Filter className="h-4 w-4 mr-2" />
            Exclude Trackers
            {excludedTrackers.length > 0 && (
              <>
                <div className="ml-2 h-4 w-px bg-border" />
                <Badge
                  variant="secondary"
                  className="ml-2 rounded-sm px-1 font-normal"
                >
                  {excludedTrackers.length}
                </Badge>
              </>
            )}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-[280px]" align="start">
          <div className="p-2 pb-0">
            <Input
              placeholder="Search trackers..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-8"
            />
          </div>
          {excludedTrackers.length > 0 && (
            <div className="px-2 py-1.5">
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs w-full"
                onClick={handleClearAll}
              >
                Clear all exclusions
              </Button>
            </div>
          )}
          <DropdownMenuSeparator />
          <ScrollArea className="h-[300px]">
            <div className="p-1">
              {filteredTrackers.length === 0 ? (
                <div className="py-6 text-center text-sm text-muted-foreground">
                  No trackers found
                </div>
              ) : (
                filteredTrackers.map((tracker) => (
                  <DropdownMenuCheckboxItem
                    key={tracker}
                    checked={excludedTrackers.includes(tracker)}
                    onCheckedChange={() => handleToggleTracker(tracker)}
                    className="cursor-pointer"
                  >
                    <span className="truncate">{tracker}</span>
                  </DropdownMenuCheckboxItem>
                ))
              )}
            </div>
          </ScrollArea>
        </DropdownMenuContent>
      </DropdownMenu>
      
      {excludedTrackers.length > 0 && (
        <div className="flex flex-wrap gap-1 max-w-md">
          {excludedTrackers.slice(0, 3).map((tracker) => (
            <Badge
              key={tracker}
              variant="secondary"
              className="pl-2 pr-1 text-xs"
            >
              {tracker}
              <button
                type="button"
                className="ml-1 rounded-full outline-none ring-offset-background focus:ring-2 focus:ring-ring focus:ring-offset-2"
                onClick={(e) => handleRemoveTracker(tracker, e)}
              >
                <X className="h-3 w-3 text-muted-foreground hover:text-foreground" />
              </button>
            </Badge>
          ))}
          {excludedTrackers.length > 3 && (
            <Badge variant="secondary" className="text-xs">
              +{excludedTrackers.length - 3} more
            </Badge>
          )}
        </div>
      )}
    </div>
  )
}
