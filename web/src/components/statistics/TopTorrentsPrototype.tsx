/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (discussion #4332): throwaway Top torrents variants with synthetic data and hardcoded English. Do not merge.
// Four variants of the Top torrents card on /statistics, switchable with ?variant=.

import { Facts } from "@/components/statistics/Facts"
import { RowLimit } from "@/components/statistics/RowLimit"
import { TorrentMarks } from "@/components/statistics/TopTorrents"
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useTrackerGroupIcon } from "@/hooks/useTrackerGroupName"
import { useIsPhone } from "@/hooks/useMediaQuery"
import type { TopTorrent } from "@/lib/statistics"
import { cn, formatBytes } from "@/lib/utils"
import { ChevronRight, Link2, Trash2 } from "lucide-react"
import { createContext, Fragment, useContext, useEffect, useRef, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

const MiB = 1024 ** 2
const GiB = 1024 ** 3

interface Proto {
  hash: string
  name: string
  tracker: string
  size: number
  uploaded: number
  downloaded: number
  crossSeed: boolean
  deleted: boolean
  first24h: number | null
  // Upload before the period, for the lifetime Seeded.
  before: number
}

// Built names only. Downloaded is the download in the period (last 30 days): 0 for a torrent that finished before it.
const t = (name: string, tracker: string, size: number, uploaded: number, downloaded: number, extra: Partial<Proto> = {}): Proto => ({
  hash: name, name, tracker, size, uploaded, downloaded, crossSeed: false, deleted: false, first24h: null, before: 0, ...extra,
})
const ALL: Proto[] = [
  t("Northern.Lights.Over.Tromso.2019.2160p.UHD.BluRay.REMUX.HDR.HEVC.Atmos-ORCA", "Aurora", 74 * GiB, 412 * GiB, 0, { before: 980 * GiB }),
  t("Glass.Harbor.2026.1080p.WEB-DL.DDP5.1.H.264-PELICAN", "Basalt", 6.2 * GiB, 168 * GiB, 6.2 * GiB, { first24h: 94 * GiB }),
  t("Copper.Valley.S03.1080p.WEB-DL.DDP5.1.H.264-LANTERN", "Basalt", 38 * GiB, 120 * GiB, 38 * GiB, { first24h: 61 * GiB }),
  t("Harbor.Lights.2018.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-ORCA", "Cobalt", 29 * GiB, 64 * GiB, 0, { crossSeed: true, before: 310 * GiB }),
  t("Midnight.Ferry.2024.2160p.UHD.BluRay.x265.10bit.HDR-TIDEPOOL", "Aurora", 18 * GiB, 51 * GiB, 0, { deleted: true, before: 140 * GiB }),
  t("Ironwood.S02E05.1080p.WEB-DL.DDP5.1.H.264-FENNEL", "Basalt", 3.4 * GiB, 47 * GiB, 3.4 * GiB, { first24h: 39 * GiB }),
  t("Fjord.Patrol.S01.1080p.BluRay.x264-PELICAN", "Cobalt", 54 * GiB, 44 * GiB, 0, { crossSeed: true, before: 22 * GiB }),
  t("Ember.and.Ash.2025.1080p.WEB-DL.DDP5.1.H.264-LANTERN", "Aurora", 7.8 * GiB, 39 * GiB, 0, { before: 61 * GiB }),
  t("Cinder.Peak.S01E02.1080p.WEB-DL.DDP5.1.H.264-FENNEL", "Basalt", 2.9 * GiB, 33 * GiB, 2.9 * GiB, { first24h: 21 * GiB }),
  t("The.Quiet.Orchard.S01.2160p.WEB-DL.DDP5.1.HDR.H.265-FENNEL", "Cobalt", 212 * GiB, 31 * GiB, 0, { crossSeed: true, before: 4 * GiB }),
  // Below the top ten by upload.
  t("Harvest.Moon.Tales.2026.1080p.WEB-DL.H.264-TIDEPOOL", "Aurora", 4.4 * GiB, 29 * GiB, 4.4 * GiB, { deleted: true, first24h: 18 * GiB }),
  t("Saltmarsh.Sessions.2026.24bit.FLAC-REEDS", "Driftnet", 1.1 * GiB, 22 * GiB, 9 * MiB, { crossSeed: true }),
  t("Quarry.Road.2023.1080p.BluRay.x264-PELICAN", "Cobalt", 9.6 * GiB, 21 * GiB, 0),
  t("Driftwood.Coast.2022.720p.WEB-DL.H.264-TIDEPOOL", "Aurora", 1.0 * GiB, 20 * GiB, 0),
  t("Old.Lighthouse.Collection.1080p.BluRay.REMUX-ORCA", "Cobalt", 640 * GiB, 18 * GiB, 0, { crossSeed: true }),
  t("Lantern.Festival.Live.2025.FLAC-REEDS", "Driftnet", 480 * MiB, 14 * GiB, 480 * MiB, { first24h: 6 * GiB }),
  t("Slate.Mountain.S04.1080p.WEB-DL.DDP5.1.H.264-LANTERN", "Basalt", 41 * GiB, 12 * GiB, 0, { crossSeed: true }),
  t("Mossgrove.Field.Guides.2026.EPUB-INKWELL", "Driftnet", 46 * MiB, 7.9 * GiB, 46 * MiB, { first24h: 3.1 * GiB }),
  t("Tidewater.Sampler.2026.MP3.320-REEDS", "Driftnet", 112 * MiB, 5.2 * GiB, 0, { crossSeed: true }),
  t("Pinecone.Radio.Hour.E112.MP3-INKWELL", "Driftnet", 64 * MiB, 2.4 * GiB, 0, { crossSeed: true }),
]
const BY_UPLOAD = [...ALL].sort((a, b) => b.uploaded - a.uploaded).slice(0, 10)

const multiple = (torrent: Proto, lifetime = false) => (torrent.uploaded + (lifetime ? torrent.before : 0)) / torrent.size
// The format of Seeded in the tracker table: two decimals and an x.
const formatSeeded = (value: number) => `${value.toFixed(2)}x`
// The Ratio of today: upload ÷ download in the period.
function todayRatio(torrent: Proto): string {
  return torrent.downloaded > 0 ? (torrent.uploaded / torrent.downloaded).toFixed(2) : torrent.uploaded > 0 ? "∞" : "–"
}
// The Ratio of qBittorrent: it divides by the size when the torrent downloaded less than 1% of its size.
function qbitRatio(torrent: Proto): { value: string; bySize: boolean } {
  const bySize = torrent.downloaded < torrent.size * 0.01
  return { value: (torrent.uploaded / (bySize ? torrent.size : torrent.downloaded)).toFixed(2), bySize }
}
// How a row shows Cross-seed and Deleted: today's badges, icons, or muted text after the tracker.
type MarkStyle = "badge" | "icon" | "text"
const MARK_STYLES: { key: MarkStyle; label: string }[] = [{ key: "badge", label: "Badges" }, { key: "icon", label: "Icons" }, { key: "text", label: "Text by tracker" }]
const MarkStyleContext = createContext<[MarkStyle, (style: MarkStyle) => void]>(["badge", () => {}])

function Marks({ torrent }: { torrent: Proto }) {
  const [style] = useContext(MarkStyleContext)
  if (style === "badge") return <TorrentMarks torrent={{ deleted: torrent.deleted, statisticsCrossSeed: torrent.crossSeed } as TopTorrent} />
  if (style === "icon") {
    return (
      <>
        {torrent.crossSeed && <MarkIcon icon={<Link2 className="size-3.5" />} labelKey="topTorrents.crossSeed" />}
        {torrent.deleted && <MarkIcon icon={<Trash2 className="size-3.5" />} labelKey="topTorrents.deleted" />}
      </>
    )
  }
  return null
}
// An icon with the app's tooltip. A tap on a phone opens the tooltip and does not open the row.
function MarkIcon({ icon, labelKey }: { icon: ReactNode; labelKey: "topTorrents.crossSeed" | "topTorrents.deleted" }) {
  const { t } = useTranslation("statistics")
  return (
    <Tooltip>
      <TooltipTrigger asChild openOnTap="only">
        <span role="img" aria-label={t(labelKey)} className="inline-flex shrink-0 p-0.5 text-muted-foreground">{icon}</span>
      </TooltipTrigger>
      <TooltipContent>{t(labelKey)}</TooltipContent>
    </Tooltip>
  )
}

// TorrentName without its native title: the full name shows in the app's tooltip, only when part of it is hidden,
// either past the end of a line or below the clamped lines. A tap stays with the row.
function ProtoTorrentName({ name, className }: { name: string; className?: string }) {
  const parts = name.split(/([._-])/)
  const ref = useRef<HTMLSpanElement>(null)
  const [cut, setCut] = useState(false)
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const check = () => setCut(element.scrollWidth > element.clientWidth || element.scrollHeight > element.clientHeight)
    check()
    const observer = new ResizeObserver(check)
    observer.observe(element)
    return () => observer.disconnect()
  }, [name])
  return (
    <Tooltip disabled={!cut}>
      <TooltipTrigger asChild openOnTap={false}>
        <span ref={ref} className={cn("whitespace-normal [overflow-wrap:anywhere]", className)}>
          {parts.map((part, index) => <Fragment key={index}>{part}{index % 2 === 1 && <wbr />}</Fragment>)}
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-md [overflow-wrap:anywhere]">{name}</TooltipContent>
    </Tooltip>
  )
}

