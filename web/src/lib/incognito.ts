/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Incognito mode utilities for disguising torrents as Linux ISOs (default) or as
// office documents while the Spreadsheet theme is active. getLinux* names are kept
// as the stable incognito API; each getter picks its vocabulary at call time.

import type { Category } from "@/types"
import { useClientSetting } from "@/lib/client-settings"
import { isSpreadsheetDisguiseActive } from "./spreadsheet-disguise"

// Linux ISO names for incognito mode
const linuxIsoNames = [
  "ubuntu-24.04.1-desktop-amd64.iso",
  "ubuntu-24.10-desktop-amd64.iso",
  "ubuntu-22.04.4-server-amd64.iso",
  "debian-12.7.0-amd64-DVD-1.iso",
  "debian-13-trixie-alpha-netinst.iso",
  "Fedora-Workstation-Live-x86_64-41.iso",
  "Fedora-Server-dvd-x86_64-42.iso",
  "archlinux-2024.12.01-x86_64.iso",
  "archlinux-2024.11.01-x86_64.iso",
  "Pop!_OS-24.04-amd64-intel.iso",
  "linuxmint-22-cinnamon-64bit.iso",
  "openSUSE-Tumbleweed-DVD-x86_64-Current.iso",
  "openSUSE-Leap-15.6-DVD-x86_64.iso",
  "manjaro-kde-24.0-240513-linux66.iso",
  "EndeavourOS-Galileo-11-2024.iso",
  "elementary-os-7.1-stable.20231129rc.iso",
  "zorin-os-17.1-core-64bit.iso",
  "MX-23.3_x64.iso",
  "kali-linux-2024.3-installer-amd64.iso",
  "parrot-security-6.0_amd64.iso",
  "rocky-9.4-x86_64-dvd.iso",
  "almalinux-9.4-x86_64-dvd.iso",
  "centos-stream-9-latest-x86_64-dvd1.iso",
  "garuda-dr460nized-linux-zen-240131.iso",
  "artix-base-openrc-20241201-x86_64.iso",
  "void-live-x86_64-20240314-xfce.iso",
  "solus-4.5-budgie.iso",
  "alpine-standard-3.19.1-x86_64.iso",
  "slackware64-15.0-install-dvd.iso",
  "gentoo-install-amd64-minimal-20241201.iso",
  "nixos-24.05-plasma6-x86_64.iso",
  "endeavouros-2024.09.22-x86_64.iso",
  "kubuntu-24.04.1-desktop-amd64.iso",
  "xubuntu-24.04-desktop-amd64.iso",
  "lubuntu-24.04-desktop-amd64.iso",
  "ubuntu-mate-24.04-desktop-amd64.iso",
  "ubuntu-budgie-24.04-desktop-amd64.iso",
  "deepin-desktop-community-23.0-amd64.iso",
  "kde-neon-user-20241205-1344.iso",
  "peppermint-2024-02-02-amd64.iso",
  "tails-amd64-6.8.1.iso",
  "qubes-r4.2.3-x86_64.iso",
  "proxmox-ve_8.2-2.iso",
  "truenas-scale-24.04.2.iso",
  "opnsense-24.7-dvd-amd64.iso",
  "pfsense-ce-2.7.2-amd64.iso",
]

// Linux-themed categories for incognito mode
const LINUX_CATEGORIES: Record<string, Category> = {
  "distributions": { name: "distributions", savePath: "/home/downloads/distributions" },
  "documentation": { name: "documentation", savePath: "/home/downloads/docs" },
  "source-code": { name: "source-code", savePath: "/home/downloads/source" },
  "live-usb": { name: "live-usb", savePath: "/home/downloads/live" },
  "server-editions": { name: "server-editions", savePath: "/home/downloads/server" },
  "desktop-environments": { name: "desktop-environments", savePath: "/home/downloads/desktop" },
  "arm-builds": { name: "arm-builds", savePath: "/home/downloads/arm" },
}

const LINUX_CATEGORIES_ARRAY = [
  "distributions",
  "documentation",
  "source-code",
  "live-usb",
  "server-editions",
  "desktop-environments",
  "arm-builds",
]

