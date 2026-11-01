/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// The parts of the torrent details view on the mobile layout.

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { TapMenu } from "@/components/ui/tap-menu"
import { getLinuxTracker } from "@/lib/incognito"
import { renderTextWithLinks } from "@/lib/linkUtils"
import { getTrackerStatusBadge, isValidTrackerUrl } from "@/lib/tracker-utils"
import { breakableName, cn, copyTextToClipboard } from "@/lib/utils"
import type { TorrentTracker } from "@/types"
import { ChevronLeft, Copy, Edit, Folder, Tag } from "lucide-react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

/**
 * The one layout of each mobile section: an optional fixed header, a single
 * scroll area, and an optional fixed footer. A section passes its title and
 * actions here, never inside its scroll area.
 */
export function MobilePane({ header, footer, children }: { header?: ReactNode; footer?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header && <div className="shrink-0 border-b px-4 py-3">{header}</div>}
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">{children}</div>
      {footer && <div className="shrink-0 border-t bg-background px-4 py-2">{footer}</div>}
    </div>
  )
}

export function MobileSectionTitle({ title, summary }: { title: string; summary?: ReactNode }) {
  return (
    <div className="min-w-0 space-y-0.5">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{title}</h3>
      {summary && <p className="text-xs text-muted-foreground">{summary}</p>}
    </div>
  )
}

export function MobileInfoGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="px-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">{title}</h3>
      <div className="divide-y rounded-lg border bg-card/50">{children}</div>
    </section>
  )
}

/** A label and a short value on one line. */
export function MobileInfoRow({ label, children, sub, valueClassName }: { label: string; children: ReactNode; sub?: ReactNode; valueClassName?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4 px-3 py-2">
      <span className="shrink-0 text-xs text-muted-foreground">{label}</span>
      <div className="min-w-0 text-right">
        <div className={cn("text-sm tabular-nums", valueClassName)}>{children}</div>
        {sub && <div className="text-xs text-muted-foreground tabular-nums">{sub}</div>}
      </div>
    </div>
  )
}

/** The label above the value, for names, hashes, and paths. */
export function MobileInfoBlock({ label, children, mono, copyValue }: { label: string; children: ReactNode; mono?: boolean; copyValue?: string }) {
  return (
    <div className="flex items-center gap-1 py-1.5 pl-3 pr-1">
      <div className="min-w-0 flex-1 space-y-0.5 py-0.5">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className={cn("break-all text-sm", mono && "font-mono")}>{children}</div>
      </div>
      {copyValue && <CopyButton value={copyValue} label={label} className="size-11" iconClassName="size-4" />}
    </div>
  )
}

interface HeaderProps {
  name: string
  status: { label: string; variant: "default" | "secondary" | "destructive" | "outline"; className: string }
  progress: number
  size: string
  ratio: number
  onBack?: () => void
}

export function MobileDetailsHeader({ name, status, progress, size, ratio, onBack }: HeaderProps) {
  const { t } = useTranslation("torrents")
  const percent = Math.floor(progress * 1000) / 10
  return (
    <div className="flex shrink-0 items-center gap-1 border-b bg-background py-2 pl-1 pr-4">
      {onBack && (
        <Button variant="ghost" size="icon" className="size-11 shrink-0" onClick={onBack} aria-label={t("detailsPanel.closePanel")}>
          <ChevronLeft className="size-6" />
        </Button>
      )}
      <div className="min-w-0 flex-1">
        <h2 className="line-clamp-2 break-words text-sm font-semibold leading-tight">{breakableName(name)}</h2>
        <div className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground tabular-nums">
          <Badge variant={status.variant} className={cn("h-5 shrink-0 px-1.5 text-xs", status.className)}>{status.label}</Badge>
          <span className="truncate">
            {percent}% · {size} · {t("generalTab.ratio")} {ratio < 0 ? "∞" : ratio.toFixed(2)}
          </span>
        </div>
      </div>
    </div>
  )
}

export function MobileCategoryTagsRow({ category, tags }: { category: string; tags: string }) {
  const tagList = tags.split(",").map(tag => tag.trim()).filter(Boolean)
  if (!category && tagList.length === 0) return null
  return (
    // About three rows of badges: many tags must leave room for the section
    <div className="flex max-h-24 shrink-0 flex-wrap gap-1.5 overflow-y-auto border-b px-4 py-2.5">
      {category && (
        <Badge variant="secondary" className="gap-1 text-xs">
          <Folder className="size-3" />{category}
        </Badge>
      )}
      {tagList.map(tag => (
        <Badge key={tag} variant="outline" className="gap-1 text-xs">
          <Tag className="size-3" />{tag}
        </Badge>
      ))}
    </div>
  )
}

// qBittorrent lists DHT, PeX, and LSD as trackers, first and often as working
const PSEUDO_TRACKER = /^\*\* \[(DHT|PeX|LSD)\] \*\*$/

