/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Facts } from "@/components/kit/Facts"
import { Field } from "@/components/kit/Field"
import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { StatisticsDeleteHistory } from "@/components/settings/StatisticsDeleteHistory"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { api } from "@/lib/api"
import { formatBytes } from "@/lib/utils"
import type { DetailRetentionDays, TransferHistorySettings, TransferHistorySettingsStatus } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const TRANSFER_HISTORY_STATUS_QUERY_KEY = ["transfer-history", "settings-status"] as const

const RETENTIONS = [
  { days: 90, labelKey: "statistics.retention.days90" },
  { days: 400, labelKey: "statistics.retention.days400" },
  { days: 0, labelKey: "statistics.retention.forever" },
] as const satisfies readonly { days: DetailRetentionDays; labelKey: string }[]

export function StatisticsSettingsCard() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const statusQuery = useQuery({
    queryKey: TRANSFER_HISTORY_STATUS_QUERY_KEY,
    queryFn: () => api.getTransferHistorySettingsStatus(),
  })
  const mutation = useMutation({
    mutationFn: (settings: TransferHistorySettings) => api.updateTransferHistorySettings(settings),
    onSuccess: async (settings) => {
      // A status fetch that started before the save must not bring the old settings back.
      await queryClient.cancelQueries({ queryKey: TRANSFER_HISTORY_STATUS_QUERY_KEY })
      queryClient.setQueryData<TransferHistorySettingsStatus>(TRANSFER_HISTORY_STATUS_QUERY_KEY, (old) => old && { ...old, settings })
      toast.success(t("statistics.toasts.saved"))
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("statistics.toasts.saveFailed"))
    },
  })

  const status = statusQuery.data
  const save = (change: Partial<TransferHistorySettings>) => {
    if (status) {
      mutation.mutate({ ...status.settings, ...change })
    }
  }

  return (
    <Section title={t("statistics.cardTitle")} description={t("statistics.cardDescription")} queries={[statusQuery]}>
      {status && (
        <>
          <SwitchRow
            label={t("statistics.recording.label")}
            help={t("statistics.recording.description")}
            checked={status.settings.recording}
            disabled={mutation.isPending}
            onCheckedChange={(recording) => save({ recording })}
          />

          <Field label={t("statistics.retention.label")} description={t("statistics.retention.description")}>
            {(control) => (
              <Select
                value={String(status.settings.detailRetentionDays)}
                disabled={mutation.isPending}
                onValueChange={(value) => save({ detailRetentionDays: Number(value) as DetailRetentionDays })}
              >
                <SelectTrigger {...control} className="h-9 w-full sm:w-48">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {RETENTIONS.map((retention) => (
                    <SelectItem key={retention.days} value={String(retention.days)}>
                      {t(retention.labelKey)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>

          <Facts
            items={[{
              label: t("statistics.size.label"),
              value: <span className="tabular-nums" data-testid="statistics-history-size">{formatBytes(status.historyBytes)}</span>,
            }]}
          />

          <StatisticsDeleteHistory instances={status.instances} />
        </>
      )}
    </Section>
  )
}
