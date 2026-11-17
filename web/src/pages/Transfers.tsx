/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Page } from "@/components/layout/Page"
import { SharesTab } from "@/components/shares/SharesTab"
import { TransfersTab } from "@/components/transfers/TransfersTab"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useTranslation } from "react-i18next"

/** The Transfers page: the Transfers and Export runs, and the Shares. #4170 adds its route and sidebar link. */
export function TransfersPage() {
  const { t } = useTranslation("transfers")
  return (
    <Page title={t("page.title")} description={t("page.description")}>
      <Tabs defaultValue="transfers">
        <TabsList>
          <TabsTrigger value="transfers">{t("tabs.transfers")}</TabsTrigger>
          <TabsTrigger value="shares">{t("tabs.shares")}</TabsTrigger>
        </TabsList>
        <TabsContent value="transfers" className="mt-4">
          <TransfersTab />
        </TabsContent>
        <TabsContent value="shares" className="mt-4">
          <SharesTab />
        </TabsContent>
      </Tabs>
    </Page>
  )
}
