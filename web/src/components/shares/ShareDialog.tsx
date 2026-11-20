/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ShareOptionFields } from "@/components/shares/ShareOptionFields"
import { useIncognitoDisplay } from "@/components/transfers/use-incognito-display"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { BetaBadge } from "@/components/transfers/BetaBadge"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { APIError } from "@/lib/api"
import {
  getShareSettings,
  shareSettingsQueryKey,
  sharesQueryKey,
  shareTorrentUrl,
  startShares,
  type Share,
  type ShareSettings,
  type StartSharesResponse
} from "@/lib/shares-api"
import { copyTextToClipboard, formatBytes } from "@/lib/utils"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Copy, Download, Loader2, TriangleAlert } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

export interface ShareDialogTorrent {
  hash: string
  name: string
}

interface ShareDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  /** One Share starts for each torrent. */
  torrents: ShareDialogTorrent[]
}

/**
 * The Share dialog: three fields filled in from the defaults of Settings →
 * Shares, then a result step with the .torrent of each Share and a message to
 * send with it. qui hosts no download link: the user sends both.
 */
export function ShareDialog({ open, onOpenChange, instanceId, torrents }: ShareDialogProps) {
  const { t } = useTranslation("shares")
  const display = useIncognitoDisplay()
  const settingsQuery = useQuery({ queryKey: shareSettingsQueryKey, queryFn: getShareSettings, enabled: open })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">{t("dialog.title")}<BetaBadge /></DialogTitle>
          <DialogDescription>{t("dialog.description", { count: torrents.length, name: torrents[0] ? display.name(torrents[0]) : "" })}</DialogDescription>
        </DialogHeader>
        {settingsQuery.isPending && <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />}
        {settingsQuery.isError && <p className="text-sm text-destructive">{t("settings.loadFailed")}</p>}
        {settingsQuery.data && (
          <ShareFlow settings={settingsQuery.data} instanceId={instanceId} torrents={torrents} onClose={() => onOpenChange(false)} />
        )}
      </DialogContent>
    </Dialog>
  )
}

/** The days of the field, or null for text that is not a whole number of 1 or more. */
function daysOf(text: string): number | null {
  const days = Number(text.trim())
  return Number.isInteger(days) && days >= 1 ? days : null
}

interface ShareFlowProps {
  settings: ShareSettings
  instanceId: number
  torrents: ShareDialogTorrent[]
  onClose: () => void
}

function ShareFlow({ settings, instanceId, torrents, onClose }: ShareFlowProps) {
  const { t } = useTranslation("shares")
  const display = useIncognitoDisplay()
  const queryClient = useQueryClient()
  const [days, setDays] = useState(String(settings.defaults.days))
  const [endAtFirstDownload, setEndAtFirstDownload] = useState(settings.defaults.endAtFirstDownload)
  const [firstDownloaderOnly, setFirstDownloaderOnly] = useState(settings.defaults.firstDownloaderOnly)
  const outside = settings.trackerInUse === "outside"
  const validDays = daysOf(days)
  const mutation = useMutation({
    mutationFn: (shareDays: number) => startShares({
      instanceId,
      hashes: torrents.map((torrent) => torrent.hash),
      days: shareDays,
      endAtFirstDownload,
      // The lock needs the announces that only qui's own tracker sees.
      firstDownloaderOnly: firstDownloaderOnly && !outside,
    }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: sharesQueryKey }),
  })

  if (mutation.data) {
    return <ShareResult response={mutation.data} torrents={torrents} onClose={onClose} />
  }

  const refused = mutation.error instanceof APIError ? (mutation.error.data as Partial<StartSharesResponse> | undefined)?.refused : undefined

  return (
    <>
      <div className="space-y-5">
        <TrackerInUse settings={settings} onClose={onClose} />
        <ShareOptionFields
          days={days}
          daysError={validDays === null ? t("options.daysInvalid") : undefined}
          endAtFirstDownload={endAtFirstDownload}
          firstDownloaderOnly={firstDownloaderOnly}
          lockUnavailable={outside}
          disabled={mutation.isPending}
          onChange={(change) => {
            if (change.days !== undefined) setDays(change.days)
            if (change.endAtFirstDownload !== undefined) setEndAtFirstDownload(change.endAtFirstDownload)
            if (change.firstDownloaderOnly !== undefined) setFirstDownloaderOnly(change.firstDownloaderOnly)
          }}
        />
        {/* A standing warning, not news: role "note" keeps screen readers from announcing it. */}
        <Alert role="note">
          <TriangleAlert className="h-4 w-4" />
          <AlertDescription>{t("dialog.warning")}</AlertDescription>
        </Alert>
        {mutation.isError && (
          <Alert variant="destructive">
            <AlertDescription>
              {display.text(mutation.error.message)}
              {Array.isArray(refused) && refused.length > 1 && <RefusedList refused={refused} torrents={torrents} />}
            </AlertDescription>
          </Alert>
        )}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>{t("dialog.cancel")}</Button>
        <Button
          onClick={() => validDays !== null && mutation.mutate(validDays)}
          disabled={settings.trackerInUse === "" || validDays === null || mutation.isPending}
        >
          {mutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("dialog.create")}
        </Button>
      </DialogFooter>
    </>
  )
}