// The tracker with the marks as text, for the "text" style.
function trackerLabel(torrent: Proto, style: MarkStyle): string {
  if (style !== "text") return torrent.tracker
  return [torrent.tracker, torrent.crossSeed && "cross-seed", torrent.deleted && "deleted"].filter(Boolean).join(" · ")
}
const marks = (torrent: Proto) => <Marks torrent={torrent} />

function Shell({ title, description, action, children, notes }: { title: string; description: string; action?: ReactNode; children: ReactNode; notes: ReactNode[] }) {
  return (
    <Card className="gap-0 overflow-hidden pt-4 pb-4 ring-2 ring-fuchsia-500/60 sm:pt-6">
      <CardHeader className="px-4 pb-4 sm:px-6">
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
        {action && <CardAction>{action}</CardAction>}
      </CardHeader>
      {children}
      <div className="space-y-1 px-4 pt-3 text-xs text-muted-foreground sm:px-6">
        {notes.map((note, index) => <p key={index}>{note}</p>)}
        <MarkStylePicker />
      </div>
    </Card>
  )
}

function MarkStylePicker() {
  const [style, setStyle] = useContext(MarkStyleContext)
  return (
    <span className="mt-1 inline-flex flex-wrap items-center gap-2 rounded border border-dashed border-fuchsia-500 px-2 py-1">
      Prototype control, Cross-seed and Deleted marks:
      {MARK_STYLES.map(option => (
        <button key={option.key} type="button" className={cn("rounded px-1.5", style === option.key ? "bg-fuchsia-500 text-white" : "hover:bg-muted")} onClick={() => setStyle(option.key)}>{option.label}</button>
      ))}
    </span>
  )
}

