/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { api, APIError } from "@/lib/api"
import { ACTION_LABEL_KEYS, getRuleActions } from "@/lib/workflow-utils"
import type { Automation } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

export interface RunWorkflowNowTarget {
  instanceId: number
  rule: Automation
}

interface RunWorkflowNowDialogProps {
  target: RunWorkflowNowTarget | null
  onClose: () => void
}

// Confirms a real run of one saved workflow, then runs it and reports the result in a toast.
export function RunWorkflowNowDialog({ target, onClose }: RunWorkflowNowDialogProps) {
  const { t } = useTranslation("instances")
  const queryClient = useQueryClient()

  const runNow = useMutation({
    mutationFn: ({ instanceId, rule }: RunWorkflowNowTarget) => api.applyAutomationNow(instanceId, rule.id),
    onMutate: ({ rule }) => toast.loading(t("preferences.runWorkflowNow.toast.running", { name: rule.name })),
    onSuccess: (result, { instanceId, rule }, toastId) => {
      const name = { name: rule.name }
      if (result.status === "dry-run-completed") {
        toast.success(t("preferences.runWorkflowNow.toast.dryRunCompleted", name), { id: toastId })
      } else if (result.activities?.some(activity => activity.outcome === "failed")) {
        toast.error(t("preferences.runWorkflowNow.toast.completedWithErrors", name), { id: toastId })
      } else if (!result.activities?.length && !rule.conditions?.externalProgram?.enabled && !rule.conditions?.exportToInstance?.enabled) {
        // External programs and exports can write their activity rows after the reply, so an empty list says nothing about them.
        toast.success(t("preferences.runWorkflowNow.toast.noChanges", name), { id: toastId })
      } else {
        toast.success(t("preferences.runWorkflowNow.toast.completed", name), { id: toastId })
      }
      void queryClient.invalidateQueries({ queryKey: ["automation-activity", instanceId] })
    },
    onError: (error, _target, toastId) => {
      let message = t("preferences.runWorkflowNow.toast.failed")
      if (error instanceof APIError && error.status === 409) {
        message = t("preferences.runWorkflowNow.toast.inProgress")
      } else if (error instanceof Error) {
        message = error.message
      }
      toast.error(message, { id: toastId })
    },
  })

  const rule = target?.rule
  const actions = rule ? getRuleActions(rule) : []

  return (
    <AlertDialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("preferences.runWorkflowNow.title", { name: rule?.name })}</AlertDialogTitle>
          <AlertDialogDescription>
            {rule?.dryRun ? t("preferences.runWorkflowNow.dryRunDescription") : t("preferences.runWorkflowNow.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {actions.length > 0 && (
          <div className="text-sm">
            <p className="font-medium">{t("preferences.runWorkflowNow.actions")}</p>
            <ul className="mt-1 list-disc pl-5 text-muted-foreground">
              {actions.map(action => <li key={action}>{t(ACTION_LABEL_KEYS[action])}</li>)}
            </ul>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>{t("preferences.runWorkflowNow.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              if (target) {
                runNow.mutate(target)
              }
              onClose()
            }}
          >
            {t("preferences.runWorkflowNow.confirm")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
