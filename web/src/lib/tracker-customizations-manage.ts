/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TrackerCustomization, TrackerCustomizationInput } from "@/types"

const rowCollator = new Intl.Collator(undefined, { sensitivity: "base" })

export interface TrackerRow {
  displayName: string
  domain: string
  domains: string[]
  customizationId?: number
}

export function buildTrackerRows(activeDomains: Iterable<string>, customizations: readonly TrackerCustomization[]): TrackerRow[] {
  const customized = new Set<string>()
  const rows: TrackerRow[] = []
  for (const c of customizations) {
    for (const d of c.domains) customized.add(d.toLowerCase())
    rows.push({ displayName: c.displayName, domain: c.domains[0] ?? c.displayName, domains: c.domains, customizationId: c.id })
  }
  const seen = new Set<string>()
  for (const domain of activeDomains) {
    const key = domain.toLowerCase()
    if (customized.has(key) || seen.has(key)) continue
    seen.add(key)
    rows.push({ displayName: domain, domain, domains: [domain] })
  }
  return rows.sort((a, b) => rowCollator.compare(a.displayName, b.displayName))
}

export interface ImportEntry {
  index: number
  displayName: string
  domains: string[]
  conflict: { id: number; displayName: string; domains: string[] } | null
  isIdentical: boolean
}

export type ImportError = "invalidJson" | "invalidFormat" | "missingDisplayName" | "invalidDomains"

export type ImportParse = { valid: true; entries: ImportEntry[] } | { valid: false; error: ImportError | null }

export function parseTrackerCustomizationsImport(text: string, existing: readonly TrackerCustomization[]): ImportParse {
  let jsonText = text.trim()
  if (!jsonText) return { valid: false, error: null }
  if (jsonText.startsWith("```")) {
    jsonText = jsonText.replace(/^```(?:json)?\s*\n?/, "").replace(/\n?```\s*$/, "")
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(jsonText)
  } catch {
    return { valid: false, error: "invalidJson" }
  }

  const raw = (parsed as { trackerCustomizations?: unknown } | null)?.trackerCustomizations
  if (!Array.isArray(raw)) return { valid: false, error: "invalidFormat" }

  const byDomain = new Map<string, TrackerCustomization>()
  for (const c of existing) {
    for (const d of c.domains) byDomain.set(d.toLowerCase(), c)
  }

  const entries: ImportEntry[] = []
  for (const [index, entry] of raw.entries()) {
    const { displayName, domains } = (entry ?? {}) as { displayName?: unknown; domains?: unknown }
    if (!displayName || typeof displayName !== "string") return { valid: false, error: "missingDisplayName" }
    if (!Array.isArray(domains) || !domains.every(d => typeof d === "string")) {
      return { valid: false, error: "invalidDomains" }
    }
    const normalized = [...new Set(domains.map(d => d.trim().toLowerCase()).filter(Boolean))]
    if (normalized.length === 0) return { valid: false, error: "invalidDomains" }

    const match = normalized.map(d => byDomain.get(d)).find(c => c !== undefined)
    const conflict = match ? { id: match.id, displayName: match.displayName, domains: match.domains } : null
    let isIdentical = false
    if (conflict) {
      const have = conflict.domains.map(d => d.toLowerCase())
      isIdentical = conflict.displayName === displayName && have.length === normalized.length && normalized.every((d, i) => d === have[i])
    }
    entries.push({ index, displayName, domains: normalized, conflict, isIdentical })
  }
  return { valid: true, entries }
}

export function toCustomizationInput(entry: Pick<ImportEntry, "displayName" | "domains">): TrackerCustomizationInput {
  return { displayName: entry.displayName, domains: entry.domains }
}

export function exportTrackerCustomizations(customizations: readonly TrackerCustomization[]): string {
  const data = {
    comment: "qui tracker customizations",
    trackerCustomizations: customizations.map(toCustomizationInput),
  }
  return "```json\n" + JSON.stringify(data, null, 2) + "\n```"
}
