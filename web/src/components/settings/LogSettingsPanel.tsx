/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { ArrowDownToLine, ChevronDown, Copy, Download, EyeOff, Eraser, FileText, Filter, Lock, Pause, Search, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type KeyboardEvent, type MouseEvent, type ReactNode, type Ref } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Editor } from "@/components/kit/Editor"
import { Facts } from "@/components/kit/Facts"
import { Field } from "@/components/kit/Field"
import { ResourceList } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { usePersistedLogExclusions } from "@/hooks/usePersistedLogExclusions"
import { ALL_LEVELS_SET, ALL_LOG_LEVELS, usePersistedLogLevels, type LogLevel } from "@/hooks/usePersistedLogLevels"
import { api } from "@/lib/api"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { cn, copyTextToClipboard, formatBytes } from "@/lib/utils"
import type { LogSettings, LogSettingsUpdate } from "@/types"

const LOG_SETTINGS_KEY = ["log-settings"]

export function LogSettingsPanel() {
  return (
    <>
      <LiveLogSection />
      <LogSettingsSection />
      <LogFilesSection />
    </>
  )
}

const LOG_LEVELS = ["TRACE", "DEBUG", "INFO", "WARN", "ERROR"] as const
type ServerLogLevel = (typeof LOG_LEVELS)[number]

function normalizeLogLevel(level: string | undefined): ServerLogLevel {
  const normalized = level?.trim().toUpperCase()
  if (normalized && (LOG_LEVELS as readonly string[]).includes(normalized)) {
    return normalized as ServerLogLevel
  }
  return "INFO"
}

function LogSettingsSection() {
  const { t } = useTranslation("settings")
  const query = useQuery({ queryKey: LOG_SETTINGS_KEY, queryFn: () => api.getLogSettings() })
  const settings = query.data

  if (!settings) {
    return <Section title={t("logs.configuration.title")} description={t("logs.configuration.description")} queries={[query]} />
  }
  const key = [settings.level, settings.path, settings.maxSize, settings.maxBackups].join("\n")
  return <LogSettingsForm key={key} settings={settings} query={query} />
}

interface LogSettingsDraft {
  level: ServerLogLevel
  path: string
  maxSize: string
  maxBackups: string
}

type NumberField = "maxSize" | "maxBackups"

function parseCount(text: string, min: number): number | undefined {
  const value = text.trim() === "" ? Number.NaN : Math.trunc(Number(text))
  return Number.isSafeInteger(value) && value >= min ? value : undefined
}

