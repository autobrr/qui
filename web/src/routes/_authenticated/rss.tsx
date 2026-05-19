/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { RSSPage } from "@/pages/RSSPage"
import { createFileRoute } from "@tanstack/react-router"
import { z } from "zod"

const rssSearchSchema = z.object({
  tab: z.enum(["feeds", "rules"]).optional().catch(undefined),
  feedPath: z.string().optional().catch(undefined),
  ruleName: z.string().optional().catch(undefined),
})

export const Route = createFileRoute("/_authenticated/rss")({
  validateSearch: rssSearchSchema,
  component: RSSRoute,
})

function RSSRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const handleTabChange = (tab: "feeds" | "rules") => {
    navigate({
      search: (prev) => ({ ...prev, tab }),
      replace: true,
    })
  }

  const handleFeedSelect = (feedPath: string | undefined) => {
    navigate({
      search: (prev) => ({ ...prev, feedPath }),
      replace: true,
    })
  }

  const handleRuleSelect = (ruleName: string | undefined) => {
    navigate({
      search: (prev) => ({ ...prev, ruleName }),
      replace: true,
    })
  }

  return (
    <RSSPage
      activeTab={search.tab ?? "feeds"}
      selectedFeedPath={search.feedPath}
      selectedRuleName={search.ruleName}
      onTabChange={handleTabChange}
      onFeedSelect={handleFeedSelect}
      onRuleSelect={handleRuleSelect}
    />
  )
}
