/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { renderTextWithLinks } from "@/lib/linkUtils"
import { formatSpeedWithUnit, type SpeedUnit } from "@/lib/speedUnits"
import { copyTextToClipboard, formatBytes, formatDuration, getRatioColor } from "@/lib/utils"
import type { Torrent, TorrentProperties } from "@/types"
import { Copy, Loader2 } from "lucide-react"
import { memo } from "react"
import { toast } from "sonner"
import { StatRow } from "./StatRow"

interface GeneralTabHorizontalProps {
  torrent: Torrent
  properties: TorrentProperties | undefined
  loading: boolean
  speedUnit: SpeedUnit
  downloadLimit: number
  uploadLimit: number
  displayName?: string
  displaySavePath: string
  displayTempPath?: string
  tempPathEnabled: boolean
  displayInfohashV1: string
  displayInfohashV2?: string
  displayComment?: string
  displayCreatedBy?: string
  queueingEnabled?: boolean
  maxActiveDownloads?: number
  maxActiveUploads?: number
  maxActiveTorrents?: number
}

export const GeneralTabHorizontal = memo(function GeneralTabHorizontal({
  torrent,
  properties,
  loading,
  speedUnit,
  downloadLimit,
  uploadLimit,
  displayName,
  displaySavePath,
  displayTempPath,
  tempPathEnabled,
  displayInfohashV1,
  displayInfohashV2,
  displayComment,
  displayCreatedBy,
  queueingEnabled,
  maxActiveDownloads,
  maxActiveUploads,
  maxActiveTorrents,
}: GeneralTabHorizontalProps) {
  const { formatTimestamp } = useDateTimeFormatters()

  const copyToClipboard = async (text: string, label: string) => {
    try {
      await copyTextToClipboard(text)
      toast.success(`${label} copied to clipboard`)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }

  const downloadLimitLabel = downloadLimit > 0
    ? formatSpeedWithUnit(downloadLimit, speedUnit)
    : "Unlimited"
  const uploadLimitLabel = uploadLimit > 0
    ? formatSpeedWithUnit(uploadLimit, speedUnit)
    : "Unlimited"

  if (loading && !properties) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin" />
      </div>
    )
  }

  if (!properties) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No data available
      </div>
    )
  }

  return (
    <ScrollArea className="h-full">
      <div className="p-3">
        {/* Row 1: Name + Save Path */}
        <div className="flex gap-6 h-5">
          {displayName && (
            <div className="flex items-center gap-2 flex-1">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 w-20">
                Name:
              </span>
              <code className="text-xs font-mono text-muted-foreground truncate" title={displayName}>
                {displayName}
              </code>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayName, "Torrent name")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          )}
          <div className="flex items-center gap-2 flex-1">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 w-20">
              Save Path:
            </span>
            <code className="text-xs font-mono text-muted-foreground truncate">
              {displaySavePath || "N/A"}
            </code>
            {displaySavePath && (
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displaySavePath, "Save path")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            )}
          </div>
        </div>

        {/* Row 2: Temp Path (if enabled) */}
        {tempPathEnabled && displayTempPath && (
          <div className="flex gap-6 h-5">
            {displayName && <div className="flex-1" />}
            <div className="flex items-center gap-2 flex-1">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 w-20">
                Temp Path:
              </span>
              <code className="text-xs font-mono text-muted-foreground truncate">
                {displayTempPath}
              </code>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayTempPath, "Temp path")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          </div>
        )}

        {/* Row 3: Hashes */}
        <div className="flex gap-6 h-5">
          <div className="flex items-center gap-2 flex-1">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 w-20">
              Hash v1:
            </span>
            <code className="text-xs font-mono text-muted-foreground truncate">
              {displayInfohashV1 || "N/A"}
            </code>
            {displayInfohashV1 && (
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayInfohashV1, "Info Hash v1")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            )}
          </div>
          {displayInfohashV2 && (
            <div className="flex items-center gap-2 flex-1">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 w-20">
                Hash v2:
              </span>
              <code className="text-xs font-mono text-muted-foreground truncate">
                {displayInfohashV2}
              </code>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayInfohashV2, "Info Hash v2")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          )}
        </div>


        {/* Row 4: Additional Info (if present) */}
        {(displayComment || displayCreatedBy) && (
          <div className="flex gap-6 h-5">
            {displayCreatedBy && (
              <div className="flex items-center gap-2 flex-1">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 w-20 whitespace-nowrap">
                  Created By:
                </span>
                <span className="text-xs text-muted-foreground truncate" title={displayCreatedBy}>
                  {renderTextWithLinks(displayCreatedBy)}
                </span>
              </div>
            )}
            {displayComment && (
              <div className="flex items-center gap-2 flex-1">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 w-20">
                  Comment:
                </span>
                <span className="text-xs text-muted-foreground truncate" title={displayComment}>
                  {renderTextWithLinks(displayComment)}
                </span>
              </div>
            )}
          </div>
        )}

        <Separator className="opacity-30 mt-2" />

        {/* Row 5: Transfer Stats + Speed + Network + Time */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-x-6 m-0 mt-2">
          {/* Transfer Stats */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">Transfer</h4>
            <StatRow label="Size" value={formatBytes(properties.total_size || torrent.size)} />
            <StatRow label="Downloaded" value={formatBytes(properties.total_downloaded || 0)} />
            <StatRow label="Uploaded" value={formatBytes(properties.total_uploaded || 0)} />
            <StatRow
              label="Ratio"
              value={(properties.share_ratio || 0).toFixed(2)}
              valueStyle={{ color: getRatioColor(properties.share_ratio || 0) }}
            />
            <StatRow label="Wasted" value={formatBytes(properties.total_wasted || 0)} />
            {torrent.seq_dl && <StatRow label="Sequential Download" value="Enabled" />}
          </div>

          {/* Speed */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">Speed</h4>
            <StatRow
              label="DL"
              value={formatSpeedWithUnit(properties.dl_speed || 0, speedUnit)}
              highlight="green"
            />
            <StatRow
              label="UL"
              value={formatSpeedWithUnit(properties.up_speed || 0, speedUnit)}
              highlight="blue"
            />
            <StatRow
              label="DL Avg"
              value={formatSpeedWithUnit(properties.dl_speed_avg || 0, speedUnit)}
            />
            <StatRow
              label="UL Avg"
              value={formatSpeedWithUnit(properties.up_speed_avg || 0, speedUnit)}
            />
            <StatRow label="DL Limit" value={downloadLimitLabel} />
            <StatRow label="UL Limit" value={uploadLimitLabel} />
          </div>

          {/* Network */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">Network</h4>
            <StatRow
              label="Seeds"
              value={`${properties.seeds || 0} / ${properties.seeds_total || 0}`}
            />
            <StatRow
              label="Peers"
              value={`${properties.peers || 0} / ${properties.peers_total || 0}`}
            />
            <StatRow
              label="Pieces"
              value={`${properties.pieces_have || 0} / ${properties.pieces_num || 0} (${formatBytes(properties.piece_size || 0)} each)`}
            />
            {queueingEnabled && (
              <StatRow
                label="Priority"
                value={torrent.priority > 0 ? String(torrent.priority) : "Normal"}
              />
            )}
          </div>

          {/* Time */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">Time</h4>
            <StatRow label="Active" value={formatDuration(properties.time_elapsed || 0)} />
            <StatRow label="Seeding" value={formatDuration(properties.seeding_time || 0)} />
            <StatRow label="Added" value={formatTimestamp(properties.addition_date)} />
            <StatRow label="Completed" value={formatTimestamp(properties.completion_date)} />
            <StatRow label="Created" value={formatTimestamp(properties.creation_date)} />
          </div>
        </div>

        {/* Queue Management (if enabled) */}
        {queueingEnabled && (maxActiveDownloads || maxActiveUploads || maxActiveTorrents) && (
          <>
            <Separator className="opacity-30" />
            <div className="flex items-center gap-4 text-xs">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                Queue Limits:
              </span>
              {maxActiveDownloads !== undefined && maxActiveDownloads > 0 && (
                <StatRow label="Max DL" value={String(maxActiveDownloads)} />
              )}
              {maxActiveUploads !== undefined && maxActiveUploads > 0 && (
                <StatRow label="Max UL" value={String(maxActiveUploads)} />
              )}
              {maxActiveTorrents !== undefined && maxActiveTorrents > 0 && (
                <StatRow label="Max Active" value={String(maxActiveTorrents)} />
              )}
            </div>
          </>
        )}
      </div>
    </ScrollArea>
  )
})