// The tracker logo as the real table shows it since #4334. The synthetic trackers have no logo, so it is the letter.
function TrackerIcon({ torrent }: { torrent: Proto }) {
  const icon = useTrackerGroupIcon()(torrent.tracker)
  return icon ? <TrackerIconImage {...icon} /> : <span className="size-4 shrink-0" />
}

function TrackerCell({ torrent }: { torrent: Proto }) {
  const [style] = useContext(MarkStyleContext)
  return <TableCell className="hidden max-w-64 text-muted-foreground @2xl:table-cell"><span className="flex items-center gap-1.5"><TrackerIcon torrent={torrent} /><span className="truncate">{trackerLabel(torrent, style)}</span></span></TableCell>
}

function NameCell({ torrent, facts, factsClass = "@2xl:hidden" }: { torrent: Proto; facts: [string, string, string?][]; factsClass?: string }) {
  const [style] = useContext(MarkStyleContext)
  return (
    <TableCell className="w-full max-w-0 pl-4 font-medium sm:pl-6">
      <span className="flex w-full min-w-0 items-center gap-2 text-left">
        <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
        <ProtoTorrentName name={torrent.name} className="line-clamp-2 @2xl:line-clamp-1" />
        <span className="hidden shrink-0 items-center gap-2 @2xl:flex">{marks(torrent)}</span>
      </span>
      <div className="flex min-w-0 items-center gap-2 pl-5.5 text-xs font-normal text-muted-foreground @2xl:hidden">
        <span className="flex min-w-0 items-center gap-1.5"><TrackerIcon torrent={torrent} /><span className="truncate">{trackerLabel(torrent, style)}</span></span>
        {marks(torrent)}
      </div>
      <Facts items={facts} className={factsClass} />
    </TableCell>
  )
}

