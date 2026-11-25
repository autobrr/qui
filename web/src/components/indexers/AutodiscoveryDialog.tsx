/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Check, ChevronDown, Server } from "lucide-react"
import { useId, useRef, useState, type ChangeEvent } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Editor } from "@/components/kit/Editor"
import { EmptyTile } from "@/components/kit/EmptyTile"
import { Field } from "@/components/kit/Field"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"
import { deriveSavedConnections, type SavedDiscoveryConnection } from "@/lib/discovery-connections"
import { cn } from "@/lib/utils"
import type { JackettIndexer, TorznabIndexer, TorznabIndexerFormData, TorznabIndexerUpdate } from "@/types"

const DEFAULT_URL = "http://localhost:9696"

interface AutodiscoveryDialogProps {
  open: boolean
  onClose: () => void
  indexers: TorznabIndexer[]
}

interface ManualValues {
  baseUrl: string
  apiKey: string
  basicAuth: boolean
  basicUsername: string
  basicPassword: string
}

const EMPTY_MANUAL: ManualValues = { baseUrl: DEFAULT_URL, apiKey: "", basicAuth: false, basicUsername: "", basicPassword: "" }

type ManualErrors = Partial<Record<"baseUrl" | "apiKey" | "basicUsername" | "basicPassword", string>>

interface DiscoveryRequest {
  baseUrl: string
  apiKey: string
  basicUsername?: string
  basicPassword?: string
  sourceId?: number
}

type DiscoveredIndexer = JackettIndexer & { rowKey: string; existing?: TorznabIndexer }

interface Discovery {
  request: DiscoveryRequest
  rows: DiscoveredIndexer[]
  selected: ReadonlySet<string>
}

function normalizeBaseUrl(value: string) {
  const trimmed = value.trim()
  return trimmed.replace(/\/+$/, "") || trimmed
}

function isAbsoluteUrl(value: string) {
  try {
    new URL(value)
    return true
  } catch {
    return false
  }
}

function matchDiscovered(discovered: readonly JackettIndexer[], existing: readonly TorznabIndexer[], baseUrl: string): DiscoveredIndexer[] {
  return discovered.flatMap((indexer, rowIndex) => {
    const id = indexer.id.trim()
    if (!id) return []

    const candidates = existing.filter((candidate) =>
      candidate.backend === (indexer.backend ?? "jackett") &&
      normalizeBaseUrl(candidate.base_url) === baseUrl &&
      candidate.name === indexer.name
    )
    const idMatches = candidates.filter((candidate) => candidate.indexer_id === id)
    let match = candidates.length === 1 ? candidates[0] : undefined
    if (idMatches.length === 1) match = idMatches[0]

    return [{ ...indexer, id, rowKey: `discovered-indexer-${rowIndex}`, existing: match }]
  })
}

