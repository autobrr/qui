/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// In-memory library for the getqui.com demo build. Two instances of synthetic
// torrents, a query engine that answers the list endpoint, and the fake writes
// behind bulk actions. Built once from a fixed seed, so every visitor sees the
// same library and a reload resets it.

import type { AppPreferences } from "@/types/app"
import type { InstanceCapabilities, InstanceResponse } from "@/types/instances"
import type {
  Category,
  CrossInstanceTorrent,
  ServerState,
  SortedPeersResponse,
  Torrent,
  TorrentCounts,
  TorrentFile,
  TorrentFilters,
  TorrentProperties,
  TorrentResponse,
  TorrentStats,
  TorrentTracker
} from "@/types/torrents"

const KIB = 1024
const MIB = KIB * 1024
const GIB = MIB * 1024
const QBIT_INFINITE_ETA = 8640000
const TICK_SECONDS = 2

export function mulberry32(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function pick<T>(rng: () => number, list: readonly T[]): T {
  return list[Math.floor(rng() * list.length)]
}

function hex(rng: () => number, length: number): string {
  let out = ""
  for (let i = 0; i < length; i++) out += Math.floor(rng() * 16).toString(16)
  return out
}

const DISTROS = [
  { name: "ubuntu", versions: ["22.04.5", "24.04.3", "25.04"], variants: ["desktop", "server", "live-server"], sub: "distributions/ubuntu" },
  { name: "debian", versions: ["12.9.0", "13.0.0", "13.1.0"], variants: ["netinst", "DVD-1", "live-gnome"], sub: "distributions/debian" },
  { name: "fedora", versions: ["41-1.4", "42-1.1", "43-1.2"], variants: ["Workstation-Live", "Server-dvd", "KDE-Live"], sub: "distributions/fedora" },
  { name: "archlinux", versions: ["2026.06.01", "2026.07.01", "2026.09.01"], variants: [""], sub: "distributions/arch" },
  { name: "linuxmint", versions: ["22", "22.1", "22.2"], variants: ["cinnamon", "mate", "xfce"], sub: "distributions" },
  { name: "opensuse", versions: ["Leap-15.6", "Tumbleweed-20260901"], variants: ["DVD", "NET"], sub: "distributions" },
  { name: "rocky", versions: ["9.5", "10.0"], variants: ["minimal", "dvd"], sub: "server-editions" },
  { name: "almalinux", versions: ["9.5", "10.0"], variants: ["boot", "dvd"], sub: "server-editions" },
  { name: "proxmox-ve", versions: ["8.3-1", "9.0-1"], variants: [""], sub: "server-editions" },
  { name: "tails", versions: ["6.10", "6.12"], variants: ["amd64"], sub: "live-usb" },
  { name: "kali-linux", versions: ["2026.1", "2026.2"], variants: ["live", "installer"], sub: "live-usb" },
  { name: "raspios", versions: ["2026-05-13", "2026-08-20"], variants: ["lite", "full"], sub: "arm-builds" },
  { name: "armbian", versions: ["25.5", "25.8"], variants: ["bookworm", "noble"], sub: "arm-builds" },
] as const

const ARCHES = ["amd64", "x86_64", "arm64", "aarch64"] as const

const SOURCES = [
  { name: "linux", versions: ["6.12.4", "6.14.1", "6.16.0"] },
  { name: "gcc", versions: ["14.2.0", "15.1.0"] },
  { name: "llvm-project", versions: ["19.1.7", "20.1.0"] },
  { name: "postgresql", versions: ["17.2", "18.0"] },
  { name: "qemu", versions: ["9.2.0", "10.0.0"] },
] as const

const DOCS = [
  "debian-handbook-2026", "arch-wiki-offline-2026-08", "gentoo-handbook-2026", "freebsd-handbook-14",
  "the-linux-command-line-2nd", "pro-git-2nd-edition", "sqlite-docs-3.49", "postgresql-17-manual",
] as const

// Public-domain films, so the release-style names infringe nothing.
const FILMS = [
  ["Nosferatu", 1922], ["Metropolis", 1927], ["The.General", 1926], ["Safety.Last", 1923],
  ["The.Cabinet.of.Dr.Caligari", 1920], ["Battleship.Potemkin", 1925], ["Sherlock.Jr", 1924],
  ["A.Trip.to.the.Moon", 1902], ["The.Kid", 1921], ["Haxan", 1922], ["Sunrise", 1927],
  ["The.Phantom.of.the.Opera", 1925], ["Steamboat.Bill.Jr", 1928], ["Night.of.the.Living.Dead", 1968],
  ["His.Girl.Friday", 1940], ["Charade", 1963], ["The.Last.Man.on.Earth", 1964], ["Carnival.of.Souls", 1962],
] as const
const RESOLUTIONS = ["720p", "1080p", "2160p"] as const
const FILM_SOURCES = ["BluRay.x264", "WEB-DL.x265", "BluRay.REMUX.AVC", "WEB.H264"] as const

const TRACKERS = [
  "tracker.debian-mirror.example", "announce.fedora-seeds.example", "linuxtracker.example",
  "open.archive-seeds.example", "tracker.iso-hub.example", "public-domain.example",
] as const

const TAGS = ["stable", "lts", "keep", "cross-seed", "raw", "nightly"] as const

export const CATEGORY_NAMES = [
  "distributions", "distributions/ubuntu", "distributions/debian", "distributions/fedora", "distributions/arch",
  "server-editions", "live-usb", "arm-builds", "documentation", "source-code", "films",
] as const

const STATE_SORT_ORDER: Record<string, number> = {
  downloading: 20, metaDL: 21, forcedDL: 22, allocating: 23, checkingDL: 24, queuedDL: 25, stalledDL: 30,
  uploading: 40, forcedUP: 41, stoppedDL: 42, stoppedUP: 43, queuedUP: 44, stalledUP: 45,
  pausedDL: 50, pausedUP: 51, checkingUP: 60, checkingResumeData: 61, moving: 70, error: 80, missingFiles: 81,
}

// Mirrors torrentStateCategories in internal/qbittorrent/sync_manager.go.
const STATE_CATEGORIES: Record<string, readonly string[]> = {
  downloading: ["downloading", "stalledDL", "metaDL", "queuedDL", "allocating", "checkingDL", "forcedDL"],
  uploading: ["uploading", "stalledUP", "queuedUP", "checkingUP", "forcedUP"],
  seeding: ["uploading", "stalledUP", "queuedUP", "checkingUP", "forcedUP"],
  paused: ["pausedDL", "pausedUP", "stoppedDL", "stoppedUP"],
  active: ["downloading", "uploading", "forcedDL", "forcedUP"],
  stalled: ["stalledDL", "stalledUP"],
  checking: ["checkingDL", "checkingUP", "checkingResumeData"],
  errored: ["error", "missingFiles"],
  moving: ["moving"],
  stalled_uploading: ["stalledUP"],
  stalled_downloading: ["stalledDL"],
  stopped: ["stoppedDL", "stoppedUP"],
}

function inCategory(state: string, category: string): boolean {
  return STATE_CATEGORIES[category]?.includes(state) ?? false
}

function isPausedOrStopped(state: string): boolean {
  return inCategory(state, "paused") || inCategory(state, "stopped")
}

export function matchesStatus(t: Torrent, status: string): boolean {
  switch (status.toLowerCase()) {
    case "unregistered":
    case "tracker_down":
    case "tracker_error":
      return t.tracker_health === status
    case "all":
      return true
    case "completed":
      return t.progress === 1
    case "inactive":
      return !inCategory(t.state, "active")
    case "running":
    case "resumed":
      return !isPausedOrStopped(t.state)
    case "stopped":
    case "paused":
      return isPausedOrStopped(t.state)
    case "error":
      return inCategory(t.state, "errored")
  }
  if (STATE_CATEGORIES[status]) return inCategory(t.state, status)
  return t.state === status
}

export interface DemoInstance {
  id: number
  name: string
  torrents: Torrent[]
  categories: Record<string, Category>
  tags: Set<string>
  serverState: ServerState
  // Detail writes the app can make. Details are derived from the hash, so these overlay them.
  comments: Map<string, string>
  renames: Map<string, Array<[string, string]>>
  filePriorities: Map<string, Map<number, number>>
  bannedPeers: Set<string>
}

export interface ListParams {
  page: number
  limit: number
  sort: string
  order: "asc" | "desc"
  search: string
  filters: TorrentFilters | null
}

export interface DemoStore {
  instances: DemoInstance[]
  instance(id: number): DemoInstance | undefined
  query(instanceIds: number[], params: ListParams, crossInstance?: boolean): TorrentResponse
  tick(): void
  bulkAction(instanceId: number, body: BulkActionBody): void
  addTorrent(instanceId: number, name: string, category: string, tags: string[], paused: boolean, autoTMM: boolean, savePath: string): void
  addCategory(instanceId: number, name: string, savePath: string): void
  removeCategories(instanceId: number, names: string[]): void
  addTags(instanceId: number, tags: string[]): void
  removeTags(instanceId: number, tags: string[]): void
  rename(instanceId: number, hash: string, name: string): void
  renamePath(instanceId: number, hash: string, oldPath: string, newPath: string): void
  setFilePriority(instanceId: number, hash: string, indices: number[], priority: number): void
  banPeers(instanceId: number, peers: string[]): void
  toggleAltSpeedLimits(instanceId: number): boolean
  find(instanceId: number, hash: string): Torrent | undefined
  details(instanceId: number, hash: string): TorrentDetails | undefined
  instanceResponses(): InstanceResponse[]
}

export interface BulkActionBody {
  hashes?: string[]
  targets?: Array<{ instanceId: number; hash: string }>
  action: string
  deleteFiles?: boolean
  category?: string
  tags?: string
  comment?: string
  enable?: boolean
  selectAll?: boolean
  filters?: TorrentFilters
  search?: string
  excludeHashes?: string[]
  excludeTargets?: Array<{ instanceId: number; hash: string }>
  instanceIds?: number[]
  ratioLimit?: number
  seedingTimeLimit?: number
  inactiveSeedingTimeLimit?: number
  uploadLimit?: number
  downloadLimit?: number
  location?: string
  trackerOldURL?: string
  trackerNewURL?: string
}

export interface TorrentDetails {
  properties: TorrentProperties
  trackers: TorrentTracker[]
  files: TorrentFile[]
  peers: SortedPeersResponse
  pieces: number[]
}

function generateName(rng: () => number) {
  const roll = rng()
  if (roll < 0.62) {
    const d = pick(rng, DISTROS)
    const variant = pick(rng, d.variants)
    const arch = pick(rng, ARCHES)
    const name = [d.name, pick(rng, d.versions), variant, arch].filter(Boolean).join("-") + ".iso"
    const size = Math.round((0.6 + rng() * 5) * GIB)
    return { name, category: d.sub, size }
  }
  if (roll < 0.74) {
    const s = pick(rng, SOURCES)
    return { name: `${s.name}-${pick(rng, s.versions)}.tar.xz`, category: "source-code", size: Math.round((30 + rng() * 200) * MIB) }
  }
  if (roll < 0.84) {
    return { name: `${pick(rng, DOCS)}.pdf`, category: "documentation", size: Math.round((4 + rng() * 60) * MIB) }
  }
  const [title, year] = pick(rng, FILMS)
  const res = pick(rng, RESOLUTIONS)
  const src = pick(rng, FILM_SOURCES)
  const size = res === "2160p" ? (20 + rng() * 60) * GIB : res === "1080p" ? (4 + rng() * 20) * GIB : (1 + rng() * 4) * GIB
  return { name: `${title}.${year}.${res}.${src}-DEMO`, category: "films", size: Math.round(size) }
}

function generateState(rng: () => number): string {
  const roll = rng()
  if (roll < 0.62) return "uploading"
  if (roll < 0.905) return "stalledUP"
  if (roll < 0.92) return "downloading"
  if (roll < 0.935) return "stalledDL"
  if (roll < 0.945) return "queuedDL"
  if (roll < 0.955) return "stoppedDL"
  if (roll < 0.97) return "stoppedUP"
  if (roll < 0.98) return "checkingUP"
  if (roll < 0.99) return "queuedUP"
  return "error"
}

function generateTorrent(rng: () => number, now: number, index: number): Torrent {
  const { name, category, size } = generateName(rng)
  const state = generateState(rng)
  const hash = hex(rng, 40)
  const added = now - Math.floor(rng() * 730 * 86400)
  const tracker = pick(rng, TRACKERS)
  const tagCount = rng() < 0.3 ? 0 : 1 + Math.floor(rng() * 2)
  const tags = new Set<string>()
  for (let i = 0; i < tagCount; i++) tags.add(pick(rng, TAGS))
  const seeding = inCategory(state, "uploading") || state === "stoppedUP" || state === "error"
  const progress = seeding ? 1 : Math.round(rng() * 0.97 * 1000) / 1000
  const downloaded = Math.round(size * progress)
  const ratio = seeding ? Math.round(Math.pow(rng(), 2) * 12 * 100) / 100 : Math.round(rng() * progress * 100) / 100
  const uploaded = Math.round(size * ratio)
  const completion = seeding ? added + Math.floor(rng() * 3 * 86400) : 0
  const active = state === "downloading" || (state === "uploading" && rng() < 0.08)
  const dlspeed = state === "downloading" ? Math.round((0.3 + rng() * 6) * MIB) : 0
  const upspeed = active && state === "uploading" ? Math.round((50 + rng() * 4000) * KIB) : 0
  const health = rng() < 0.012 ? "unregistered" : rng() < 0.004 ? "tracker_down" : undefined
  const savePath = `/data/torrents/${category.split("/")[0]}`
  return {
    added_on: added,
    amount_left: size - downloaded,
    auto_tmm: true,
    availability: seeding ? -1 : Math.round((1 + rng() * 20) * 100) / 100,
    category,
    completed: downloaded,
    completion_on: completion,
    content_path: `${savePath}/${name}`,
    dl_limit: 0,
    dlspeed,
    download_path: "",
    downloaded,
    downloaded_session: state === "downloading" ? Math.round(downloaded * rng()) : 0,
    eta: dlspeed > 0 ? Math.round((size - downloaded) / dlspeed) : QBIT_INFINITE_ETA,
    f_l_piece_prio: false,
    force_start: false,
    hash,
    infohash_v1: hash,
    infohash_v2: "",
    popularity: Math.round((ratio / Math.max(1, (now - added) / 86400)) * 1000) / 1000,
    private: tracker !== "public-domain.example",
    last_activity: active ? now : completion || added,
    max_ratio: -1,
    max_seeding_time: -1,
    name,
    num_complete: Math.floor(rng() * 800),
    num_incomplete: Math.floor(rng() * 40),
    num_leechs: active ? Math.floor(rng() * 12) : 0,
    num_seeds: active ? Math.floor(rng() * 60) : 0,
    priority: state === "downloading" || state === "stalledDL" || state === "queuedUP" ? index + 1 : 0,
    progress,
    ratio,
    ratio_limit: -2,
    reannounce: Math.floor(rng() * 1800),
    save_path: savePath,
    seeding_time: seeding ? Math.max(0, now - completion) : 0,
    seeding_time_limit: -2,
    seen_complete: seeding ? now - Math.floor(rng() * 86400) : 0,
    seq_dl: false,
    size,
    state,
    super_seeding: false,
    tags: [...tags].sort().join(", "),
    time_active: now - added,
    total_size: size,
    tracker: `https://${tracker}/announce`,
    trackers_count: 1,
    tracker_health: health,
    up_limit: 0,
    uploaded,
    uploaded_session: active ? Math.round(uploaded * rng() * 0.1) : 0,
    upspeed,
  }
}

export function trackerDomain(t: Torrent): string {
  try {
    return new URL(t.tracker).hostname
  } catch {
    return ""
  }
}

function tagList(t: Torrent): string[] {
  return t.tags ? t.tags.split(",").map(s => s.trim()).filter(Boolean) : []
}

function makeServerState(name: string): ServerState {
  return {
    connection_status: "connected",
    dht_nodes: 342,
    dl_info_data: 0,
    dl_info_speed: 0,
    dl_rate_limit: 0,
    up_info_data: 0,
    up_info_speed: 0,
    up_rate_limit: 0,
    queueing: true,
    use_alt_speed_limits: false,
    use_subcategories: true,
    refresh_interval: 1500,
    free_space_on_disk: name === "seedbox" ? 6.2 * 1024 * GIB : 1.1 * 1024 * GIB,
    total_peer_connections: 0,
    alltime_dl: 0,
    alltime_ul: 0,
    global_ratio: "0",
  }
}

function buildInstance(id: number, name: string, count: number, seed: number, now: number): DemoInstance {
  const rng = mulberry32(seed)
  const torrents: Torrent[] = []
  for (let i = 0; i < count; i++) torrents.push(generateTorrent(rng, now, i))
  const categories: Record<string, Category> = {}
  for (const c of CATEGORY_NAMES) categories[c] = { name: c, savePath: `/data/torrents/${c.split("/")[0]}` }
  const tags = new Set<string>(TAGS)
  const serverState = makeServerState(name)
  serverState.alltime_dl = torrents.reduce((sum, t) => sum + t.downloaded, 0)
  serverState.alltime_ul = torrents.reduce((sum, t) => sum + t.uploaded, 0)
  serverState.global_ratio = (serverState.alltime_ul / Math.max(1, serverState.alltime_dl)).toFixed(2)
  return { id, name, torrents, categories, tags, serverState, comments: new Map(), renames: new Map(), filePriorities: new Map(), bannedPeers: new Set() }
}

function compareBy(sort: string, order: "asc" | "desc") {
  const dir = order === "desc" ? -1 : 1
  return (a: Torrent, b: Torrent): number => {
    let cmp: number
    if (sort === "state") {
      cmp = (STATE_SORT_ORDER[a.state] ?? 1000) - (STATE_SORT_ORDER[b.state] ?? 1000)
    } else if (sort === "tracker") {
      cmp = trackerDomain(a).localeCompare(trackerDomain(b))
    } else {
      const av = a[sort as keyof Torrent]
      const bv = b[sort as keyof Torrent]
      if (typeof av === "number" && typeof bv === "number") cmp = av - bv
      else if (typeof av === "string" && typeof bv === "string") cmp = av.localeCompare(bv, undefined, { sensitivity: "base", numeric: true })
      else if (typeof av === "boolean" && typeof bv === "boolean") cmp = Number(av) - Number(bv)
      else cmp = 0
    }
    if (cmp === 0) cmp = a.hash < b.hash ? -1 : a.hash > b.hash ? 1 : 0
    return cmp * dir
  }
}

function expandCategories(names: readonly string[], all: readonly string[]): Set<string> {
  const set = new Set<string>()
  for (const name of names) {
    set.add(name)
    if (name === "") continue
    for (const candidate of all) if (candidate.startsWith(name + "/")) set.add(candidate)
  }
  return set
}

function matchesFilters(t: Torrent, f: TorrentFilters, categoryNames: readonly string[], hashes: Set<string> | null): boolean {
  if (hashes && !hashes.has(t.hash.toUpperCase())) return false
  if (f.status?.length && !f.status.some(s => matchesStatus(t, s))) return false
  if (f.excludeStatus?.some(s => matchesStatus(t, s))) return false
  if (f.categories?.length && !expandCategories(f.categories, categoryNames).has(t.category)) return false
  if (f.excludeCategories?.length && expandCategories(f.excludeCategories, categoryNames).has(t.category)) return false
  const tags = tagList(t)
  if (f.tags?.length) {
    const wantUntagged = f.tags.includes("")
    if (!((wantUntagged && tags.length === 0) || tags.some(tag => f.tags.includes(tag)))) return false
  }
  if (f.excludeTags?.length) {
    if (f.excludeTags.includes("") && tags.length === 0) return false
    if (tags.some(tag => f.excludeTags.includes(tag))) return false
  }
  const domain = trackerDomain(t)
  if (f.trackers?.length && !f.trackers.includes(domain)) return false
  if (f.excludeTrackers?.length && f.excludeTrackers.includes(domain)) return false
  return true
}

function computeCounts(torrents: readonly Torrent[]): TorrentCounts {
  const status: Record<string, number> = {
    all: 0, downloading: 0, seeding: 0, completed: 0, paused: 0, active: 0, inactive: 0, resumed: 0, running: 0,
    stopped: 0, stalled: 0, stalled_uploading: 0, stalled_downloading: 0, errored: 0, checking: 0, moving: 0,
    unregistered: 0, tracker_down: 0, tracker_error: 0,
  }
  const categories: Record<string, number> = {}
  const categorySizes: Record<string, number> = {}
  const tags: Record<string, number> = {}
  const tagSizes: Record<string, number> = {}
  const trackers: Record<string, number> = {}
  for (const t of torrents) {
    status.all++
    status[inCategory(t.state, "active") ? "active" : "inactive"]++
    if (isPausedOrStopped(t.state)) {
      status.stopped++
      status.paused++
    } else {
      status.running++
      status.resumed++
    }
    for (const [name, states] of Object.entries(STATE_CATEGORIES)) {
      if (name === "active" || name === "paused" || name === "stopped") continue
      if (states.includes(t.state)) status[name] = (status[name] ?? 0) + 1
    }
    if (t.progress === 1) status.completed++
    if (t.tracker_health) status[t.tracker_health]++
    categories[t.category] = (categories[t.category] ?? 0) + 1
    categorySizes[t.category] = (categorySizes[t.category] ?? 0) + t.size
    const list = tagList(t)
    if (list.length === 0) {
      tags[""] = (tags[""] ?? 0) + 1
      tagSizes[""] = (tagSizes[""] ?? 0) + t.size
    }
    for (const tag of list) {
      tags[tag] = (tags[tag] ?? 0) + 1
      tagSizes[tag] = (tagSizes[tag] ?? 0) + t.size
    }
    const domain = trackerDomain(t)
    trackers[domain] = (trackers[domain] ?? 0) + 1
  }
  return { status, categories, categorySizes, tags, tagSizes, trackers, total: torrents.length }
}

function computeStats(torrents: readonly Torrent[]): TorrentStats {
  const stats: TorrentStats = {
    total: torrents.length, downloading: 0, seeding: 0, paused: 0, error: 0,
    totalDownloadSpeed: 0, totalUploadSpeed: 0, totalDownloadData: 0, totalUploadData: 0,
    totalSize: 0, totalRemainingSize: 0, totalSeedingSize: 0,
  }
  for (const t of torrents) {
    if (inCategory(t.state, "downloading")) stats.downloading++
    if (inCategory(t.state, "seeding")) {
      stats.seeding++
      stats.totalSeedingSize! += t.size
    }
    if (isPausedOrStopped(t.state)) stats.paused++
    if (inCategory(t.state, "errored")) stats.error++
    stats.totalDownloadSpeed! += t.dlspeed
    stats.totalUploadSpeed! += t.upspeed
    stats.totalDownloadData! += t.downloaded
    stats.totalUploadData! += t.uploaded
    stats.totalSize! += t.size
    stats.totalRemainingSize! += t.amount_left
  }
  return stats
}

export const PREFERENCES = {
  save_path: "/data/torrents",
  temp_path: "/data/incomplete",
  temp_path_enabled: false,
  use_subcategories: true,
  queueing_enabled: true,
  auto_tmm_enabled: true,
  add_trackers_enabled: false,
  add_trackers: "",
  excluded_file_names_enabled: false,
  excluded_file_names: "",
  torrent_content_layout: "Original",
  listen_port: 51413,
}

export const CAPABILITIES: InstanceCapabilities = {
  supportsTorrentCreation: false,
  supportsTorrentExport: false,
  supportsSetTags: true,
  supportsSetComment: true,
  supportsTrackerHealth: true,
  supportsTrackerEditing: true,
  supportsRenameTorrent: true,
  supportsRenameFile: true,
  supportsRenameFolder: true,
  supportsFilePriority: true,
  supportsSubcategories: true,
  subcategoriesAlwaysEnabled: false,
  supportsTorrentTmpPath: true,
  supportsPathAutocomplete: false,
  supportsFreeSpacePathSource: false,
  supportsSetRSSFeedURL: false,
  supportsShareLimitsAction: true,
  supportsShareLimitsMode: true,
  webAPIVersion: "2.11.4",
}

const PEER_CLIENTS = ["qBittorrent 5.1.2", "qBittorrent 5.0.4", "Transmission 4.0.6", "Deluge 2.1.1", "rTorrent 0.9.8", "libtorrent 2.0.11"] as const
const COUNTRIES = [["DE", "Germany"], ["NL", "Netherlands"], ["US", "United States"], ["FR", "France"], ["SE", "Sweden"], ["CA", "Canada"], ["GB", "United Kingdom"], ["FI", "Finland"]] as const

function buildDetails(t: Torrent, now: number): TorrentDetails {
  const rng = mulberry32(parseInt(t.hash.slice(0, 8), 16))
  const pieceSize = t.size > 4 * GIB ? 16 * MIB : t.size > 512 * MIB ? 4 * MIB : 1 * MIB
  const piecesNum = Math.max(1, Math.ceil(t.size / pieceSize))
  const piecesHave = Math.round(piecesNum * t.progress)

  const isSingle = /\.(iso|xz|pdf)$/.test(t.name)
  const fileCount = isSingle ? 1 : 1 + Math.floor(rng() * 8)
  const files: TorrentFile[] = []
  let remaining = t.size
  let pieceCursor = 0
  for (let i = 0; i < fileCount; i++) {
    const last = i === fileCount - 1
    const size = last ? remaining : Math.max(1, Math.round(remaining * (0.15 + rng() * 0.5)))
    remaining -= size
    const pieces = Math.max(1, Math.ceil(size / pieceSize))
    const name = isSingle ? t.name : i === 0 ? `${t.name}/${t.name}.mkv` : `${t.name}/${pick(rng, ["sample.mkv", "README.txt", "subs/english.srt", "subs/german.srt", "cover.jpg", "checksums.sha256", "extras/trailer.mkv", "nfo.txt"])}`
    files.push({
      index: i,
      name,
      size,
      progress: t.progress === 1 ? 1 : Math.min(1, Math.max(0, t.progress + (rng() - 0.5) * 0.2)),
      priority: 1,
      is_seed: t.progress === 1,
      piece_range: [pieceCursor, pieceCursor + pieces - 1],
      availability: t.progress === 1 ? -1 : Math.round((1 + rng() * 10) * 100) / 100,
    })
    pieceCursor += pieces
  }

  const domain = trackerDomain(t)
  const trackerStatus = t.tracker_health === "unregistered" ? 4 : t.tracker_health === "tracker_down" ? 4 : 2
  const trackers: TorrentTracker[] = [
    { url: "** [DHT] **", status: t.private ? 0 : 2, num_peers: 0, num_seeds: 0, num_leeches: 0, num_downloaded: 0, msg: t.private ? "This torrent is private" : "" },
    { url: "** [PeX] **", status: t.private ? 0 : 2, num_peers: 0, num_seeds: 0, num_leeches: 0, num_downloaded: 0, msg: t.private ? "This torrent is private" : "" },
    { url: "** [LSD] **", status: t.private ? 0 : 2, num_peers: 0, num_seeds: 0, num_leeches: 0, num_downloaded: 0, msg: t.private ? "This torrent is private" : "" },
    {
      url: t.tracker,
      status: trackerStatus,
      num_peers: t.num_seeds + t.num_leechs,
      num_seeds: t.num_complete,
      num_leeches: t.num_incomplete,
      num_downloaded: Math.floor(rng() * 5000),
      msg: t.tracker_health === "unregistered" ? "Unregistered torrent" : t.tracker_health === "tracker_down" ? `Connection to ${domain} timed out` : "",
    },
  ]

  const peerCount = t.num_seeds + t.num_leechs
  const peers: SortedPeersResponse = { rid: 1, show_flags: true, peers: {}, sorted_peers: [] }
  for (let i = 0; i < Math.min(peerCount, 40); i++) {
    const [code, country] = pick(rng, COUNTRIES)
    const ip = `${10 + Math.floor(rng() * 200)}.${Math.floor(rng() * 255)}.${Math.floor(rng() * 255)}.${1 + Math.floor(rng() * 254)}`
    const port = 6881 + Math.floor(rng() * 50000)
    const seed = i < t.num_seeds
    const peer = {
      key: `${ip}:${port}`, ip, port,
      client: pick(rng, PEER_CLIENTS),
      connection: rng() < 0.7 ? "uTP" : "BT",
      flags: seed ? "D" : "U",
      flags_desc: seed ? "D = interested (local) and unchoked (peer)" : "U = interested (peer) and unchoked (local)",
      progress: seed ? 1 : Math.round(rng() * 1000) / 1000,
      dl_speed: seed && t.dlspeed > 0 ? Math.round(t.dlspeed / Math.max(1, t.num_seeds) * (0.5 + rng())) : 0,
      up_speed: !seed && t.upspeed > 0 ? Math.round(t.upspeed / Math.max(1, t.num_leechs) * (0.5 + rng())) : 0,
      downloaded: Math.round(rng() * 200 * MIB),
      uploaded: Math.round(rng() * 200 * MIB),
      relevance: 1,
      files: "",
      country,
      country_code: code.toLowerCase(),
    }
    peers.peers![peer.key] = peer
    peers.sorted_peers!.push(peer)
  }

  const pieces: number[] = new Array(piecesNum)
  for (let i = 0; i < piecesNum; i++) pieces[i] = i < piecesHave ? 2 : i < piecesHave + 3 && t.dlspeed > 0 ? 1 : 0

  const elapsed = Math.max(1, now - t.added_on)
  const properties: TorrentProperties = {
    addition_date: t.added_on,
    comment: t.category === "films" ? "Public domain. Restored scan." : "",
    completion_date: t.completion_on || -1,
    created_by: "mkbrr/1.15.0",
    creation_date: t.added_on - 86400,
    dl_limit: t.dl_limit || -1,
    dl_speed: t.dlspeed,
    dl_speed_avg: Math.round(t.downloaded / elapsed),
    download_path: t.download_path,
    eta: t.eta,
    hash: t.hash,
    infohash_v1: t.infohash_v1,
    infohash_v2: t.infohash_v2,
    is_private: t.private,
    last_seen: t.seen_complete || -1,
    name: t.name,
    nb_connections: peerCount,
    nb_connections_limit: 100,
    peers: t.num_leechs,
    peers_total: t.num_incomplete,
    piece_size: pieceSize,
    pieces_have: piecesHave,
    pieces_num: piecesNum,
    reannounce: t.reannounce,
    save_path: t.save_path,
    seeding_time: t.seeding_time,
    seeds: t.num_seeds,
    seeds_total: t.num_complete,
    share_ratio: t.ratio,
    time_elapsed: t.time_active,
    total_downloaded: t.downloaded,
    total_downloaded_session: t.downloaded_session,
    total_size: t.total_size,
    total_uploaded: t.uploaded,
    total_uploaded_session: t.uploaded_session,
    total_wasted: Math.round(t.downloaded * 0.002),
    up_limit: t.up_limit || -1,
    up_speed: t.upspeed,
    up_speed_avg: Math.round(t.uploaded / elapsed),
  }

  return { properties, trackers, files, peers, pieces }
}

function nowSeconds(): number {
  return Math.floor(Date.now() / 1000)
}

export function createStore(options: { seed?: number; counts?: [number, number] } = {}): DemoStore {
  const seed = options.seed ?? 20260919
  const [seedboxCount, homeCount] = options.counts ?? [7500, 2500]
  const now = nowSeconds()
  const instances: DemoInstance[] = [
    buildInstance(1, "seedbox", seedboxCount, seed, now),
    buildInstance(2, "home", homeCount, seed + 1, now),
  ]
  const byId = new Map(instances.map(i => [i.id, i]))
  const tickRng = mulberry32(seed + 2)
  // One stream for every add, so two adds in the same millisecond get distinct hashes.
  const addRng = mulberry32(seed + 3)
  let sessionDl = 0
  let sessionUl = 0

  const instance = (id: number) => byId.get(id)

  const withInstances = (ids: number[]) => ids.map(instance).filter((i): i is DemoInstance => i !== undefined)

  function resolveHashes(inst: DemoInstance, body: BulkActionBody): Set<string> {
    const set = new Set<string>()
    if (body.selectAll) {
      const rows = filterRows(inst, body.filters ?? null, body.search ?? "")
      for (const t of rows) set.add(t.hash)
      for (const h of body.excludeHashes ?? []) set.delete(h)
      for (const e of body.excludeTargets ?? []) if (e.instanceId === inst.id) set.delete(e.hash)
      return set
    }
    for (const h of body.hashes ?? []) set.add(h)
    for (const e of body.targets ?? []) if (e.instanceId === inst.id) set.add(e.hash)
    return set
  }

  function filterRows(inst: DemoInstance, filters: TorrentFilters | null, search: string): Torrent[] {
    const needle = search.trim().toLowerCase()
    const hashes = filters?.hashes?.length ? new Set(filters.hashes.map(h => h.toUpperCase())) : null
    const categoryNames = Object.keys(inst.categories)
    return inst.torrents.filter(t => {
      if (needle && !t.name.toLowerCase().includes(needle)) return false
      return filters ? matchesFilters(t, filters, categoryNames, hashes) : true
    })
  }

  // Queue moves renumber the queued rows (priority > 0) from 1 like qBittorrent does.
  function reorderQueue(inst: DemoInstance, hashes: Set<string>, action: string): void {
    const queue = inst.torrents.filter(t => t.priority > 0).sort((a, b) => a.priority - b.priority)
    const selected = queue.filter(t => hashes.has(t.hash))
    const rest = queue.filter(t => !hashes.has(t.hash))
    if (action === "topPriority") queue.splice(0, queue.length, ...selected, ...rest)
    else if (action === "bottomPriority") queue.splice(0, queue.length, ...rest, ...selected)
    else {
      const step = action === "increasePriority" ? -1 : 1
      const positions = queue.flatMap((t, i) => hashes.has(t.hash) ? [i] : [])
      if (step === 1) positions.reverse()
      for (const i of positions) {
        const j = i + step
        if (j < 0 || j >= queue.length || hashes.has(queue[j].hash)) continue
        ;[queue[i], queue[j]] = [queue[j], queue[i]]
      }
    }
    queue.forEach((t, i) => { t.priority = i + 1 })
  }

  function applyAction(inst: DemoInstance, t: Torrent, body: BulkActionBody, now: number): void {
    switch (body.action) {
      case "pause":
        t.state = t.progress === 1 ? "stoppedUP" : "stoppedDL"
        t.dlspeed = 0
        t.upspeed = 0
        t.eta = QBIT_INFINITE_ETA
        t.force_start = false
        break
      case "resume":
        t.state = t.progress === 1 ? "uploading" : "downloading"
        t.force_start = false
        break
      case "forceStart":
        t.force_start = body.enable ?? true
        t.state = t.progress === 1 ? (t.force_start ? "forcedUP" : "uploading") : (t.force_start ? "forcedDL" : "downloading")
        break
      case "recheck":
        t.state = t.progress === 1 ? "checkingUP" : "checkingDL"
        t.dlspeed = 0
        t.upspeed = 0
        break
      case "setCategory":
        t.category = body.category ?? ""
        t.save_path = body.category ? (inst.categories[body.category]?.savePath ?? t.save_path) : "/data/torrents"
        t.content_path = `${t.save_path}/${t.name}`
        break
      case "addTags": {
        const set = new Set(tagList(t))
        for (const tag of (body.tags ?? "").split(",").map(s => s.trim()).filter(Boolean)) {
          set.add(tag)
          inst.tags.add(tag)
        }
        t.tags = [...set].sort().join(", ")
        break
      }
      case "removeTags": {
        const remove = new Set((body.tags ?? "").split(",").map(s => s.trim()).filter(Boolean))
        t.tags = tagList(t).filter(tag => !remove.has(tag)).join(", ")
        break
      }
      case "setTags": {
        const list = (body.tags ?? "").split(",").map(s => s.trim()).filter(Boolean)
        for (const tag of list) inst.tags.add(tag)
        t.tags = [...new Set(list)].sort().join(", ")
        break
      }
      case "setLocation":
        t.save_path = body.location ?? t.save_path
        t.content_path = `${t.save_path}/${t.name}`
        break
      case "setShareLimit":
        t.ratio_limit = body.ratioLimit ?? t.ratio_limit
        t.seeding_time_limit = body.seedingTimeLimit ?? t.seeding_time_limit
        t.inactive_seeding_time_limit = body.inactiveSeedingTimeLimit ?? t.inactive_seeding_time_limit
        break
      case "setUploadLimit":
        t.up_limit = (body.uploadLimit ?? 0) * KIB
        break
      case "setDownloadLimit":
        t.dl_limit = (body.downloadLimit ?? 0) * KIB
        break
      case "toggleAutoTMM":
        t.auto_tmm = body.enable ?? !t.auto_tmm
        break
      case "toggleSequentialDownload":
        t.seq_dl = body.enable ?? !t.seq_dl
        break
      case "reannounce":
        t.reannounce = 1800
        t.last_activity = now
        break
      case "setComment":
        inst.comments.set(t.hash, body.comment ?? "")
        break
      case "editTrackers":
        if (body.trackerNewURL && t.tracker === body.trackerOldURL) t.tracker = body.trackerNewURL
        break
    }
  }

  return {
    instances,
    instance,

    query(instanceIds, params, crossInstance = false) {
      const insts = withInstances(instanceIds)
      const rows: Array<Torrent | CrossInstanceTorrent> = []
      const allForCounts = insts.flatMap(i => i.torrents)
      for (const inst of insts) {
        const matched = filterRows(inst, params.filters, params.search)
        if (crossInstance) {
          for (const t of matched) rows.push({ ...t, instanceId: inst.id, instanceName: inst.name })
        } else {
          for (const t of matched) rows.push(t)
        }
      }
      rows.sort(compareBy(params.sort, params.order))
      const start = params.page * params.limit
      const page = rows.slice(start, start + params.limit)
      const primary = insts[0]
      const response: TorrentResponse = {
        torrents: crossInstance ? [] : (page as Torrent[]),
        total: rows.length,
        hasMore: start + params.limit < rows.length,
        stats: computeStats(allForCounts),
        counts: computeCounts(allForCounts),
        categories: primary ? { ...primary.categories } : {},
        tags: primary ? [...primary.tags].sort() : [],
        serverState: primary ? { ...primary.serverState } : undefined,
        preferences: PREFERENCES as AppPreferences,
        useSubcategories: true,
        trackerHealthSupported: true,
        cacheMetadata: { source: "fresh", age: 0, isStale: false },
        instanceMeta: { connected: true, hasDecryptionError: false, connectionStatus: "connected" },
      }
      if (crossInstance) {
        response.isCrossInstance = true
        response.crossInstanceTorrents = page as CrossInstanceTorrent[]
      }
      return response
    },

    tick() {
      const now = nowSeconds()
      for (const inst of instances) {
        let dl = 0
        let ul = 0
        let peers = 0
        for (const t of inst.torrents) {
          if (t.state === "checkingUP" || t.state === "checkingDL") {
            // A recheck finishes on the next tick.
            t.state = t.progress === 1 ? "uploading" : "downloading"
            continue
          }
          if (t.state === "downloading" || t.state === "forcedDL") {
            if (t.dlspeed === 0) t.dlspeed = Math.round((0.3 + tickRng() * 6) * MIB)
            t.dlspeed = Math.max(256 * KIB, Math.round(t.dlspeed * (0.85 + tickRng() * 0.3)))
            const gained = Math.min(t.amount_left, t.dlspeed * TICK_SECONDS)
            t.downloaded += gained
            t.downloaded_session += gained
            t.completed = t.downloaded
            t.amount_left -= gained
            t.progress = Math.round((t.downloaded / t.size) * 10000) / 10000
            t.last_activity = now
            t.time_active += TICK_SECONDS
            sessionDl += gained
            if (t.amount_left <= 0) {
              t.progress = 1
              t.state = t.force_start ? "forcedUP" : "uploading"
              t.completion_on = now
              t.dlspeed = 0
              t.eta = QBIT_INFINITE_ETA
              t.upspeed = Math.round((100 + tickRng() * 2000) * KIB)
              t.availability = -1
            } else {
              t.eta = Math.round(t.amount_left / t.dlspeed)
            }
          }
          if (t.upspeed > 0) {
            t.upspeed = Math.max(16 * KIB, Math.round(t.upspeed * (0.85 + tickRng() * 0.3)))
            const sent = t.upspeed * TICK_SECONDS
            t.uploaded += sent
            t.uploaded_session += sent
            t.ratio = Math.round((t.uploaded / Math.max(1, t.size)) * 100) / 100
            t.last_activity = now
            sessionUl += sent
          }
          if (t.state === "uploading" || t.state === "forcedUP") t.seeding_time += TICK_SECONDS
          dl += t.dlspeed
          ul += t.upspeed
          peers += t.num_seeds + t.num_leechs
        }
        inst.serverState.dl_info_speed = dl
        inst.serverState.up_info_speed = ul
        inst.serverState.dl_info_data = sessionDl
        inst.serverState.up_info_data = sessionUl
        inst.serverState.total_peer_connections = peers
      }
    },

    bulkAction(instanceId, body) {
      const now = nowSeconds()
      const targetsByInstance = new Set<number>([instanceId, ...(body.targets ?? []).map(t => t.instanceId), ...(body.instanceIds ?? [])])
      for (const inst of withInstances([...targetsByInstance])) {
        const hashes = resolveHashes(inst, body)
        if (hashes.size === 0) continue
        if (body.action === "delete") {
          inst.torrents = inst.torrents.filter(t => !hashes.has(t.hash))
          continue
        }
        if (body.action.endsWith("Priority")) {
          reorderQueue(inst, hashes, body.action)
          continue
        }
        for (const t of inst.torrents) if (hashes.has(t.hash)) applyAction(inst, t, body, now)
      }
    },

    addTorrent(instanceId, name, category, tags, paused, autoTMM, savePath) {
      const inst = instance(instanceId)
      if (!inst) return
      const t = generateTorrent(addRng, nowSeconds(), inst.torrents.length)
      t.name = name
      t.category = category
      t.tags = tags.join(", ")
      t.state = paused ? "stoppedDL" : "downloading"
      t.progress = 0
      t.downloaded = 0
      t.completed = 0
      t.uploaded = 0
      t.ratio = 0
      t.amount_left = t.size
      t.completion_on = 0
      t.seeding_time = 0
      t.added_on = nowSeconds()
      t.dlspeed = paused ? 0 : Math.round((2 + addRng() * 10) * MIB)
      t.upspeed = 0
      t.tracker_health = undefined
      t.auto_tmm = autoTMM
      t.save_path = (!autoTMM && savePath) || (category ? (inst.categories[category]?.savePath ?? "/data/torrents") : "/data/torrents")
      t.content_path = `${t.save_path}/${name}`
      inst.torrents.unshift(t)
    },

    addCategory(instanceId, name, savePath) {
      const inst = instance(instanceId)
      if (inst) inst.categories[name] = { name, savePath: savePath || `/data/torrents/${name}` }
    },

    removeCategories(instanceId, names) {
      const inst = instance(instanceId)
      if (!inst) return
      for (const name of names) {
        delete inst.categories[name]
        for (const t of inst.torrents) if (t.category === name) t.category = ""
      }
    },

    addTags(instanceId, tags) {
      const inst = instance(instanceId)
      if (inst) for (const tag of tags) inst.tags.add(tag)
    },

    removeTags(instanceId, tags) {
      const inst = instance(instanceId)
      if (!inst) return
      const remove = new Set(tags)
      for (const tag of tags) inst.tags.delete(tag)
      for (const t of inst.torrents) t.tags = tagList(t).filter(tag => !remove.has(tag)).join(", ")
    },

    rename(instanceId, hash, name) {
      const t = instance(instanceId)?.torrents.find(x => x.hash === hash)
      if (!t) return
      t.name = name
      t.content_path = `${t.save_path}/${name}`
    },

    find(instanceId, hash) {
      return instance(instanceId)?.torrents.find(t => t.hash === hash)
    },

    renamePath(instanceId, hash, oldPath, newPath) {
      const inst = instance(instanceId)
      if (!inst) return
      inst.renames.set(hash, [...(inst.renames.get(hash) ?? []), [oldPath, newPath]])
    },

    setFilePriority(instanceId, hash, indices, priority) {
      const inst = instance(instanceId)
      if (!inst) return
      const map = inst.filePriorities.get(hash) ?? new Map<number, number>()
      for (const i of indices) map.set(i, priority)
      inst.filePriorities.set(hash, map)
    },

    banPeers(instanceId, peers) {
      const inst = instance(instanceId)
      if (!inst) return
      for (const p of peers) inst.bannedPeers.add(p)
    },

    toggleAltSpeedLimits(instanceId) {
      const inst = instance(instanceId)
      if (!inst) return false
      inst.serverState.use_alt_speed_limits = !inst.serverState.use_alt_speed_limits
      return inst.serverState.use_alt_speed_limits
    },

    details(instanceId, hash) {
      const inst = instance(instanceId)
      const t = inst?.torrents.find(x => x.hash === hash)
      if (!inst || !t) return undefined
      const d = buildDetails(t, nowSeconds())
      const comment = inst.comments.get(hash)
      if (comment !== undefined) d.properties.comment = comment
      for (const [i, priority] of inst.filePriorities.get(hash) ?? []) d.files[i].priority = priority
      d.peers.sorted_peers = d.peers.sorted_peers!.filter(p => !inst.bannedPeers.has(p.key))
      for (const key of inst.bannedPeers) delete d.peers.peers![key]
      for (const [oldPath, newPath] of inst.renames.get(hash) ?? []) {
        for (const f of d.files) {
          if (f.name === oldPath) f.name = newPath
          else if (f.name.startsWith(`${oldPath}/`)) f.name = newPath + f.name.slice(oldPath.length)
        }
      }
      return d
    },

    instanceResponses() {
      return instances.map((inst, i) => ({
        id: inst.id,
        name: inst.name,
        host: inst.id === 1 ? "https://qbit.seedbox.example" : "http://10.0.0.5:8080",
        username: "admin",
        tlsSkipVerify: false,
        hasLocalFilesystemAccess: false,
        useHardlinks: false,
        hardlinkBaseDir: "",
        hardlinkDirPreset: "flat",
        useReflinks: false,
        fallbackToRegularMode: true,
        sortOrder: i,
        isActive: true,
        reannounceSettings: {
          enabled: false, initialWaitSeconds: 7, reannounceIntervalSeconds: 7, maxAgeSeconds: 3600, maxRetries: 50,
          aggressive: false, monitorAll: false, excludeCategories: false, categories: [], excludeTags: false, tags: [],
          excludeTrackers: false, trackers: [],
        },
        connected: true,
        hasDecryptionError: false,
        connectionStatus: "connected",
      }))
    },
  }
}