function LogSettingsForm({ settings, query }: { settings: LogSettings; query: UseQueryResult<LogSettings> }) {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<LogSettingsDraft>(() => ({
    level: normalizeLogLevel(settings.level),
    path: settings.path ?? "",
    maxSize: String(settings.maxSize ?? 50),
    maxBackups: String(settings.maxBackups ?? 3),
  }))
  const [errors, setErrors] = useState<Partial<Record<NumberField, string>>>({})
  const inputs = useRef<Partial<Record<NumberField, HTMLInputElement | null>>>({})

  const mutation = useMutation({
    mutationFn: (update: LogSettingsUpdate) => api.updateLogSettings(update),
    onSuccess: (data) => {
      queryClient.setQueryData(LOG_SETTINGS_KEY, data)
      toast.success(t("logs.toasts.settingsUpdated"))
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("logs.toasts.settingsFailed"))
    },
  })

  const submit = () => {
    const isLocked = (name: NumberField) => settings.locked?.[name] !== undefined
    const maxSize = parseCount(draft.maxSize, 1)
    const maxBackups = parseCount(draft.maxBackups, 0)
    const next = {
      maxSize: maxSize === undefined && !isLocked("maxSize") ? t("logs.validation.maxSize") : undefined,
      maxBackups: maxBackups === undefined && !isLocked("maxBackups") ? t("logs.validation.maxBackups") : undefined,
    }
    setErrors(next)
    if (next.maxSize || next.maxBackups) {
      inputs.current[next.maxSize ? "maxSize" : "maxBackups"]?.focus()
      return
    }

    const update: LogSettingsUpdate = {}
    if (draft.level !== normalizeLogLevel(settings.level)) update.level = draft.level
    if (draft.path !== (settings.path ?? "")) update.path = draft.path
    if (maxSize !== undefined && maxSize !== (settings.maxSize ?? 50)) update.maxSize = maxSize
    if (maxBackups !== undefined && maxBackups !== (settings.maxBackups ?? 3)) update.maxBackups = maxBackups
    if (Object.keys(update).length > 0) mutation.mutate(update)
  }

  const lockNote = (field: keyof LogSettingsUpdate): ReactNode => {
    const source = settings.locked?.[field]
    if (source === undefined) return undefined
    return (
      <span className="inline-flex items-center gap-1">
        <Lock aria-hidden="true" className="size-3 shrink-0" />
        {t(source === "environment (empty)" ? "logs.configuration.lockedByEmptyEnv" : "logs.configuration.lockedByEnv")}
      </span>
    )
  }

  const numberInput = (name: NumberField, min: number) => ({
    ref: (input: HTMLInputElement | null) => {
      inputs.current[name] = input
    },
    type: "number",
    inputMode: "numeric" as const,
    min,
    className: "max-sm:h-10",
    value: draft[name],
    disabled: settings.locked?.[name] !== undefined,
    onChange: (event: ChangeEvent<HTMLInputElement>) => {
      setDraft((prev) => ({ ...prev, [name]: event.target.value }))
      setErrors((prev) => ({ ...prev, [name]: undefined }))
    },
  })

  return (
    <Section
      title={t("logs.configuration.title")}
      description={t("logs.configuration.description")}
      queries={[query]}
      submit={{ label: t("logs.configuration.saveSettings"), onSubmit: submit, pending: mutation.isPending }}
    >
      <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:gap-x-6">
        <div className="col-span-2 sm:col-span-1">
          <Field label={t("logs.configuration.level")} description={lockNote("level")}>
            {(control) => (
              <Select
                value={draft.level}
                onValueChange={(value) => setDraft((prev) => ({ ...prev, level: normalizeLogLevel(value) }))}
                disabled={settings.locked?.level !== undefined}
              >
                <SelectTrigger {...control} className="w-full max-sm:data-[size=default]:h-10">
                  <SelectValue placeholder={t("logs.configuration.levelPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {LOG_LEVELS.map((level) => <SelectItem key={level} value={level}>{level}</SelectItem>)}
                </SelectContent>
              </Select>
            )}
          </Field>
        </div>
        <div className="col-span-2 sm:col-span-1">
          <Field label={t("logs.configuration.path")} description={lockNote("path")}>
            <Input
              className={cn("max-sm:h-10", settings.locked?.path !== undefined && "text-muted-foreground")}
              placeholder={t("logs.configuration.pathPlaceholder")}
              value={draft.path}
              onChange={(event) => setDraft((prev) => ({ ...prev, path: event.target.value }))}
              readOnly={settings.locked?.path !== undefined}
            />
          </Field>
        </div>
        <Field label={t("logs.configuration.maxSize")} description={lockNote("maxSize")} error={errors.maxSize}>
          <Input {...numberInput("maxSize", 1)} />
        </Field>
        <Field label={t("logs.configuration.maxBackups")} description={lockNote("maxBackups")} error={errors.maxBackups}>
          <Input {...numberInput("maxBackups", 0)} />
        </Field>
      </div>
      {settings.configPath && (
        <Facts items={[{ label: t("logs.configuration.configFile"), value: settings.configPath, mono: true, copy: settings.configPath }]} />
      )}
    </Section>
  )
}

interface RawLogLine {
  id: number
  text: string
}

interface ParsedLogEntry {
  id: number
  level: LogLevel
  time: string
  message: string
  extra: Record<string, unknown>
  raw: string
  isJson: boolean
}

const LEVEL_COLORS: Record<LogLevel, string> = {
  trace: "text-muted-foreground",
  debug: "text-foreground",
  info: "text-info",
  warn: "text-warning",
  error: "text-destructive",
}

const LEVEL_BADGE_COLORS: Record<LogLevel, string> = {
  trace: "bg-muted text-muted-foreground",
  debug: "bg-muted text-foreground",
  info: "bg-info/10 text-info",
  warn: "bg-warning/10 text-warning",
  error: "bg-destructive/10 text-destructive",
}

function normalizeLevel(raw: string | undefined): LogLevel {
  const level = raw?.toLowerCase()
  if (level && ALL_LEVELS_SET.has(level as LogLevel)) {
    return level as LogLevel
  }
  if (level === "fatal" || level === "panic") {
    return "error"
  }
  return "info"
}

function parseLogLine(entry: RawLogLine): ParsedLogEntry {
  try {
    const parsed = JSON.parse(entry.text) as Record<string, unknown>
    const level = normalizeLevel(typeof parsed.level === "string" ? parsed.level : undefined)
    const time = typeof parsed.time === "string" ? parsed.time : ""
    const message = typeof parsed.message === "string" ? parsed.message : ""

    const { level: _l, time: _t, message: _m, ...extra } = parsed

    return { id: entry.id, level, time, message, extra, raw: entry.text, isJson: true }
  } catch {
    return { id: entry.id, level: "info", time: "", message: entry.text, extra: {}, raw: entry.text, isJson: false }
  }
}

function LogEntry({
  entry,
  formatTimeOnly,
  onSelect,
  onMute,
}: {
  entry: ParsedLogEntry
  formatTimeOnly: (timestamp: number, includeSeconds?: boolean) => string
  onSelect: () => void
  onMute?: () => void
}) {
  const { t } = useTranslation("settings")
  const extraKeys = Object.keys(entry.extra)
  const isClickable = entry.isJson

  const handleKeyDown = (e: KeyboardEvent) => {
    if (isClickable && e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) {
      e.preventDefault()
      onSelect()
    }
  }

  const handleMute = (e: MouseEvent) => {
    e.stopPropagation()
    onMute?.()
  }

  const badgeClass = cn(
    "mr-2 inline-flex h-4 w-12 shrink-0 items-center justify-center rounded align-[1px] text-10 font-medium uppercase sm:mr-0",
    LEVEL_BADGE_COLORS[entry.level]
  )

  return (
    <div
      className={cn(
        "group py-1 hover:bg-muted/50 max-sm:border-b max-sm:border-border/40 max-sm:last:border-b-0 sm:py-0.5",
        isClickable && "cursor-pointer focus:bg-muted/50 focus:outline-none"
      )}
      onClick={isClickable ? onSelect : undefined}
      onKeyDown={handleKeyDown}
      role={isClickable ? "button" : undefined}
      tabIndex={isClickable ? 0 : undefined}
    >
      <div className={cn("max-sm:[overflow-wrap:anywhere] sm:flex sm:gap-2 sm:whitespace-nowrap", isClickable && "max-sm:line-clamp-4")}>
        <span className="mr-2 text-muted-foreground/60 sm:mr-0 sm:shrink-0">{entry.time ? formatTimeOnly(Date.parse(entry.time) / 1000, true) : ""}</span>
        {onMute ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                onClick={handleMute}
                className={cn("group/mute cursor-pointer", badgeClass)}
              >
                <span className="group-hover/mute:hidden">{entry.level}</span>
                <X className="hidden size-2.5 group-hover/mute:block" />
              </button>
            </TooltipTrigger>
            <TooltipContent>{t("logs.viewer.muteSimilarEntries")}</TooltipContent>
          </Tooltip>
        ) : (
          <span className={badgeClass}>{entry.level}</span>
        )}
        <span className={LEVEL_COLORS[entry.level]}>{entry.message}</span>
        {extraKeys.length > 0 && (
          <span className="text-muted-foreground/50">
            {extraKeys.map((key) => (
              <span key={key} className="ml-2">
                <span className="text-muted-foreground/70">{key}</span>
                <span className="text-muted-foreground/40">=</span>
                <span className="text-muted-foreground/60">
                  {typeof entry.extra[key] === "string" ? entry.extra[key] as string : JSON.stringify(entry.extra[key])}
                </span>
              </span>
            ))}
          </span>
        )}
      </div>
    </div>
  )
}

