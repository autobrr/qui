/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  instanceIdsOf,
  useActiveInstances,
  useAggregatedInstanceMetadata,
  useCrossSeedSettings,
  useFormatDateValue,
  usePatchCrossSeedSettings
} from "@/components/cross-seed/cross-seed-settings"
import { HardlinkModeSettings } from "@/components/cross-seed/HardlinkModeSettings"
import { TitleRescueSetting } from "@/components/cross-seed/TitleRescueSetting"
import { CategoryMappingRulesEditor } from "@/components/crossseed/CategoryMappingRulesEditor"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import { buildCategorySelectOptions } from "@/lib/category-utils"
import { parseNonNegativeInt } from "@/lib/cross-seed-utils"
import type { CategoryMappingRule, CrossSeedAutomationSettings, CrossSeedAutomationSettingsPatch } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

type CategoryMode = "reuse" | "affix" | "indexer" | "custom"

interface RulesFormState {
  gazelleEnabled: boolean
  redactedApiKey: string
  orpheusApiKey: string
  categoryMappingRules: CategoryMappingRule[]
  findIndividualEpisodes: boolean
  rescueTitleMismatches: boolean
  skipRecheck: boolean
  skipPieceBoundarySafetyCheck: boolean
  categoryMode: CategoryMode
  categoryAffixMode: "prefix" | "suffix"
  categoryAffix: string
  customCategory: string
  inheritSourceTags: boolean
  pooledPartialCompletionEnabled: boolean
  autoResumeMaxDownloadMb: number
  runExternalProgramId: number | null
}

// Exactly one category mode is active; priority when the flags disagree: custom > indexer > affix > reuse.
function categoryModeOf(settings: CrossSeedAutomationSettings): CategoryMode {
  if (settings.useCustomCategory) return "custom"
  if (settings.useCategoryFromIndexer) return "indexer"
  if (settings.useCrossCategoryAffix) return "affix"
  return "reuse"
}

function seedRulesForm(settings: CrossSeedAutomationSettings): RulesFormState {
  return {
    gazelleEnabled: settings.gazelleEnabled,
    redactedApiKey: settings.redactedApiKey ?? "",
    orpheusApiKey: settings.orpheusApiKey ?? "",
    categoryMappingRules: settings.categoryMappingRules ?? [],
    findIndividualEpisodes: settings.findIndividualEpisodes,
    rescueTitleMismatches: settings.rescueTitleMismatches,
    skipRecheck: settings.skipRecheck,
    skipPieceBoundarySafetyCheck: settings.skipPieceBoundarySafetyCheck,
    categoryMode: categoryModeOf(settings),
    categoryAffixMode: settings.categoryAffixMode,
    categoryAffix: settings.categoryAffix,
    customCategory: settings.customCategory ?? "",
    inheritSourceTags: settings.inheritSourceTags,
    pooledPartialCompletionEnabled: settings.pooledPartialCompletionEnabled,
    autoResumeMaxDownloadMb: settings.autoResumeMaxDownloadMb,
    runExternalProgramId: settings.runExternalProgramId ?? null,
  }
}

function rulesPatch({ categoryMode, ...rest }: RulesFormState): CrossSeedAutomationSettingsPatch {
  return {
    ...rest,
    useCrossCategoryAffix: categoryMode === "affix",
    useCategoryFromIndexer: categoryMode === "indexer",
    useCustomCategory: categoryMode === "custom",
  }
}

export function RulesTab() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return <RulesCard settings={settings} />
}

function RulesCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const formatDateValue = useFormatDateValue()
  const patchSettings = usePatchCrossSeedSettings()
  const { activeInstances } = useActiveInstances()
  const activeInstanceIds = useMemo(() => instanceIdsOf(activeInstances), [activeInstances])
  const { data: metadata } = useAggregatedInstanceMetadata(activeInstanceIds)

  const [form, setForm] = useState<RulesFormState>(() => seedRulesForm(settings))
  const [customCategoryError, setCustomCategoryError] = useState("")

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

  const customCategorySelectOptions = useMemo(
    () => buildCategorySelectOptions(metadata?.categories ?? {}, form.customCategory ? [form.customCategory] : []),
    [form.customCategory, metadata?.categories]
  )

  const handleSave = () => {
    setCustomCategoryError("")
    if (form.categoryMode === "custom" && !form.customCategory.trim()) {
      setCustomCategoryError(t("toast.customCategoryRequired"))
      return
    }
    patchSettings.mutate(rulesPatch(form))
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("rules.title")}</CardTitle>
        <CardDescription>{t("rules.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <HardlinkModeSettings
          pooledPartialCompletionEnabled={form.pooledPartialCompletionEnabled}
          onPooledPartialCompletionEnabledChange={pooledPartialCompletionEnabled => setForm(prev => ({ ...prev, pooledPartialCompletionEnabled }))}
        />

        <div className="flex items-center gap-2 pt-1">
          <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60 shrink-0">{t("rules.sections.matching")}</span>
          <Separator className="flex-1" />
        </div>

        {/* Gazelle (OPS/RED) */}
        <div id="gazelle-settings" className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3 scroll-mt-24">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.gazelle.title")}</p>
            <p className="text-xs text-muted-foreground">
              {t("rules.gazelle.description")}
            </p>
          </div>

          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="gazelle-enabled" className="font-medium">{t("rules.gazelle.enableMatching")}</Label>
              <FieldHelp>{t("rules.gazelle.enableDescription")}</FieldHelp>
            </div>
            <Switch
              id="gazelle-enabled"
              checked={form.gazelleEnabled}
              onCheckedChange={value => setForm(prev => ({ ...prev, gazelleEnabled: !!value }))}
            />
          </div>

          <div className="grid gap-4 md:grid-cols-2 pt-3 border-t border-border/50">
            <div className="space-y-2">
              <div className="flex items-center gap-1.5">
                <Label htmlFor="gazelle-red-api-key">{t("rules.gazelle.redactedApiKey")}</Label>
                <FieldHelp>{t("rules.gazelle.redDescription")}</FieldHelp>
              </div>
              <Input
                id="gazelle-red-api-key"
                type="password"
                value={form.redactedApiKey}
                data-1p-ignore="true"
                onChange={event => setForm(prev => ({ ...prev, redactedApiKey: event.target.value }))}
                placeholder={form.gazelleEnabled ? t("rules.gazelle.pasteRedKey") : t("rules.gazelle.enableToConfigure")}
                disabled={!form.gazelleEnabled}
                autoComplete="off"
              />
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-1.5">
                <Label htmlFor="gazelle-ops-api-key">{t("rules.gazelle.orpheusApiKey")}</Label>
                <FieldHelp>{t("rules.gazelle.opsDescription")}</FieldHelp>
              </div>
              <Input
                id="gazelle-ops-api-key"
                type="password"
                value={form.orpheusApiKey}
                data-1p-ignore="true"
                onChange={event => setForm(prev => ({ ...prev, orpheusApiKey: event.target.value }))}
                placeholder={form.gazelleEnabled ? t("rules.gazelle.pasteOpsKey") : t("rules.gazelle.enableToConfigure")}
                disabled={!form.gazelleEnabled}
                autoComplete="off"
              />
            </div>
          </div>
        </div>

        {/* Search category rules */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.matching.categoryMapping.title")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.matching.categoryMapping.description")}</p>
          </div>
          <CategoryMappingRulesEditor
            value={form.categoryMappingRules}
            onChange={rules => setForm(prev => ({ ...prev, categoryMappingRules: rules }))}
            categoryMetadata={metadata?.categories ?? {}}
          />
        </div>

        {/* Safety & validation */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.safety.title")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.safety.description")}</p>
          </div>
          <TitleRescueSetting
            checked={form.rescueTitleMismatches && !form.skipRecheck}
            disabled={form.skipRecheck}
            onCheckedChange={rescueTitleMismatches => setForm(prev => ({ ...prev, rescueTitleMismatches }))}
          />
          <div className="flex items-center justify-between gap-3 pt-3 border-t border-border/50">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="skip-recheck" className="font-medium">{t("rules.safety.skipRecheck")}</Label>
              <FieldHelp>{t("rules.safety.skipRecheckDescription")}</FieldHelp>
            </div>
            <Switch
              id="skip-recheck"
              checked={form.skipRecheck}
              onCheckedChange={value => setForm(prev => ({ ...prev, skipRecheck: !!value }))}
            />
          </div>
          <div className="flex items-center justify-between gap-3 pt-3 border-t border-border/50">
            <div className="space-y-0.5">
              <Label
                htmlFor="skip-piece-boundary-check"
                className={`font-medium ${form.skipPieceBoundarySafetyCheck ? "text-yellow-600 dark:text-yellow-500" : "text-green-600 dark:text-green-500"}`}
              >
                {form.skipPieceBoundarySafetyCheck ? t("rules.safety.pieceBoundaryDisabled") : t("rules.safety.pieceBoundaryEnabled")}
              </Label>
              <p className="text-xs text-muted-foreground">
                {form.skipPieceBoundarySafetyCheck ? t("rules.safety.pieceBoundaryDisabledDescription") : t("rules.safety.pieceBoundaryEnabledDescription")}
              </p>
            </div>
            <Switch
              id="skip-piece-boundary-check"
              checked={!form.skipPieceBoundarySafetyCheck}
              onCheckedChange={value => setForm(prev => ({ ...prev, skipPieceBoundarySafetyCheck: !value }))}
            />
          </div>
        </div>

        {/* Episodes */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.matching.title")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.matching.description")}</p>
          </div>
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="global-find-individual-episodes" className="font-medium">{t("rules.matching.crossSeedEpisodes")}</Label>
            <Switch
              id="global-find-individual-episodes"
              checked={form.findIndividualEpisodes}
              onCheckedChange={value => setForm(prev => ({ ...prev, findIndividualEpisodes: !!value }))}
            />
          </div>
        </div>

        <div className="flex items-center gap-2 pt-1">
          <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60 shrink-0">{t("rules.sections.organization")}</span>
          <Separator className="flex-1" />
        </div>

        {/* Categories */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.categories.title")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.categories.description")}</p>
          </div>
          <RadioGroup
            value={form.categoryMode}
            onValueChange={(value) => setForm(prev => ({ ...prev, categoryMode: value as CategoryMode }))}
            className="space-y-3"
          >
            <div className="flex items-start gap-3">
              <RadioGroupItem value="reuse" id="category-reuse" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-reuse" className="font-medium cursor-pointer">{t("rules.categories.reuseCategory")}</Label>
                  <FieldHelp>{t("rules.categories.reuseCategoryHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.reuseCategoryDescription")}</p>
              </div>
            </div>
            <div className="flex items-start gap-3">
              <RadioGroupItem value="affix" id="category-affix" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-affix" className="font-medium cursor-pointer">{t("rules.categories.categoryAffix")}</Label>
                  <FieldHelp>{t("rules.categories.categoryAffixHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.categoryAffixDescription")}</p>
                {form.categoryMode === "affix" && (
                  <div className="flex flex-wrap items-center gap-3 mt-2">
                    <div className="inline-flex h-9 items-center justify-center rounded-lg bg-muted p-1 text-muted-foreground">
                      <button
                        type="button"
                        onClick={() => setForm(prev => ({ ...prev, categoryAffixMode: "prefix" }))}
                        className={`inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium transition-all ${form.categoryAffixMode === "prefix" ? "bg-background text-primary shadow-sm" : "hover:bg-background/50 hover:text-foreground"}`}
                      >
                        {t("rules.categories.prefix")}
                      </button>
                      <button
                        type="button"
                        onClick={() => setForm(prev => ({ ...prev, categoryAffixMode: "suffix" }))}
                        className={`inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium transition-all ${form.categoryAffixMode === "suffix" ? "bg-background text-primary shadow-sm" : "hover:bg-background/50 hover:text-foreground"}`}
                      >
                        {t("rules.categories.suffix")}
                      </button>
                    </div>
                    <Input
                      value={form.categoryAffix}
                      onChange={e => setForm(prev => ({ ...prev, categoryAffix: e.target.value }))}
                      placeholder={form.categoryAffixMode === "prefix" ? "cross-seed/" : ".cross"}
                      className="max-w-[140px] h-9"
                    />
                  </div>
                )}
              </div>
            </div>
            <div className="flex items-start gap-3">
              <RadioGroupItem value="indexer" id="category-indexer" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-indexer" className="font-medium cursor-pointer">{t("rules.categories.indexerCategory")}</Label>
                  <FieldHelp>{t("rules.categories.indexerCategoryHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.indexerCategoryDescription")}</p>
              </div>
            </div>
            <div className="flex items-start gap-3">
              <RadioGroupItem value="custom" id="category-custom" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-custom" className="font-medium cursor-pointer">{t("rules.categories.customCategory")}</Label>
                  <FieldHelp>{t("rules.categories.customCategoryHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.customCategoryDescription")}</p>
                {form.categoryMode === "custom" && (
                  <>
                    <MultiSelect
                      options={customCategorySelectOptions}
                      selected={form.customCategory ? [form.customCategory] : []}
                      onChange={values => {
                        setForm(prev => ({ ...prev, customCategory: values[0] ?? "" }))
                        setCustomCategoryError("")
                      }}
                      placeholder={t("rules.categories.selectOrTypeCategory")}
                      className={`mt-2 max-w-xs ${customCategoryError ? "border-destructive" : ""}`}
                      creatable
                      single
                      onCreateOption={value => {
                        setForm(prev => ({ ...prev, customCategory: value }))
                        setCustomCategoryError("")
                      }}
                    />
                    {customCategoryError && (
                      <p className="text-sm text-destructive">{customCategoryError}</p>
                    )}
                  </>
                )}
              </div>
            </div>
          </RadioGroup>
        </div>

        {/* Tagging */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.tagging.title")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.tagging.description")}</p>
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="inherit-source-tags" className="font-medium">{t("rules.tagging.inheritSourceTags")}</Label>
              <FieldHelp>{t("rules.tagging.inheritSourceTagsDescription")}</FieldHelp>
            </div>
            <Switch
              id="inherit-source-tags"
              checked={form.inheritSourceTags}
              onCheckedChange={value => setForm(prev => ({ ...prev, inheritSourceTags: !!value }))}
            />
          </div>
        </div>

        <div className="flex items-center gap-2 pt-1">
          <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60 shrink-0">{t("rules.sections.afterInjection")}</span>
          <Separator className="flex-1" />
        </div>

        {/* Post-add behavior */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-4">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.postInjection.title")}</p>
            <p className="text-xs text-muted-foreground">
              {t("rules.postInjection.description")}
            </p>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
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
          </div>

          <div className="space-y-2 pt-3 border-t border-border/50">
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
      <CardFooter className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
        <Button className="min-h-11 md:min-h-9" onClick={handleSave} disabled={patchSettings.isPending}>
          {patchSettings.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("rules.saveGlobalSettings")}
        </Button>
      </CardFooter>
    </Card>
  )
}
