/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { StatisticsDeleteHistory } from "@/components/settings/StatisticsDeleteHistory"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import { formatBytes } from "@/lib/utils"
import type { DetailRetentionDays, TransferHistorySettings, TransferHistorySettingsStatus } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
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
    <Card>
      <CardHeader>
        <CardTitle>{t("statistics.cardTitle")}</CardTitle>
        <CardDescription>{t("statistics.cardDescription")}</CardDescription>
      </CardHeader>
      <CardContent>
        {statusQuery.isPending && <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />}
        {statusQuery.isError && <p className="text-sm text-destructive">{t("statistics.loadFailed")}</p>}
        {status && (
          <div className="space-y-6">
            <div className="flex items-center justify-between gap-4">
              <div className="flex items-center gap-1.5">
                <Label htmlFor="statistics-recording">{t("statistics.recording.label")}</Label>
                <FieldHelp>{t("statistics.recording.description")}</FieldHelp>
              </div>
              <Switch
                id="statistics-recording"
                checked={status.settings.recording}
                disabled={mutation.isPending}
                onCheckedChange={(recording) => save({ recording })}
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="statistics-retention">{t("statistics.retention.label")}</Label>
              <Select
                value={String(status.settings.detailRetentionDays)}
                disabled={mutation.isPending}
                onValueChange={(value) => save({ detailRetentionDays: Number(value) as DetailRetentionDays })}
              >
                <SelectTrigger id="statistics-retention" className="h-9 w-full sm:w-48">
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
              <p className="text-sm text-muted-foreground">{t("statistics.retention.description")}</p>
            </div>

            <div className="flex items-baseline justify-between gap-4">
              <span className="text-sm font-medium">{t("statistics.size.label")}</span>
              <span className="text-sm tabular-nums" data-testid="statistics-history-size">{formatBytes(status.historyBytes)}</span>
            </div>

            <StatisticsDeleteHistory instances={status.instances} />
          </div>
        )}
      </CardContent>
    </Card>
  )
}
