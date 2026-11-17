/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ShareOptionFields } from "@/components/shares/ShareOptionFields"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { SaveFooter } from "@/components/ui/save-footer"
import { Textarea } from "@/components/ui/textarea"
import { getShareSettings, saveShareSettings, shareSettingsQueryKey, type ShareSettings } from "@/lib/shares-api"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

type TrackerChoice = "own" | "outside"

/** The form keeps numbers as typed text, so a field can be empty while the user types. */
interface Draft {
  tracker: TrackerChoice
  port: string
  publicAddress: string
  trustedProxies: string
  trackerUrl: string
  maxActive: string
  peersPerShare: string
  days: string
  endAtFirstDownload: boolean
  firstDownloaderOnly: boolean
}

function draftOf(settings: ShareSettings): Draft {
  const outside = settings.trackerInUse === "outside" || (settings.trackerInUse === "" && settings.trackerUrl !== "" && settings.port === 0 && settings.publicAddress === "")
  return {
    tracker: outside ? "outside" : "own",
    port: settings.port > 0 ? String(settings.port) : "",
    publicAddress: settings.publicAddress,
    trustedProxies: settings.trustedProxies.join("\n"),
    trackerUrl: settings.trackerUrl,
    maxActive: String(settings.maxActive),
    peersPerShare: String(settings.peersPerShare),
    days: String(settings.defaults.days),
    endAtFirstDownload: settings.defaults.endAtFirstDownload,
    firstDownloaderOnly: settings.defaults.firstDownloaderOnly,
  }
}

/** An empty or bad number goes to the server as 0, and the server names the field. */
function toNumber(text: string): number {
  const value = Number(text.trim())
  return Number.isInteger(value) ? value : 0
}

function settingsOf(draft: Draft, saved: ShareSettings): ShareSettings {
  const own = draft.tracker === "own"
  return {
    // An outside tracker closes qui's port: qui uses the tracker URL only while its own tracker is off.
    port: own ? toNumber(draft.port) : 0,
    publicAddress: own ? draft.publicAddress.trim() : "",
    trustedProxies: draft.trustedProxies.split(/[\s,]+/).filter(Boolean),
    trackerUrl: draft.trackerUrl.trim(),
    maxActive: toNumber(draft.maxActive),
    peersPerShare: toNumber(draft.peersPerShare),
    defaults: {
      days: toNumber(draft.days),
      endAtFirstDownload: draft.endAtFirstDownload,
      firstDownloaderOnly: draft.firstDownloaderOnly,
    },
    trackerInUse: saved.trackerInUse,
  }
}

/** Settings → Shares: the Share tracker, the trusted proxies, the limits, and the defaults of a new Share. */
export function ShareSettingsForm() {
  const { t } = useTranslation("shares")
  const query = useQuery({ queryKey: shareSettingsQueryKey, queryFn: getShareSettings })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.title")}</CardTitle>
        <CardDescription>{t("settings.description")}</CardDescription>
      </CardHeader>
      {query.isPending && (
        <CardContent>
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </CardContent>
      )}
      {query.isError && (
        <CardContent>
          <p className="text-sm text-destructive">{t("settings.loadFailed")}</p>
        </CardContent>
      )}
      {query.data && <SettingsFields saved={query.data} />}
    </Card>
  )
}

