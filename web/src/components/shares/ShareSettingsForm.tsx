/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Field } from "@/components/kit/Field"
import { Section } from "@/components/kit/Section"
import { ShareOptionFields } from "@/components/shares/ShareOptionFields"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Textarea } from "@/components/ui/textarea"
import { getShareSettings, saveShareSettings, shareSettingsQueryKey, type ShareSettings } from "@/lib/shares-api"
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

type TrackerChoice = "own" | "outside"

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

function toNumber(text: string): number {
  const value = Number(text.trim())
  return Number.isInteger(value) ? value : 0
}

function toPort(text: string): number {
  if (text.trim() === "") return 0
  const value = Number(text.trim())
  return Number.isInteger(value) ? value : -1
}

function settingsOf(draft: Draft, saved: ShareSettings): ShareSettings {
  const own = draft.tracker === "own"
  return {
    port: own ? toPort(draft.port) : 0,
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

export function ShareSettingsForm() {
  const { t } = useTranslation("shares")
  const query = useQuery({ queryKey: shareSettingsQueryKey, queryFn: getShareSettings })

  if (!query.data) {
    return <Section title={t("settings.title")} description={t("settings.description")} queries={[query]} />
  }
  return <SettingsSection saved={query.data} query={query} />
}

function SettingsSection({ saved, query }: { saved: ShareSettings; query: UseQueryResult<ShareSettings> }) {
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
    <Section
      title={t("settings.title")}
      description={t("settings.description")}
      queries={[query]}
      submit={{ label: t("settings.save"), pending: mutation.isPending, onSubmit: () => mutation.mutate(settingsOf(draft, saved)) }}
    >
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
            <Field label={t("settings.tracker.port")}>
              <Input inputMode="numeric" value={draft.port} onChange={(e) => update({ port: e.target.value })} />
            </Field>
            <Field label={t("settings.tracker.publicAddress")} help={t("settings.tracker.publicAddressDescription")}>
              <Input
                placeholder={t("settings.tracker.publicAddressPlaceholder")}
                value={draft.publicAddress}
                onChange={(e) => update({ publicAddress: e.target.value })}
                spellCheck={false}
              />
            </Field>
            <div className="sm:col-span-2">
              <Field label={t("settings.tracker.trustedProxies")} help={t("settings.tracker.trustedProxiesDescription")}>
                <Textarea
                  rows={3}
                  placeholder={"127.0.0.1\n172.17.0.1"}
                  value={draft.trustedProxies}
                  onChange={(e) => update({ trustedProxies: e.target.value })}
                  spellCheck={false}
                />
              </Field>
            </div>
          </div>
        ) : (
          <Field label={t("settings.tracker.trackerUrl")} help={t("settings.tracker.trackerUrlDescription")}>
            <Input
              placeholder={t("settings.tracker.trackerUrlPlaceholder")}
              value={draft.trackerUrl}
              onChange={(e) => update({ trackerUrl: e.target.value })}
              spellCheck={false}
            />
          </Field>
        )}
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.limits.heading")}</h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("settings.limits.maxActive")}>
            <Input inputMode="numeric" value={draft.maxActive} onChange={(e) => update({ maxActive: e.target.value })} />
          </Field>
          <Field label={t("settings.limits.peersPerShare")}>
            <Input inputMode="numeric" value={draft.peersPerShare} onChange={(e) => update({ peersPerShare: e.target.value })} />
          </Field>
        </div>
      </section>

      <section className="space-y-4">
        <div className="space-y-1">
          <h3 className="text-sm font-medium">{t("settings.defaults.heading")}</h3>
          <p className="text-sm text-muted-foreground">{t("settings.defaults.description")}</p>
        </div>
        <ShareOptionFields
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
    </Section>
  )
}