// Linux-themed tags for incognito mode
const LINUX_TAGS = [
  "stable",
  "lts",
  "bleeding-edge",
  "minimal",
  "gnome",
  "kde",
  "xfce",
  "server",
  "desktop",
  "arm64",
  "x86_64",
  "enterprise",
  "community",
  "official",
  "beta",
  "rc",
  "nightly",
  "security-focused",
  "lightweight",
  "rolling-release",
]

// Linux-themed trackers for incognito mode
const LINUX_TRACKERS = [
  "releases.ubuntu.com",
  "cdimage.debian.org",
  "download.fedoraproject.org",
  "mirror.archlinux.org",
  "distro.ibiblio.org",
  "ftp.osuosl.org",
  "mirrors.kernel.org",
  "linuxtracker.org",
  "academic-torrents.com",
  "fosshost.org",
]

const LINUX_RELEASE_TEAMS = [
  "Canonical Release Engineering",
  "Debian CD Images Team",
  "Fedora QA Collective",
  "Arch Linux Release Crew",
  "Gentoo Build Farm",
  "openSUSE Release Engineering",
  "EndeavourOS Packaging Team",
  "Linux Mint ISO Squad",
]

const LINUX_RELEASE_NOTES = [
  "Checksum verified against upstream SHA256 manifest.",
  "Built using reproducible toolchain; see README for package list.",
  "Preseeded with latest security updates as of 2024-12-01.",
  "Includes Linux kernel 6.12 and Mesa 24.3 stack.",
  "Boot media tested on QEMU and bare metal hardware.",
  "Localized language packs trimmed for minimal footprint.",
  "Installer ships with default LUKS full-disk encryption profile.",
  "Live session user password documented in /DOCS/credentials.txt.",
]

// Linux save paths for incognito mode
const LINUX_SAVE_PATHS = [
  "/home/downloads/distributions",
  "/home/downloads/docs",
  "/home/downloads/source",
  "/home/downloads/live",
  "/home/downloads/server",
  "/home/downloads/desktop",
  "/home/downloads/arm",
  "/mnt/storage/linux-isos",
  "/media/nas/linux",
]

// Linux-themed folder names for incognito mode
const LINUX_FOLDER_NAMES = [
  "ubuntu",
  "debian",
  "fedora",
  "archlinux",
  "gentoo",
  "opensuse",
  "manjaro",
  "mint",
  "centos",
  "rocky",
  "alma",
  "kali",
  "parrot",
  "tails",
  "qubes",
  "nixos",
  "void",
  "alpine",
  "slackware",
  "docs",
  "source",
  "patches",
  "builds",
  "releases",
  "mirrors",
  "checksums",
]

interface IncognitoVocab {
  names: string[]
  categories: Record<string, Category>
  categoryKeys: string[]
  tags: string[]
  trackers: string[]
  releaseTeams: string[]
  releaseNotes: string[]
  savePaths: string[]
  folderNames: string[]
}

const LINUX_VOCAB: IncognitoVocab = {
  names: linuxIsoNames,
  categories: LINUX_CATEGORIES,
  categoryKeys: LINUX_CATEGORIES_ARRAY,
  tags: LINUX_TAGS,
  trackers: LINUX_TRACKERS,
  releaseTeams: LINUX_RELEASE_TEAMS,
  releaseNotes: LINUX_RELEASE_NOTES,
  savePaths: LINUX_SAVE_PATHS,
  folderNames: LINUX_FOLDER_NAMES,
}

