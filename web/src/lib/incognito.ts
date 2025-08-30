/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Incognito mode utilities for disguising torrents as Linux ISOs

import { useState, useEffect } from "react"

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

// Generate 1000+ Linux-themed categories for testing virtual scrolling
const generateLinuxCategories = (): Record<string, { save_path: string }> => {
  const baseCategories = [
    "distributions", "documentation", "source-code", "live-usb", "server-editions",
    "desktop-environments", "arm-builds", "container-images", "virtual-machines",
    "development-tools", "security-tools", "multimedia", "gaming", "education",
    "scientific", "embedded", "iot", "cloud", "backup", "recovery",
  ]

  const distros = [
    "ubuntu", "debian", "fedora", "arch", "centos", "rhel", "opensuse", "manjaro", "mint",
    "elementary", "zorin", "pop", "endeavour", "garuda", "artix", "void", "alpine", "gentoo",
    "nixos", "slackware", "kali", "parrot", "rocky", "alma", "mx", "solus", "deepin", "tails",
    "qubes", "proxmox", "truenas", "opnsense", "pfsense", "freebsd", "openbsd", "netbsd",
  ]

  const purposes = [
    "workstation", "server", "development", "gaming", "multimedia", "education", "scientific",
    "security", "privacy", "forensics", "penetration-testing", "network-admin", "database",
    "web-server", "mail-server", "dns-server", "firewall", "router", "nas", "htpc", "backup",
    "monitoring", "virtualization", "container", "cloud", "devops", "ci-cd", "testing",
  ]

  const architectures = [
    "x86_64", "i386", "arm", "arm64", "armhf", "armel", "mips", "mipsel", "powerpc",
    "s390x", "riscv64", "sparc64", "alpha", "hppa", "ia64", "m68k", "sh4",
  ]

  const versions = Array.from({ length: 50 }, (_, i) => `v${i + 1}`).concat(
    Array.from({ length: 30 }, (_, i) => `20${18 + Math.floor(i/10)}.${(i % 10) + 1}`),
    Array.from({ length: 20 }, (_, i) => `${i + 15}.04`),
    Array.from({ length: 20 }, (_, i) => `${i + 15}.10`)
  )

  const environments = [
    "gnome", "kde", "xfce", "lxde", "lxqt", "mate", "cinnamon", "budgie", "pantheon", "unity",
    "i3", "awesome", "bspwm", "dwm", "qtile", "herbstluftwm", "openbox", "fluxbox", "enlightenment",
    "sway", "hyprland", "river", "wayfire", "labwc", "cosmic",
  ]

  const categories = new Map<string, { save_path: string }>()

  // Add base categories
  baseCategories.forEach(category => {
    categories.set(category, { save_path: `/home/downloads/${category}` })
  })

  // Generate distro-based categories
  distros.forEach(distro => {
    categories.set(distro, { save_path: `/home/downloads/distros/${distro}` })

    // Add version combinations
    versions.slice(0, 3).forEach(version => {
      categories.set(`${distro}-${version}`, { save_path: `/home/downloads/distros/${distro}/${version}` })
    })

    // Add architecture combinations
    architectures.slice(0, 4).forEach(arch => {
      categories.set(`${distro}-${arch}`, { save_path: `/home/downloads/distros/${distro}/${arch}` })
    })

    // Add purpose combinations
    purposes.slice(0, 5).forEach(purpose => {
      categories.set(`${distro}-${purpose}`, { save_path: `/home/downloads/${purpose}/${distro}` })
    })

    // Add environment combinations
    environments.slice(0, 3).forEach(env => {
      categories.set(`${distro}-${env}`, { save_path: `/home/downloads/desktop/${distro}-${env}` })
    })
  })

  // Generate purpose-based categories
  purposes.forEach(purpose => {
    categories.set(purpose, { save_path: `/home/downloads/${purpose}` })

    architectures.slice(0, 3).forEach(arch => {
      categories.set(`${purpose}-${arch}`, { save_path: `/home/downloads/${purpose}/${arch}` })
    })

    versions.slice(0, 2).forEach(version => {
      categories.set(`${purpose}-${version}`, { save_path: `/home/downloads/${purpose}/${version}` })
    })
  })

  // Generate architecture-based categories
  architectures.forEach(arch => {
    categories.set(arch, { save_path: `/home/downloads/arch/${arch}` })

    environments.slice(0, 2).forEach(env => {
      categories.set(`${arch}-${env}`, { save_path: `/home/downloads/arch/${arch}/${env}` })
    })
  })

  // Generate environment-based categories
  environments.forEach(env => {
    categories.set(env, { save_path: `/home/downloads/desktop/${env}` })

    versions.slice(0, 2).forEach(version => {
      categories.set(`${env}-${version}`, { save_path: `/home/downloads/desktop/${env}/${version}` })
    })
  })

  // Add year-based categories
  for (let year = 2015; year <= 2024; year++) {
    categories.set(`${year}`, { save_path: `/home/downloads/releases/${year}` })

    for (let month = 1; month <= 12; month++) {
      if (categories.size < 1200) { // Don't go crazy
        const monthStr = month.toString().padStart(2, "0")
        categories.set(`${year}.${monthStr}`, { save_path: `/home/downloads/releases/${year}/${monthStr}` })
      }
    }
  }

  // Add some specialty categories
  const specialties = [
    "kernel-sources", "firmware", "drivers", "patches", "themes", "icons", "wallpapers",
    "fonts", "codecs", "plugins", "extensions", "addons", "scripts", "configs", "dotfiles",
    "benchmarks", "stress-tests", "monitoring-tools", "diagnostic-tools", "recovery-tools",
    "forensic-tools", "penetration-tools", "network-tools", "system-tools", "admin-tools",
  ]

  specialties.forEach(specialty => {
    categories.set(specialty, { save_path: `/home/downloads/tools/${specialty}` })

    distros.slice(0, 5).forEach(distro => {
      categories.set(`${specialty}-${distro}`, { save_path: `/home/downloads/tools/${specialty}/${distro}` })
    })
  })

  return Object.fromEntries(categories)
}

