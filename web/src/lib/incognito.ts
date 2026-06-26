/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Incognito mode utilities for disguising torrents as Linux ISOs

import type { Category } from "@/types"
import { useEffect, useState } from "react"

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
export const LINUX_CATEGORIES: Record<string, Category> = {
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
export const LINUX_TAGS = [
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
export const LINUX_TRACKERS = [
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

// Generate deterministic Linux folder name based on hash and depth
export function getLinuxFolderName(hash: string, depth: number): string {
  if (!hash) {
    return LINUX_FOLDER_NAMES[depth % LINUX_FOLDER_NAMES.length]
  }

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 2)
  }

  const offset = hashSum % LINUX_FOLDER_NAMES.length
  return LINUX_FOLDER_NAMES[(offset + depth) % LINUX_FOLDER_NAMES.length]
}

// Generate a deterministic but seemingly random Linux ISO name based on hash
export function getLinuxIsoName(hash: string): string {
  // Use hash to deterministically select an ISO name
  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i)
  }
  return linuxIsoNames[hashSum % linuxIsoNames.length]
}

export function getLinuxFileName(hash: string, index: number): string {
  if (!hash) {
    return linuxIsoNames[index % linuxIsoNames.length]
  }

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 3)
  }

  const offset = hashSum % linuxIsoNames.length
  return linuxIsoNames[(offset + index) % linuxIsoNames.length]
}

// Generate deterministic Linux category based on hash
export function getLinuxCategory(hash: string): string {
  let hashSum = 0
  for (let i = 0; i < Math.min(10, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 1)
  }
  // 30% chance of no category
  if (hashSum % 10 < 3) return ""
  return LINUX_CATEGORIES_ARRAY[hashSum % LINUX_CATEGORIES_ARRAY.length]
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
  const tags: string[] = []

  for (let i = 0; i < numTags; i++) {
    const tagIndex = (hashSum + i * 7) % LINUX_TAGS.length
    if (!tags.includes(LINUX_TAGS[tagIndex])) {
      tags.push(LINUX_TAGS[tagIndex])
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
  return LINUX_SAVE_PATHS[hashSum % LINUX_SAVE_PATHS.length]
}

export function getLinuxCreatedBy(hash: string): string {
  if (!hash) return LINUX_RELEASE_TEAMS[0]

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 1)
  }

  return LINUX_RELEASE_TEAMS[hashSum % LINUX_RELEASE_TEAMS.length]
}

export function getLinuxComment(hash: string): string {
  if (!hash) return "Release notes hidden in incognito mode."

  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i) * (i + 3)
  }

  return `${LINUX_RELEASE_NOTES[hashSum % LINUX_RELEASE_NOTES.length]}`
}

// Helper to compute tracker index from hash
function getTrackerIndex(hash: string): number {
  let hashSum = 0
  for (let i = 0; i < Math.min(12, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 4)
  }
  return hashSum % LINUX_TRACKERS.length
}

// Generate deterministic Linux tracker based on hash
export function getLinuxTracker(hash: string): string {
  return `https://${LINUX_TRACKERS[getTrackerIndex(hash)]}/announce`
}

// Generate deterministic Linux tracker domain based on hash (without URL prefix/suffix)
export function getLinuxTrackerDomain(hash: string): string {
  return LINUX_TRACKERS[getTrackerIndex(hash)]
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

// Custom hook for managing incognito mode state with localStorage persistence
export function useIncognitoMode(): [boolean, (value: boolean) => void] {
  const [incognitoMode, setIncognitoModeState] = useState(() => {
    const stored = localStorage.getItem(INCOGNITO_STORAGE_KEY)
    return stored === "true"
  })

  // Listen for storage changes to sync incognito mode across components
  useEffect(() => {
    const handleStorageChange = () => {
      const stored = localStorage.getItem(INCOGNITO_STORAGE_KEY)
      setIncognitoModeState(stored === "true")
    }

    // Listen for both storage events (cross-tab) and custom events (same-tab)
    window.addEventListener("storage", handleStorageChange)
    window.addEventListener("incognito-mode-changed", handleStorageChange)

    return () => {
      window.removeEventListener("storage", handleStorageChange)
      window.removeEventListener("incognito-mode-changed", handleStorageChange)
    }
  }, [])

  const setIncognitoMode = (value: boolean) => {
    setIncognitoModeState(value)
    localStorage.setItem(INCOGNITO_STORAGE_KEY, String(value))
    // Dispatch custom event for same-tab updates
    window.dispatchEvent(new Event("incognito-mode-changed"))
  }

  return [incognitoMode, setIncognitoMode]
}
