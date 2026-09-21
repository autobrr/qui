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
  usePatchCrossSeedSettings
} from "@/components/cross-seed/cross-seed-settings"
import { CompletionOverview } from "@/components/instances/preferences/CompletionOverview"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Switch } from "@/components/ui/switch"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import type { CrossSeedAutomationSettings } from "@/types"
import { Loader2 } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

export function WebhookTab() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return (
    <>
      <WebhookCard settings={settings} />
      <CompletionCard settings={settings} />
      <CompletionOverview />
    </>
  )
}

interface WebhookFormState {
  webhookSourceCategories: string[]
  webhookSourceTags: string[]
  webhookSourceExcludeCategories: string[]
  webhookSourceExcludeTags: string[]
  webhookTags: string[]
  skipAutoResumeWebhook: boolean
}

function WebhookCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const patchSettings = usePatchCrossSeedSettings()
  const { activeInstances } = useActiveInstances()
  const activeInstanceIds = useMemo(() => instanceIdsOf(activeInstances), [activeInstances])
  const { data: sourceMetadata } = useAggregatedInstanceMetadata(activeInstanceIds)

  const [form, setForm] = useState<WebhookFormState>(() => ({
    webhookSourceCategories: settings.webhookSourceCategories ?? [],
    webhookSourceTags: settings.webhookSourceTags ?? [],
    webhookSourceExcludeCategories: settings.webhookSourceExcludeCategories ?? [],
    webhookSourceExcludeTags: settings.webhookSourceExcludeTags ?? [],
    webhookTags: settings.webhookTags,
    skipAutoResumeWebhook: settings.skipAutoResumeWebhook,
  }))

  const sourceTagNames = useMemo(() => sourceMetadata?.tags ?? [], [sourceMetadata])

  const categorySelectOptions = useMemo(
    () => buildCategorySelectOptions(
      sourceMetadata?.categories ?? {},
      form.webhookSourceCategories,
      form.webhookSourceExcludeCategories
    ),
    [form.webhookSourceCategories, form.webhookSourceExcludeCategories, sourceMetadata?.categories]
  )

  const tagSelectOptions = useMemo(
    () => buildTagSelectOptions(sourceTagNames, form.webhookSourceTags, form.webhookSourceExcludeTags),
    [sourceTagNames, form.webhookSourceTags, form.webhookSourceExcludeTags]
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("webhook.title")}</CardTitle>
        <CardDescription>{t("webhook.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-3">
            <Label>{t("automation.includeCategories")}</Label>
            <MultiSelect
              options={categorySelectOptions}
              selected={form.webhookSourceCategories}
              onChange={values => setForm(prev => ({ ...prev, webhookSourceCategories: values }))}
              placeholder={categorySelectOptions.length ? t("automation.allCategories") : t("automation.typeToAddCategories")}
              creatable
            />
            <p className="text-xs text-muted-foreground">
              {form.webhookSourceCategories.length === 0 ? t("automation.allCategoriesIncluded") : t("automation.selectedCategoriesMatched", { count: form.webhookSourceCategories.length })}
            </p>
          </div>

          <div className="space-y-3">
            <Label>{t("automation.includeTags")}</Label>
            <MultiSelect
              options={tagSelectOptions}
              selected={form.webhookSourceTags}
              onChange={values => setForm(prev => ({ ...prev, webhookSourceTags: values }))}
              placeholder={tagSelectOptions.length ? t("automation.allTags") : t("automation.typeToAddTags")}
              creatable
            />
            <p className="text-xs text-muted-foreground">
              {form.webhookSourceTags.length === 0 ? t("automation.allTagsIncluded") : t("automation.selectedTagsMatched", { count: form.webhookSourceTags.length })}
            </p>
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-3">
            <Label>{t("automation.excludeCategories")}</Label>
            <MultiSelect
              options={categorySelectOptions}
              selected={form.webhookSourceExcludeCategories}
              onChange={values => setForm(prev => ({ ...prev, webhookSourceExcludeCategories: values }))}
              placeholder={categorySelectOptions.length ? t("automation.none") : t("automation.typeToAddCategories")}
              creatable
            />
            <p className="text-xs text-muted-foreground">
              {form.webhookSourceExcludeCategories.length === 0 ? t("automation.noCategoriesExcluded") : t("automation.categoriesSkipped", { count: form.webhookSourceExcludeCategories.length })}
            </p>
          </div>

          <div className="space-y-3">
            <Label>{t("automation.excludeTags")}</Label>
            <MultiSelect
              options={tagSelectOptions}
              selected={form.webhookSourceExcludeTags}
              onChange={values => setForm(prev => ({ ...prev, webhookSourceExcludeTags: values }))}
              placeholder={tagSelectOptions.length ? t("automation.none") : t("automation.typeToAddTags")}
              creatable
            />
            <p className="text-xs text-muted-foreground">
              {form.webhookSourceExcludeTags.length === 0 ? t("automation.noTagsExcluded") : t("automation.tagsSkipped", { count: form.webhookSourceExcludeTags.length })}
            </p>
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          {t("webhook.emptyFiltersNote")}
        </p>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="webhook-tags">{t("sourceCard.crossSeedTags")}</Label>
              <FieldHelp>{t("rules.tagging.webhookTagsDescription")}</FieldHelp>
            </div>
            <MultiSelect
              options={[
                { label: t("rules.tagging.tagCrossSeed"), value: "cross-seed" },
                { label: t("rules.tagging.tagWebhook"), value: "webhook" },
                { label: t("rules.tagging.tagAutobrr"), value: "autobrr" },
              ]}
              selected={form.webhookTags}
              onChange={values => setForm(prev => ({ ...prev, webhookTags: normalizeStringList(values) }))}
              placeholder={t("rules.tagging.selectWebhookTags")}
              creatable
              onCreateOption={value => setForm(prev => ({ ...prev, webhookTags: normalizeStringList([...prev.webhookTags, value]) }))}
            />
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="auto-resume-webhook" className="font-medium">{t("sourceCard.autoResume")}</Label>
              <FieldHelp>{t("sourceCard.autoResumeHelp")} {t("rules.postInjection.webhookDescription")}</FieldHelp>
            </div>
            <Switch
              id="auto-resume-webhook"
              checked={!form.skipAutoResumeWebhook}
              onCheckedChange={value => setForm(prev => ({ ...prev, skipAutoResumeWebhook: !value }))}
            />
          </div>
        </div>
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

function CompletionCard({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const patchSettings = usePatchCrossSeedSettings()
  const [completionSearchTags, setCompletionSearchTags] = useState<string[]>(settings.completionSearchTags)
  const [skipAutoResumeCompletion, setSkipAutoResumeCompletion] = useState(settings.skipAutoResumeCompletion)

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("completion.title")}</CardTitle>
        <CardDescription>{t("completion.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="completion-search-tags">{t("sourceCard.crossSeedTags")}</Label>
              <FieldHelp>{t("rules.tagging.completionTagsDescription")}</FieldHelp>
            </div>
            <MultiSelect
              options={[
                { label: t("rules.tagging.tagCrossSeed"), value: "cross-seed" },
                { label: t("rules.tagging.tagCompletion"), value: "completion" },
              ]}
              selected={completionSearchTags}
              onChange={values => setCompletionSearchTags(normalizeStringList(values))}
              placeholder={t("rules.tagging.selectCompletionTags")}
              creatable
              onCreateOption={value => setCompletionSearchTags(prev => normalizeStringList([...prev, value]))}
            />
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="auto-resume-completion" className="font-medium">{t("sourceCard.autoResume")}</Label>
              <FieldHelp>{t("sourceCard.autoResumeHelp")}</FieldHelp>
            </div>
            <Switch
              id="auto-resume-completion"
              checked={!skipAutoResumeCompletion}
              onCheckedChange={value => setSkipAutoResumeCompletion(!value)}
            />
          </div>
        </div>
      </CardContent>
      <CardFooter className="flex justify-end">
        <Button
          className="min-h-11 md:min-h-9"
          onClick={() => patchSettings.mutate({ completionSearchTags, skipAutoResumeCompletion })}
          disabled={patchSettings.isPending}
        >
          {patchSettings.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("rules.saveChanges")}
        </Button>
      </CardFooter>
    </Card>
  )
}
