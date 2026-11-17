/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { systemNoticesQueryKey } from "@/hooks/useSystemNotices"
import { api } from "@/lib/api"

export function SystemNoticeDismissButton({ kind }: { kind: string }) {
  const { t } = useTranslation("common")
  const queryClient = useQueryClient()
  const dismiss = useMutation({
    mutationFn: () => api.dismissSystemNotice(kind),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: systemNoticesQueryKey }),
    onError: (error) => toast.error(error.message),
  })

  return (
    <Button size="sm" variant="ghost" className="h-7 px-2.5 text-xs" disabled={dismiss.isPending} onClick={() => dismiss.mutate()}>
      {t("systemNotice.actions.dismiss")}
    </Button>
  )
}
