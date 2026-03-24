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
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import type { TorznabIndexer, TorznabIndexerFormData } from "@/types"
import { useEffect, useState } from "react"
import { Trans, useTranslation } from "react-i18next"
import { toast } from "sonner"

interface IndexerDialogProps {
  open: boolean
  onClose: () => void
  mode: "create" | "edit"
  indexer?: TorznabIndexer | null
}

const DEFAULT_FORM: TorznabIndexerFormData = {
  name: "",
  base_url: "",
  indexer_id: "",
  api_key: "",
  basic_username: "",
  basic_password: "",
  backend: "jackett",
  enabled: true,
  priority: 0,
  timeout_seconds: 30,
}

const REDACTED_TOKEN = "<redacted>"

function useCommonTr() {
  const { t } = useTranslation("common")
  return (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
}

export function IndexerDialog({ open, onClose, mode, indexer }: IndexerDialogProps) {
  const tr = useCommonTr()
  const [loading, setLoading] = useState(false)
  const [formData, setFormData] = useState<TorznabIndexerFormData>(DEFAULT_FORM)
  const [showBasicAuth, setShowBasicAuth] = useState(false)
  const backend = formData.backend ?? "jackett"
  const baseUrlPlaceholder = backend === "prowlarr"
    ? tr("indexerDialog.placeholders.baseUrlProwlarr")
    : tr("indexerDialog.placeholders.baseUrlJackett")
  const requiresIndexerId = backend === "prowlarr"

  useEffect(() => {
    if (mode === "edit" && indexer) {
      const hasBasic = !!indexer.basic_username
      setFormData({
        name: indexer.name,
        base_url: indexer.base_url,
        indexer_id: indexer.indexer_id,
        api_key: "", // API key not returned from backend for security
        basic_username: indexer.basic_username ?? "",
        basic_password: hasBasic ? REDACTED_TOKEN : "",
        backend: indexer.backend,
        enabled: indexer.enabled,
        priority: indexer.priority,
        timeout_seconds: indexer.timeout_seconds,
      })
      setShowBasicAuth(hasBasic)
    } else {
      setFormData({ ...DEFAULT_FORM })
      setShowBasicAuth(false)
    }
  }, [mode, indexer, open])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)

    try {
      const backendValue = formData.backend ?? "jackett"
      const trimmedIndexerId = formData.indexer_id !== undefined ? formData.indexer_id.trim() : undefined
      const trimmedBasicUser = (formData.basic_username ?? "").trim()
      const basicPass = formData.basic_password ?? ""
      const isRedactedPassword = basicPass === REDACTED_TOKEN

      if (showBasicAuth) {
        if (!trimmedBasicUser) {
          toast.error(tr("indexerDialog.toasts.basicUsernameRequired"))
          return
        }
        if (mode === "create" && !basicPass.trim()) {
          toast.error(tr("indexerDialog.toasts.basicPasswordRequired"))
          return
        }
        if (mode === "edit" && !isRedactedPassword && !basicPass.trim()) {
          toast.error(tr("indexerDialog.toasts.basicPasswordRequiredOrRedacted", { redacted: REDACTED_TOKEN }))
          return
        }
      }

      if (mode === "create") {
        const createPayload: TorznabIndexerFormData = {
          name: formData.name,
          base_url: formData.base_url,
          api_key: formData.api_key.trim(),
          backend: backendValue,
          enabled: formData.enabled,
          priority: formData.priority,
          timeout_seconds: formData.timeout_seconds,
        }
        if (trimmedIndexerId) {
          createPayload.indexer_id = trimmedIndexerId
        }
        if (showBasicAuth) {
          createPayload.basic_username = trimmedBasicUser
          createPayload.basic_password = basicPass
        }

        const response = await api.createTorznabIndexer(createPayload)
        if (response.warnings?.length) {
          toast.warning(tr("indexerDialog.toasts.createdWithWarnings", { warnings: response.warnings.join(", ") }))
        } else {
          toast.success(tr("indexerDialog.toasts.created"))
        }
      } else if (mode === "edit" && indexer) {
        const updatePayload: Partial<TorznabIndexerFormData> = {
          name: formData.name,
          base_url: formData.base_url,
          backend: backendValue,
          enabled: formData.enabled,
          priority: formData.priority,
          timeout_seconds: formData.timeout_seconds,
        }

        if (formData.indexer_id !== undefined) {
          updatePayload.indexer_id = trimmedIndexerId ?? ""
        }

        const trimmedApiKey = formData.api_key.trim()
        if (trimmedApiKey) {
          updatePayload.api_key = trimmedApiKey
        }

        if (showBasicAuth) {
          updatePayload.basic_username = trimmedBasicUser
          if (basicPass !== REDACTED_TOKEN) {
            updatePayload.basic_password = basicPass
          }
        } else {
          // Explicit clear.
          updatePayload.basic_username = ""
          updatePayload.basic_password = ""
        }

        const response = await api.updateTorznabIndexer(indexer.id, updatePayload)
        if (response.warnings?.length) {
          toast.warning(tr("indexerDialog.toasts.updatedWithWarnings", { warnings: response.warnings.join(", ") }))
        } else {
          toast.success(tr("indexerDialog.toasts.updated"))
        }
      }
      onClose()
    } catch {
      toast.error(mode === "create" ? tr("indexerDialog.toasts.failedCreate") : tr("indexerDialog.toasts.failedEdit"))
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[525px] max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>
            {mode === "create" ? tr("indexerDialog.titles.add") : tr("indexerDialog.titles.edit")}
          </DialogTitle>
          <DialogDescription>
            {mode === "create"
              ? tr("indexerDialog.descriptions.add")
              : tr("indexerDialog.descriptions.edit")}
          </DialogDescription>
        </DialogHeader>
        <form id="indexer-form" onSubmit={handleSubmit} autoComplete="off" data-1p-ignore className="flex-1 overflow-y-auto min-h-0">
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="name">{tr("indexerDialog.labels.name")}</Label>
              <Input
                id="name"
                value={formData.name}
                onChange={(e) =>
                  setFormData({ ...formData, name: e.target.value })
                }
                placeholder={tr("indexerDialog.placeholders.name")}
                autoComplete="off"
                data-1p-ignore
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="backend">{tr("indexerDialog.labels.backend")}</Label>
              <Select
                value={backend}
                onValueChange={(value) =>
                  setFormData(prev => ({
                    ...prev,
                    backend: value as TorznabIndexerFormData["backend"],
                    indexer_id: value === "native" ? "" : prev.indexer_id ?? "",
                  }))
                }
              >
                <SelectTrigger id="backend">
                  <SelectValue placeholder={tr("indexerDialog.placeholders.selectBackend")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="jackett">{tr("indexerDialog.backends.jackett")}</SelectItem>
                  <SelectItem value="prowlarr">{tr("indexerDialog.backends.prowlarr")}</SelectItem>
                  <SelectItem value="native">{tr("indexerDialog.backends.nativeTorznab")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="baseUrl">{tr("indexerDialog.labels.baseUrl")}</Label>
              <Input
                id="baseUrl"
                type="url"
                value={formData.base_url}
                onChange={(e) =>
                  setFormData({ ...formData, base_url: e.target.value })
                }
                placeholder={baseUrlPlaceholder}
                autoComplete="off"
                data-1p-ignore
                required
              />
            </div>
            {backend !== "native" && (
              <div className="grid gap-2">
                <Label htmlFor="indexerId">
                  {tr("indexerDialog.labels.indexerId")} {requiresIndexerId && <span className="text-destructive">*</span>}
                </Label>
                <Input
                  id="indexerId"
                  value={formData.indexer_id ?? ""}
                  onChange={(e) =>
                    setFormData({ ...formData, indexer_id: e.target.value })
                  }
                  placeholder={backend === "prowlarr"
                    ? tr("indexerDialog.placeholders.prowlarrIndexerId")
                    : tr("indexerDialog.placeholders.jackettIndexerId")}
                  autoComplete="off"
                  data-1p-ignore
                  required={requiresIndexerId}
                />
                <p className="text-xs text-muted-foreground">
                  {backend === "prowlarr"
                    ? tr("indexerDialog.help.prowlarrIndexerId")
                    : tr("indexerDialog.help.jackettIndexerId")}
                </p>
              </div>
            )}
            <div className="grid gap-2">
              <Label htmlFor="apiKey">{tr("indexerDialog.labels.apiKey")}</Label>
              <Input
                id="apiKey"
                type="password"
                value={formData.api_key}
                onChange={(e) =>
                  setFormData({ ...formData, api_key: e.target.value })
                }
                placeholder={mode === "edit"
                  ? tr("indexerDialog.placeholders.apiKeyKeepExisting")
                  : tr("indexerDialog.placeholders.apiKey")}
                autoComplete="off"
                data-1p-ignore
                required={mode === "create"}
              />
            </div>
            <div className="flex items-start justify-between gap-4 rounded-lg border bg-muted/40 p-4">
              <div className="space-y-1">
                <Label htmlFor="indexer-basic-auth">{tr("indexerDialog.labels.basicAuth")}</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  {tr("indexerDialog.help.basicAuth")}
                </p>
              </div>
              <Switch
                id="indexer-basic-auth"
                checked={showBasicAuth}
                onCheckedChange={(checked) => {
                  setShowBasicAuth(checked)
                  if (!checked) {
                    setFormData(prev => ({ ...prev, basic_username: "", basic_password: "" }))
                  } else if ((formData.basic_username ?? "").trim() === "") {
                    setFormData(prev => ({ ...prev, basic_username: "", basic_password: "" }))
                  }
                }}
              />
            </div>
            {showBasicAuth && (
              <div className="grid gap-4 rounded-lg border bg-muted/20 p-4">
                <div className="grid gap-2">
                  <Label htmlFor="basicUsername">{tr("indexerDialog.labels.basicUsername")}</Label>
                  <Input
                    id="basicUsername"
                    value={formData.basic_username ?? ""}
                    onChange={(e) => setFormData({ ...formData, basic_username: e.target.value })}
                    placeholder={tr("indexerDialog.placeholders.username")}
                    autoComplete="off"
                    data-1p-ignore
                    required
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="basicPassword">{tr("indexerDialog.labels.basicPassword")}</Label>
                  <Input
                    id="basicPassword"
                    type="password"
                    value={formData.basic_password ?? ""}
                    onChange={(e) => setFormData({ ...formData, basic_password: e.target.value })}
                    placeholder={mode === "edit" ? REDACTED_TOKEN : tr("indexerDialog.placeholders.password")}
                    autoComplete="off"
                    data-1p-ignore
                    required={mode === "create"}
                  />
                  {mode === "edit" && (
                    <p className="text-xs text-muted-foreground">
                      <Trans
                        i18nKey="indexerDialog.help.keepRedacted"
                        ns="common"
                        components={{
                          token: <span className="font-mono" />,
                        }}
                        values={{
                          token: REDACTED_TOKEN,
                        }}
                      />
                    </p>
                  )}
                </div>
              </div>
            )}
            <div className="grid grid-cols-2 gap-4">
              <div className="grid gap-2">
                <Label htmlFor="priority">{tr("indexerDialog.labels.priority")}</Label>
                <Input
                  id="priority"
                  type="number"
                  value={formData.priority}
                  onChange={(e) =>
                    setFormData({ ...formData, priority: parseInt(e.target.value, 10) })
                  }
                  min="0"
                  autoComplete="off"
                  data-1p-ignore
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="timeout">{tr("indexerDialog.labels.timeoutSeconds")}</Label>
                <Input
                  id="timeout"
                  type="number"
                  value={formData.timeout_seconds}
                  onChange={(e) =>
                    setFormData({ ...formData, timeout_seconds: parseInt(e.target.value, 10) })
                  }
                  min="5"
                  max="120"
                  autoComplete="off"
                  data-1p-ignore
                  required
                />
              </div>
            </div>
            <div className="flex items-center justify-between">
              <Label htmlFor="enabled">{tr("indexerDialog.labels.enabled")}</Label>
              <Switch
                id="enabled"
                checked={formData.enabled}
                onCheckedChange={(checked) =>
                  setFormData({ ...formData, enabled: checked })
                }
              />
            </div>
          </div>
        </form>
        <DialogFooter className="flex-shrink-0">
          <Button type="button" variant="outline" onClick={onClose}>
            {tr("indexerDialog.actions.cancel")}
          </Button>
          <Button type="submit" form="indexer-form" disabled={loading}>
            {loading
              ? tr("indexerDialog.actions.saving")
              : mode === "create"
                ? tr("indexerDialog.actions.add")
                : tr("indexerDialog.actions.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