/** The tracker of a new Share, with a link to Settings → Shares, where the user sets it. */
function TrackerInUse({ settings, onClose }: { settings: ShareSettings; onClose: () => void }) {
  const { t } = useTranslation("shares")
  const display = useIncognitoDisplay()
  let text: string
  switch (settings.trackerInUse) {
    case "own":
      text = t("dialog.tracker.own", { address: display.tracker(settings.publicAddress) })
      break
    case "outside":
      text = t("dialog.tracker.outside", { url: display.tracker(settings.trackerUrl) })
      break
    default:
      text = t("settings.inUse.none")
  }
  return (
    <div className="space-y-0.5">
      <div className="flex items-baseline justify-between gap-2">
        <p className="text-sm font-medium">{t("dialog.tracker.label")}</p>
        <Link to="/settings" search={{ tab: "shares" }} onClick={onClose} className="text-sm text-primary underline-offset-4 hover:underline">
          {t("dialog.tracker.settings")}
        </Link>
      </div>
      <p className={settings.trackerInUse === "" ? "text-sm text-destructive" : "text-sm text-muted-foreground break-all"}>{text}</p>
    </div>
  )
}

function RefusedList({ refused, torrents }: { refused: StartSharesResponse["refused"]; torrents: ShareDialogTorrent[] }) {
  const { t } = useTranslation("shares")
  const display = useIncognitoDisplay()
  const nameOf = (hash: string) => display.name(torrents.find((torrent) => torrent.hash === hash) ?? { hash, name: hash })
  return (
    <ul aria-label={t("dialog.refused")} className="mt-2 list-disc space-y-1 pl-5">
      {refused.map((entry) => (
        <li key={entry.hash} className="break-words">
          {t("dialog.refusedEntry", { name: nameOf(entry.hash), reason: display.text(entry.reason) })}
        </li>
      ))}
    </ul>
  )
}

function ShareResult({ response, torrents, onClose }: { response: StartSharesResponse; torrents: ShareDialogTorrent[]; onClose: () => void }) {
  const { t } = useTranslation("shares")
  return (
    <>
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">{t("dialog.result.description", { count: response.shares.length })}</p>
        <ul className="space-y-3">
          {response.shares.map((share) => <ShareResultRow key={share.id} share={share} />)}
        </ul>
        {response.refused.length > 0 && (
          <div className="space-y-1">
            <p className="text-sm font-medium">{t("dialog.refused")}</p>
            <RefusedList refused={response.refused} torrents={torrents} />
          </div>
        )}
      </div>
      <DialogFooter>
        <Button onClick={onClose}>{t("dialog.done")}</Button>
      </DialogFooter>
    </>
  )
}

function ShareResultRow({ share }: { share: Share }) {
  const { t } = useTranslation("shares")
  const { formatDate } = useDateTimeFormatters()
  const display = useIncognitoDisplay()

  const copyMessage = async () => {
    try {
      await copyTextToClipboard(shareMessage(share, t, formatDate))
    } catch {
      toast.error(t("dialog.result.copyFailed"))
      return
    }
    toast.success(t("dialog.result.copied"))
  }

  return (
    <li className="space-y-2 rounded-md border p-3">
      <p className="text-sm font-medium break-all">{display.name(share)}</p>
      <div className="flex flex-wrap gap-2">
        <Button asChild variant="outline" size="sm">
          <a href={shareTorrentUrl(share.id)} download>
            <Download className="mr-1.5 h-4 w-4" />
            {t("dialog.result.torrent")}
          </a>
        </Button>
        <Button variant="outline" size="sm" onClick={copyMessage}>
          <Copy className="mr-1.5 h-4 w-4" />
          {t("dialog.result.copyMessage")}
        </Button>
      </div>
    </li>
  )
}

/** The text that the user sends with the .torrent: what it is, which clients open it, and when it stops working. */
function shareMessage(share: Share, t: (key: string, values?: Record<string, unknown>) => string, formatDate: (date: Date) => string): string {
  const parts = [t("message.intro", { name: share.name, size: formatBytes(share.size), date: formatDate(new Date(share.expiresAt)) })]
  if (share.endAtFirstDownload) {
    parts.push(t("message.endAtFirstDownload"))
  }
  return parts.join(" ")
}