function highlightJson(json: string): ReactNode[] {
  if (!json) return []

  const tokenRegex = /("(?:\\.|[^"\\])*")\s*:|("(?:\\.|[^"\\])*")|(-?\d+\.?\d*(?:[eE][+-]?\d+)?)|(\btrue\b|\bfalse\b)|(\bnull\b)|([{}[\],:])/g

  const result: ReactNode[] = []
  let lastIndex = 0
  let key = 0

  for (const match of json.matchAll(tokenRegex)) {
    const matchIndex = match.index
    if (matchIndex == null) continue

    if (matchIndex > lastIndex) {
      result.push(json.slice(lastIndex, matchIndex))
    }

    const [fullMatch, keyMatch, stringMatch, numberMatch, boolMatch, nullMatch, punctMatch] = match

    if (keyMatch) {
      result.push(
        <span key={key++} className="text-foreground">{keyMatch}</span>,
        ":"
      )
    } else if (stringMatch) {
      result.push(<span key={key++} className="text-chart-text-2">{stringMatch}</span>)
    } else if (numberMatch) {
      result.push(<span key={key++} className="text-chart-text-3">{numberMatch}</span>)
    } else if (boolMatch) {
      result.push(<span key={key++} className="text-chart-text-3">{boolMatch}</span>)
    } else if (nullMatch) {
      result.push(<span key={key++} className="text-muted-foreground">{nullMatch}</span>)
    } else if (punctMatch) {
      result.push(<span key={key++} className="text-muted-foreground/70">{punctMatch}</span>)
    } else {
      result.push(fullMatch)
    }

    lastIndex = matchIndex + fullMatch.length
  }

  if (lastIndex < json.length) {
    result.push(json.slice(lastIndex))
  }

  return result
}

