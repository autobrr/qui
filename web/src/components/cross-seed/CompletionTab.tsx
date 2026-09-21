/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedSettings, usePatchCrossSeedSettings } from "@/components/cross-seed/cross-seed-settings"
import { AutoResumeSwitch, SourceTagsField } from "@/components/cross-seed/SourceCardFields"
import { CompletionOverview } from "@/components/instances/preferences/CompletionOverview"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import type { CrossSeedAutomationSettings } from "@/types"
import { Loader2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

export function CompletionTab() {
  const { data: settings } = useCrossSeedSettings()
  if (!settings) {
    return null
  }
  return (
    <>
      <CompletionCard settings={settings} />
      <CompletionOverview />
    </>
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
