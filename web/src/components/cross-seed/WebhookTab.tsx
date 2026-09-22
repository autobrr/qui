/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SaveFooter } from "@/components/cross-seed/SaveFooter"
import { AutoResumeSwitch, SourceFilterFields, SourceTagsField } from "@/components/cross-seed/SourceCardFields"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  useActiveInstances,
  useAggregatedInstanceMetadata,
  usePatchCrossSeedSettings
} from "@/hooks/useCrossSeedSettings"
import type { CrossSeedAutomationSettings } from "@/types"
import { useState } from "react"
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

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("webhook.title")}</CardTitle>
        <CardDescription>{t("webhook.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <SourceFilterFields
          filters={{
            categories: form.webhookSourceCategories,
            tags: form.webhookSourceTags,
            excludeCategories: form.webhookSourceExcludeCategories,
            excludeTags: form.webhookSourceExcludeTags,
          }}
          onChange={filters => setForm(prev => ({
            ...prev,
            webhookSourceCategories: filters.categories,
            webhookSourceTags: filters.tags,
            webhookSourceExcludeCategories: filters.excludeCategories,
            webhookSourceExcludeTags: filters.excludeTags,
          }))}
          metadata={sourceMetadata}
        />

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