// Spreadsheet flavor: served instead of the linux vocabulary while the
// Spreadsheet theme is active, so incognito rows read as office documents.
const SHEET_DOC_NAMES = [
  "Q1 Budget Consolidation v3.xlsx",
  "Q2 Budget Consolidation v7 FINAL.xlsx",
  "Q3 Forecast - Draft (do not distribute).xlsx",
  "Headcount Plan FY27 DRAFT.xlsx",
  "Travel Expense Reconciliation May.xlsx",
  "Travel Expense Reconciliation June.xlsx",
  "Fixed Asset Register 2026.xlsx",
  "Inventory Recount - Warehouse B.xlsx",
  "Inventory Recount - Warehouse C.xlsx",
  "Vendor Master Cleanup - Phase 2.xlsx",
  "PO Backlog Review 2026-08.xlsx",
  "Contract Renewal Tracker.xlsx",
  "Capex Requests FY27 Round 1.xlsx",
  "Opex Run Rate Analysis.xlsx",
  "Timesheet Export Week 27.csv",
  "Facilities Maintenance Log.xlsx",
  "Insurance Certificates Index.xlsx",
  "Utilization Dashboard Data.csv",
  "Vendor W9 Tracker.xlsx",
  "Vendor SLA Comparison.xlsx",
  "Server Inventory - Rack B4.xlsx",
  "Server Patch Schedule 2026-H2.xlsx",
  "Patch Compliance Report 2026-07.xlsx",
  "Patch Compliance Report 2026-08.xlsx",
  "SSL Cert Expiry Tracker.xlsx",
  "Certificate Inventory - Wildcards.xlsx",
  "License Audit - Adobe 2026.xlsx",
  "O365 License Assignment.csv",
  "License Renewal Calendar FY27.xlsx",
  "VM Capacity Planning Q3.xlsx",
  "Storage Growth Forecast.xlsx",
  "Backup Job Status Export.csv",
  "Restore Test Log 2026-Q2.xlsx",
  "AD Group Membership Audit.csv",
  "Access Review Q2 - Finance Apps.xlsx",
  "MFA Enrollment Status.xlsx",
  "Vulnerability Scan Summary 2026-07.xlsx",
  "Firewall Rule Review 2026.xlsx",
  "VPN Usage Report 2026-07.csv",
  "DNS Zone Export 2026-08.csv",
  "IP Address Allocation Plan.xlsx",
  "Switch Port Mapping - Floor 2.xlsx",
  "WiFi Survey Results - HQ.xlsx",
  "Network Diagram - HQ Floor 1.vsdx",
  "Laptop Refresh Wave 3.xlsx",
  "Asset Tag Reconciliation.xlsx",
  "Printer Fleet Inventory.xlsx",
  "UPS Battery Replacement Log.xlsx",
  "Datacenter Power Usage 2026-07.csv",
  "Egress Cost Breakdown 2026-06.csv",
  "Decommission Checklist - legacy-web01.xlsx",
  "Change Freeze Calendar FY27.xlsx",
  "On-call Rota 2026-H2.xlsx",
  "Helpdesk SLA Metrics 2026-Q2.xlsx",
  "Ticket Volume Dashboard Data.csv",
  "Incident Postmortem Index.xlsx",
  "DR Runbook v4.docx",
  "SOC2 Evidence Index.pdf",
  "Hardware EOL Matrix 2027.xlsx",
]

const SHEET_CATEGORIES: Record<string, Category> = {
  "Finance": { name: "Finance", savePath: "/shares/finance" },
  "Procurement": { name: "Procurement", savePath: "/shares/procurement" },
  "Operations": { name: "Operations", savePath: "/shares/operations" },
  "HR": { name: "HR", savePath: "/shares/hr" },
  "Facilities": { name: "Facilities", savePath: "/shares/facilities" },
  "Compliance": { name: "Compliance", savePath: "/shares/compliance" },
  "IT Assets": { name: "IT Assets", savePath: "/shares/it-assets" },
  "Infrastructure": { name: "Infrastructure", savePath: "/shares/infrastructure" },
  "Licensing": { name: "Licensing", savePath: "/shares/licensing" },
  "Security": { name: "Security", savePath: "/shares/security" },
}

const SHEET_TAGS = [
  "pending review",
  "approved",
  "draft",
  "final",
  "q1",
  "q2",
  "q3",
  "q4",
  "fy26",
  "fy27",
  "audit",
  "restricted",
  "archived",
  "shared",
  "monthly close",
  "board",
  "reconciled",
  "needs signoff",
  "template",
  "export",
  "patching",
  "licensing",
  "renewals",
]

const SHEET_TRACKERS = [
  "fileserver-01.corp.internal",
  "fileserver-02.corp.internal",
  "sharepoint.corp.internal",
  "dms.corp.internal",
  "backup-nas.corp.internal",
  "finance-share.corp.internal",
  "archive.corp.internal",
  "sync-gw.corp.internal",
  "erp-export.corp.internal",
  "print-scan.corp.internal",
  "itsm.corp.internal",
  "wiki.corp.internal",
]

const SHEET_TEAMS = [
  "Finance Operations",
  "FP&A Team",
  "Procurement Office",
  "Internal Audit",
  "IT Operations",
  "Infrastructure Team",
  "Service Desk",
  "Security Office",
]

