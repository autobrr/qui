/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  useActiveInstances,
  useAggregatedInstanceMetadata,
  useCrossSeedSettings,
  usePatchCrossSeedSettings
} from "@/components/cross-seed/cross-seed-settings"
import { TitleRescueSetting } from "@/components/cross-seed/TitleRescueSetting"
import { CategoryMappingRulesEditor } from "@/components/crossseed/CategoryMappingRulesEditor"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import type { CategoryMappingRule, CrossSeedAutomationSettings } from "@/types"
import { Loader2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

interface MatchingFormState {
  categoryMappingRules: CategoryMappingRule[]
  findIndividualEpisodes: boolean
  rescueTitleMismatches: boolean
  skipRecheck: boolean
  skipPieceBoundarySafetyCheck: boolean
}

export function MatchingTab() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return <MatchingCard settings={settings} />
}

function MatchingCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const patchSettings = usePatchCrossSeedSettings()
  const { activeInstanceIds } = useActiveInstances()
  const { data: metadata } = useAggregatedInstanceMetadata(activeInstanceIds)

  const [form, setForm] = useState<MatchingFormState>(() => ({
    categoryMappingRules: settings.categoryMappingRules ?? [],
    findIndividualEpisodes: settings.findIndividualEpisodes,
    rescueTitleMismatches: settings.rescueTitleMismatches,
    skipRecheck: settings.skipRecheck,
    skipPieceBoundarySafetyCheck: settings.skipPieceBoundarySafetyCheck,
  }))

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("tabs.matching")}</CardTitle>
        <CardDescription>{t("rules.tabDescriptions.matching")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
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