export function AutodiscoveryDialog({ open, onClose, indexers }: AutodiscoveryDialogProps) {
  const { t } = useTranslation("settings")
  const savedConnections = deriveSavedConnections(indexers)
  const [source, setSource] = useState<SavedDiscoveryConnection | "manual" | null>(null)
  const [values, setValues] = useState(EMPTY_MANUAL)
  const [errors, setErrors] = useState<ManualErrors>({})
  const [discovery, setDiscovery] = useState<Discovery | null>(null)
  const [pending, setPending] = useState(false)
  const inputs = useRef<Partial<Record<keyof ManualErrors, HTMLInputElement | null>>>({})
  const manual = savedConnections.length === 0 || source === "manual"
  const connection = typeof source === "object" ? source : null

  const close = () => {
    setSource(null)
    setValues(EMPTY_MANUAL)
    setErrors({})
    setDiscovery(null)
    onClose()
  }

  const manualRequest = (): DiscoveryRequest | null => {
    const baseUrl = normalizeBaseUrl(values.baseUrl)
    const basicUsername = values.basicUsername.trim()
    const next: ManualErrors = {
      baseUrl: !baseUrl
        ? t("indexers.autodiscovery.toast.urlRequired")
        : isAbsoluteUrl(baseUrl) ? undefined : t("indexers.dialog.validation.baseUrlInvalid", { example: DEFAULT_URL }),
      apiKey: values.apiKey ? undefined : t("indexers.dialog.validation.apiKeyRequired"),
      basicUsername: values.basicAuth && !basicUsername ? t("indexers.autodiscovery.toast.basicAuthRequired") : undefined,
      basicPassword: values.basicAuth && !values.basicPassword ? t("indexers.autodiscovery.toast.basicAuthRequired") : undefined,
    }
    setErrors(next)
    const firstInvalid = (["baseUrl", "apiKey", "basicUsername", "basicPassword"] as const).find((name) => next[name])
    if (firstInvalid) {
      inputs.current[firstInvalid]?.focus()
      return null
    }
    return values.basicAuth
      ? { baseUrl, apiKey: values.apiKey, basicUsername, basicPassword: values.basicPassword }
      : { baseUrl, apiKey: values.apiKey }
  }

  const discover = async () => {
    const request = connection
      ? { baseUrl: normalizeBaseUrl(connection.baseUrl), apiKey: "", sourceId: connection.sourceIndexerId }
      : manualRequest()
    if (!request) return

    setPending(true)
    try {
      const [response, existing] = await Promise.all([
        api.discoverJackettIndexers(request.baseUrl, request.apiKey, request.basicUsername, request.basicPassword, request.sourceId),
        api.listTorznabIndexers(),
      ])
      const rows = matchDiscovered(response.indexers, existing, request.baseUrl)
      setDiscovery({ request, rows, selected: new Set() })

      const existingCount = rows.filter((row) => row.existing).length
      if (existingCount > 0) {
        toast.success(t("indexers.autodiscovery.toast.discoveredWithExisting", { total: rows.length, existing: existingCount }))
      } else {
        toast.success(t("indexers.autodiscovery.toast.discovered", { count: rows.length }))
      }
      for (const warning of response.warnings ?? []) {
        toast.warning(warning)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : t("indexers.autodiscovery.unknownError")
      toast.error(t("indexers.autodiscovery.toast.discoverFailed", { error: message }))
    } finally {
      setPending(false)
    }
  }

  const importSelected = async () => {
    if (!discovery) return
    const { request, rows, selected } = discovery
    const basicAuth = request.basicUsername !== undefined
    setPending(true)
    let createdCount = 0
    let updatedCount = 0
    const errorDetails: string[] = []
    const warningDetails: string[] = []

    for (const indexer of rows) {
      if (!selected.has(indexer.rowKey)) continue
      const backend = indexer.backend ?? "jackett"

      try {
        let warnings: string[] | undefined
        if (indexer.existing) {
          const data: TorznabIndexerUpdate = {
            base_url: request.baseUrl,
            api_key: request.apiKey,
            source_indexer_id: request.sourceId,
            backend,
            indexer_id: indexer.id,
            capabilities: indexer.caps,
            categories: indexer.categories,
          }
          if (basicAuth) {
            data.basic_username = request.basicUsername
            data.basic_password = request.basicPassword
          }
          warnings = (await api.updateTorznabIndexer(indexer.existing.id, data)).warnings
          updatedCount++
        } else {
          const data: TorznabIndexerFormData = {
            name: indexer.name,
            base_url: request.baseUrl,
            api_key: request.apiKey,
            source_indexer_id: request.sourceId,
            backend,
            enabled: true,
            indexer_id: indexer.id,
            capabilities: indexer.caps,
            categories: indexer.categories,
          }
          if (basicAuth) {
            data.basic_username = request.basicUsername
            data.basic_password = request.basicPassword
          }
          warnings = (await api.createTorznabIndexer(data)).warnings
          createdCount++
        }
        if (warnings?.length) {
          warningDetails.push(`${indexer.name}: ${warnings.join(", ")}`)
        }
      } catch (error) {
        errorDetails.push(`${indexer.name}: ${error instanceof Error ? error.message : String(error)}`)
      }
    }
    setPending(false)

    const counts = [
      createdCount > 0 && t("indexers.autodiscovery.toast.created", { count: createdCount }),
      updatedCount > 0 && t("indexers.autodiscovery.toast.updated", { count: updatedCount }),
    ].filter((message): message is string => Boolean(message))

    if (errorDetails.length > 0) {
      toast.error([...counts, t("indexers.autodiscovery.toast.failed", { count: errorDetails.length })].join(", "))
      for (const detail of errorDetails.slice(0, 3)) toast.error(detail)
      if (errorDetails.length > 3) toast.error(t("indexers.autodiscovery.toast.moreErrors", { count: errorDetails.length - 3 }))
    } else if (warningDetails.length > 0) {
      toast.warning(t("indexers.autodiscovery.toast.importWithWarnings", { details: counts.join(", "), warningCount: warningDetails.length }))
      for (const detail of warningDetails.slice(0, 3)) toast.warning(detail)
      if (warningDetails.length > 3) toast.warning(t("indexers.autodiscovery.toast.moreWarnings", { count: warningDetails.length - 3 }))
    } else {
      toast.success(t("indexers.autodiscovery.toast.importSuccess", { details: counts.join(", ") }))
    }

    close()
  }

  const setManual = (patch: Partial<ManualValues>, clear: readonly (keyof ManualErrors)[]) => {
    setValues((prev) => ({ ...prev, ...patch }))
    setErrors((prev) => ({ ...prev, ...Object.fromEntries(clear.map((name) => [name, undefined])) }))
  }

  const input = (name: keyof ManualErrors & keyof ManualValues) => ({
    ref: (element: HTMLInputElement | null) => {
      inputs.current[name] = element
    },
    value: values[name] as string,
    autoComplete: "off",
    onChange: (event: ChangeEvent<HTMLInputElement>) => setManual({ [name]: event.target.value }, [name]),
  })

  const manualFields = (
    <>
      <Field label={t("indexers.autodiscovery.labels.indexerUrl")} help={t("indexers.autodiscovery.hints.indexerUrl")} error={errors.baseUrl}>
        <Input
          {...input("baseUrl")}
          inputMode="url"
          autoCapitalize="off"
          spellCheck={false}
          placeholder={t("indexers.autodiscovery.placeholders.indexerUrl")}
          className="font-mono text-[13px]"
        />
      </Field>
      <Field label={t("indexers.autodiscovery.labels.apiKey")} error={errors.apiKey}>
        <Input {...input("apiKey")} type="password" placeholder={t("indexers.autodiscovery.placeholders.apiKey")} />
      </Field>
      <SwitchRow
        label={t("indexers.autodiscovery.labels.basicAuth")}
        help={t("indexers.autodiscovery.labels.basicAuthDescription")}
        checked={values.basicAuth}
        onCheckedChange={(basicAuth) => setManual(basicAuth ? { basicAuth } : { basicAuth, basicUsername: "", basicPassword: "" }, ["basicUsername", "basicPassword"])}
      />
      {values.basicAuth && (
        <div className="grid gap-5 sm:grid-cols-2">
          <Field label={t("indexers.autodiscovery.labels.basicUsername")} error={errors.basicUsername}>
            <Input {...input("basicUsername")} placeholder={t("indexers.autodiscovery.placeholders.username")} />
          </Field>
          <Field label={t("indexers.autodiscovery.labels.basicPassword")} error={errors.basicPassword}>
            <Input {...input("basicPassword")} type="password" placeholder={t("indexers.autodiscovery.placeholders.password")} />
          </Field>
        </div>
      )}
    </>
  )

  const shared = { open, onOpenChange: (next: boolean) => !next && close(), title: t("indexers.autodiscovery.title"), pending, size: "wide" as const }

  if (discovery) {
    const count = discovery.selected.size
    return (
      <Editor
        {...shared}
        description={t("indexers.autodiscovery.descriptionSelect")}
        submitLabel={count !== 1 ? t("indexers.autodiscovery.buttons.importPlural", { count }) : t("indexers.autodiscovery.buttons.import", { count })}
        onSubmit={() => void importSelected()}
        submitDisabled={count === 0}
        secondaryActions={
          <Button type="button" variant="outline" disabled={pending} onClick={() => setDiscovery(null)}>
            {t("indexers.autodiscovery.buttons.back")}
          </Button>
        }
      >
        <DiscoveredList discovery={discovery} onSelect={(selected) => setDiscovery({ ...discovery, selected })} />
      </Editor>
    )
  }

  return (
    <Editor
      {...shared}
      description={t("indexers.autodiscovery.descriptionInput")}
      submitLabel={t("indexers.autodiscovery.buttons.discover")}
      onSubmit={() => void discover()}
      submitDisabled={!manual && !connection}
    >
      {savedConnections.length > 0 && (
        <SavedConnectionPicker connections={savedConnections} selected={connection} onSelect={(next) => {
          setSource(next)
          setErrors({})
        }} />
      )}
      {savedConnections.length > 0 ? (
        <Collapsible open={manual} onOpenChange={(next) => setSource(next ? "manual" : null)}>
          <CollapsibleTrigger asChild>
            <Button type="button" variant="outline" className="group w-full max-sm:h-11">
              {t("indexers.autodiscovery.savedConnections.manual")}
              <ChevronDown className="transition-transform group-data-[state=open]:rotate-180" />
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="space-y-5 pt-5">{manualFields}</CollapsibleContent>
        </Collapsible>
      ) : (
        manualFields
      )}
    </Editor>
  )
}

interface SavedConnectionPickerProps {
  connections: readonly SavedDiscoveryConnection[]
  selected: SavedDiscoveryConnection | null
  onSelect: (connection: SavedDiscoveryConnection) => void
}

function SavedConnectionPicker({ connections, selected, onSelect }: SavedConnectionPickerProps) {
  const { t } = useTranslation("settings")
  const labelId = useId()
  const descriptionId = useId()

  return (
    <div role="group" aria-labelledby={labelId} aria-describedby={descriptionId} className="space-y-2">
      <div>
        <p id={labelId} className="text-sm font-medium leading-5">{t("indexers.autodiscovery.savedConnections.title")}</p>
        <p id={descriptionId} className="text-xs text-muted-foreground">{t("indexers.autodiscovery.savedConnections.description")}</p>
      </div>
      <ul className="flex flex-col gap-1.5">
        {connections.map((connection) => {
          const active = selected?.sourceIndexerId === connection.sourceIndexerId
          return (
            <li key={`${connection.backend}|${connection.baseUrl}`}>
              <button
                type="button"
                aria-pressed={active}
                onClick={() => onSelect(connection)}
                className={cn(
                  "flex min-h-14 w-full items-center gap-3 rounded-lg border px-3 py-2 text-left outline-none transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50",
                  active && "border-primary bg-accent/50"
                )}
              >
                <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
                  <Server className="size-4" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block font-mono text-[13px] [overflow-wrap:anywhere] pointer-fine:truncate">{connection.baseUrl}</span>
                  <span className="block text-xs text-muted-foreground">
                    {t(`indexers.dialog.backends.${connection.backend}`)} · {t("indexers.autodiscovery.savedConnections.indexerCount", { count: connection.indexerCount })}
                  </span>
                </span>
                <Check aria-hidden="true" className={cn("size-4 shrink-0 text-primary", !active && "invisible")} />
              </button>
            </li>
          )
        })}
      </ul>
      {selected && <p className="text-xs text-muted-foreground">{t("indexers.autodiscovery.savedConnections.storedKey")}</p>}
    </div>
  )
}

const CHECKBOX_TAP = "relative pointer-coarse:after:absolute pointer-coarse:after:-inset-3"

function DiscoveredList({ discovery, onSelect }: { discovery: Discovery; onSelect: (selected: ReadonlySet<string>) => void }) {
  const { t } = useTranslation("settings")
  const { rows, selected } = discovery
  const allSelected = rows.length > 0 && rows.every((row) => selected.has(row.rowKey))

  if (rows.length === 0) {
    return <EmptyTile title={t("indexers.autodiscovery.noIndexersFound")} />
  }

  const toggle = (rowKey: string, on: boolean) => {
    const next = new Set(selected)
    if (on) next.add(rowKey)
    else next.delete(rowKey)
    onSelect(next)
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="max-sm:h-10"
          onClick={() => onSelect(allSelected ? new Set() : new Set(rows.map((row) => row.rowKey)))}
        >
          {t(allSelected ? "indexers.autodiscovery.buttons.deselectAll" : "indexers.autodiscovery.buttons.selectAll")}
        </Button>
        <span className="ml-auto text-sm tabular-nums text-muted-foreground">
          {t("indexers.autodiscovery.selectionCount", { selected: selected.size, total: rows.length })}
        </span>
      </div>
      <ul className="divide-y rounded-lg border">
        {rows.map((indexer) => (
          <li key={indexer.rowKey} className="flex items-start gap-3 px-3 py-3">
            <Checkbox
              id={indexer.rowKey}
              checked={selected.has(indexer.rowKey)}
              onCheckedChange={(on) => toggle(indexer.rowKey, on === true)}
              className={cn("mt-0.5", CHECKBOX_TAP)}
            />
            <label htmlFor={indexer.rowKey} className="min-w-0 flex-1 cursor-pointer">
              <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="text-sm font-medium [overflow-wrap:anywhere]">{indexer.name}</span>
                {indexer.existing && (
                  <span className="rounded-md bg-primary/10 px-1.5 py-0.5 text-xs text-primary">{t("indexers.autodiscovery.willUpdate")}</span>
                )}
              </span>
              {indexer.description && <span className="mt-0.5 block text-xs text-muted-foreground">{indexer.description}</span>}
              <span className="mt-0.5 block text-xs text-muted-foreground">
                {[
                  `${t("indexers.autodiscovery.typeLabel")} ${indexer.type}`,
                  indexer.backend && t(`indexers.dialog.backends.${indexer.backend}`),
                  !indexer.configured && t("indexers.autodiscovery.notConfigured"),
                ].filter(Boolean).join(" · ")}
              </span>
            </label>
          </li>
        ))}
      </ul>
    </div>
  )
}
