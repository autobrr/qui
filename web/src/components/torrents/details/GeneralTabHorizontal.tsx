/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import { useTranslation } from "react-i18next"
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
  const { t } = useTranslation("torrents")
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
    const totalFromProperties = properties?.pieces_num || 0
    const haveFromProperties = properties?.pieces_have || 0
    const isCompleteFromProperties = (properties?.completion_date != null && properties.completion_date !== -1)
      || (totalFromProperties > 0 && haveFromProperties >= totalFromProperties)

    // When we don't need piece states anymore, prefer properties (react-query can keep stale pieceStates cached).
    if (!needsPieceStates) {
      const total = totalFromProperties
      const have = isCompleteFromProperties ? total : haveFromProperties
      const progress = total > 0 ? (have / total) * 100 : (isCompleteFromProperties ? 100 : 0)
      return { have, total, progress: Math.min(100, progress) }
    }

    if (pieceStates && pieceStates.length > 0) {
      const total = pieceStates.length
      const have = pieceStates.filter(state => state === 2).length
      const progress = (have / total) * 100
      return { have, total, progress }
    }
    // For completed torrents (no piece states fetched), use properties
    const total = totalFromProperties
    const have = haveFromProperties
    const progress = total > 0 ? (have / total) * 100 : 0
    return { have, total, progress }
  }, [needsPieceStates, pieceStates, properties?.completion_date, properties?.pieces_have, properties?.pieces_num])

  const copyToClipboard = async (text: string, label: string) => {
    try {
      await copyTextToClipboard(text)
      toast.success(t("detailsPanel.toast.copied", { type: label }))
    } catch {
      toast.error(t("detailsPanel.toast.copyFailed"))
    }
  }

  const downloadLimitLabel = downloadLimit > 0? formatSpeedWithUnit(downloadLimit, speedUnit): t("generalTab.unlimited")
  const uploadLimitLabel = uploadLimit > 0? formatSpeedWithUnit(uploadLimit, speedUnit): t("generalTab.unlimited")
  const pieceSizeLabel = properties?.piece_size? formatBytes(properties.piece_size): "—"

  const formatTimeLimit = (minutes: number | undefined): string => {
    if (minutes === undefined || minutes === -1) return t("generalTab.unlimited")
    if (minutes === -2) return t("generalTab.useGlobal")
    return formatDuration((minutes || 0) * 60)
  }

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
        {t("generalTab.noDataAvailable")}
      </div>
    )
  }

  return (
    <ScrollArea className="h-full">
      <div className="p-3">
        {/* Row 1: Name + Size */}
        <div className="grid grid-cols-2 gap-6 h-5">
          <div className="flex items-start gap-2 min-w-0">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
              {t("generalTab.name")}
            </span>
            <TruncatedText className="text-xs font-mono text-muted-foreground">
              {displayName || t("generalTab.na")}
            </TruncatedText>
            {displayName && (
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayName, t("generalTab.name"))}
              >
                <Copy className="h-4 w-4" />
              </Button>
            )}
          </div>
          <div className="flex items-start gap-2 min-w-0">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
              {t("generalTab.size")}
            </span>
            <span className="text-xs text-muted-foreground">
              {formatBytes(properties.total_size || torrent.size)}
            </span>
          </div>
        </div>

        {/* Row 2: Hash v1 + Hash v2 */}
        <div className="grid grid-cols-2 gap-6 h-5">
          <div className="flex items-start gap-2 min-w-0">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
              {t("generalTab.hashV1")}
            </span>
            <TruncatedText className="text-xs font-mono text-muted-foreground">
              {displayInfohashV1 || t("generalTab.na")}
            </TruncatedText>
            {displayInfohashV1 && (
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayInfohashV1, t("generalTab.hashV1"))}
              >
                <Copy className="h-4 w-4" />
              </Button>
            )}
          </div>
          {displayInfohashV2 && (
            <div className="flex items-start gap-2 min-w-0">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                {t("generalTab.hashV2")}
              </span>
              <TruncatedText className="text-xs font-mono text-muted-foreground">
                {displayInfohashV2}
              </TruncatedText>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayInfohashV2, t("generalTab.hashV2"))}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          )}
        </div>

        {/* Row 3: Save Path + Temp Path (if enabled) */}
        <div className="grid grid-cols-2 gap-6 h-5">
          <div className="flex items-start gap-2 min-w-0">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
              {t("generalTab.savePath")}
            </span>
            <TruncatedText className="text-xs font-mono text-muted-foreground">
              {displaySavePath || t("generalTab.na")}
            </TruncatedText>
            {displaySavePath && (
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displaySavePath, t("generalTab.savePath"))}
              >
                <Copy className="h-4 w-4" />
              </Button>
            )}
          </div>
          {tempPathEnabled && displayTempPath ? (
            <div className="flex items-start gap-2 min-w-0">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                {t("generalTab.tempPath")}
              </span>
              <TruncatedText className="text-xs font-mono text-muted-foreground">
                {displayTempPath}
              </TruncatedText>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => copyToClipboard(displayTempPath, t("generalTab.tempPath"))}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          ) : null}
        </div>


        {/* Row 4: Comment & Created By */}
        {(displayComment) && (
          <div className="grid grid-cols-2 gap-6">
            {displayComment && (
              <div className="flex items-start gap-2 min-w-0">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                  {t("generalTab.comment")}
                </span>
                <span className="min-w-0 flex-1 font-mono text-xs text-muted-foreground whitespace-pre-wrap break-words">
                  {renderTextWithLinks(displayComment)}
                </span>
              </div>
            )}
            {displayCreatedBy && (
              <div className="flex items-start gap-2 min-w-0">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground shrink-0 whitespace-nowrap">
                  {t("generalTab.createdBy")}
                </span>
                <span className="min-w-0 flex-1 text-xs text-muted-foreground truncate" title={displayCreatedBy}>
                  {renderTextWithLinks(displayCreatedBy)}
                </span>
              </div>
            )}
          </div>
        )}

        <Separator className="opacity-30 mt-2" />

        {/* Row 5: Transfer Stats + Network + Time + Limits */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-x-6 gap-y-6 lg:gap-y-0 m-0 mt-2">
          {/* Transfer Stats */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">{t("generalTab.transfer")}</h4>
            <StatRow label={t("generalTab.downloaded")} value={formatBytes(properties.total_downloaded || 0)} />
            <StatRow label={t("generalTab.uploaded")} value={formatBytes(properties.total_uploaded || 0)} />
            <StatRow
              label={t("generalTab.ratio")}
              value={(properties.share_ratio || 0).toFixed(2)}
              valueStyle={{ color: getRatioColor(properties.share_ratio || 0) }}
            />
            <StatRow label={t("generalTab.wasted")} value={formatBytes(properties.total_wasted || 0)} />
            {torrent.seq_dl && <StatRow label={t("generalTab.sequentialDownload")} value={t("generalTab.enabled")} />}
          </div>

          {/* Network */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">{t("generalTab.network")}</h4>
            <StatRow
              label="DL"
              value={`${formatSpeedWithUnit(properties.dl_speed || 0, speedUnit)} - (${formatSpeedWithUnit(properties.dl_speed_avg || 0, speedUnit)} avg.)`}
              highlight="green"
            />
            <StatRow
              label="UL"
              value={`${formatSpeedWithUnit(properties.up_speed || 0, speedUnit)} - (${formatSpeedWithUnit(properties.up_speed_avg || 0, speedUnit)} avg.)`}
              highlight="blue"
            />
            <StatRow
              label={t("generalTab.seeds")}
              value={`${properties.seeds || 0} / ${properties.seeds_total || 0}`}
            />
            <StatRow
              label={t("generalTab.peers")}
              value={`${properties.peers || 0} / ${properties.peers_total || 0}`}
            />
            <StatRow
              label={t("generalTab.pieces")}
              value={piecesStats.total > 0 ? `${piecesStats.have} / ${piecesStats.total}` : "—"}
            />
            <StatRow label={t("generalTab.pieceSize")} value={pieceSizeLabel} />
            {queueingEnabled && (
              <StatRow
                label={t("generalTab.queue")}
                value={torrent.priority > 0 ? String(torrent.priority) : "Normal"}
              />
            )}
          </div>

          {/* Time */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">{t("generalTab.time")}</h4>
            <StatRow label={t("generalTab.timeActive")} value={formatDuration(properties.time_elapsed || 0)} />
            <StatRow label={t("generalTab.seedingTime")} value={formatDuration(properties.seeding_time || 0)} />
            <StatRow label={t("generalTab.addedOn")} value={formatTimestamp(properties.addition_date, true)} />
            {properties.completion_date > 0 && (
              <StatRow label={t("generalTab.completedOn")} value={formatTimestamp(properties.completion_date, true)} />
            )}
            {properties.creation_date > 0 && (
              <StatRow label={t("generalTab.createdOn")} value={formatTimestamp(properties.creation_date, true)} />
            )}
          </div>

          {/* Limits */}
          <div className="space-y-1">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">{t("generalTab.limits")}</h4>
            <StatRow label={t("generalTab.ratioLimit")} value={torrent.max_ratio > 0 ? torrent.max_ratio.toFixed(2) : "∞"} />
            <StatRow label={t("generalTab.downLimit")} value={downloadLimitLabel} />
            <StatRow label={t("generalTab.upLimit")} value={uploadLimitLabel} />
            <StatRow
              label={t("generalTab.seedingTimeLimit")}
              value={formatTimeLimit(torrent.seeding_time_limit)}
            />
            {torrent.inactive_seeding_time_limit !== undefined && (
              <StatRow
                label={t("generalTab.inactiveSeedingLimit")}
                value={formatTimeLimit(torrent.inactive_seeding_time_limit)}
              />
            )}
          </div>
        </div>

        {/* Pieces visualization */}
        <Separator className="opacity-30 mt-2" />
        <div className="mt-2">
          <div className="flex items-center justify-between mb-1.5">
            <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              {t("generalTab.pieces")}
            </h4>
            <span className="text-xs text-muted-foreground tabular-nums">
              {piecesStats.total > 0? `${piecesStats.have} / ${piecesStats.total} (${piecesStats.progress.toFixed(1)}%)`: "—"}
            </span>
          </div>
          <PieceBar
            pieceStates={needsPieceStates ? pieceStates : undefined}
            isLoading={needsPieceStates ? loadingPieces : false}
            isComplete={!needsPieceStates && piecesStats.progress >= 100}
          />
        </div>
      </div>
    </ScrollArea>
  )
})