const SHEET_NOTES = [
  "Pending sign-off from FP&A.",
  "Source data pulled from ERP export 2026-07-01.",
  "Updated per auditor request, see tab Notes.",
  "Figures preliminary until monthly close completes.",
  "Pending CAB approval before the next change window.",
  "Exported from the asset system 2026-07-14.",
  "Do not edit, rows synced from monitoring export.",
  "Cert expiry dates verified against inventory 2026-08-01.",
]

const SHEET_SAVE_PATHS = [
  "/shares/finance/FY26",
  "/shares/finance/monthly-close",
  "/shares/procurement/vendors",
  "/shares/operations/reports",
  "/shares/hr/planning",
  "/shares/facilities/contracts",
  "/shares/compliance/audit-2026",
  "/shares/it-assets/inventory",
  "/shares/infrastructure/capacity",
  "/shares/licensing/renewals",
  "/shares/security/access-reviews",
  "/shares/archive/2025",
]

const SHEET_FOLDER_NAMES = [
  "FY26",
  "FY25",
  "Q1",
  "Q2",
  "Q3",
  "Q4",
  "reports",
  "exports",
  "archive",
  "vendors",
  "invoices",
  "receipts",
  "statements",
  "reconciliations",
  "budgets",
  "forecasts",
  "templates",
  "backup",
  "monthly-close",
  "audit",
  "contracts",
  "approved",
  "drafts",
  "final",
  "working",
  "shared",
  "inventory",
  "licenses",
  "patching",
  "runbooks",
  "network",
  "assets",
]

const SHEET_VOCAB: IncognitoVocab = {
  names: SHEET_DOC_NAMES,
  categories: SHEET_CATEGORIES,
  categoryKeys: Object.keys(SHEET_CATEGORIES),
  tags: SHEET_TAGS,
  trackers: SHEET_TRACKERS,
  releaseTeams: SHEET_TEAMS,
  releaseNotes: SHEET_NOTES,
  savePaths: SHEET_SAVE_PATHS,
  folderNames: SHEET_FOLDER_NAMES,
}

function vocab(): IncognitoVocab {
  return isSpreadsheetDisguiseActive() ? SHEET_VOCAB : LINUX_VOCAB
}

// The explicit flag (from useSpreadsheetDisguise) keeps callers' memo
// dependencies honest instead of hiding the theme read in this module.
export function getIncognitoCategories(spreadsheetDisguise: boolean): Record<string, Category> {
  return spreadsheetDisguise ? SHEET_VOCAB.categories : LINUX_VOCAB.categories
}

export function getIncognitoTags(spreadsheetDisguise: boolean): string[] {
  return spreadsheetDisguise ? SHEET_VOCAB.tags : LINUX_VOCAB.tags
}

export function getIncognitoTrackers(spreadsheetDisguise: boolean): string[] {
  return spreadsheetDisguise ? SHEET_VOCAB.trackers : LINUX_VOCAB.trackers
}

// Generate deterministic Linux folder name based on hash and depth
export function getLinuxFolderName(hash: string, depth: number): string {
  const folderNames = vocab().folderNames
  if (!hash) {
    return folderNames[depth % folderNames.length]
  }

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 2)
  }

  const offset = hashSum % folderNames.length
  return folderNames[(offset + depth) % folderNames.length]
}

// Generate a deterministic but seemingly random Linux ISO name based on hash
export function getLinuxIsoName(hash: string): string {
  // Use hash to deterministically select an ISO name
  const names = vocab().names
  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i)
  }
  return names[hashSum % names.length]
}

export function getLinuxFileName(hash: string, index: number): string {
  const names = vocab().names
  if (!hash) {
    return names[index % names.length]
  }

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 3)
  }

  const offset = hashSum % names.length
  return names[(offset + index) % names.length]
}

// Generate deterministic Linux category based on hash
export function getLinuxCategory(hash: string): string {
  let hashSum = 0
  for (let i = 0; i < Math.min(10, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 1)
  }
  // 30% chance of no category
  if (hashSum % 10 < 3) return ""
  const categoryKeys = vocab().categoryKeys
  return categoryKeys[hashSum % categoryKeys.length]
}

