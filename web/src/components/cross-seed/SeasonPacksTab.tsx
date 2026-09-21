/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  instanceIdsOf,
  normalizeStringList,
  useActiveInstances,
  useAggregatedInstanceMetadata,
  useCrossSeedSettings,
  useFormatDateValue,
  usePatchCrossSeedSettings
} from "@/components/cross-seed/cross-seed-settings"
import { SeasonPackRunsPanel } from "@/components/cross-seed/SeasonPackRunsPanel"
import { SeasonPackCategoryRulesEditor } from "@/components/crossseed/SeasonPackCategoryRulesEditor"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import { buildCategorySelectOptions } from "@/lib/category-utils"
import type { CrossSeedAutomationSettings, SeasonPackCategoryRule } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

interface SeasonPackFormState {
  seasonPackEnabled: boolean
  seasonPackAutomationEnabled: boolean
  seasonPackSkipRepackCompare: boolean
  seasonPackSimplifyHdrCompare: boolean
  seasonPackSimplifyWebCompare: boolean
  seasonPackSkipYearCompare: boolean
  seasonPackCoverageThreshold: number
  seasonPackTags: string[]
  seasonPackCategory: string
  seasonPackCategoryRules: SeasonPackCategoryRule[]
  seasonPackTvdbApiKey: string
  seasonPackTvdbPin: string
}

export function SeasonPacksTab() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return <SeasonPacksCard settings={settings} />
}

function SeasonPacksCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const formatDateValue = useFormatDateValue()
  const patchSettings = usePatchCrossSeedSettings()
  const { activeInstances } = useActiveInstances()
  const activeInstanceIds = useMemo(() => instanceIdsOf(activeInstances), [activeInstances])
  const { data: metadata } = useAggregatedInstanceMetadata(activeInstanceIds)

  const [form, setForm] = useState<SeasonPackFormState>(() => ({
    seasonPackEnabled: settings.seasonPackEnabled,
    seasonPackAutomationEnabled: settings.seasonPackAutomationEnabled,
    seasonPackSkipRepackCompare: settings.seasonPackSkipRepackCompare,
    seasonPackSimplifyHdrCompare: settings.seasonPackSimplifyHdrCompare,
    seasonPackSimplifyWebCompare: settings.seasonPackSimplifyWebCompare,
    seasonPackSkipYearCompare: settings.seasonPackSkipYearCompare,
    seasonPackCoverageThreshold: settings.seasonPackCoverageThreshold,
    seasonPackTags: settings.seasonPackTags,
    seasonPackCategory: settings.seasonPackCategory,
    seasonPackCategoryRules: settings.seasonPackCategoryRules ?? [],
    seasonPackTvdbApiKey: settings.seasonPackTvdbApiKey ?? "",
    seasonPackTvdbPin: settings.seasonPackTvdbPin ?? "",
  }))

  const {
    data: seasonPackRuns = [],
    isLoading: seasonPackRunsLoading,
    isFetching: seasonPackRunsFetching,
    error: seasonPackRunsError,
    refetch: refetchSeasonPackRuns,
  } = useQuery({
    queryKey: ["cross-seed", "season-pack", "runs"],
    queryFn: () => api.listSeasonPackRuns({ limit: 50 }),
  })

  const fallbackCategorySelectOptions = useMemo(
    () => buildCategorySelectOptions(metadata?.categories ?? {}, form.seasonPackCategory ? [form.seasonPackCategory] : []),
    [form.seasonPackCategory, metadata?.categories]
  )

  const inactive = !form.seasonPackEnabled && !form.seasonPackAutomationEnabled

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("rules.seasonPack.title")}</CardTitle>
        <CardDescription>{t("rules.seasonPack.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-1.5">
            <Label htmlFor="season-pack-enabled" className="font-medium">{t("rules.seasonPack.enable")}</Label>
            <FieldHelp>{t("rules.seasonPack.enableDescription")}</FieldHelp>
          </div>
          <Switch
            id="season-pack-enabled"
            checked={form.seasonPackEnabled}
            onCheckedChange={value => setForm(prev => ({ ...prev, seasonPackEnabled: !!value }))}
          />
        </div>
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-1.5">
            <Label htmlFor="season-pack-automation-enabled" className="font-medium">{t("rules.seasonPack.enableAutomation")}</Label>
            <FieldHelp>{t("rules.seasonPack.enableAutomationDescription")}</FieldHelp>
          </div>
          <Switch
            id="season-pack-automation-enabled"
            checked={form.seasonPackAutomationEnabled}
            onCheckedChange={value => setForm(prev => ({ ...prev, seasonPackAutomationEnabled: !!value }))}
          />
        </div>
        <div className="space-y-2 pt-3 border-t border-border/50">
          <div className="flex items-center gap-1.5">
            <Label htmlFor="season-pack-threshold">{t("rules.seasonPack.coverageThreshold")}</Label>
            <FieldHelp>{t("rules.seasonPack.coverageThresholdDescription")}</FieldHelp>
          </div>
          <Input
            id="season-pack-threshold"
            type="number"
            min={1}
            max={100}
            className="w-32"
            value={Math.round(form.seasonPackCoverageThreshold * 100)}
            onChange={event => {
              const parsed = Number(event.target.value)
              if (!Number.isNaN(parsed)) {
                setForm(prev => ({ ...prev, seasonPackCoverageThreshold: Math.max(1, Math.min(100, parsed)) / 100 }))
              }
            }}
          />
        </div>

        <div className="space-y-3 pt-3 border-t border-border/50">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.seasonPack.matchingTitle")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.seasonPack.matchingDescription")}</p>
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="season-pack-skip-repack-compare" className="font-medium">{t("rules.seasonPack.ignoreRepackProper")}</Label>
              <FieldHelp>{t("rules.seasonPack.ignoreRepackProperDescription")}</FieldHelp>
            </div>
            <Switch
              id="season-pack-skip-repack-compare"
              checked={form.seasonPackSkipRepackCompare}
              onCheckedChange={value => setForm(prev => ({ ...prev, seasonPackSkipRepackCompare: !!value }))}
              disabled={inactive}
            />
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="season-pack-simplify-hdr-compare" className="font-medium">{t("rules.seasonPack.simplifyHdr")}</Label>
              <FieldHelp>{t("rules.seasonPack.simplifyHdrDescription")}</FieldHelp>
            </div>
            <Switch
              id="season-pack-simplify-hdr-compare"
              checked={form.seasonPackSimplifyHdrCompare}
              onCheckedChange={value => setForm(prev => ({ ...prev, seasonPackSimplifyHdrCompare: !!value }))}
              disabled={inactive}
            />
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="season-pack-simplify-web-compare" className="font-medium">{t("rules.seasonPack.simplifyWeb")}</Label>
              <FieldHelp>{t("rules.seasonPack.simplifyWebDescription")}</FieldHelp>
            </div>
            <Switch
              id="season-pack-simplify-web-compare"
              checked={form.seasonPackSimplifyWebCompare}
              onCheckedChange={value => setForm(prev => ({ ...prev, seasonPackSimplifyWebCompare: !!value }))}
              disabled={inactive}
            />
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="season-pack-skip-year-compare" className="font-medium">{t("rules.seasonPack.ignoreYear")}</Label>
              <FieldHelp>{t("rules.seasonPack.ignoreYearDescription")}</FieldHelp>
            </div>
            <Switch
              id="season-pack-skip-year-compare"
              checked={form.seasonPackSkipYearCompare}
              onCheckedChange={value => setForm(prev => ({ ...prev, seasonPackSkipYearCompare: !!value }))}
              disabled={inactive}
            />
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2 pt-3 border-t border-border/50">
          <div className="space-y-2">
            <Label htmlFor="season-pack-tvdb-api-key">{t("rules.seasonPack.tvdbApiKey")}</Label>
            <Input
              id="season-pack-tvdb-api-key"
              type="password"
              value={form.seasonPackTvdbApiKey}
              data-1p-ignore="true"
              onChange={event => setForm(prev => ({ ...prev, seasonPackTvdbApiKey: event.target.value }))}
              placeholder={inactive ? t("rules.seasonPack.enableToConfigure") : t("rules.seasonPack.pasteTvdbApiKey")}
              disabled={inactive}
              autoComplete="off"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="season-pack-tvdb-pin">{t("rules.seasonPack.tvdbPin")}</Label>
            <Input
              id="season-pack-tvdb-pin"
              type="password"
              value={form.seasonPackTvdbPin}
              data-1p-ignore="true"
              onChange={event => setForm(prev => ({ ...prev, seasonPackTvdbPin: event.target.value }))}
              placeholder={inactive ? t("rules.seasonPack.enableToConfigure") : t("rules.seasonPack.pasteTvdbPin")}
              disabled={inactive}
              autoComplete="off"
            />
          </div>
        </div>
        <p className="text-xs text-muted-foreground">
          {t("rules.seasonPack.tvdbDescription")}
        </p>

        <div className="space-y-4 pt-3 border-t border-border/50">
          <SeasonPackCategoryRulesEditor
            value={form.seasonPackCategoryRules}
            onChange={rules => setForm(prev => ({ ...prev, seasonPackCategoryRules: rules }))}
            categoryMetadata={metadata?.categories ?? {}}
            disabled={inactive}
          />
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="season-pack-category">{t("rules.seasonPack.categoryRouting.fallbackLabel")}</Label>
              <FieldHelp>
                {t("rules.seasonPack.categoryRouting.fallbackDescriptionBefore")}<code>tv-hd</code>{t("rules.seasonPack.categoryRouting.fallbackDescriptionAfter")}
              </FieldHelp>
            </div>
            <MultiSelect
              options={fallbackCategorySelectOptions}
              selected={form.seasonPackCategory ? [form.seasonPackCategory] : []}
              onChange={values => setForm(prev => ({ ...prev, seasonPackCategory: values[0] ?? "" }))}
              onCreateOption={value => setForm(prev => ({ ...prev, seasonPackCategory: value }))}
              placeholder={inactive ? t("rules.seasonPack.enableToConfigure") : t("rules.categories.selectOrTypeCategory")}
              className="max-w-sm"
              creatable
              single
              disabled={inactive}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="season-pack-tags">{t("sourceCard.crossSeedTags")}</Label>
            <MultiSelect
              options={[
                { label: t("rules.tagging.tagCrossSeed"), value: "cross-seed" },
                { label: t("rules.tagging.tagSeasonPack"), value: "season-pack" },
              ]}
              selected={form.seasonPackTags}
              onChange={values => setForm(prev => ({ ...prev, seasonPackTags: normalizeStringList(values) }))}
              placeholder={t("rules.tagging.selectSeasonPackTags")}
              className="max-w-sm"
              creatable
              onCreateOption={value => setForm(prev => ({ ...prev, seasonPackTags: normalizeStringList([...prev.seasonPackTags, value]) }))}
            />
          </div>
        </div>

        <SeasonPackRunsPanel
          runs={seasonPackRuns}
          isLoading={seasonPackRunsLoading}
          isFetching={seasonPackRunsFetching}
          error={seasonPackRunsError}
          onRefresh={() => { void refetchSeasonPackRuns() }}
          formatDateValue={formatDateValue}
        />
      </CardContent>
      <CardFooter className="flex justify-end">
        <Button className="min-h-11 md:min-h-9" onClick={() => patchSettings.mutate({ ...form })} disabled={patchSettings.isPending}>
          {patchSettings.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("rules.saveChanges")}
        </Button>
      </CardFooter>
    </Card>
  )
}
