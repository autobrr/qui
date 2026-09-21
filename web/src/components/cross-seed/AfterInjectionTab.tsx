/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedSettings, useFormatDateValue, usePatchCrossSeedSettings } from "@/components/cross-seed/cross-seed-settings"
import { HardlinkModeSettings } from "@/components/cross-seed/HardlinkModeSettings"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { api } from "@/lib/api"
import { parseNonNegativeInt } from "@/lib/cross-seed-utils"
import type { CrossSeedAutomationSettings } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

interface AfterInjectionFormState {
  pooledPartialCompletionEnabled: boolean
  autoResumeMaxDownloadMb: number
  runExternalProgramId: number | null
}

export function AfterInjectionTab() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return <AfterInjectionCard settings={settings} />
}

function AfterInjectionCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const formatDateValue = useFormatDateValue()
  const patchSettings = usePatchCrossSeedSettings()

  const [form, setForm] = useState<AfterInjectionFormState>(() => ({
    pooledPartialCompletionEnabled: settings.pooledPartialCompletionEnabled,
    autoResumeMaxDownloadMb: settings.autoResumeMaxDownloadMb,
    runExternalProgramId: settings.runExternalProgramId ?? null,
  }))

  const { data: externalPrograms } = useQuery({
    queryKey: ["external-programs"],
    queryFn: () => api.listExternalPrograms(),
  })
  const enabledExternalPrograms = useMemo(
    () => (externalPrograms ?? []).filter(program => program.enabled),
    [externalPrograms]
  )

  const { data: searchCacheStats } = useQuery({
    queryKey: ["torznab", "search-cache", "stats", "cross-seed"],
    queryFn: () => api.getTorznabSearchCacheStats(),
    staleTime: 60 * 1000,
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("rules.postInjection.title")}</CardTitle>
        <CardDescription>{t("rules.postInjection.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <HardlinkModeSettings
          pooledPartialCompletionEnabled={form.pooledPartialCompletionEnabled}
          onPooledPartialCompletionEnabledChange={pooledPartialCompletionEnabled => setForm(prev => ({ ...prev, pooledPartialCompletionEnabled }))}
        />

        <div className="space-y-2">
          <div className="flex items-center gap-1.5">
            <Label htmlFor="global-auto-resume-max-download">{t("rules.postInjection.maxAutoResumeDownload")}</Label>
            <FieldHelp>{t("rules.postInjection.maxAutoResumeDownloadDescription")}</FieldHelp>
          </div>
          <Input
            id="global-auto-resume-max-download"
            type="number"
            min="0"
            step="1"
            value={form.autoResumeMaxDownloadMb}
            onChange={event => setForm(prev => ({ ...prev, autoResumeMaxDownloadMb: parseNonNegativeInt(event.target.value) }))}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="global-external-program">{t("rules.postInjection.externalProgram")}</Label>
          <Select
            value={form.runExternalProgramId ? String(form.runExternalProgramId) : "none"}
            onValueChange={(value) => setForm(prev => ({ ...prev, runExternalProgramId: value === "none" ? null : Number(value) }))}
            disabled={!enabledExternalPrograms.length}
          >
            <SelectTrigger className="w-full">
              <SelectValue placeholder={
                !enabledExternalPrograms.length ? t("rules.postInjection.noExternalPrograms") : t("rules.postInjection.selectExternalProgram")
              } />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">{t("rules.postInjection.noneOption")}</SelectItem>
              {enabledExternalPrograms.map(program => (
                <SelectItem key={program.id} value={String(program.id)}>
                  {program.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {t("rules.postInjection.externalProgramDescription")}
            {!enabledExternalPrograms.length && (
              <> <Link to="/settings" search={{ tab: "external-programs" }} className="font-medium text-primary underline-offset-4 hover:underline">{t("rules.postInjection.configureExternalPrograms")}</Link> {t("rules.postInjection.configureExternalProgramsSuffix")}</>
            )}
          </p>
        </div>

        {searchCacheStats && (
          <div className="rounded-lg border border-dashed border-border/70 bg-muted/60 p-3 text-xs text-muted-foreground">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={searchCacheStats.enabled ? "secondary" : "outline"}>
                {searchCacheStats.enabled ? t("rules.cache.cacheEnabled") : t("rules.cache.cacheDisabled")}
              </Badge>
              <span>{t("rules.cache.ttlMinutes", { ttl: searchCacheStats.ttlMinutes })}</span>
              <span>{t("rules.cache.cachedSearches", { count: searchCacheStats.entries })}</span>
              <span>{t("rules.cache.lastUsed", { timestamp: formatDateValue(searchCacheStats.lastUsedAt) })}</span>
              <Button variant="link" size="xs" className="px-0 ml-auto" asChild>
                <Link to="/settings" search={{ tab: "search-cache" }}>
                  {t("rules.cache.manageCacheSettings")}
                </Link>
              </Button>
            </div>
          </div>
        )}
      </CardContent>
      <CardFooter className="flex justify-end">
        <Button className="min-h-11 md:min-h-9" onClick={() => patchSettings.mutate(form)} disabled={patchSettings.isPending}>
          {patchSettings.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("rules.saveChanges")}
        </Button>
      </CardFooter>
    </Card>
  )
}