// Linux-themed categories for incognito mode (1000+ for testing virtual scrolling)
export const LINUX_CATEGORIES = generateLinuxCategories()

const LINUX_CATEGORIES_ARRAY = Object.keys(LINUX_CATEGORIES)

// Generate 1000+ Linux-themed tags for testing virtual scrolling
const generateLinuxTags = (): string[] => {
  const baseTags = [
    "stable", "lts", "bleeding-edge", "minimal", "gnome", "kde", "xfce", "server", "desktop",
    "arm64", "x86_64", "enterprise", "community", "official", "beta", "rc", "nightly",
    "security-focused", "lightweight", "rolling-release",
  ]

  const distros = [
    "ubuntu", "debian", "fedora", "arch", "centos", "rhel", "opensuse", "manjaro", "mint",
    "elementary", "zorin", "pop", "endeavour", "garuda", "artix", "void", "alpine", "gentoo",
    "nixos", "slackware", "kali", "parrot", "rocky", "alma", "mx", "solus", "deepin", "tails",
  ]

  const versions = Array.from({ length: 50 }, (_, i) => `v${i + 1}`).concat(
    Array.from({ length: 30 }, (_, i) => `20${18 + Math.floor(i/10)}.${(i % 10) + 1}`),
    Array.from({ length: 20 }, (_, i) => `${i + 15}.04`),
    Array.from({ length: 20 }, (_, i) => `${i + 15}.10`)
  )

  const architectures = [
    "i386", "amd64", "arm", "arm64", "armhf", "armel", "mips", "mipsel", "powerpc", "s390x",
    "riscv64", "sparc64", "alpha", "hppa", "ia64", "m68k", "sh4",
  ]

  const desktops = [
    "gnome", "kde", "xfce", "lxde", "lxqt", "mate", "cinnamon", "budgie", "pantheon", "unity",
    "i3", "awesome", "bspwm", "dwm", "qtile", "herbstluftwm", "openbox", "fluxbox", "enlightenment",
  ]

  const features = [
    "docker", "kubernetes", "systemd", "sysvinit", "openrc", "runit", "s6", "wayland", "x11",
    "pipewire", "pulseaudio", "alsa", "jack", "firefox", "chromium", "libreoffice", "gimp",
    "blender", "obs", "steam", "wine", "flatpak", "snap", "appimage", "python", "nodejs",
    "rust", "go", "java", "php", "ruby", "perl", "lua", "bash", "zsh", "fish", "tmux", "screen",
    "vim", "emacs", "nano", "vscode", "atom", "sublime", "jetbrains", "eclipse",
  ]

  const purposes = [
    "gaming", "workstation", "development", "multimedia", "education", "scientific", "medical",
    "financial", "embedded", "iot", "cloud", "container", "virtualization", "security", "privacy",
    "forensics", "penetration-testing", "reverse-engineering", "malware-analysis", "network-admin",
    "database", "web-server", "mail-server", "dns-server", "firewall", "router", "nas", "htpc",
  ]

  const statuses = [
    "stable", "testing", "unstable", "experimental", "deprecated", "legacy", "maintained",
    "unmaintained", "discontinued", "alpha", "beta", "rc", "release-candidate", "final",
    "patched", "updated", "latest", "current", "previous", "old", "ancient", "vintage",
  ]

  const tags = new Set<string>()

  // Add base tags
  baseTags.forEach(tag => tags.add(tag))

  // Generate combinations
  distros.forEach(distro => {
    tags.add(distro)
    versions.slice(0, 5).forEach(version => {
      tags.add(`${distro}-${version}`)
    })
    architectures.slice(0, 3).forEach(arch => {
      tags.add(`${distro}-${arch}`)
    })
    desktops.slice(0, 5).forEach(desktop => {
      tags.add(`${distro}-${desktop}`)
    })
    purposes.slice(0, 3).forEach(purpose => {
      tags.add(`${distro}-${purpose}`)
    })
  })

  // Add architecture tags
  architectures.forEach(arch => {
    tags.add(arch)
    statuses.slice(0, 3).forEach(status => {
      tags.add(`${arch}-${status}`)
    })
  })

  // Add desktop environment tags
  desktops.forEach(desktop => {
    tags.add(desktop)
    versions.slice(0, 3).forEach(version => {
      tags.add(`${desktop}-${version}`)
    })
  })

  // Add feature tags
  features.forEach(feature => {
    tags.add(feature)
    statuses.slice(0, 2).forEach(status => {
      tags.add(`${feature}-${status}`)
    })
  })

  // Add purpose tags
  purposes.forEach(purpose => {
    tags.add(purpose)
    statuses.slice(0, 2).forEach(status => {
      tags.add(`${purpose}-${status}`)
    })
  })

  // Add year-based tags
  for (let year = 2010; year <= 2024; year++) {
    tags.add(`${year}`)
    for (let month = 1; month <= 12; month++) {
      if (tags.size < 1500) { // Don't go crazy
        tags.add(`${year}.${month.toString().padStart(2, "0")}`)
      }
    }
  }

  return Array.from(tags).sort()
}

// Linux-themed tags for incognito mode (1000+ for testing virtual scrolling)
export const LINUX_TAGS = generateLinuxTags()

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

// Generate a deterministic but seemingly random Linux ISO name based on hash
export function getLinuxIsoName(hash: string): string {
  // Use hash to deterministically select an ISO name
  let hashSum = 0
  for (let i = 0; i < hash.length; i++) {
    hashSum += hash.charCodeAt(i)
  }
  return linuxIsoNames[hashSum % linuxIsoNames.length]
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

// Generate deterministic Linux tracker based on hash
export function getLinuxTracker(hash: string): string {
  let hashSum = 0
  for (let i = 0; i < Math.min(12, hash.length); i++) {
    hashSum += hash.charCodeAt(i) * (i + 4)
  }
  return `https://${LINUX_TRACKERS[hashSum % LINUX_TRACKERS.length]}/announce`
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