function LogEntryDetail({
  entry,
  open,
  onOpenChange,
  onMute,
}: {
  entry: ParsedLogEntry
  open: boolean
  onOpenChange: (open: boolean) => void
  onMute: () => void
}) {
  const { t } = useTranslation("settings")
  const { formatISOTimestamp } = useDateTimeFormatters()
  const { prettyJson, highlightedJson } = useMemo(() => {
    const pretty = JSON.stringify({ level: entry.level, time: entry.time, message: entry.message, ...entry.extra }, null, 2)
    return { prettyJson: pretty, highlightedJson: highlightJson(pretty) }
  }, [entry])

  const copy = async (text: string, successKey: "logs.toasts.jsonCopied" | "logs.toasts.rawCopied") => {
    try {
      await copyTextToClipboard(text)
      toast.success(t(successKey))
    } catch {
      toast.error(t("logs.toasts.copyFailed"))
    }
  }

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      size="wide"
      title={
        <span className="flex items-center gap-2">
          {t("logs.entryDialog.title")}
          <span className={cn("rounded px-1.5 py-0.5 text-xs font-medium uppercase", LEVEL_BADGE_COLORS[entry.level])}>{entry.level}</span>
        </span>
      }
      description={entry.time ? formatISOTimestamp(entry.time) : t("logs.entryDialog.rawLine")}
      secondaryActions={
        <>
          <Button type="button" variant="outline" onClick={() => void copy(prettyJson, "logs.toasts.jsonCopied")}>
            <Copy />
            {t("logs.entryDialog.copyJson")}
          </Button>
          <Button type="button" variant="outline" onClick={() => void copy(entry.raw, "logs.toasts.rawCopied")}>
            <Copy />
            {t("logs.entryDialog.copyRaw")}
          </Button>
          <Button type="button" variant="outline" onClick={onMute}>
            <EyeOff />
            {t("logs.entryDialog.mute")}
          </Button>
        </>
      }
    >
      <pre className="rounded-md border bg-muted/30 p-4 font-mono text-sm whitespace-pre-wrap [overflow-wrap:anywhere] max-sm:p-3 max-sm:text-xs">{highlightedJson}</pre>
    </Editor>
  )
}

const LOG_SOFT_CAP = 1000
const LOG_HARD_CAP = 10000

type Connection = "connecting" | "open" | "lost"

const CONNECTION_DOT: Record<Connection, string> = {
  connecting: "bg-muted-foreground/50",
  open: "bg-success",
  lost: "bg-warning",
}

