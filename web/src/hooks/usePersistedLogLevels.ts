/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"

export type LogLevel = "trace" | "debug" | "info" | "warn" | "error"

export const ALL_LOG_LEVELS: LogLevel[] = ["trace", "debug", "info", "warn", "error"]

export const ALL_LEVELS_SET = new Set<LogLevel>(ALL_LOG_LEVELS)

const parseLogLevels = (raw: string): Set<LogLevel> => {
  const parsed = JSON.parse(raw)
  if (!Array.isArray(parsed)) throw new Error("invalid log levels")
  return new Set(parsed.filter((level): level is LogLevel => ALL_LEVELS_SET.has(level)))
}

const serializeLogLevels = (value: Set<LogLevel>): string => JSON.stringify(Array.from(value))

/** The live log viewer's level filter; it never changes the server log level. */
export function usePersistedLogLevels() {
  return useClientSetting<Set<LogLevel>>("qui-log-levels", {
    defaultValue: ALL_LEVELS_SET,
    parse: parseLogLevels,
    serialize: serializeLogLevels,
  })
}
