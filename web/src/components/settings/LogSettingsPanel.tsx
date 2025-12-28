/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
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
import { api } from "@/lib/api"
import type { LogSettingsUpdate } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, ChevronDown, FileText, Filter, Loader2, Lock, Search, Settings, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
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
  const queryClient = useQueryClient()

  const updateMutation = useMutation({
    mutationFn: (update: LogSettingsUpdate) => api.updateLogSettings(update),
    onSuccess: (data) => {
      queryClient.setQueryData(["log-settings"], data)
      toast.success("Log settings updated")
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to update log settings")
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
              <Label htmlFor="level" className="text-sm">Log Level</Label>
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
                <SelectValue placeholder="Select log level" />
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
              <Label htmlFor="path" className="text-sm">Log File Path</Label>
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
              placeholder="Leave empty for stdout only"
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
                <Label htmlFor="maxSize" className="text-sm">Max Size (MB)</Label>
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
                <Label htmlFor="maxBackups" className="text-sm">Max Backups</Label>
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
                Saving...
              </>
            ) : (
              "Save Settings"
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

function parseLogLine(entry: RawLogLine): ParsedLogEntry | null {
  try {
    const parsed = JSON.parse(entry.text) as Record<string, unknown>
    const level = normalizeLevel(typeof parsed.level === "string" ? parsed.level : undefined)
    const time = typeof parsed.time === "string" ? parsed.time : ""
    const message = typeof parsed.message === "string" ? parsed.message : ""

    // Extract extra fields (everything except level, time, message)
    const { level: _l, time: _t, message: _m, ...extra } = parsed

    return { id: entry.id, level, time, message, extra, raw: entry.text }
  } catch {
    // Not valid JSON, return as raw text with info level
    return { id: entry.id, level: "info", time: "", message: entry.text, extra: {}, raw: entry.text }
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

function LogEntry({ entry }: { entry: ParsedLogEntry }) {
  const extraKeys = Object.keys(entry.extra)

  return (
    <div className="flex gap-2 py-0.5 whitespace-nowrap hover:bg-muted/50">
      <span className="shrink-0 text-muted-foreground/60">{formatTime(entry.time)}</span>
      <span
        className={`shrink-0 w-12 inline-flex items-center justify-center text-[10px] font-medium uppercase rounded py-0.5 ${LEVEL_BADGE_COLORS[entry.level]}`}
      >
        {entry.level}
      </span>
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

const ALL_LOG_LEVELS: LogLevel[] = ["trace", "debug", "info", "warn", "error"]

// Buffer limits: soft cap when following live, hard cap always
const LOG_SOFT_CAP = 1000
const LOG_HARD_CAP = 10000

function LiveLogViewer({ configPath }: { configPath?: string }) {
  const [lines, setLines] = useState<RawLogLine[]>([])
  const [autoScroll, setAutoScroll] = useState(true)
  const [isConnected, setIsConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selectedLevels, setSelectedLevels] = useState<Set<LogLevel>>(new Set(ALL_LOG_LEVELS))
  const [searchQuery, setSearchQuery] = useState("")
  const [droppedWhilePaused, setDroppedWhilePaused] = useState(false)
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
      setError("Connection lost. Reconnecting...")
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

  // Parse and filter log entries
  const filteredEntries = useMemo(() => {
    const entries = lines
      .map(parseLogLine)
      .filter((e): e is ParsedLogEntry => e !== null)

    const query = searchQuery.toLowerCase().trim()

    return entries.filter((e) => {
      // Filter by level
      if (!selectedLevels.has(e.level)) return false

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
  }, [lines, selectedLevels, searchQuery])

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

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <div
            className={`h-2 w-2 rounded-full ${isConnected ? "bg-green-500" : "bg-red-500"}`}
          />
          <span className="text-sm text-muted-foreground">
            {isConnected ? "Connected" : "Disconnected"}
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
              placeholder="Search logs..."
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
                    ? "All Levels"
                    : selectedLevels.size === 0
                      ? "None"
                      : `${selectedLevels.size} Level${selectedLevels.size > 1 ? "s" : ""}`}
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
                  All
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  onClick={selectNone}
                >
                  None
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
          <Button variant="outline" size="sm" onClick={handleClear}>
            Clear
          </Button>
          <div className="flex items-center gap-2">
            <Switch
              id="autoscroll"
              checked={autoScroll}
              onCheckedChange={setAutoScroll}
            />
            <Label htmlFor="autoscroll" className="text-sm">
              Auto-scroll
            </Label>
          </div>
        </div>
      </div>

      <div
        ref={scrollRef}
        className="h-[400px] overflow-auto rounded-md border bg-muted/30 p-3"
        style={{ overflowAnchor: "none" }}
      >
        <div className="font-mono text-xs leading-relaxed w-fit min-w-full">
          {filteredEntries.length > 0 ? (
            filteredEntries.map((entry) => <LogEntry key={entry.id} entry={entry} />)
          ) : (
            <span className="text-muted-foreground">
              {lines.length > 0
                ? "No entries match the current filter"
                : "Waiting for log entries..."}
            </span>
          )}
        </div>
      </div>

      <div className="flex items-center justify-between gap-4 text-xs text-muted-foreground">
        <span className="flex items-center gap-2">
          <span>
            Showing {filteredEntries.length} of {lines.length} entries
            {autoScroll ? ` (${LOG_SOFT_CAP.toLocaleString()} max)` : ` (${LOG_HARD_CAP.toLocaleString()} max while paused)`}
          </span>
          {droppedWhilePaused && (
            <span className="text-yellow-500">• oldest entries dropped</span>
          )}
        </span>
        {configPath && (
          <span className="flex items-center gap-1.5 font-mono text-muted-foreground/70">
            <FileText className="h-3 w-3" />
            {configPath}
          </span>
        )}
      </div>
    </div>
  )
}

export function LogSettingsPanel() {
  const { data: settings } = useQuery({
    queryKey: ["log-settings"],
    queryFn: () => api.getLogSettings(),
  })

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <CardTitle>Logs</CardTitle>
            <CardDescription>
              Real-time application logs. Disable auto-scroll to browse history.
            </CardDescription>
          </div>
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="icon" className="h-8 w-8 shrink-0">
                <Settings className="h-4 w-4" />
                <span className="sr-only">Log settings</span>
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-80" align="end">
              <div className="space-y-3">
                <div className="space-y-1">
                  <h4 className="font-medium text-sm">Log Configuration</h4>
                  <p className="text-xs text-muted-foreground">
                    Changes are applied immediately.
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
