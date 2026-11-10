/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

const LIMIT_USE_GLOBAL = -2
const LIMIT_UNLIMITED = -1

export type TorrentLimitSnapshot = Pick<
  Torrent,
  | "ratio_limit"
  | "seeding_time_limit"
  | "inactive_seeding_time_limit"
  | "max_ratio"
  | "max_seeding_time"
  | "max_inactive_seeding_time"
  | "dl_limit"
  | "up_limit"
  | "share_limit_action"
  | "share_limits_mode"
>

function checkFieldConsistency(
  torrents: TorrentLimitSnapshot[] | undefined,
  getter: (t: TorrentLimitSnapshot) => number | undefined
): { isMixed: boolean; commonValue: number | undefined } {
  if (!torrents || torrents.length === 0) {
    return { isMixed: false, commonValue: undefined }
  }
  const firstValue = getter(torrents[0])
  const allSame = torrents.every(t => getter(t) === firstValue)
  return { isMixed: !allSame, commonValue: allSame ? firstValue : undefined }
}

// A dialog field whose value can differ across the selected torrents.
interface MixedField {
  isMixed: boolean
  // The user changed the field after the dialog opened.
  touched: boolean
}

export type ShareLimitMode = "global" | "unlimited" | "custom"

interface ShareLimitField extends MixedField {
  mode: ShareLimitMode
  customValue: number
}

interface ShareLimitChoiceField extends MixedField {
  value: string
}

export interface ShareLimitFormState {
  ratio: ShareLimitField
  seedTime: ShareLimitField
  inactiveTime: ShareLimitField
  action: ShareLimitChoiceField
  matchingMode: ShareLimitChoiceField
}

interface ShareLimitValues {
  ratioLimit: number
  seedingTimeLimit: number
  inactiveSeedingTimeLimit: number
  shareLimitAction?: string
  shareLimitsMode?: string
}

export function isUnresolvedMixed(field: MixedField): boolean {
  return field.isMixed && !field.touched
}

function shareLimitField(
  torrents: TorrentLimitSnapshot[] | undefined,
  getter: (t: TorrentLimitSnapshot) => number | undefined,
  defaultCustom: number
): ShareLimitField {
  const { isMixed, commonValue } = checkFieldConsistency(torrents, getter)
  const field = { isMixed, touched: false, customValue: defaultCustom }
  if (commonValue === undefined || commonValue === LIMIT_USE_GLOBAL) {
    return { ...field, mode: "global" }
  }
  if (commonValue === LIMIT_UNLIMITED) {
    return { ...field, mode: "unlimited" }
  }
  return { ...field, mode: "custom", customValue: commonValue }
}

/** Qt share-limit enum; when torrents disagree, value is "default" placeholder and isMixed is true. */
function shareLimitChoiceField(
  torrents: TorrentLimitSnapshot[] | undefined,
  pick: (t: TorrentLimitSnapshot) => string | undefined
): ShareLimitChoiceField {
  if (!torrents?.length) return { value: "default", isMixed: false, touched: false }
  const vals = torrents.map((t) => {
    const v = (pick(t) ?? "").trim()
    return v === "" || v === "Default" ? "default" : v
  })
  const first = vals[0]!
  const isMixed = !vals.every((x) => x === first)
  return { value: isMixed ? "default" : first, isMixed, touched: false }
}

export function buildShareLimitFormState(torrents?: TorrentLimitSnapshot[]): ShareLimitFormState {
  return {
    ratio: shareLimitField(torrents, t => t.ratio_limit, 1.0),
    seedTime: shareLimitField(torrents, t => t.seeding_time_limit, 1440),
    inactiveTime: shareLimitField(torrents, t => t.inactive_seeding_time_limit, 10080),
    action: shareLimitChoiceField(torrents, t => t.share_limit_action),
    matchingMode: shareLimitChoiceField(torrents, t => t.share_limits_mode),
  }
}

// qBittorrent writes all three limits to every selected torrent in one request,
// so the dialog cannot keep the own value of each torrent. Confirm stays
// disabled until the user picks a value for each mixed field.
export function shareLimitsUnresolved(
  form: ShareLimitFormState,
  supportsShareLimitsAction: boolean,
  supportsShareLimitsMode: boolean
): boolean {
  return isUnresolvedMixed(form.ratio) ||
    isUnresolvedMixed(form.seedTime) ||
    isUnresolvedMixed(form.inactiveTime) ||
    (supportsShareLimitsAction && isUnresolvedMixed(form.action)) ||
    (supportsShareLimitsMode && isUnresolvedMixed(form.matchingMode))
}

function shareLimitFieldValue(field: ShareLimitField): number {
  switch (field.mode) {
    case "global":
      return LIMIT_USE_GLOBAL
    case "unlimited":
      return LIMIT_UNLIMITED
    case "custom":
      return field.customValue
  }
}

function shareLimitChoiceValue(field: ShareLimitChoiceField, supported: boolean): string | undefined {
  return supported && field.value !== "default" ? field.value : undefined
}

export function shareLimitValues(
  form: ShareLimitFormState,
  supportsShareLimitsAction: boolean,
  supportsShareLimitsMode: boolean
): ShareLimitValues {
  return {
    ratioLimit: Math.round(shareLimitFieldValue(form.ratio) * 100) / 100,
    seedingTimeLimit: shareLimitFieldValue(form.seedTime),
    inactiveSeedingTimeLimit: shareLimitFieldValue(form.inactiveTime),
    shareLimitAction: shareLimitChoiceValue(form.action, supportsShareLimitsAction),
    shareLimitsMode: shareLimitChoiceValue(form.matchingMode, supportsShareLimitsMode),
  }
}

export interface SpeedLimitField extends MixedField {
  enabled: boolean
  // KiB/s
  limit: number
}

export interface SpeedLimitFormState {
  upload: SpeedLimitField
  download: SpeedLimitField
}

function speedLimitField(
  torrents: TorrentLimitSnapshot[] | undefined,
  getter: (t: TorrentLimitSnapshot) => number | undefined
): SpeedLimitField {
  const { isMixed, commonValue } = checkFieldConsistency(torrents, t => getter(t) ?? 0)
  const bytes = commonValue ?? 0
  return {
    isMixed,
    touched: false,
    enabled: bytes > 0,
    limit: bytes > 0 ? Math.round(bytes / 1024) : 0,
  }
}

export function buildSpeedLimitFormState(torrents?: TorrentLimitSnapshot[]): SpeedLimitFormState {
  return {
    upload: speedLimitField(torrents, t => t.up_limit),
    download: speedLimitField(torrents, t => t.dl_limit),
  }
}

// Same rule as shareLimitsUnresolved: the limit goes to every selected torrent.
export function speedLimitsUnresolved(form: SpeedLimitFormState): boolean {
  return isUnresolvedMixed(form.upload) || isUnresolvedMixed(form.download)
}

// 0 removes the limit of the torrent.
export function speedLimitValues(form: SpeedLimitFormState): { uploadLimit: number; downloadLimit: number } {
  return {
    uploadLimit: form.upload.enabled ? form.upload.limit : 0,
    downloadLimit: form.download.enabled ? form.download.limit : 0,
  }
}