const CONNECTION_LABEL = {
  connecting: "logs.viewer.connecting",
  open: "logs.viewer.connected",
  lost: "logs.viewer.reconnecting",
} as const satisfies Record<Connection, string>

function LiveLogSection() {
  const { t } = useTranslation("settings")
  const { formatTimeOnly } = useDateTimeFormatters()
  const phone = useIsPhone()
  const [lines, setLines] = useState<RawLogLine[]>([])
  const [autoScroll, setAutoScroll] = useState(true)
  const [connection, setConnection] = useState<Connection>("connecting")
  const [selectedLevels, setSelectedLevels] = usePersistedLogLevels()
  const [searchQuery, setSearchQuery] = useState("")
  const [droppedWhilePaused, setDroppedWhilePaused] = useState(false)
  const [detail, setDetail] = useState<{ entry: ParsedLogEntry; open: boolean } | null>(null)
  const [logExclusions, setLogExclusions] = usePersistedLogExclusions()
  const scrollRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const nextIdRef = useRef(0)
  const autoScrollRef = useRef(autoScroll)

  const setFollowing = (follow: boolean) => {
    autoScrollRef.current = follow
    setAutoScroll(follow)
    if (follow) setDroppedWhilePaused(false)
  }

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }
    if (reconnectTimeoutRef.current !== null) {
      window.clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }

    const url = api.getLogStreamUrl(1000)
    const es = new EventSource(url, { withCredentials: true })
    eventSourceRef.current = es

    es.onopen = () => {
      setConnection("open")
    }

    es.onmessage = (event) => {
      const newEntry: RawLogLine = { id: nextIdRef.current++, text: event.data as string }
      setLines((prev) => {
        const newLines = [...prev, newEntry]
        if (autoScrollRef.current && newLines.length > LOG_SOFT_CAP) {
          return newLines.slice(-LOG_SOFT_CAP)
        }
        if (newLines.length > LOG_HARD_CAP) {
          setDroppedWhilePaused(true)
          return newLines.slice(-LOG_HARD_CAP)
        }
        return newLines
      })
    }

    es.onerror = () => {
      setConnection("lost")
      es.close()
      reconnectTimeoutRef.current = window.setTimeout(connect, 3000)
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
      }
      if (reconnectTimeoutRef.current !== null) {
        window.clearTimeout(reconnectTimeoutRef.current)
        reconnectTimeoutRef.current = null
      }
    }
  }, [connect])

  const parsedEntries = useMemo(() => lines.map(parseLogLine), [lines])

  const filteredEntries = useMemo(() => {
    const query = searchQuery.toLowerCase().trim()

    return parsedEntries.filter((e) => {
      if (!selectedLevels.has(e.level)) return false

      if (logExclusions.includes(e.message)) return false

      if (query) {
        const matchesMessage = e.message.toLowerCase().includes(query)
        const matchesExtra = Object.values(e.extra).some((v) => {
          const text = typeof v === "string" ? v : JSON.stringify(v) ?? ""
          return text.toLowerCase().includes(query)
        })
        if (!matchesMessage && !matchesExtra) return false
      }

      return true
    })
  }, [parsedEntries, selectedLevels, searchQuery, logExclusions])

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [filteredEntries, autoScroll])

  const handleMuteMessage = (message: string) => {
    if (!logExclusions.includes(message)) {
      setLogExclusions([...logExclusions, message])
    }
  }

  return (
    <Section
      title={t("logs.viewer.title")}
      description={t("logs.viewer.description")}
      actions={
        <>
          <Button variant="outline" size="sm" onClick={() => setFollowing(!autoScroll)}>
            {autoScroll ? <Pause /> : <ArrowDownToLine />}
            {autoScroll ? t("logs.viewer.pause") : t("logs.viewer.follow")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => setLines([])}>
            <Eraser />
            {t("logs.viewer.clear")}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="flex gap-2">
          <LogSearch value={searchQuery} onChange={setSearchQuery} />
          <LogFilter
            selectedLevels={selectedLevels}
            onSelectedLevelsChange={setSelectedLevels}
            exclusions={logExclusions}
            onExclusionsChange={setLogExclusions}
          />
        </div>

        <LogScrollBody ref={scrollRef} label={t("logs.viewer.title")}>
          {filteredEntries.length > 0 ? (
            filteredEntries.map((entry) => (
              <LogEntry
                key={entry.id}
                entry={entry}
                formatTimeOnly={formatTimeOnly}
                onSelect={() => setDetail({ entry, open: true })}
                onMute={phone ? undefined : () => handleMuteMessage(entry.message)}
              />
            ))
          ) : (
            <span className="text-muted-foreground">
              {lines.length > 0 ? t("logs.viewer.noEntriesMatch") : t("logs.viewer.waiting")}
            </span>
          )}
        </LogScrollBody>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span role="status" className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", CONNECTION_DOT[connection])} />
            <span className={connection === "lost" ? "text-warning" : undefined}>{t(CONNECTION_LABEL[connection])}</span>
          </span>
          <span>
            {t("logs.viewer.showing", {
              shown: filteredEntries.length,
              total: lines.length,
              max: autoScroll ? LOG_SOFT_CAP.toLocaleString() : LOG_HARD_CAP.toLocaleString(),
              mode: autoScroll ? t("logs.viewer.maxLive") : t("logs.viewer.maxPaused"),
            })}
          </span>
          {droppedWhilePaused && <span className="text-warning">{t("logs.viewer.oldestEntriesDropped")}</span>}
        </div>
      </div>

      {detail && (
        <LogEntryDetail
          entry={detail.entry}
          open={detail.open}
          onOpenChange={(open) => setDetail((prev) => prev && { ...prev, open })}
          onMute={() => {
            handleMuteMessage(detail.entry.message)
            setDetail((prev) => prev && { ...prev, open: false })
          }}
        />
      )}
    </Section>
  )
}