// Generate deterministic Linux tags based on hash
export function getLinuxTags(hash: string): string {
  let hashSum = 0
  for (let i = 0; i < Math.min(15, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 2)
  }

  // 20% chance of no tags
  if (hashSum % 10 < 2) return ""

  // Generate 1-3 tags
  const numTags = (hashSum % 3) + 1
  const tagPool = vocab().tags
  const tags: string[] = []

  for (let i = 0; i < numTags; i++) {
    const tagIndex = (hashSum + i * 7) % tagPool.length
    if (!tags.includes(tagPool[tagIndex])) {
      tags.push(tagPool[tagIndex])
    }
  }

  return tags.join(", ")
}

// Generate deterministic Linux save path based on hash
export function getLinuxSavePath(hash: string): string {
  let hashSum = 0
  for (let i = 0; i < Math.min(8, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 3)
  }
  const savePaths = vocab().savePaths
  return savePaths[hashSum % savePaths.length]
}

export function getLinuxCreatedBy(hash: string): string {
  const releaseTeams = vocab().releaseTeams
  if (!hash) return releaseTeams[0]

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 1)
  }

  return releaseTeams[hashSum % releaseTeams.length]
}

export function getLinuxComment(hash: string): string {
  if (!hash) return "Release notes hidden in incognito mode."

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 3)
  }

  const releaseNotes = vocab().releaseNotes
  return `${releaseNotes[hashSum % releaseNotes.length]}`
}

// Helper to compute tracker index from hash
function getTrackerIndex(hash: string, trackerCount: number): number {
  let hashSum = 0
  for (let i = 0; i < Math.min(12, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 4)
  }
  return hashSum % trackerCount
}

// Generate deterministic Linux tracker based on hash
export function getLinuxTracker(hash: string): string {
  const trackers = vocab().trackers
  return `https://${trackers[getTrackerIndex(hash, trackers.length)]}/announce`
}

// Generate deterministic Linux tracker domain based on hash (without URL prefix/suffix)
export function getLinuxTrackerDomain(hash: string): string {
  const trackers = vocab().trackers
  return trackers[getTrackerIndex(hash, trackers.length)]
}

// Generate deterministic count value based on name for UI display
export function getLinuxCount(name: string, max: number = 50): number {
  let hashSum = 0
  for (let i = 0; i < Math.min(8, name.length); i++) {
    hashSum += name.charCodeAt(i) * (i + 1)
  }
  return (hashSum % max) + 1
}
// Generate deterministic ratio value based on hash
export function getLinuxRatio(hash: string): number {
  let hashSum = 0
  for (let i = 0; i < Math.min(10, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 5)
  }

  // Generate ratios between 0.1 and 10.0 with some clustering around good values
  const ratioRanges = [
    { min: 0.1, max: 0.5, weight: 15 },   // Poor ratio
    { min: 0.5, max: 1.0, weight: 20 },   // Below 1.0
    { min: 1.0, max: 2.0, weight: 30 },   // Good ratio (most common)
    { min: 2.0, max: 5.0, weight: 25 },   // Very good ratio
    { min: 5.0, max: 10.0, weight: 10 },  // Excellent ratio
  ]

  // Use weighted distribution
  const totalWeight = ratioRanges.reduce((sum, r) => sum + r.weight, 0)
  let weightPosition = hashSum % totalWeight

  for (const range of ratioRanges) {
    if (weightPosition < range.weight) {
      // Generate value within this range
      const rangeSize = range.max - range.min
      const position = (hashSum * 7) % 1000 / 1000 // Get decimal between 0-1
      return range.min + (rangeSize * position)
    }
    weightPosition -= range.weight
  }

  return 1.5 // Default fallback
}

// Generate deterministic Linux-style hash based on input hash
export function getLinuxHash(hash: string): string {
  if (!hash || hash.length === 0) {
    return ""
  }

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 1)
  }

  // Generate a 40-character hex hash (SHA-1 style)
  const chars = "0123456789abcdef"
  let result = ""

  for (let i = 0; i < 40; i++) {
    const index = (hashSum + i * 17) % 16
    result += chars[index]
  }

  return result
}

// Storage key for incognito mode
const INCOGNITO_STORAGE_KEY = "qui-incognito-mode"

const parseIncognito = (raw: string): boolean => raw === "true"

// Custom hook for managing the DB-backed incognito mode preference
export function useIncognitoMode(): [boolean, (value: boolean) => void] {
  return useClientSetting<boolean>(INCOGNITO_STORAGE_KEY, {
    defaultValue: false,
    parse: parseIncognito,
    serialize: String,
  })
}