function usePhoneRows(rows: Proto[]) {
  const narrow = useIsPhone()
  const [all, setAll] = useState(false)
  const shown = narrow && !all ? rows.slice(0, 5) : rows
  const limit = narrow && rows.length > 5 ? <RowLimit shown={shown.length} total={rows.length} onAll={all ? undefined : () => setAll(true)} onFewer={all ? () => setAll(false) : undefined} /> : null
  return { shown, limit }
}

const first24h = (torrent: Proto) => torrent.first24h === null ? "–" : formatBytes(torrent.first24h)
const NOTE = "Synthetic data, Last 30 days. Downloaded is the download in the period."

/** Today's table with the synthetic data, to compare against. */
function VariantNow() {
  const { shown, limit } = usePhoneRows(BY_UPLOAD)
  return (
    <Shell title="Top torrents" description="The ten torrents with the most upload in the period" notes={[NOTE]}>
      <div className="@container border-t">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-full pl-4 sm:pl-6">Torrent</TableHead>
              <TableHead className="hidden @2xl:table-cell">Tracker</TableHead>
              <TableHead className="pr-4 text-right sm:pr-6 @xl:pr-2">Uploaded</TableHead>
              <TableHead className="hidden text-right @xl:table-cell">Downloaded</TableHead>
              <TableHead className="hidden text-right @xl:table-cell @xl:pr-6 @2xl:pr-2">Ratio</TableHead>
              <TableHead className="hidden pr-6 text-right @2xl:table-cell">First 24 hours</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {shown.map(torrent => (
              <TableRow key={torrent.hash} className="cursor-pointer">
                <NameCell torrent={torrent} facts={[["Downloaded", formatBytes(torrent.downloaded), "@xl:hidden"], ["Ratio", todayRatio(torrent), "@xl:hidden"], ["First 24 hours", first24h(torrent)]]} />
                <TrackerCell torrent={torrent} />
                <TableCell className="pr-4 text-right tabular-nums sm:pr-6 @xl:pr-2">{formatBytes(torrent.uploaded)}</TableCell>
                <TableCell className="hidden text-right tabular-nums @xl:table-cell">{formatBytes(torrent.downloaded)}</TableCell>
                <TableCell className="hidden text-right tabular-nums @xl:table-cell @xl:pr-6 @2xl:pr-2">{todayRatio(torrent)}</TableCell>
                <TableCell className="hidden pr-6 text-right tabular-nums text-muted-foreground @2xl:table-cell">{first24h(torrent)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {limit}
      </div>
    </Shell>
  )
}

/** A: an extra Seeded column, as in the tracker table. The ranking stays by upload. */
function VariantA() {
  const [lifetime, setLifetime] = useState(false)
  const { shown, limit } = usePhoneRows(BY_UPLOAD)
  const seeded = (torrent: Proto) => formatSeeded(multiple(torrent, lifetime))
  return (
    <Shell
      title="Top torrents"
      description="The ten torrents with the most upload in the period"
      action={
        <Tabs value={lifetime ? "life" : "period"} onValueChange={value => setLifetime(value === "life")}>
          <TabsList className="border border-dashed border-fuchsia-500 max-sm:h-8">
            <TabsTrigger value="period" className="px-3 max-sm:px-2 max-sm:text-xs">Period</TabsTrigger>
            <TabsTrigger value="life" className="px-3 max-sm:px-2 max-sm:text-xs">Lifetime</TabsTrigger>
          </TabsList>
        </Tabs>
      }
      notes={[
        NOTE,
        lifetime
          ? "Seeded is the lifetime upload of the torrent divided by its size, as in the tracker table."
          : "Seeded is the upload in the period divided by the size of the torrent. 1.00x is one full copy.",
        "The dashed Period / Lifetime switch is a prototype control to compare the two time bases, not part of the design.",
      ]}
    >
      <div className="@container border-t">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-full pl-4 sm:pl-6">Torrent</TableHead>
              <TableHead className="hidden @2xl:table-cell">Tracker</TableHead>
              <TableHead className="pr-4 text-right sm:pr-6 @xl:pr-2">Uploaded</TableHead>
              <TableHead className="hidden text-right @xl:table-cell">Seeded</TableHead>
              <TableHead className="hidden text-right @3xl:table-cell">Downloaded</TableHead>
              <TableHead className="hidden text-right @3xl:table-cell">Ratio</TableHead>
              <TableHead className="hidden pr-6 text-right @2xl:table-cell">First 24 hours</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {shown.map(torrent => (
              <TableRow key={torrent.hash} className="cursor-pointer">
                <NameCell
                  torrent={torrent}
                  facts={[["Size", formatBytes(torrent.size)], ["Downloaded", formatBytes(torrent.downloaded), "@3xl:hidden"], ["Ratio", todayRatio(torrent), "@3xl:hidden"], ["First 24 hours", first24h(torrent), "@2xl:hidden"]]}
                  factsClass="@3xl:hidden"
                />
                <TrackerCell torrent={torrent} />
                <TableCell className="pr-4 text-right tabular-nums sm:pr-6 @xl:pr-2">
                  {formatBytes(torrent.uploaded)}
                  <div className="text-xs text-muted-foreground @xl:hidden">{seeded(torrent)}</div>
                </TableCell>
                <TableCell className="hidden text-right tabular-nums @xl:table-cell">{seeded(torrent)}</TableCell>
                <TableCell className="hidden text-right tabular-nums @3xl:table-cell">{formatBytes(torrent.downloaded)}</TableCell>
                <TableCell className="hidden text-right tabular-nums @3xl:table-cell">{todayRatio(torrent)}</TableCell>
                <TableCell className="hidden pr-6 text-right tabular-nums text-muted-foreground @2xl:table-cell">{first24h(torrent)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {limit}
      </div>
    </Shell>
  )
}

const FLOORS = [0, 1 * GiB, 5 * GiB]

/** B: a switch that ranks by Seeded instead of by upload. */
function VariantB() {
  const [bySize, setBySize] = useState(true)
  const [floor, setFloor] = useState(1 * GiB)
  const rows = bySize ? ALL.filter(torrent => torrent.size >= floor).sort((a, b) => multiple(b) - multiple(a)).slice(0, 10) : BY_UPLOAD
  const { shown, limit } = usePhoneRows(rows)
  return (
    <Shell
      title="Top torrents"
      description={bySize ? "The ten torrents that uploaded the most for their size in the period" : "The ten torrents with the most upload in the period"}
      action={
        <Tabs value={bySize ? "size" : "upload"} onValueChange={value => setBySize(value === "size")}>
          <TabsList className="max-sm:h-8">
            <TabsTrigger value="upload" className="px-3 max-sm:px-2 max-sm:text-xs">Upload</TabsTrigger>
            <TabsTrigger value="size" className="px-3 max-sm:px-2 max-sm:text-xs">Seeded</TabsTrigger>
          </TabsList>
        </Tabs>
      }
      notes={[
        NOTE,
        bySize && floor > 0 ? `Seeded leaves out torrents under ${formatBytes(floor)}.` : bySize ? "Seeded counts every torrent, also tiny ones." : "",
        <span key="floor" className="inline-flex flex-wrap items-center gap-2 rounded border border-dashed border-fuchsia-500 px-2 py-1">
          Prototype control, smallest torrent in Seeded:
          {FLOORS.map(value => (
            <button key={value} type="button" className={cn("rounded px-1.5", floor === value ? "bg-fuchsia-500 text-white" : "hover:bg-muted")} onClick={() => setFloor(value)}>
              {value === 0 ? "no limit" : formatBytes(value)}
            </button>
          ))}
        </span>,
      ]}
    >
      <div className="@container border-t">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-full pl-4 sm:pl-6">Torrent</TableHead>
              <TableHead className="hidden @2xl:table-cell">Tracker</TableHead>
              <TableHead className="hidden text-right @xl:table-cell">Size</TableHead>
              <TableHead className={cn("text-right", bySize ? "hidden @xl:table-cell" : "pr-4 sm:pr-6 @xl:pr-2")}>Uploaded</TableHead>
              <TableHead className={cn("text-right", bySize ? "pr-4 sm:pr-6" : "hidden pr-6 @xl:table-cell")}>Seeded</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {shown.map(torrent => (
              <TableRow key={torrent.hash} className="cursor-pointer">
                <NameCell
                  torrent={torrent}
                  facts={bySize
                    ? [["Uploaded", formatBytes(torrent.uploaded), "@xl:hidden"], ["Size", formatBytes(torrent.size), "@xl:hidden"], ["Ratio", todayRatio(torrent)]]
                    : [["Size", formatBytes(torrent.size), "@xl:hidden"], ["Seeded", formatSeeded(multiple(torrent)), "@xl:hidden"], ["Ratio", todayRatio(torrent)]]}
                />
                <TrackerCell torrent={torrent} />
                <TableCell className="hidden text-right tabular-nums text-muted-foreground @xl:table-cell">{formatBytes(torrent.size)}</TableCell>
                <TableCell className={cn("text-right tabular-nums", bySize ? "hidden @xl:table-cell" : "pr-4 sm:pr-6 @xl:pr-2")}>{formatBytes(torrent.uploaded)}</TableCell>
                <TableCell className={cn("text-right tabular-nums", bySize ? "pr-4 font-semibold sm:pr-6" : "hidden pr-6 @xl:table-cell")}>{formatSeeded(multiple(torrent))}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {limit}
      </div>
    </Shell>
  )
}

/** C: no new column. Ratio counts as qBittorrent does: by size when the torrent downloaded under 1% of its size. */
function VariantC() {
  const { shown, limit } = usePhoneRows(BY_UPLOAD)
  const ratio = (torrent: Proto) => {
    const { value, bySize } = qbitRatio(torrent)
    return bySize ? `${value}*` : value
  }
  return (
    <Shell
      title="Top torrents"
      description="The ten torrents with the most upload in the period"
      notes={[NOTE, "* Ratio of size: the torrent downloaded less than 1% of its size in the period, so the ratio divides the upload by the size, as qBittorrent does for a cross-seed."]}
    >
      <div className="@container border-t">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-full pl-4 sm:pl-6">Torrent</TableHead>
              <TableHead className="hidden @2xl:table-cell">Tracker</TableHead>
              <TableHead className="pr-4 text-right sm:pr-6 @xl:pr-2">Uploaded</TableHead>
              <TableHead className="hidden text-right @xl:table-cell">Downloaded</TableHead>
              <TableHead className="hidden text-right @xl:table-cell @xl:pr-6 @2xl:pr-2">Ratio</TableHead>
              <TableHead className="hidden pr-6 text-right @2xl:table-cell">First 24 hours</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {shown.map(torrent => (
              <TableRow key={torrent.hash} className="cursor-pointer">
                <NameCell torrent={torrent} facts={[["Downloaded", formatBytes(torrent.downloaded), "@xl:hidden"], ["Ratio", ratio(torrent), "@xl:hidden"], ["First 24 hours", first24h(torrent)]]} />
                <TrackerCell torrent={torrent} />
                <TableCell className="pr-4 text-right tabular-nums sm:pr-6 @xl:pr-2">{formatBytes(torrent.uploaded)}</TableCell>
                <TableCell className="hidden text-right tabular-nums @xl:table-cell">{formatBytes(torrent.downloaded)}</TableCell>
                <TableCell className="hidden text-right tabular-nums @xl:table-cell @xl:pr-6 @2xl:pr-2" title={qbitRatio(torrent).bySize ? `Upload ÷ size (${formatBytes(torrent.size)})` : "Upload ÷ download"}>{ratio(torrent)}</TableCell>
                <TableCell className="hidden pr-6 text-right tabular-nums text-muted-foreground @2xl:table-cell">{first24h(torrent)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {limit}
      </div>
    </Shell>
  )
}

export const TOP_TORRENTS_VARIANTS = [
  { key: "now", label: "Today (baseline)" },
  { key: "A", label: "Extra Seeded column" },
  { key: "B", label: "Rank by Seeded" },
  { key: "C", label: "Ratio like qBittorrent" },
]

export function TopTorrentsPrototype({ variant }: { variant: string }) {
  const markStyle = useState<MarkStyle>("icon")
  return (
    <MarkStyleContext.Provider value={markStyle}>
      {variant === "A" ? <VariantA /> : variant === "B" ? <VariantB /> : variant === "C" ? <VariantC /> : <VariantNow />}
    </MarkStyleContext.Provider>
  )
}
