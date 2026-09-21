/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SaveFooter } from "@/components/cross-seed/SaveFooter"
import { AutoResumeSwitch, SourceTagsField } from "@/components/cross-seed/SourceCardFields"
import { CompletionOverview } from "@/components/instances/preferences/CompletionOverview"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { usePatchCrossSeedSettings } from "@/hooks/useCrossSeedSettings"
import type { CrossSeedAutomationSettings } from "@/types"
import { useState } from "react"
import { useTranslation } from "react-i18next"

export function CompletionTab({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const patchSettings = usePatchCrossSeedSettings()
  const [completionSearchTags, setCompletionSearchTags] = useState<string[]>(settings.completionSearchTags)
  const [skipAutoResumeCompletion, setSkipAutoResumeCompletion] = useState(settings.skipAutoResumeCompletion)

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>{t("completion.title")}</CardTitle>
          <CardDescription>{t("completion.description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-2">
            <SourceTagsField
              id="completion-search-tags"
              suggestions={[{ label: t("rules.tagging.tagCompletion"), value: "completion" }]}
              selected={completionSearchTags}
              onChange={setCompletionSearchTags}
              placeholder={t("rules.tagging.selectCompletionTags")}
              help={t("rules.tagging.completionTagsDescription")}
            />
            <AutoResumeSwitch
              id="auto-resume-completion"
              skip={skipAutoResumeCompletion}
              onSkipChange={setSkipAutoResumeCompletion}
            />
          </div>
        </CardContent>
        <SaveFooter pending={patchSettings.isPending} onSave={() => patchSettings.mutate({ completionSearchTags, skipAutoResumeCompletion })} />
      </Card>
      <CompletionOverview />
    </>
  )
}
