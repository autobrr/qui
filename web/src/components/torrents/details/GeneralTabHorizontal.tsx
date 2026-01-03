/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import { renderTextWithLinks } from "@/lib/linkUtils"
import { formatSpeedWithUnit, type SpeedUnit } from "@/lib/speedUnits"
import { copyTextToClipboard, formatBytes, formatDuration, getRatioColor } from "@/lib/utils"
import type { Torrent, TorrentProperties } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Copy, Loader2 } from "lucide-react"
import { memo, useMemo } from "react"
import { toast } from "sonner"
import { PieceBar } from "./PieceBar"
import { StatRow } from "./StatRow"

interface GeneralTabHorizontalProps {
  instanceId: number
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
}

export const GeneralTabHorizontal = memo(function GeneralTabHorizontal({
  instanceId,
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
}: GeneralTabHorizontalProps) {
  const { formatTimestamp } = useDateTimeFormatters()

  // Only fetch piece states when downloading or rechecking (not needed for completed torrents)
  const isDownloading = torrent.progress < 1
  const isChecking = torrent.state?.includes("checking") ?? false
  const needsPieceStates = isDownloading || isChecking

  const { data: pieceStates, isLoading: loadingPieces } = useQuery({
    queryKey: ["torrent-piece-states", instanceId, torrent.hash],
    queryFn: () => api.getTorrentPieceStates(instanceId, torrent.hash),
    enabled: instanceId != null && !!torrent.hash && needsPieceStates,
    staleTime: 3000,
    refetchInterval: needsPieceStates ? 5000 : false,
  })

  // Calculate pieces stats from actual piece states when available (more accurate than properties)
  const piecesStats = useMemo(() => {
    if (pieceStates && pieceStates.length > 0) {
      const total = pieceStates.length
      const have = pieceStates.filter(state => state === 2).length
      const progress = (have / total) * 100
      return { have, total, progress }
    }
    // For completed torrents (no piece states fetched), use properties
    const total = properties?.pieces_num || 0
    const have = properties?.pieces_have || 0
    const progress = total > 0 ? (have / total) * 100 : 0
    return { have, total, progress }
  }, [pieceStates, properties?.pieces_have, properties?.pieces_num])

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
        {/* Row 1: Name + Hash v1 */}
        <div className="grid grid-cols-2 gap-6 h-5">
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
              Name:
            </span>
            <TruncatedText className="text-xs font-mono text-muted-foreground">
              {displayName || "N/A"}
            </TruncatedText>
            {displayName && (
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayName, "Torrent name")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            )}
          </div>
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
              Hash v1:
            </span>
            <TruncatedText className="text-xs font-mono text-muted-foreground">
              {displayInfohashV1 || "N/A"}
            </TruncatedText>
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
        </div>

        {/* Row 2: Save Path + Hash v2 (or Created By if no v2) */}
        <div className="grid grid-cols-2 gap-6 h-5">
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
              Save Path:
            </span>
            <TruncatedText className="text-xs font-mono text-muted-foreground">
              {displaySavePath || "N/A"}
            </TruncatedText>
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
          {displayInfohashV2 ? (
            <div className="flex items-center gap-2 min-w-0">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                Hash v2:
              </span>
              <TruncatedText className="text-xs font-mono text-muted-foreground">
                {displayInfohashV2}
              </TruncatedText>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayInfohashV2, "Info Hash v2")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          ) : displayCreatedBy ? (
            <div className="flex items-center gap-2 min-w-0">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                Created By:
              </span>
              <span className="text-xs text-muted-foreground truncate" title={displayCreatedBy}>
                {renderTextWithLinks(displayCreatedBy)}
              </span>
            </div>
          ) : null}
        </div>

        {/* Row 3: Temp Path (if enabled) */}
        {tempPathEnabled && displayTempPath && (
          <div className="grid grid-cols-2 gap-6 h-5">
            <div className="flex items-center gap-2 min-w-0">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                Temp Path:
              </span>
              <TruncatedText className="text-xs font-mono text-muted-foreground">
                {displayTempPath}
              </TruncatedText>
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


        {/* Row 4: Additional Info (if present) - Created By only shows here if Hash v2 exists */}
        {(displayComment || (displayCreatedBy && displayInfohashV2)) && (
          <div className="grid grid-cols-2 gap-6 h-5">
            {displayComment && (
              <div className="flex items-center gap-2 min-w-0">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                  Comment:
                </span>
                <span className="text-xs text-muted-foreground truncate" title={displayComment}>
                  {renderTextWithLinks(displayComment)}
                </span>
              </div>
            )}
            {displayCreatedBy && displayInfohashV2 && (
              <div className="flex items-center gap-2 min-w-0">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                  Created By:
                </span>
                <span className="text-xs text-muted-foreground truncate" title={displayCreatedBy}>
                  {renderTextWithLinks(displayCreatedBy)}
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
              value={`${piecesStats.have} / ${piecesStats.total}`}
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
            {properties.completion_date && properties.completion_date !== -1 && (
              <StatRow label="Completed" value={formatTimestamp(properties.completion_date)} />
            )}
            {properties.creation_date && properties.creation_date !== -1 && (
              <StatRow label="Created" value={formatTimestamp(properties.creation_date)} />
            )}
          </div>
        </div>

        {/* Pieces visualization */}
        <Separator className="opacity-30 mt-2" />
        <div className="mt-2">
          <div className="flex items-center justify-between mb-1.5">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Pieces
            </h4>
            <span className="text-xs text-muted-foreground tabular-nums">
              {piecesStats.have} / {piecesStats.total} ({piecesStats.progress.toFixed(1)}%)
            </span>
          </div>
          <PieceBar pieceStates={pieceStates} isLoading={loadingPieces} isComplete={!needsPieceStates} />
        </div>
      </div>
    </ScrollArea>
  )
})