function SettingsFields({ saved }: { saved: ShareSettings }) {
  const { t } = useTranslation("shares")
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState(() => draftOf(saved))
  const mutation = useMutation({
    mutationFn: saveShareSettings,
    onSuccess: async (settings) => {
      await queryClient.cancelQueries({ queryKey: shareSettingsQueryKey })
      queryClient.setQueryData(shareSettingsQueryKey, settings)
      toast.success(t("settings.saved"))
    },
  })

  const update = (change: Partial<Draft>) => {
    setDraft((current) => ({ ...current, ...change }))
    if (mutation.isError) mutation.reset()
  }

  const inUseKey = saved.trackerInUse === "own" ? "settings.inUse.own" : saved.trackerInUse === "outside" ? "settings.inUse.outside" : "settings.inUse.none"

  return (
    <>
      <CardContent className="space-y-8">
        <section className="space-y-4">
          <div className="space-y-1">
            <h3 className="text-sm font-medium">{t("settings.tracker.heading")}</h3>
            <p className="text-sm text-muted-foreground">{t(inUseKey)}</p>
          </div>
          <RadioGroup value={draft.tracker} onValueChange={(value) => update({ tracker: value as TrackerChoice })} className="gap-3">
            <div className="flex items-start gap-2">
              <RadioGroupItem id="share-tracker-own" value="own" className="mt-0.5" />
              <div className="space-y-0.5">
                <Label htmlFor="share-tracker-own">{t("settings.tracker.own")}</Label>
                <p className="text-sm text-muted-foreground">{t("settings.tracker.ownDescription")}</p>
              </div>
            </div>
            <div className="flex items-start gap-2">
              <RadioGroupItem id="share-tracker-outside" value="outside" className="mt-0.5" />
              <div className="space-y-0.5">
                <Label htmlFor="share-tracker-outside">{t("settings.tracker.outside")}</Label>
                <p className="text-sm text-muted-foreground">{t("settings.tracker.outsideDescription")}</p>
              </div>
            </div>
          </RadioGroup>

          {draft.tracker === "own" ? (
            <div className="grid gap-4 sm:grid-cols-[10rem_1fr]">
              <div className="space-y-1.5">
                <Label htmlFor="share-port">{t("settings.tracker.port")}</Label>
                <Input id="share-port" inputMode="numeric" value={draft.port} onChange={(e) => update({ port: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="share-public-address">{t("settings.tracker.publicAddress")}</Label>
                  <FieldHelp>{t("settings.tracker.publicAddressDescription")}</FieldHelp>
                </div>
                <Input
                  id="share-public-address"
                  placeholder={t("settings.tracker.publicAddressPlaceholder")}
                  value={draft.publicAddress}
                  onChange={(e) => update({ publicAddress: e.target.value })}
                  spellCheck={false}
                />
              </div>
              <div className="space-y-1.5 sm:col-span-2">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="share-trusted-proxies">{t("settings.tracker.trustedProxies")}</Label>
                  <FieldHelp>{t("settings.tracker.trustedProxiesDescription")}</FieldHelp>
                </div>
                <Textarea
                  id="share-trusted-proxies"
                  rows={3}
                  placeholder={"172.17.0.1\n10.0.0.0/8"}
                  value={draft.trustedProxies}
                  onChange={(e) => update({ trustedProxies: e.target.value })}
                  spellCheck={false}
                />
              </div>
            </div>
          ) : (
            <div className="space-y-1.5">
              <div className="flex items-center gap-1.5">
                <Label htmlFor="share-tracker-url">{t("settings.tracker.trackerUrl")}</Label>
                <FieldHelp>{t("settings.tracker.trackerUrlDescription")}</FieldHelp>
              </div>
              <Input
                id="share-tracker-url"
                placeholder={t("settings.tracker.trackerUrlPlaceholder")}
                value={draft.trackerUrl}
                onChange={(e) => update({ trackerUrl: e.target.value })}
                spellCheck={false}
              />
            </div>
          )}
        </section>

        <section className="space-y-4">
          <h3 className="text-sm font-medium">{t("settings.limits.heading")}</h3>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="share-max-active">{t("settings.limits.maxActive")}</Label>
              <Input id="share-max-active" inputMode="numeric" value={draft.maxActive} onChange={(e) => update({ maxActive: e.target.value })} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="share-peers">{t("settings.limits.peersPerShare")}</Label>
              <Input id="share-peers" inputMode="numeric" value={draft.peersPerShare} onChange={(e) => update({ peersPerShare: e.target.value })} />
            </div>
          </div>
        </section>

        <section className="space-y-4">
          <div className="space-y-1">
            <h3 className="text-sm font-medium">{t("settings.defaults.heading")}</h3>
            <p className="text-sm text-muted-foreground">{t("settings.defaults.description")}</p>
          </div>
          <ShareOptionFields
            idPrefix="share-defaults"
            days={draft.days}
            endAtFirstDownload={draft.endAtFirstDownload}
            firstDownloaderOnly={draft.firstDownloaderOnly}
            onChange={update}
          />
        </section>

        {mutation.isError && (
          <Alert variant="destructive">
            <AlertDescription>{mutation.error instanceof Error ? mutation.error.message : t("settings.saveFailed")}</AlertDescription>
          </Alert>
        )}
      </CardContent>
      <SaveFooter pending={mutation.isPending} onSave={() => mutation.mutate(settingsOf(draft, saved))} label={t("settings.save")} />
    </>
  )
}