function trackerHost(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

// Working first, disabled last, the others in their order
function trackerRank(status: number): number {
  return status === 2 ? 0 : status === 0 ? 2 : 1
}

interface MobileTrackerListProps {
  trackers: TorrentTracker[]
  torrentHash: string
  incognitoMode: boolean
  onEditTracker: (tracker: TorrentTracker) => void
  supportsTrackerEditing: boolean
}

export function MobileTrackerList({ trackers, torrentHash, incognitoMode, onEditTracker, supportsTrackerEditing }: MobileTrackerListProps) {
  const { t } = useTranslation("torrents")
  const indexed = trackers.map((tracker, index) => ({ tracker, index }))
  const real = indexed.filter(({ tracker }) => !PSEUDO_TRACKER.test(tracker.url)).sort((a, b) => trackerRank(a.tracker.status) - trackerRank(b.tracker.status))
  const pseudo = indexed.filter(({ tracker }) => PSEUDO_TRACKER.test(tracker.url))
  const message = (tracker: TorrentTracker) => incognitoMode && tracker.msg ? t("detailsPanel.trackerMessageHidden") : tracker.msg
  const pseudoMessage = pseudo.length > 0 ? message(pseudo[0].tracker) : ""

  return (
    <div className="space-y-3 p-4">
      {real.length > 0 && (
        <div className="divide-y rounded-lg border bg-card/50">
          {real.map(({ tracker, index }) => {
            const url = incognitoMode ? getLinuxTracker(`${torrentHash}-${index}`) : tracker.url
            const msg = message(tracker)
            const row = (
              <div className={cn("space-y-1.5 px-3 py-2.5", tracker.status === 0 && "opacity-60")}>
                <div className="flex items-center gap-2">
                  {getTrackerStatusBadge(tracker.status, true)}
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{trackerHost(url)}</span>
                </div>
                <p className="break-all font-mono text-xs text-muted-foreground">{url}</p>
                <div className="flex flex-wrap gap-x-3 text-xs text-muted-foreground tabular-nums">
                  <span>{t("trackersTable.seeds")} <span className="text-foreground">{tracker.num_seeds}</span></span>
                  <span>{t("detailsPanel.labels.leechers")} <span className="text-foreground">{tracker.num_leeches}</span></span>
                  <span>{t("generalTab.peers")} <span className="text-foreground">{tracker.num_peers}</span></span>
                  <span>{t("trackersTable.downloaded")} <span className="text-foreground">{tracker.num_downloaded}</span></span>
                </div>
                {msg && <p className={cn("break-words text-xs", tracker.status >= 4 ? "text-destructive" : "text-muted-foreground")}>{renderTextWithLinks(msg)}</p>}
              </div>
            )
            if (!isValidTrackerUrl(tracker.url)) return <div key={index}>{row}</div>
            return (
              <TapMenu
                key={index}
                content={
                  <DropdownMenuItem disabled={!supportsTrackerEditing} onClick={() => onEditTracker(tracker)}>
                    <Edit className="mr-2 size-4" />
                    {t("trackerContextMenu.editTrackerUrl")}
                  </DropdownMenuItem>
                }
              >
                {row}
              </TapMenu>
            )
          })}
        </div>
      )}
      {pseudo.length > 0 && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 px-1 text-xs text-muted-foreground">
          {pseudo.map(({ tracker, index }) => (
            <span key={index} className="flex items-center gap-1.5">
              {tracker.url.replace(/[*[\] ]/g, "")}
              {getTrackerStatusBadge(tracker.status, true)}
            </span>
          ))}
          {pseudoMessage && <span className="w-full">{pseudoMessage}</span>}
        </div>
      )}
    </div>
  )
}

interface MobilePeerRowProps {
  address: string
  countryCode?: string
  country?: string
  client: string
  connection?: string
  flags?: string
  progressPercent: number
  isSeeder: boolean
  downSpeed: string
  upSpeed: string
}

export function MobilePeerRow({ address, countryCode, country, client, connection, flags, progressPercent, isSeeder, downSpeed, upSpeed }: MobilePeerRowProps) {
  return (
    <div className="space-y-1 px-3 py-2.5">
      <div className="flex items-center gap-2">
        {countryCode && <span className={`fi fi-${countryCode.toLowerCase()} shrink-0 rounded text-sm`} title={country || countryCode} />}
        <span className="min-w-0 flex-1 truncate font-mono text-sm">{address}</span>
        <span className="shrink-0 text-xs tabular-nums">
          {downSpeed && <span className="text-green-500">↓ {downSpeed}</span>}
          {downSpeed && upSpeed && " "}
          {upSpeed && <span className="text-blue-500">↑ {upSpeed}</span>}
        </span>
      </div>
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="min-w-0 flex-1 truncate">{[client, connection, flags].filter(Boolean).join(" · ")}</span>
        <div className="h-1 w-12 shrink-0 overflow-hidden rounded-full bg-muted">
          <div className={cn("h-full", isSeeder ? "bg-green-500" : "bg-foreground/60")} style={{ width: `${progressPercent}%` }} />
        </div>
        <span className={cn("w-11 shrink-0 text-right tabular-nums", isSeeder && "text-green-500")}>{progressPercent}%</span>
      </div>
    </div>
  )
}

export function MobileWebSeedList({ urls, incognitoMode }: { urls: string[]; incognitoMode: boolean }) {
  const { t } = useTranslation("torrents")
  const copyUrl = async (url: string) => {
    try {
      await copyTextToClipboard(url)
      toast.success(t("webSeedsTable.toast.urlCopied"))
    } catch {
      toast.error(t("detailsPanel.toast.copyFailed"))
    }
  }
  return (
    <div className="space-y-2 px-4 pb-4">
      <h3 className="px-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("webSeedsTable.httpSources", { count: urls.length })}</h3>
      <div className="divide-y rounded-lg border bg-card/50">
        {urls.map((url, index) => (
          <TapMenu
            key={index}
            content={
              <DropdownMenuItem disabled={incognitoMode} onClick={() => void copyUrl(url)}>
                <Copy className="mr-2 size-4" />
                {t("webSeedsTable.copyUrl")}
              </DropdownMenuItem>
            }
          >
            <div className="break-all px-3 py-2.5 font-mono text-sm">{incognitoMode ? "***masked***" : url}</div>
          </TapMenu>
        ))}
      </div>
    </div>
  )
}
