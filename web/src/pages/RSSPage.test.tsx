/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TooltipProvider } from "@/components/ui/tooltip"
import { ArticlesPanel, EditRuleDialog } from "@/pages/RSSPage"
import type { RSSArticle, RSSAutoDownloadRule } from "@/types"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  markAsRead: vi.fn(),
  setRule: { mutateAsync: vi.fn().mockResolvedValue(undefined), isPending: false },
  virtualizer: {
    getTotalSize: () => 156,
    getVirtualItems: () => [
      { index: 0, key: "article-0", start: 0, size: 52, end: 52, lane: 0 },
      { index: 1, key: "article-1", start: 52, size: 52, end: 104, lane: 0 },
      { index: 2, key: "article-2", start: 104, size: 52, end: 156, lane: 0 },
    ],
    measureElement: vi.fn(),
  },
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: () => "date" }),
}))

vi.mock("@/hooks/useRSS", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/hooks/useRSS")>(),
  useMarkRSSAsRead: () => ({ mutateAsync: mocks.markAsRead }),
  useSetRSSRule: () => mocks.setRule,
}))

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: () => mocks.virtualizer,
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("ArticlesPanel", () => {
  it("mounts only virtual rows for a synthetic 1,000-article feed", () => {
    const articles: RSSArticle[] = Array.from({ length: 1000 }, (_, index) => ({
      id: `article-${index}`,
      date: new Date(2026, 0, 1, 0, 0, 1000 - index).toISOString(),
      title: `Article ${index}`,
      description: `Synthetic description ${index}`,
      isRead: false,
    }))

    render(
      <ArticlesPanel
        instanceId={1}
        feed={{ uid: "feed", url: "https://example.invalid/feed", hasError: false, isLoading: false, articles }}
        feedPath="Synthetic Feed"
        onDownload={() => {}}
      />
    )

    expect(document.querySelectorAll("[data-index]")).toHaveLength(3)
    expect(screen.queryByText("Article 0")).not.toBeNull()
    expect(screen.queryByText("Article 999")).toBeNull()
  })
})


describe("EditRuleDialog", () => {
  it.each([false, true])("retains seed_mode=%s and share_limits_mode on an unrelated edit", async (seedMode) => {
    const rule: RSSAutoDownloadRule = {
      enabled: true,
      priority: 0,
      useRegex: false,
      mustContain: "Before",
      mustNotContain: "",
      affectedFeeds: [],
      ignoreDays: 0,
      smartFilter: false,
      torrentParams: { seed_mode: seedMode, skip_checking: seedMode, share_limits_mode: "MatchAll" },
    }
    render(
      <TooltipProvider>
        <EditRuleDialog
          instanceId={1}
          open
          onOpenChange={() => {}}
          ruleName="Synthetic"
          rule={rule}
          feedsData={{}}
          categories={{}}
          tags={[]}
        />
      </TooltipProvider>
    )

    fireEvent.change(screen.getByLabelText("ruleForm.mustContain"), { target: { value: "After" } })
    fireEvent.click(screen.getByRole("button", { name: "editRuleDialog.saveChanges" }))

    await waitFor(() => expect(mocks.setRule.mutateAsync).toHaveBeenCalledWith({
      name: "Synthetic",
      rule: expect.objectContaining({
        mustContain: "After",
        torrentParams: expect.objectContaining({ seed_mode: seedMode, skip_checking: seedMode, share_limits_mode: "MatchAll" }),
      }),
    }))
  })
})