function LogScrollBody({ ref, label, children }: { ref: Ref<HTMLDivElement>; label: string; children: ReactNode }) {
  return (
    <div
      ref={ref}
      role="region"
      aria-label={label}
      tabIndex={0}
      className="h-[clamp(20rem,calc(100dvh-24rem),60rem)] overflow-auto rounded-md border bg-muted/30 p-3 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 max-sm:-mx-4 max-sm:h-[max(20rem,calc(100dvh-13rem))] max-sm:overflow-x-hidden max-sm:rounded-none max-sm:border-x-0 max-sm:px-4"
      style={{ overflowAnchor: "none" }}
    >
      <div className="font-mono text-xs leading-relaxed sm:w-fit sm:min-w-full">{children}</div>
    </div>
  )
}

function LogSearch({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const { t } = useTranslation("settings")

  return (
    <div className="relative min-w-0 flex-1 sm:max-w-sm">
      <Search aria-hidden="true" className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        type="text"
        aria-label={t("logs.viewer.searchPlaceholder")}
        placeholder={t("logs.viewer.searchPlaceholder")}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="pr-9 pl-8 max-sm:h-10"
      />
      {value && (
        <button
          type="button"
          aria-label={t("common:header.clearSearch")}
          onClick={() => onChange("")}
          className="absolute top-1/2 right-1.5 grid size-7 -translate-y-1/2 place-items-center rounded-sm text-muted-foreground hover:text-foreground pointer-coarse:size-9"
        >
          <X className="size-4" />
        </button>
      )}
    </div>
  )
}

interface LogFilterProps {
  selectedLevels: ReadonlySet<LogLevel>
  onSelectedLevelsChange: (update: (prev: Set<LogLevel>) => Set<LogLevel>) => void
  exclusions: readonly string[]
  onExclusionsChange: (patterns: string[]) => void
}

function LogFilter({ selectedLevels, onSelectedLevelsChange, exclusions, onExclusionsChange }: LogFilterProps) {
  const { t } = useTranslation("settings")
  const settled = useClientSettingsSettled()
  const summary = selectedLevels.size === ALL_LOG_LEVELS.length
    ? t("logs.viewer.allLevels")
    : selectedLevels.size === 0 ? t("logs.viewer.none") : t("logs.viewer.levelCount", { count: selectedLevels.size })

  const toggleLevel = (level: LogLevel) => {
    onSelectedLevelsChange((prev) => {
      const next = new Set(prev)
      if (next.has(level)) {
        next.delete(level)
      } else {
        next.add(level)
      }
      return next
    })
  }

  const smallButton = "h-7 px-2 text-xs pointer-coarse:h-9"

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" className="min-w-0 shrink-0 max-sm:h-10">
          <Filter />
          <span>{summary}</span>
          {exclusions.length > 0 && (
            <span className="text-muted-foreground">· {t("logs.viewer.mutedCount", { count: exclusions.length })}</span>
          )}
          <ChevronDown className="opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 p-0">
        <div className="p-2">
          <div className="mb-1 flex items-center justify-between gap-2 pl-2">
            <span className="text-xs font-medium">{t("logs.viewer.levels")}</span>
            <span className="flex">
              <Button variant="ghost" size="sm" className={smallButton} disabled={!settled} onClick={() => onSelectedLevelsChange(() => new Set(ALL_LOG_LEVELS))}>
                {t("logs.viewer.all")}
              </Button>
              <Button variant="ghost" size="sm" className={smallButton} disabled={!settled} onClick={() => onSelectedLevelsChange(() => new Set())}>
                {t("logs.viewer.none")}
              </Button>
            </span>
          </div>
          {ALL_LOG_LEVELS.map((level) => (
            <label key={level} className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-muted pointer-coarse:py-2.5">
              <Checkbox checked={selectedLevels.has(level)} disabled={!settled} onCheckedChange={() => toggleLevel(level)} />
              <span className={cn("rounded px-1.5 py-0.5 text-xs font-medium uppercase", LEVEL_BADGE_COLORS[level])}>{level}</span>
            </label>
          ))}
        </div>
        {exclusions.length > 0 && (
          <div className="border-t p-2">
            <div className="mb-1 flex items-center justify-between gap-2 pl-2">
              <span className="text-xs font-medium">{t("logs.viewer.mutedMessages")}</span>
              <Button variant="ghost" size="sm" className={smallButton} onClick={() => onExclusionsChange([])}>
                {t("logs.viewer.clearAll")}
              </Button>
            </div>
            <ul className="max-h-48 overflow-y-auto">
              {exclusions.map((message) => (
                <li key={message} className="flex items-center gap-2 rounded py-0.5 pl-2 hover:bg-muted">
                  <span className="min-w-0 flex-1 text-xs [overflow-wrap:anywhere]">{message}</span>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t("logs.viewer.unmute", { message })}
                    onClick={() => onExclusionsChange(exclusions.filter((m) => m !== message))}
                    className="size-7 shrink-0 text-muted-foreground hover:text-foreground pointer-coarse:size-10"
                  >
                    <X className="size-3.5" />
                  </Button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}

function LogFilesSection() {
  const { t } = useTranslation("settings")
  const { formatDate } = useDateTimeFormatters()
  const [downloadingFiles, setDownloadingFiles] = useState<ReadonlySet<string>>(new Set())
  const query = useQuery({
    queryKey: ["log-files"],
    queryFn: () => api.getLogFiles(),
    staleTime: 30 * 1000,
  })

  const handleDownload = async (filename: string) => {
    setDownloadingFiles((prev) => new Set(prev).add(filename))
    try {
      await api.downloadLogFile(filename)
    } catch {
      toast.error(t("logs.files.toasts.downloadFailed", { filename }))
    } finally {
      setDownloadingFiles((prev) => {
        const next = new Set(prev)
        next.delete(filename)
        return next
      })
    }
  }

  return (
    <Section title={t("logs.files.title")} description={t("logs.files.description")} queries={[query]}>
      <ResourceList
        label={t("logs.files.title")}
        items={query.data ?? []}
        itemKey={(file) => file.name}
        row={(file) => ({
          title: file.name,
          icon: FileText,
          meta: [formatBytes(file.sizeBytes), formatDate(new Date(file.modTime))],
          actions: [{
            label: t("logs.files.download"),
            icon: Download,
            pending: downloadingFiles.has(file.name),
            onSelect: () => void handleDownload(file.name),
          }],
        })}
        empty={{ title: t("logs.files.emptyTitle"), description: t("logs.files.emptyDescription") }}
      />
    </Section>
  )
}
