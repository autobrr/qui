/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { RefreshCw } from "lucide-react"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { Facts } from "@/components/kit/Facts"
import { Section } from "@/components/kit/Section"
import { InstallUpdateButton } from "@/components/settings/InstallUpdateButton"
import { RestartButton } from "@/components/settings/RestartButton"
import { RestartOverlay } from "@/components/settings/RestartOverlay"
import { SupportSettings } from "@/components/support/SupportSettings"
import { Button } from "@/components/ui/button"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useSelfUpdate } from "@/hooks/useSelfUpdate"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { cn, formatDuration } from "@/lib/utils"
import type { User } from "@/types"

function isDevVersion(version?: string): boolean {
  const value = version?.trim().toLowerCase() || ""
  return value === "0.0.0-dev" || value.includes("dev") || value === "main"
}

function formatSessionAuth(user: User | undefined): string {
  if (!user) return "—"
  const method = user.auth_method?.trim() || "builtin"
  const username = user.username?.trim()
  return username ? `${method} (${username})` : method
}

export function ApplicationInfoPanel({ openUpdate, onUpdateOpened }: { openUpdate: boolean; onUpdateOpened: () => void }) {
  const { t } = useTranslation("settings")
  const { formatISOTimestamp, formatTimestamp } = useDateTimeFormatters()
  const appInfoQuery = useQuery({
    queryKey: ["application-info"],
    queryFn: () => api.getApplicationInfo(),
    staleTime: 30 * 1000,
  })
  const currentUserQuery = useQuery({
    queryKey: ["auth-me", "application-tab"],
    queryFn: () => api.checkAuth(),
    staleTime: 60 * 1000,
  })
  const latestVersionQuery = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    staleTime: 5 * 60 * 1000,
  })

  const selfUpdate = useSelfUpdate()
  const { setConfirmOpen } = selfUpdate

  useEffect(() => {
    if (openUpdate && latestVersionQuery.data) {
      setConfirmOpen(true)
      onUpdateOpened()
    }
  }, [openUpdate, latestVersionQuery.data, setConfirmOpen, onUpdateOpened])

  const info = appInfoQuery.data
  const latest = latestVersionQuery.data
  const refreshing = appInfoQuery.isFetching || latestVersionQuery.isFetching || currentUserQuery.isFetching
  const refresh = () => {
    void appInfoQuery.refetch()
    void latestVersionQuery.refetch()
    void currentUserQuery.refetch()
  }

  const installButton = (className: string) => latest && (
    <InstallUpdateButton selfUpdate={selfUpdate} release={latest} className={className} />
  )

  const updateStatus = (): { label: string; detail?: string } => {
    if (!info?.checkForUpdates) {
      return { label: t("application.build.statuses.disabled"), detail: t("application.build.statuses.disabledDetail") }
    }
    if (isDevVersion(info.version)) {
      return { label: t("application.build.statuses.devBuild") }
    }
    if (latestVersionQuery.isLoading || latestVersionQuery.isFetching) {
      return { label: t("application.build.statuses.checking"), detail: t("application.build.statuses.checkingDetail") }
    }
    if (latest) {
      return { label: t("application.build.statuses.updateAvailable"), detail: latest.tag_name }
    }
    return { label: t("application.build.statuses.upToDate"), detail: t("application.build.statuses.upToDateDetail") }
  }

  const buildFacts = () => {
    if (!info) return []
    const status = updateStatus()
    const checked = latestVersionQuery.dataUpdatedAt > 0
      ? t("application.build.statuses.lastChecked", { date: formatTimestamp(latestVersionQuery.dataUpdatedAt / 1000) })
      : t("application.build.statuses.notCheckedYet")
    const buildDate = info.buildDate?.trim()
    return [
      { label: t("application.build.version"), value: info.version || "—", mono: true, copy: info.version },
      { label: t("application.build.commit"), value: info.commitShort || info.commit || "—", mono: true, copy: info.commit },
      {
        label: t("application.build.buildDate"),
        value: buildDate ? formatISOTimestamp(buildDate) : "—",
        hint: buildDate && formatRelativeTime(buildDate),
      },
      {
        label: t("application.build.updateStatus"),
        value: status.label,
        hint: (
          <>
            {status.detail && <span className="max-sm:block">{status.detail}<span aria-hidden="true" className="max-sm:hidden"> · </span></span>}
            {checked}
          </>
        ),
        action: installButton("shrink-0 max-sm:h-10 sm:-my-1.5"),
      },
    ]
  }

  return (
    <>
      <Section
        title={t("application.build.title")}
        description={t("application.build.description")}
        queries={[appInfoQuery]}
        actions={appInfoQuery.isLoadingError ? installButton("") : (
          <>
            <RestartButton />
            <Button variant="outline" size="sm" disabled={refreshing} onClick={refresh}>
              <RefreshCw className={cn(refreshing && "animate-spin")} />
              {t("application.build.refresh")}
            </Button>
          </>
        )}
      >
        <Facts items={buildFacts()} />
      </Section>

      <SupportSettings />

      {info && (
        <>
          <Section title={t("application.runtime.title")} description={t("application.runtime.description")}>
            <Facts
              items={[
                {
                  label: t("application.runtime.uptime"),
                  value: <LiveUptime seconds={info.uptimeSeconds} since={appInfoQuery.dataUpdatedAt} />,
                },
                { label: t("application.runtime.runtime"), value: `${info.goVersion} · ${info.goOS}/${info.goArch}`, mono: true },
                { label: t("application.runtime.bindAddress"), value: `${info.host}:${info.port}${info.baseUrl}`, mono: true },
              ]}
            />
          </Section>

          <Section title={t("application.auth.title")} description={t("application.auth.description")} queries={[currentUserQuery]}>
            <Facts
              items={[
                { label: t("application.auth.currentSessionAuth"), value: formatSessionAuth(currentUserQuery.data), mono: true },
                { label: t("application.auth.oidcEnabled"), value: info.oidcEnabled ? t("application.auth.yes") : t("application.auth.no") },
                { label: t("application.auth.builtInLoginEnabled"), value: info.builtInLoginEnabled ? t("application.auth.yes") : t("application.auth.no") },
                { label: t("application.auth.oidcIssuerHost"), value: info.oidcIssuerHost || "—", mono: true },
              ]}
            />
          </Section>

          <Section title={t("application.storage.title")} description={t("application.storage.description")}>
            <Facts
              items={[
                {
                  label: t("application.storage.database"),
                  value: info.database.target ? `${info.database.engine} (${info.database.target})` : info.database.engine,
                  mono: true,
                  copy: info.database.target,
                },
                { label: t("application.storage.configDir"), value: info.configDir || "—", mono: true, copy: info.configDir },
                { label: t("application.storage.dataDir"), value: info.dataDir || "—", mono: true, copy: info.dataDir },
              ]}
            />
          </Section>
        </>
      )}

      <RestartOverlay state={selfUpdate.overlay} update={selfUpdate.update} />
    </>
  )
}

function LiveUptime({ seconds, since }: { seconds: number; since: number }) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const elapsed = Math.max(0, Math.floor((now - since) / 1000))
  return <span className="tabular-nums">{formatDuration(Math.max(0, seconds) + elapsed)}</span>
}
