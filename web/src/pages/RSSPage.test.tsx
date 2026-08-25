/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ArticlesPanel } from "@/pages/RSSPage"
import type { RSSArticle } from "@/types"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  markAsRead: vi.fn(),
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
}))

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: () => mocks.virtualizer,
}))

afterEach(cleanup)

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
