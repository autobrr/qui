/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { usePersistedLogExclusions } from "@/hooks/usePersistedLogExclusions"
import { api } from "@/lib/api"
import { copyTextToClipboard } from "@/lib/utils"
import type { LogSettingsUpdate } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, ChevronDown, Copy, FileText, Filter, Loader2, Lock, Search, Settings, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const LOG_LEVELS = ["TRACE", "DEBUG", "INFO", "WARN", "ERROR"] as const

function normalizeLogLevel(level: string | undefined): (typeof LOG_LEVELS)[number] {
  const normalized = level?.trim().toUpperCase()
  if (normalized && (LOG_LEVELS as readonly string[]).includes(normalized)) {
    return normalized as (typeof LOG_LEVELS)[number]
  }
  return "INFO"
}

function LogSettingsFormInner({ settings }: { settings: NonNullable<Awaited<ReturnType<typeof api.getLogSettings>>> }) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const queryClient = useQueryClient()

  const updateMutation = useMutation({
    mutationFn: (update: LogSettingsUpdate) => api.updateLogSettings(update),
    onSuccess: (data) => {
      queryClient.setQueryData(["log-settings"], data)
      toast.success(tr("logSettingsPanel.toasts.settingsUpdated"))
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : tr("logSettingsPanel.toasts.failedUpdateSettings"))
    },
  })

  // Initialize with actual settings values
  const initialLevel = normalizeLogLevel(settings.level)
  const form = useForm({
    defaultValues: {
      level: initialLevel,
      path: settings.path,
      maxSize: settings.maxSize,
      maxBackups: settings.maxBackups,
    },
    onSubmit: async ({ value }) => {
      const update: LogSettingsUpdate = {}
      if (value.level !== normalizeLogLevel(settings.level)) update.level = value.level
      if (value.path !== (settings.path ?? "")) update.path = value.path
      if (value.maxSize !== (settings.maxSize ?? 50)) update.maxSize = value.maxSize
      if (value.maxBackups !== (settings.maxBackups ?? 3)) update.maxBackups = value.maxBackups

      if (Object.keys(update).length > 0) {
        await updateMutation.mutateAsync(update)
      }
    },
  })

  // Reset form when settings change externally (e.g., config file edited, query refetch)
  useEffect(() => {
    form.reset({
      level: normalizeLogLevel(settings.level),
      path: settings.path,
      maxSize: settings.maxSize,
      maxBackups: settings.maxBackups,
    })
  }, [form, settings.level, settings.path, settings.maxSize, settings.maxBackups])

  const isLocked = (field: string) => settings.locked?.[field] !== undefined

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-3"
    >
      <form.Field name="level">
        {(field) => (
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <Label htmlFor="level" className="text-sm">{tr("logSettingsPanel.form.logLevel")}</Label>
              {isLocked("level") && (
                <Badge variant="outline" className="gap-1 text-xs">
                  <Lock className="h-3 w-3" />
                  {settings?.locked?.level}
                </Badge>
              )}
            </div>
            <Select
              value={field.state.value}
              onValueChange={(value) => field.handleChange(normalizeLogLevel(value))}
              disabled={isLocked("level")}
            >
              <SelectTrigger id="level" className="h-9">
                <SelectValue placeholder={tr("logSettingsPanel.form.selectLogLevel")} />
              </SelectTrigger>
              <SelectContent>
                {LOG_LEVELS.map((level) => (
                  <SelectItem key={level} value={level}>
                    {level}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </form.Field>

      <form.Field name="path">
        {(field) => (
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <Label htmlFor="path" className="text-sm">{tr("logSettingsPanel.form.logFilePath")}</Label>
              {isLocked("path") && (
                <Badge variant="outline" className="gap-1 text-xs">
                  <Lock className="h-3 w-3" />
                  {settings?.locked?.path}
                </Badge>
              )}
            </div>
            <Input
              id="path"
              className="h-9"
              placeholder={tr("logSettingsPanel.form.pathPlaceholder")}
              value={field.state.value}
              onChange={(e) => field.handleChange(e.target.value)}
              disabled={isLocked("path")}
            />
          </div>
        )}
      </form.Field>

      <div className="grid grid-cols-2 gap-3">
        <form.Field name="maxSize">
          {(field) => (
            <div className="space-y-1.5">
              <div className="flex items-center gap-2">
                <Label htmlFor="maxSize" className="text-sm">{tr("logSettingsPanel.form.maxSizeMb")}</Label>
                {isLocked("maxSize") && (
                  <Badge variant="outline" className="gap-1 text-xs">
                    <Lock className="h-3 w-3" />
                  </Badge>
                )}
              </div>
              <Input
                id="maxSize"
                className="h-9"
                type="number"
                min={1}
                value={field.state.value}
                onChange={(e) => field.handleChange(parseInt(e.target.value) || 50)}
                disabled={isLocked("maxSize")}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="maxBackups">
          {(field) => (
            <div className="space-y-1.5">
              <div className="flex items-center gap-2">
                <Label htmlFor="maxBackups" className="text-sm">{tr("logSettingsPanel.form.maxBackups")}</Label>
                {isLocked("maxBackups") && (
                  <Badge variant="outline" className="gap-1 text-xs">
                    <Lock className="h-3 w-3" />
                  </Badge>
                )}
              </div>
              <Input
                id="maxBackups"
                className="h-9"
                type="number"
                min={0}
                value={field.state.value}
                onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                disabled={isLocked("maxBackups")}
              />
            </div>
          )}
        </form.Field>
      </div>

      <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
        {([canSubmit, isSubmitting]) => (
          <Button type="submit" size="sm" className="w-full" disabled={!canSubmit || isSubmitting || updateMutation.isPending}>
            {isSubmitting || updateMutation.isPending ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                {tr("logSettingsPanel.actions.saving")}
              </>
            ) : (
              tr("logSettingsPanel.actions.saveSettings")
            )}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}

function LogSettingsForm() {
  const { data: settings, isLoading } = useQuery({
    queryKey: ["log-settings"],
    queryFn: () => api.getLogSettings(),
  })

  if (isLoading || !settings) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return <LogSettingsFormInner settings={settings} />
}

type LogLevel = "trace" | "debug" | "info" | "warn" | "error"

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
  debug: "text-blue-400",
  info: "text-green-400",
  warn: "text-yellow-400",
  error: "text-red-400",
}

const LEVEL_BADGE_COLORS: Record<LogLevel, string> = {
  trace: "bg-muted text-muted-foreground",
  debug: "bg-blue-500/20 text-blue-400",
  info: "bg-green-500/20 text-green-400",
  warn: "bg-yellow-500/20 text-yellow-400",
  error: "bg-red-500/20 text-red-400",
}

const VALID_LEVELS = new Set<LogLevel>(["trace", "debug", "info", "warn", "error"])

function normalizeLevel(raw: string | undefined): LogLevel {
  const level = raw?.toLowerCase()
  if (level && VALID_LEVELS.has(level as LogLevel)) {
    return level as LogLevel
  }
  // Coerce fatal/panic to error
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

    // Extract extra fields (everything except level, time, message)
    const { level: _l, time: _t, message: _m, ...extra } = parsed

    return { id: entry.id, level, time, message, extra, raw: entry.text, isJson: true }
  } catch {
    // Not valid JSON, return as raw text with info level
    return { id: entry.id, level: "info", time: "", message: entry.text, extra: {}, raw: entry.text, isJson: false }
  }
}

function formatTime(isoTime: string): string {
  if (!isoTime) return ""
  try {
    const date = new Date(isoTime)
    return date.toLocaleTimeString("en-US", {
      hour12: false,
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    })
  } catch {
    return ""
  }
}

function LogEntry({
  entry,
  onSelect,
  onMute,
}: {
  entry: ParsedLogEntry
  onSelect?: () => void
  onMute?: () => void
}) {
  const { t } = useTranslation("common")
  const tr = useCallback(
    (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never)),
    [t]
  )
  const extraKeys = Object.keys(entry.extra)
  const isClickable = onSelect && entry.isJson

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (isClickable && (e.key === "Enter" || e.key === " ")) {
      e.preventDefault()
      onSelect?.()
    }
  }

  const handleMute = (e: React.MouseEvent) => {
    e.stopPropagation()
    onMute?.()
  }

  return (
    <div
      className={`group flex gap-2 py-0.5 whitespace-nowrap hover:bg-muted/50 ${isClickable ? "cursor-pointer focus:outline-none focus:bg-muted/50" : ""}`}
      onClick={isClickable ? onSelect : undefined}
      onKeyDown={handleKeyDown}
      role={isClickable ? "button" : undefined}
      tabIndex={isClickable ? 0 : undefined}
    >
      <span className="shrink-0 text-muted-foreground/60">{formatTime(entry.time)}</span>
      <button
        type="button"
        onClick={onMute ? handleMute : undefined}
        disabled={!onMute}
        title={onMute ? tr("logSettingsPanel.logEntry.muteSimilarEntries") : undefined}
        className={`group/mute shrink-0 w-12 h-4 inline-flex items-center justify-center text-[10px] font-medium uppercase rounded ${LEVEL_BADGE_COLORS[entry.level]} ${onMute ? "cursor-pointer" : ""}`}
      >
        <span className={onMute ? "group-hover/mute:hidden" : ""}>{entry.level}</span>
        {onMute && <X className="hidden group-hover/mute:block size-2.5" />}
      </button>
      <span className={LEVEL_COLORS[entry.level]}>{entry.message}</span>
      {extraKeys.length > 0 && (
        <span className="text-muted-foreground/50">
          {extraKeys.map((key) => (
            <span key={key} className="ml-2">
              <span className="text-muted-foreground/70">{key}</span>
              <span className="text-muted-foreground/40">=</span>
              <span className="text-muted-foreground/60">
                {typeof entry.extra[key] === "string"
                  ? entry.extra[key] as string
                  : JSON.stringify(entry.extra[key])}
              </span>
            </span>
          ))}
        </span>
      )}
    </div>
  )
}

// Syntax highlight JSON string - returns React elements
function highlightJson(json: string): React.ReactNode[] {
  if (!json) return []

  // Token pattern: keys, strings, numbers, booleans, null, punctuation
  const tokenRegex = /("(?:\\.|[^"\\])*")\s*:|("(?:\\.|[^"\\])*")|(-?\d+\.?\d*(?:[eE][+-]?\d+)?)|(\btrue\b|\bfalse\b)|(\bnull\b)|([{}[\],:])/g

  const result: React.ReactNode[] = []
  let lastIndex = 0
  let key = 0

  for (const match of json.matchAll(tokenRegex)) {
    const matchIndex = match.index
    if (matchIndex == null) continue

    // Add any whitespace/text before this match
    if (matchIndex > lastIndex) {
      result.push(json.slice(lastIndex, matchIndex))
    }

    const [fullMatch, keyMatch, stringMatch, numberMatch, boolMatch, nullMatch, punctMatch] = match

    if (keyMatch) {
      // Key (property name) - theme-aware cyan/teal
      result.push(
        <span key={key++} className="text-cyan-700 dark:text-cyan-400">{keyMatch}</span>,
        ":"
      )
    } else if (stringMatch) {
      // String value - theme-aware green
      result.push(<span key={key++} className="text-green-700 dark:text-green-400">{stringMatch}</span>)
    } else if (numberMatch) {
      // Number - theme-aware amber
      result.push(<span key={key++} className="text-amber-700 dark:text-amber-400">{numberMatch}</span>)
    } else if (boolMatch) {
      // Boolean - theme-aware purple
      result.push(<span key={key++} className="text-purple-700 dark:text-purple-400">{boolMatch}</span>)
    } else if (nullMatch) {
      // null - muted
      result.push(<span key={key++} className="text-muted-foreground">{nullMatch}</span>)
    } else if (punctMatch) {
      // Punctuation - default color
      result.push(<span key={key++} className="text-muted-foreground/70">{punctMatch}</span>)
    } else {
      result.push(fullMatch)
    }

    lastIndex = matchIndex + fullMatch.length
  }

  // Add any remaining text
  if (lastIndex < json.length) {
    result.push(json.slice(lastIndex))
  }

  return result
}

function LogEntryDialog({
  entry,
  open,
  onOpenChange,
}: {
  entry: ParsedLogEntry | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  // Memoize JSON stringification and highlighting to avoid re-tokenizing on unrelated renders
  const { prettyJson, highlightedJson } = useMemo(() => {
    if (!entry) return { prettyJson: "", highlightedJson: [] as React.ReactNode[] }
    const jsonObj = {
      level: entry.level,
      time: entry.time,
      message: entry.message,
      ...entry.extra,
    }
    const pretty = JSON.stringify(jsonObj, null, 2)
    return { prettyJson: pretty, highlightedJson: highlightJson(pretty) }
  }, [entry])

  const handleCopyJson = useCallback(async () => {
    if (!prettyJson) return
    try {
      await copyTextToClipboard(prettyJson)
      toast.success(tr("logSettingsPanel.toasts.jsonCopied"))
    } catch {
      toast.error(tr("logSettingsPanel.toasts.failedCopyClipboard"))
    }
  }, [prettyJson, tr])

  const handleCopyRaw = useCallback(async () => {
    if (!entry) return
    try {
      await copyTextToClipboard(entry.raw)
      toast.success(tr("logSettingsPanel.toasts.rawLineCopied"))
    } catch {
      toast.error(tr("logSettingsPanel.toasts.failedCopyClipboard"))
    }
  }, [entry, tr])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="!max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {tr("logSettingsPanel.dialog.logEntryTitle")}
            {entry && (
              <span className={`text-xs font-medium uppercase px-1.5 py-0.5 rounded ${LEVEL_BADGE_COLORS[entry.level]}`}>
                {entry.level}
              </span>
            )}
          </DialogTitle>
          <DialogDescription>
            {entry?.time ? new Date(entry.time).toLocaleString() : tr("logSettingsPanel.dialog.rawLogLine")}
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-[400px] overflow-auto rounded-md border bg-muted/30 p-4">
          <pre className="font-mono text-sm whitespace-pre-wrap break-all">{highlightedJson}</pre>
        </div>
        <DialogFooter className="gap-2 sm:gap-0">
          <Button variant="outline" size="sm" onClick={handleCopyRaw}>
            <Copy className="mr-2 h-4 w-4" />
            {tr("logSettingsPanel.actions.copyRaw")}
          </Button>
          <Button variant="outline" size="sm" onClick={handleCopyJson}>
            <Copy className="mr-2 h-4 w-4" />
            {tr("logSettingsPanel.actions.copyJson")}
          </Button>
          <Button size="sm" onClick={() => onOpenChange(false)}>
            {tr("logSettingsPanel.actions.close")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const ALL_LOG_LEVELS: LogLevel[] = ["trace", "debug", "info", "warn", "error"]

// Buffer limits: soft cap when following live, hard cap always
const LOG_SOFT_CAP = 1000
const LOG_HARD_CAP = 10000

function LiveLogViewer({ configPath }: { configPath?: string }) {
  const { t } = useTranslation("common")
  const tr = useCallback(
    (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never)),
    [t]
  )
  const [lines, setLines] = useState<RawLogLine[]>([])
  const [autoScroll, setAutoScroll] = useState(true)
  const [isConnected, setIsConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selectedLevels, setSelectedLevels] = useState<Set<LogLevel>>(new Set(ALL_LOG_LEVELS))
  const [searchQuery, setSearchQuery] = useState("")
  const [droppedWhilePaused, setDroppedWhilePaused] = useState(false)
  const [selectedEntry, setSelectedEntry] = useState<ParsedLogEntry | null>(null)
  const [logExclusions, setLogExclusions] = usePersistedLogExclusions()
  const scrollRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const nextIdRef = useRef(0)
  const autoScrollRef = useRef(autoScroll)

  // Keep ref in sync for use in event handler
  useEffect(() => {
    autoScrollRef.current = autoScroll
    // Clear dropped warning when auto-scroll is enabled (trimming resumes)
    if (autoScroll) {
      setDroppedWhilePaused(false)
    }
  }, [autoScroll])

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }
    if (reconnectTimeoutRef.current !== null) {
      window.clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }

    setError(null)
    const url = api.getLogStreamUrl(1000)
    const es = new EventSource(url, { withCredentials: true })
    eventSourceRef.current = es

    es.onopen = () => {
      setIsConnected(true)
      setError(null)
    }

    es.onmessage = (event) => {
      const newEntry: RawLogLine = { id: nextIdRef.current++, text: event.data as string }
      setLines((prev) => {
        const newLines = [...prev, newEntry]
        // Soft cap when auto-scroll ON (user following live)
        if (autoScrollRef.current && newLines.length > LOG_SOFT_CAP) {
          return newLines.slice(-LOG_SOFT_CAP)
        }
        // Hard cap always to prevent unbounded memory
        if (newLines.length > LOG_HARD_CAP) {
          setDroppedWhilePaused(true)
          return newLines.slice(-LOG_HARD_CAP)
        }
        return newLines
      })
    }

    es.onerror = () => {
      setIsConnected(false)
      setError(tr("logSettingsPanel.states.connectionLostReconnecting"))
      es.close()
      reconnectTimeoutRef.current = window.setTimeout(connect, 3000)
    }
  }, [tr])

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

  // Parse entries once when lines change (avoid re-parsing on filter/search changes)
  const parsedEntries = useMemo(() => lines.map(parseLogLine), [lines])

  // Filter parsed entries
  const filteredEntries = useMemo(() => {
    const query = searchQuery.toLowerCase().trim()

    return parsedEntries.filter((e) => {
      // Filter by level
      if (!selectedLevels.has(e.level)) return false

      // Filter by muted messages
      if (logExclusions.includes(e.message)) return false

      // Filter by search query
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

  const toggleLevel = (level: LogLevel) => {
    setSelectedLevels((prev) => {
      const next = new Set(prev)
      if (next.has(level)) {
        next.delete(level)
      } else {
        next.add(level)
      }
      return next
    })
  }

  const selectAll = () => setSelectedLevels(new Set(ALL_LOG_LEVELS))
  const selectNone = () => setSelectedLevels(new Set())

  // Auto-scroll to bottom when enabled
  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [filteredEntries, autoScroll])

  const handleClear = () => {
    setLines([])
  }

  const handleMuteMessage = (message: string) => {
    if (!logExclusions.includes(message)) {
      setLogExclusions([...logExclusions, message])
    }
  }

  const handleUnmuteMessage = (message: string) => {
    setLogExclusions(logExclusions.filter((m) => m !== message))
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <div
            className={`h-2 w-2 rounded-full ${isConnected ? "bg-green-500" : "bg-red-500"}`}
          />
          <span className="text-sm text-muted-foreground">
            {isConnected ? tr("logSettingsPanel.states.connected") : tr("logSettingsPanel.states.disconnected")}
          </span>
          {error && (
            <span className="flex items-center gap-1 text-sm text-yellow-500">
              <AlertCircle className="h-3 w-3" />
              {error}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="text"
              placeholder={tr("logSettingsPanel.search.placeholder")}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="h-8 w-40 pl-7 pr-7 text-xs"
            />
            {searchQuery && (
              <button
                type="button"
                onClick={() => setSearchQuery("")}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="sm" className="h-8 gap-1">
                <Filter className="h-3.5 w-3.5" />
                <span className="text-xs">
                  {selectedLevels.size === ALL_LOG_LEVELS.length
                    ? tr("logSettingsPanel.filters.allLevels")
                    : selectedLevels.size === 0
                      ? tr("logSettingsPanel.filters.none")
                      : tr("logSettingsPanel.filters.selectedLevels", { count: selectedLevels.size })}
                </span>
                <ChevronDown className="h-3.5 w-3.5 opacity-50" />
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-44 p-2" align="start">
              <div className="flex justify-between mb-2">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  onClick={selectAll}
                >
                  {tr("logSettingsPanel.filters.all")}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  onClick={selectNone}
                >
                  {tr("logSettingsPanel.filters.none")}
                </Button>
              </div>
              <div className="space-y-1">
                {ALL_LOG_LEVELS.map((level) => (
                  <label
                    key={level}
                    className="flex items-center gap-2 px-2 py-1 rounded hover:bg-muted cursor-pointer"
                  >
                    <Checkbox
                      checked={selectedLevels.has(level)}
                      onCheckedChange={() => toggleLevel(level)}
                    />
                    <span
                      className={`text-xs font-medium uppercase ${LEVEL_BADGE_COLORS[level]} px-1.5 py-0.5 rounded`}
                    >
                      {level}
                    </span>
                  </label>
                ))}
              </div>
            </PopoverContent>
          </Popover>
          {logExclusions.length > 0 && (
            <Popover>
              <PopoverTrigger asChild>
                <Button variant="outline" size="sm" className="h-8 gap-1">
                  <span className="text-xs">{tr("logSettingsPanel.filters.mutedCount", { count: logExclusions.length })}</span>
                  <ChevronDown className="h-3.5 w-3.5 opacity-50" />
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-72 p-2" align="start">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-xs font-medium">{tr("logSettingsPanel.filters.mutedMessages")}</span>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-2 text-xs"
                    onClick={() => setLogExclusions([])}
                  >
                    {tr("logSettingsPanel.actions.clearAll")}
                  </Button>
                </div>
                <div className="space-y-1 max-h-48 overflow-auto">
                  {logExclusions.map((message) => (
                    <div
                      key={message}
                      className="flex items-center justify-between gap-2 px-2 py-1 rounded hover:bg-muted group"
                    >
                      <span className="text-xs truncate" title={message}>
                        {message}
                      </span>
                      <button
                        type="button"
                        onClick={() => handleUnmuteMessage(message)}
                        className="shrink-0 text-muted-foreground hover:text-foreground"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  ))}
                </div>
              </PopoverContent>
            </Popover>
          )}
          <Button variant="outline" size="sm" onClick={handleClear}>
            {tr("logSettingsPanel.actions.clear")}
          </Button>
          <div className="flex items-center gap-2">
            <Switch
              id="autoscroll"
              checked={autoScroll}
              onCheckedChange={setAutoScroll}
            />
            <Label htmlFor="autoscroll" className="text-sm">
              {tr("logSettingsPanel.filters.autoScroll")}
            </Label>
          </div>
        </div>
      </div>

      <div
        ref={scrollRef}
        className="h-[clamp(300px,calc(100dvh-24rem),600px)] overflow-auto rounded-md border bg-muted/30 p-3"
        style={{ overflowAnchor: "none" }}
      >
        <div className="font-mono text-xs leading-relaxed w-fit min-w-full">
          {filteredEntries.length > 0 ? (
            filteredEntries.map((entry) => (
              <LogEntry
                key={entry.id}
                entry={entry}
                onSelect={() => setSelectedEntry(entry)}
                onMute={() => handleMuteMessage(entry.message)}
              />
            ))
          ) : (
            <span className="text-muted-foreground">
              {lines.length > 0
                ? tr("logSettingsPanel.states.noEntriesMatchFilter")
                : tr("logSettingsPanel.states.waitingForEntries")}
            </span>
          )}
        </div>
      </div>

      <div className="flex items-center justify-between gap-4 text-xs text-muted-foreground">
        <span className="flex items-center gap-2">
          <span>
            {tr("logSettingsPanel.states.showingEntries", {
              shown: filteredEntries.length,
              total: lines.length,
            })}
            {autoScroll
              ? tr("logSettingsPanel.states.maxEntries", { max: LOG_SOFT_CAP.toLocaleString() })
              : tr("logSettingsPanel.states.maxEntriesWhilePaused", { max: LOG_HARD_CAP.toLocaleString() })}
          </span>
          {droppedWhilePaused && (
            <span className="text-yellow-500">{tr("logSettingsPanel.states.oldestEntriesDropped")}</span>
          )}
        </span>
        {configPath && (
          <span className="flex items-center gap-1.5 font-mono text-muted-foreground/70">
            <FileText className="h-3 w-3" />
            {configPath}
          </span>
        )}
      </div>

      <LogEntryDialog
        entry={selectedEntry}
        open={selectedEntry !== null}
        onOpenChange={(open) => !open && setSelectedEntry(null)}
      />
    </div>
  )
}

export function LogSettingsPanel() {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { data: settings } = useQuery({
    queryKey: ["log-settings"],
    queryFn: () => api.getLogSettings(),
  })

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <CardTitle>{tr("logSettingsPanel.panel.logsTitle")}</CardTitle>
            <CardDescription>
              {tr("logSettingsPanel.panel.logsDescription")}
            </CardDescription>
          </div>
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="icon" className="h-8 w-8 shrink-0">
                <Settings className="h-4 w-4" />
                <span className="sr-only">{tr("logSettingsPanel.panel.logSettingsSrOnly")}</span>
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-80" align="end">
              <div className="space-y-3">
                <div className="space-y-1">
                  <h4 className="font-medium text-sm">{tr("logSettingsPanel.panel.logConfigurationTitle")}</h4>
                  <p className="text-xs text-muted-foreground">
                    {tr("logSettingsPanel.panel.changesAppliedImmediately")}
                  </p>
                </div>
                <LogSettingsForm />
              </div>
            </PopoverContent>
          </Popover>
        </div>
      </CardHeader>
      <CardContent>
        <LiveLogViewer configPath={settings?.configPath} />
      </CardContent>
    </Card>
  )
}
