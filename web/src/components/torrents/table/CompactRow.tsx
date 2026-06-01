/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { getLinuxCategory, getLinuxIsoName, getLinuxRatio, getLinuxTags, getLinuxTracker } from "@/lib/incognito"
import { formatSpeedWithUnit } from "@/lib/speedUnits"
import { getRowBackgroundClass, getStatusBadgeProps } from "@/lib/torrent-table/row-display"
import { extractTrackerHost, resolveTrackerDisplay, type TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { resolveTrackerIconSrc } from "@/lib/tracker-icons"
import { cn, formatBytes, getRatioColor } from "@/lib/utils"
import type { Torrent } from "@/types"
import { Folder, Tag } from "lucide-react"
import { memo, useMemo } from "react"
import { useTranslation } from "react-i18next"
import { TrackerIcon } from "./TrackerIcon"

// Compact row component for desktop
interface CompactRowProps {
  torrent: Torrent
  rowId: string
  rowIndex: number
  isSelected: boolean
  isRowSelected: boolean
  showCheckbox: boolean
  onClick: (e: React.MouseEvent) => void
  onContextMenu: () => void
  onCheckboxPointerDown: (event: React.PointerEvent<HTMLDivElement>) => void
  onCheckboxChange: (torrent: Torrent, rowId: string, checked: boolean) => void
  incognitoMode: boolean
  speedUnit: "bytes" | "bits"
  supportsTrackerHealth: boolean
  trackerIcons?: Record<string, string>
  trackerCustomizationLookup: TrackerCustomizationLookup
  style: React.CSSProperties
}

export const CompactRow = memo(({
  torrent,
  rowId,
  rowIndex,
  isSelected,
  isRowSelected,
  showCheckbox,
  onClick,
  onContextMenu,
  onCheckboxPointerDown,
  onCheckboxChange,
  incognitoMode,
  speedUnit,
  supportsTrackerHealth,
  trackerIcons,
  trackerCustomizationLookup,
  style,
}: CompactRowProps) => {
  const { t } = useTranslation("torrents")
  const displayName = incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name
  const displayCategory = incognitoMode ? getLinuxCategory(torrent.hash) : torrent.category
  const displayTags = incognitoMode ? getLinuxTags(torrent.hash) : torrent.tags
  const displayRatio = incognitoMode ? getLinuxRatio(torrent.hash) : torrent.ratio

  const { variant: statusBadgeVariant, label: statusBadgeLabel, className: statusBadgeClass } = useMemo(
    () => getStatusBadgeProps(torrent, supportsTrackerHealth, t),
    [torrent, supportsTrackerHealth, t]
  )

  // Resolve tracker display name and icon using customizations
  const trackerRaw = incognitoMode ? getLinuxTracker(torrent.hash) : torrent.tracker
  const trackerHost = useMemo(() => extractTrackerHost(trackerRaw), [trackerRaw])
  const trackerDisplayInfo = useMemo(
    () => resolveTrackerDisplay(trackerHost, trackerCustomizationLookup),
    [trackerHost, trackerCustomizationLookup]
  )
  const trackerLabel = trackerDisplayInfo.displayName || ""
  const trackerIconSrc = resolveTrackerIconSrc(trackerIcons, trackerDisplayInfo.primaryDomain, trackerHost)
  const trackerTitle = trackerDisplayInfo.isCustomized ? `${trackerDisplayInfo.displayName} (${trackerHost})` : trackerHost

  // Compact view
  return (
    <div
      className={cn(
        "relative flex flex-col gap-1 px-3 py-2 cursor-pointer hover:bg-accent/40 overflow-hidden",
        getRowBackgroundClass(isRowSelected, isSelected, rowIndex)
      )}
      style={style}
      onClick={(e) => onClick(e)}
      onContextMenu={onContextMenu}
    >
      {/* Progress background overlay - only show when downloading */}
      {torrent.progress < 1 && (
        <div
          className="absolute inset-0 -z-10 bg-primary/10 transition-all duration-300"
          style={{
            width: `${Math.min(100, Math.max(0, torrent.progress * 100))}%`,
          }}
          aria-hidden="true"
        />
      )}
      {/* Name with progress inline */}
      <div className="flex items-center gap-2">
        {showCheckbox && (
          <div
            className="flex items-center justify-center flex-shrink-0"
            data-slot="checkbox"
            onPointerDown={onCheckboxPointerDown}
          >
            <Checkbox
              checked={isRowSelected}
              onCheckedChange={(checked) => onCheckboxChange(torrent, rowId, checked === true)}
              aria-label={t("tableColumns.selectAll")}
              className="h-4 w-4"
            />
          </div>
        )}
        <div className="flex items-center gap-1 flex-shrink-0" title={trackerTitle}>
          <TrackerIcon
            title={trackerTitle}
            fallback={trackerHost ? trackerHost.charAt(0).toUpperCase() : "?"}
            src={trackerIconSrc}
            size="sm"
          />
          {trackerLabel && (
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {trackerLabel}
            </span>
          )}
        </div>
        <h3 className="flex-1 font-medium text-sm truncate min-w-0" title={displayName}>
          {displayName}
        </h3>
        <Badge variant={statusBadgeVariant} className={cn("text-xs flex-shrink-0", statusBadgeClass)}>
          {statusBadgeLabel}
        </Badge>
      </div>

      {/* Downloaded/Size and Ratio */}
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">
          {formatBytes(torrent.downloaded)} / {formatBytes(torrent.size)}
        </span>
        <div className="flex items-center gap-1">
          <span className="text-muted-foreground">{t("mobileCards.ratio")}</span>
          <span
            className="font-medium"
            style={{ color: getRatioColor(displayRatio) }}
          >
            {displayRatio === -1 ? "∞" : displayRatio.toFixed(2)}
          </span>
        </div>
      </div>

      {/* Bottom row: Category/tags and percentage/speeds */}
      <div className="flex items-center justify-between gap-2 text-xs">
        {/* Left side: Category and Tags */}
        <div className="flex items-center gap-2 text-muted-foreground min-w-0 overflow-hidden">
          {displayCategory && (
            <span className="flex items-center gap-1 flex-shrink-0">
              <Folder className="h-3 w-3" />
              {displayCategory}
            </span>
          )}
          {displayTags && (
            <div className="flex items-center gap-1 min-w-0 overflow-hidden">
              <Tag className="h-3 w-3 flex-shrink-0" />
              <span className="truncate">
                {Array.isArray(displayTags) ? displayTags.join(", ") : displayTags}
              </span>
            </div>
          )}
        </div>

        {/* Right side: Percentage and Speeds */}
        <div className="flex items-center gap-2 flex-shrink-0">
          <span className="text-muted-foreground">
            {torrent.progress >= 0.99 && torrent.progress < 1 ? (
              (Math.floor(torrent.progress * 1000) / 10).toFixed(1)
            ) : (
              Math.round(torrent.progress * 100)
            )}%
          </span>
          {/* Speeds */}
          {(torrent.dlspeed > 0 || torrent.upspeed > 0) && (
            <div className="flex items-center gap-1">
              {torrent.dlspeed > 0 && (
                <span className="text-chart-2 font-medium">
                  ↓{formatSpeedWithUnit(torrent.dlspeed, speedUnit)}
                </span>
              )}
              {torrent.upspeed > 0 && (
                <span className="text-chart-3 font-medium">
                  ↑{formatSpeedWithUnit(torrent.upspeed, speedUnit)}
                </span>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}, (prev, next) =>
  // Handler props (onClick/onContextMenu/onCheckbox*) are intentionally excluded:
  // the parent passes fresh inline lambdas every render, so comparing them would
  // defeat row memoization (every row would re-render on every poll/stream tick).
  // The state those handlers act on is reflected in the compared props below.
  prev.torrent.hash === next.torrent.hash &&
  prev.rowId === next.rowId &&
  prev.rowIndex === next.rowIndex &&
  prev.torrent.name === next.torrent.name &&
  prev.torrent.category === next.torrent.category &&
  prev.torrent.tags === next.torrent.tags &&
  prev.torrent.tracker === next.torrent.tracker &&
  prev.torrent.tracker_health === next.torrent.tracker_health &&
  prev.torrent.state === next.torrent.state &&
  prev.torrent.progress === next.torrent.progress &&
  prev.torrent.dlspeed === next.torrent.dlspeed &&
  prev.torrent.upspeed === next.torrent.upspeed &&
  prev.torrent.downloaded === next.torrent.downloaded &&
  prev.torrent.size === next.torrent.size &&
  prev.torrent.ratio === next.torrent.ratio &&
  prev.isSelected === next.isSelected &&
  prev.isRowSelected === next.isRowSelected &&
  prev.showCheckbox === next.showCheckbox &&
  prev.incognitoMode === next.incognitoMode &&
  prev.speedUnit === next.speedUnit &&
  prev.supportsTrackerHealth === next.supportsTrackerHealth &&
  prev.trackerIcons === next.trackerIcons &&
  prev.trackerCustomizationLookup === next.trackerCustomizationLookup &&
  prev.style === next.style
)
