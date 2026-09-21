/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SaveFooter } from "@/components/cross-seed/SaveFooter"
import { AutoResumeSwitch, SourceTagsField } from "@/components/cross-seed/SourceCardFields"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import {
  useActiveInstances,
  useAggregatedInstanceMetadata,
  usePatchCrossSeedSettings
} from "@/hooks/useCrossSeedSettings"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import type { CrossSeedAutomationSettings } from "@/types"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

interface WebhookFormState {
  webhookSourceCategories: string[]
  webhookSourceTags: string[]
  webhookSourceExcludeCategories: string[]
  webhookSourceExcludeTags: string[]
  webhookTags: string[]
  skipAutoResumeWebhook: boolean
}

export function WebhookTab({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const patchSettings = usePatchCrossSeedSettings()
  const { activeInstanceIds } = useActiveInstances()
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
          <SourceTagsField
            id="webhook-tags"
            suggestions={[
              { label: t("rules.tagging.tagWebhook"), value: "webhook" },
              { label: t("rules.tagging.tagAutobrr"), value: "autobrr" },
            ]}
            selected={form.webhookTags}
            onChange={webhookTags => setForm(prev => ({ ...prev, webhookTags }))}
            placeholder={t("rules.tagging.selectWebhookTags")}
            help={t("rules.tagging.webhookTagsDescription")}
          />
          <AutoResumeSwitch
            id="auto-resume-webhook"
            skip={form.skipAutoResumeWebhook}
            onSkipChange={skipAutoResumeWebhook => setForm(prev => ({ ...prev, skipAutoResumeWebhook }))}
            help={t("rules.postInjection.webhookDescription")}
          />
        </div>
      </CardContent>
      <SaveFooter pending={patchSettings.isPending} onSave={() => patchSettings.mutate({ ...form })} />
    </Card>
  )
}
