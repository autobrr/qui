/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import { parseImportJSON, toEditInput, toExportFormat, toExportJSON } from "@/lib/workflow-utils"
import type { Automation } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2, Save } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface WorkflowJsonEditDialogProps {
  rule: Automation
  onOpenChange: (open: boolean) => void
}

// Mounted only while open; the parent unmounts it to close, which also resets the text.
export function WorkflowJsonEditDialog({ rule, onOpenChange }: WorkflowJsonEditDialogProps) {
  const { t } = useTranslation("instances")
  const queryClient = useQueryClient()
  const [json, setJson] = useState(() => toExportJSON(toExportFormat(rule)))

  const updateRule = useMutation({
    mutationFn: (payload: ReturnType<typeof toEditInput>) => api.updateAutomation(rule.instanceId, rule.id, payload),
    onSuccess: (updated) => {
      toast.success(t("preferences.workflowsOverview.toast.updatedWorkflow", { name: updated.name }))
      void queryClient.invalidateQueries({ queryKey: ["automations", rule.instanceId] })
      onOpenChange(false)
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("preferences.workflowsOverview.editJsonDialog.saveFailed"))
    },
  })

  const handleSave = () => {
    const result = parseImportJSON(json)
    if (result.data === null) {
      toast.error(t(result.error))
      return
    }
    updateRule.mutate(toEditInput(rule, result.data))
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[85dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("preferences.workflowsOverview.editJsonDialog.title", { name: rule.name })}</DialogTitle>
          <DialogDescription>
            {t("preferences.workflowsOverview.editJsonDialog.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="overflow-y-auto flex-1 min-h-0">
          <Textarea
            aria-label={t("preferences.workflowsOverview.editJsonDialog.title", { name: rule.name })}
            value={json}
            onChange={(e) => setJson(e.target.value)}
            className="min-h-[300px] max-h-[60vh] font-mono text-sm"
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("preferences.workflowsOverview.editJsonDialog.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={!json.trim() || updateRule.isPending}>
            {updateRule.isPending ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                {t("preferences.workflowsOverview.editJsonDialog.saving")}
              </>
            ) : (
              <>
                <Save className="h-4 w-4 mr-2" />
                {t("preferences.workflowsOverview.editJsonDialog.save")}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
