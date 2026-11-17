/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SectionedPage, type SectionNavGroup } from "@/components/layout/SectionedPage"
import { SharesTab } from "@/components/shares/SharesTab"
import { DEFAULT_TRANSFERS_TAB, TRANSFERS_NAV_GROUPS, type TransfersTab as TransfersSection } from "@/components/transfers/tabs"
import { TransfersTab } from "@/components/transfers/TransfersTab"
import { useTranslation } from "react-i18next"

interface TransfersPageProps {
  activeTab: TransfersSection | undefined
  onTabChange: (tab: TransfersSection | undefined, options: { replace: boolean }) => void
}

/** The Transfers page: the Transfers and Export runs, and the Shares, each in its own section. */
export function TransfersPage({ activeTab, onTabChange }: TransfersPageProps) {
  const { t } = useTranslation("transfers")
  const navGroups: SectionNavGroup<TransfersSection>[] = TRANSFERS_NAV_GROUPS.map(group => ({
    sections: group.tabs.map(entry => ({ value: entry.tab, label: t(entry.labelKey) })),
  }))

  return (
    <SectionedPage
      title={t("page.title")}
      description={t("page.description")}
      groups={navGroups}
      section={activeTab}
      defaultSection={DEFAULT_TRANSFERS_TAB}
      onSectionChange={onTabChange}
    >
      {(tab) => (
        <>
          {tab === "transfers" && <TransfersTab />}
          {tab === "shares" && <SharesTab />}
        </>
      )}
    </SectionedPage>
  )
}
