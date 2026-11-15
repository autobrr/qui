/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger
} from "@/components/ui/collapsible"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  Tabs,
  TabsList,
  TabsTrigger
} from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities.ts"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { useAddTorrentPreferences, useSubcategoriesEnabled } from "@/hooks/usePreferenceValues"
import { api } from "@/lib/api"
import { canOfferManualCrossSeed } from "@/lib/manual-cross-seed"
import { resolveDestinationPath } from "@/lib/save-path"
import { cn } from "@/lib/utils"
import type { AddTorrentResponse, Torrent } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, ChevronDown, FileText, Link, Loader2, Plus, SlidersHorizontal, Upload, X } from "lucide-react"
import type { Instance as ParsedTorrent } from "parse-torrent"
import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { useDropzone } from "react-dropzone"
import { toast } from "sonner"

import { CategoryPicker, TagPicker } from "./AddTorrentPickers"
import { DestinationFreeSpace } from "./DestinationFreeSpace"
import { ManualCrossSeedDialog } from "./ManualCrossSeedDialog"

// Extract info hash from magnet link
function extractHashFromMagnet(magnetUrl: string): string | null {
  const btihMatch = magnetUrl.match(/[?&]xt=urn:btih:([a-f0-9]{40}|[a-z2-7]{32})/i)
  if (btihMatch) {
    return btihMatch[1].toLowerCase()
  }

  const btmhMatch = magnetUrl.match(/[?&]xt=urn:btmh:([a-f0-9]+)/i)
  if (!btmhMatch) {
    return null
  }

  const multihash = btmhMatch[1].toLowerCase()
  // Multihash format: <code><digest-length><digest>. For v2 torrents qBittorrent expects SHA2-256 (0x12) with 32 byte digest.
  if (!multihash.startsWith("1220")) {
    return null
  }

  const digest = multihash.slice(4)
  return /^[a-f0-9]{64}$/.test(digest) ? digest : null
}

// Parse torrent file and extract info hash
async function parseTorrentFile(file: File): Promise<string | null> {
  try {
    const arrayBuffer = await file.arrayBuffer()
    // Only reached when the user actually hands us a .torrent file, so keep the
    // parser (and its node polyfills) off the boot path.
    const { default: parseTorrent } = await import("parse-torrent")
    const parsed = await parseTorrent(new Uint8Array(arrayBuffer))
    const parsedTorrent = parsed as ParsedTorrent & { infoHashV2?: string }

    const hash = parsedTorrent.infoHash || parsedTorrent.infoHashV2

    if (!hash) {
      return null
    }

    return hash.toLowerCase()
  } catch {
    return null
  }
}

export type AddTorrentDropPayload =
  | { type: "file"; files: File[] }
  | { type: "url"; urls: string[]; indexerId?: number }

interface AddTorrentDialogProps {
  instanceId: number
  open?: boolean
  onOpenChange?: (open: boolean) => void
  dropPayload?: AddTorrentDropPayload | null
  onDropPayloadConsumed?: () => void
  torrents?: Torrent[]
}

type TabValue = "file" | "url"

interface FormData {
  torrentFiles: File[] | null
  urls: string
  category: string
  tags: string[]
  startPaused: boolean
  autoTMM: boolean
  savePath: string
  skipHashCheck: boolean
  sequentialDownload: boolean
  firstLastPiecePrio: boolean
  limitUploadSpeed: number
  limitDownloadSpeed: number
  limitRatio: number
  limitSeedTime: number
  contentLayout: string
  rename: string
  tempPathEnabled: boolean
  tempPath: string
  indexerId?: number
}

interface DuplicateEntryDetails {
  label: string
  matches: string[]
}

interface DuplicateSummary {
  existingNames: string[]
  fileMatches: Record<string, DuplicateEntryDetails>
  urlMatches: Record<string, DuplicateEntryDetails>
}

function createEmptyDuplicateSummary(): DuplicateSummary {
  return {
    existingNames: [],
    fileMatches: {},
    urlMatches: {},
  }
}

function createFileKey(file: File): string {
  return `${file.name}__${file.size}__${file.lastModified}`
}

export function AddTorrentDialog({ instanceId, open: controlledOpen, onOpenChange, dropPayload, onDropPayloadConsumed, torrents = [] }: AddTorrentDialogProps) {
  const { t } = useTranslation("torrents")
  const [internalOpen, setInternalOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<TabValue>("file")
  const [showMoreOptions, setShowMoreOptions] = useState(false)
  const [wasOpen, setWasOpen] = useState(false)
  const [duplicateSummary, setDuplicateSummary] = useState<DuplicateSummary>(() => createEmptyDuplicateSummary())
  const [manualCrossSeedFile, setManualCrossSeedFile] = useState<File | null>(null)
  const [duplicateCheckStatus, setDuplicateCheckStatus] = useState<"idle" | "pending" | "visible">("idle")
  const fileInputRef = useRef<HTMLInputElement>(null)
  const moreOptionsRef = useRef<HTMLDivElement>(null)
  const duplicateCheckRequestRef = useRef(0)
  const duplicateCheckIndicatorTimeoutRef = useRef<number | null>(null)
  const queryClient = useQueryClient()

  // Use controlled state if provided, otherwise use internal state
  const open = controlledOpen !== undefined ? controlledOpen : internalOpen
  const setOpen = onOpenChange || setInternalOpen

  const { data: metadata } = useInstanceMetadata(instanceId)
  const categories = metadata?.categories
  const availableTags = metadata?.tags
  // The dialog seeds the preference defaults twice: once as form defaults, and again
  // when the preferences arrive after a cold mount.
  // Fetched again on each open, so that a default changed outside qui shows.
  const addTorrentPreferences = useAddTorrentPreferences(instanceId, { enabled: open })
  const preferenceDefaults = addTorrentPreferences.defaults

  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const supportsTorrentTmpPath = capabilities?.supportsTorrentTmpPath ?? false
  const supportsPathAutocomplete = capabilities?.supportsPathAutocomplete ?? false
  const subcategoriesEnabled = useSubcategoriesEnabled(instanceId, capabilities, false, { enabled: open })
  // qBittorrent saves a category without a path under its parent's folder since 5.0 (Web API 2.11.2, the
  // torrent creation capability), and before 5.2 only while subcategories are on.
  const parentCategoryPaths = (capabilities?.supportsTorrentCreation ?? true) && subcategoriesEnabled

  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setShowMoreOptions(false)
  }

  useEffect(() => {
    if (!open) {
      setDuplicateSummary(createEmptyDuplicateSummary())
      setDuplicateCheckStatus("idle")
      if (duplicateCheckIndicatorTimeoutRef.current !== null) {
        window.clearTimeout(duplicateCheckIndicatorTimeoutRef.current)
        duplicateCheckIndicatorTimeoutRef.current = null
      }
    }
  }, [open])

  useEffect(() => {
    return () => {
      if (duplicateCheckIndicatorTimeoutRef.current !== null) {
        window.clearTimeout(duplicateCheckIndicatorTimeoutRef.current)
        duplicateCheckIndicatorTimeoutRef.current = null
      }
    }
  }, [])

  // Check for duplicate torrents when files or URLs are loaded
  const checkForDuplicates = useCallback(async (files: File[] | null, urls: string) => {
    duplicateCheckRequestRef.current += 1
    const requestId = duplicateCheckRequestRef.current
    setDuplicateCheckStatus("pending")
    if (duplicateCheckIndicatorTimeoutRef.current !== null) {
      window.clearTimeout(duplicateCheckIndicatorTimeoutRef.current)
      duplicateCheckIndicatorTimeoutRef.current = null
    }
    duplicateCheckIndicatorTimeoutRef.current = window.setTimeout(() => {
      if (duplicateCheckRequestRef.current === requestId) {
        setDuplicateCheckStatus("visible")
      }
    }, 300)

    const isLatest = () => duplicateCheckRequestRef.current === requestId

    type HashSource =
      | { type: "file"; key: string; label: string }
      | { type: "url"; key: string }

    const duplicateNameSet = new Set<string>()
    const duplicateFileMap = new Map<string, { label: string; matches: Set<string> }>()
    const duplicateUrlMap = new Map<string, { label: string; matches: Set<string> }>()
    const hashesForApi = new Set<string>()
    const hashSources = new Map<string, HashSource[]>()
    const finalizeCheck = () => {
      if (duplicateCheckRequestRef.current !== requestId) {
        return
      }
      if (duplicateCheckIndicatorTimeoutRef.current !== null) {
        window.clearTimeout(duplicateCheckIndicatorTimeoutRef.current)
        duplicateCheckIndicatorTimeoutRef.current = null
      }
      setDuplicateCheckStatus("idle")
    }

    const getHashSources = (hash: string) => {
      const normalized = hash.toLowerCase()
      let sources = hashSources.get(normalized)
      if (!sources) {
        sources = []
        hashSources.set(normalized, sources)
      }
      return sources
    }

    const ensureFileEntry = (file: File) => {
      const key = createFileKey(file)
      let entry = duplicateFileMap.get(key)
      if (!entry) {
        entry = { label: file.name, matches: new Set<string>() }
        duplicateFileMap.set(key, entry)
      }
      return { key, entry }
    }

    const ensureUrlEntry = (urlValue: string) => {
      let entry = duplicateUrlMap.get(urlValue)
      if (!entry) {
        entry = { label: urlValue, matches: new Set<string>() }
        duplicateUrlMap.set(urlValue, entry)
      }
      return entry
    }

    const recordFileMatch = (fileKey: string, matchLabel: string | undefined, fallback: string) => {
      const entry = duplicateFileMap.get(fileKey)
      if (!entry) {
        return
      }
      const resolvedMatch = (matchLabel && matchLabel.trim()) || fallback
      if (!resolvedMatch) {
        return
      }
      entry.matches.add(resolvedMatch)
      duplicateNameSet.add(resolvedMatch)
    }

    const recordUrlMatch = (urlKey: string, matchLabel: string | undefined, fallback: string) => {
      const entry = duplicateUrlMap.get(urlKey)
      if (!entry) {
        return
      }
      const resolvedMatch = (matchLabel && matchLabel.trim()) || fallback
      if (!resolvedMatch) {
        return
      }
      entry.matches.add(resolvedMatch)
      duplicateNameSet.add(resolvedMatch)
    }

    const findMatchingTorrent = (hash: string) => {
      const normalized = hash.toLowerCase()
      return torrents.find((torrent) => {
        const candidates = [
          torrent.hash,
          torrent.infohash_v1,
          torrent.infohash_v2,
        ].filter(Boolean) as string[]

        return candidates.some((candidate) => candidate.toLowerCase() === normalized)
      })
    }

    if (files && files.length > 0) {
      try {
        const hashes = await Promise.all(files.map((file) => parseTorrentFile(file)))

        files.forEach((file, index) => {
          const hash = hashes[index]
          if (!hash) {
            return
          }

          const normalized = hash.toLowerCase()
          const { key: fileKey } = ensureFileEntry(file)
          const sources = getHashSources(normalized)
          if (!sources.some((source) => source.type === "file" && source.key === fileKey)) {
            sources.push({ type: "file", key: fileKey, label: file.name })
          }

          const existingTorrent = findMatchingTorrent(normalized)
          if (existingTorrent) {
            recordFileMatch(fileKey, existingTorrent.name, normalized)
          } else {
            hashesForApi.add(normalized)
          }
        })
      } catch (error) {
        console.error("[checkForDuplicates] Error parsing torrent files:", error)
      }
    }

    if (urls) {
      const urlList = urls
        .split("\n")
        .map((u) => u.trim())
        .filter(Boolean)

      for (const url of urlList) {
        const hash = extractHashFromMagnet(url)
        if (!hash) {
          continue
        }

        const normalized = hash.toLowerCase()
        ensureUrlEntry(url)
        const sources = getHashSources(normalized)
        if (!sources.some((source) => source.type === "url" && source.key === url)) {
          sources.push({ type: "url", key: url })
        }

        const existingTorrent = findMatchingTorrent(normalized)
        if (existingTorrent) {
          recordUrlMatch(url, existingTorrent.name, normalized)
        } else {
          hashesForApi.add(normalized)
        }
      }
    }

    const publishResults = () => {
      if (!isLatest()) {
        return
      }

      const fileMatches: Record<string, DuplicateEntryDetails> = {}
      duplicateFileMap.forEach((entry, key) => {
        if (entry.matches.size === 0) {
          return
        }
        fileMatches[key] = {
          label: entry.label,
          matches: Array.from(entry.matches).sort((a, b) => a.localeCompare(b)),
        }
      })

      const urlMatches: Record<string, DuplicateEntryDetails> = {}
      duplicateUrlMap.forEach((entry, key) => {
        if (entry.matches.size === 0) {
          return
        }
        urlMatches[key] = {
          label: entry.label,
          matches: Array.from(entry.matches).sort((a, b) => a.localeCompare(b)),
        }
      })

      setDuplicateSummary({
        existingNames: Array.from(duplicateNameSet).sort((a, b) => a.localeCompare(b)),
        fileMatches,
        urlMatches,
      })
    }

    if (hashesForApi.size === 0) {
      publishResults()
      finalizeCheck()
      return
    }

    if (!isLatest()) {
      return
    }

    try {
      const hashList = Array.from(hashesForApi).slice(0, 512)
      const response = await api.checkTorrentDuplicates(instanceId, hashList)
      if (!isLatest()) {
        return
      }

      for (const duplicate of response.duplicates ?? []) {
        const displayName =
          duplicate.name ||
          duplicate.hash ||
          duplicate.infohash_v1 ||
          duplicate.infohash_v2 ||
          t("addTorrentDialog.duplicate.existingTorrent")
        if (displayName) {
          duplicateNameSet.add(displayName)
        }

        const candidateHashes = new Set<string>()
        if (duplicate.hash) {
          candidateHashes.add(duplicate.hash.toLowerCase())
        }
        if (duplicate.infohash_v1) {
          candidateHashes.add(duplicate.infohash_v1.toLowerCase())
        }
        if (duplicate.infohash_v2) {
          candidateHashes.add(duplicate.infohash_v2.toLowerCase())
        }
        if (duplicate.matched_hashes) {
          duplicate.matched_hashes.forEach((matched) => {
            candidateHashes.add(matched.toLowerCase())
          })
        }

        candidateHashes.forEach((candidateHash) => {
          const sources = hashSources.get(candidateHash)
          if (!sources) {
            return
          }

          sources.forEach((source) => {
            if (source.type === "file") {
              recordFileMatch(source.key, displayName, candidateHash)
            } else {
              recordUrlMatch(source.key, displayName, candidateHash)
            }
          })
        })
      }
    } catch (error) {
      console.error("[checkForDuplicates] Failed to check duplicates via API:", error)
    }

    publishResults()
    finalizeCheck()
  }, [instanceId, torrents, t])


  const duplicateFileEntries = duplicateSummary.fileMatches
  const duplicateUrlEntries = duplicateSummary.urlMatches
  const duplicateFileKeys = Object.keys(duplicateFileEntries)
  const duplicateUrlKeys = Object.keys(duplicateUrlEntries)
  const duplicateSelectionCount = duplicateFileKeys.length + duplicateUrlKeys.length
  const duplicatePreviewNames = duplicateSummary.existingNames.slice(0, 2)
  const duplicatePreviewRemaining = Math.max(duplicateSummary.existingNames.length - duplicatePreviewNames.length, 0)
  const showDuplicateCheckIndicator = duplicateCheckStatus === "visible"

  const mutation = useMutation({
    retry: false, // Don't retry - could cause duplicate torrent additions
    mutationFn: async (data: FormData) => {
      // Use the user's explicit TMM choice
      const autoTMM = data.autoTMM
      // When autoTMM is enabled, temp path settings aren't visible/relevant
      const tempPathChanged =
        !autoTMM && (data.tempPathEnabled !== preferenceDefaults.tempPathEnabled ||
        (data.tempPathEnabled && data.tempPath !== preferenceDefaults.tempPath))

      const submitData: Parameters<typeof api.addTorrent>[1] = {
        startPaused: data.startPaused,
        savePath: !autoTMM && data.savePath ? data.savePath : undefined,
        useDownloadPath: tempPathChanged ? data.tempPathEnabled : undefined,
        downloadPath: tempPathChanged && data.tempPathEnabled ? data.tempPath : undefined,
        autoTMM: autoTMM,
        category: data.category || undefined,
        tags: data.tags.length > 0 ? data.tags : undefined,
        skipHashCheck: data.skipHashCheck,
        sequentialDownload: data.sequentialDownload,
        firstLastPiecePrio: data.firstLastPiecePrio,
        limitUploadSpeed: data.limitUploadSpeed > 0 ? data.limitUploadSpeed : undefined,
        limitDownloadSpeed: data.limitDownloadSpeed > 0 ? data.limitDownloadSpeed : undefined,
        limitRatio: data.limitRatio > 0 ? data.limitRatio : undefined,
        limitSeedTime: data.limitSeedTime > 0 ? data.limitSeedTime : undefined,
        contentLayout: data.contentLayout === "__global__" ? undefined : data.contentLayout || undefined,
        rename: data.rename || undefined,
      }

      if (activeTab === "file" && data.torrentFiles && data.torrentFiles.length > 0) {
        submitData.torrentFiles = data.torrentFiles
      } else if (activeTab === "url" && data.urls) {
        submitData.urls = data.urls.split("\n").map(u => u.trim()).filter(Boolean)
        if (data.indexerId) {
          submitData.indexerId = data.indexerId
        }
      }

      return api.addTorrent(instanceId, submitData)
    },
    onError: (error) => {
      let description = t("addTorrentDialog.toast.verifyInput")
      if (error instanceof Error && error.message && !error.message.startsWith("HTTP error! status:")) {
        description = error.message
      }

      toast.error(t("addTorrentDialog.toast.failedToAdd"), {
        description,
        duration: 5000,
      })
    },
    onSuccess: (response: AddTorrentResponse) => {
      // Add small delay to allow qBittorrent to process the new torrent
      setTimeout(() => {
        // Use refetch instead of invalidate to avoid loading state
        queryClient.refetchQueries({
          queryKey: ["torrents-list", instanceId],
          exact: false,
          type: "active",
        })
        // Also refetch the metadata (categories, tags, counts)
        queryClient.refetchQueries({
          queryKey: ["instance-metadata", instanceId],
          exact: false,
          type: "active",
        })
      }, 500) // Give qBittorrent time to process

      // Show appropriate toast based on results
      if (response.failed === 0) {
        toast.success(
          response.added === 1
            ? t("addTorrentDialog.toast.addedOne")
            : t("addTorrentDialog.toast.addedMany", { count: response.added })
        )
      } else if (response.added === 0) {
        // All failed
        const failedDetails = [
          ...(response.failedURLs?.map(f => `${f.url}: ${f.error}`) ?? []),
          ...(response.failedFiles?.map(f => `${f.filename}: ${f.error}`) ?? []),
        ]
        toast.error(t("addTorrentDialog.toast.failedMany", { count: response.failed }), {
          description: failedDetails.length > 0 ? failedDetails.slice(0, 3).join("\n") : undefined,
          duration: 5000,
        })
      } else {
        // Partial success
        const failedDetails = [
          ...(response.failedURLs?.map(f => `${f.url}: ${f.error}`) ?? []),
          ...(response.failedFiles?.map(f => `${f.filename}: ${f.error}`) ?? []),
        ]
        toast.warning(t("addTorrentDialog.toast.partialSuccess", { added: response.added, failed: response.failed }), {
          description: failedDetails.length > 0 ? failedDetails.slice(0, 3).join("\n") : undefined,
          duration: 5000,
        })
      }

      setOpen(false)
      form.reset()
    },
  })

  const form = useForm({
    defaultValues: {
      torrentFiles: null as File[] | null,
      urls: "",
      category: "",
      tags: [] as string[],
      ...preferenceDefaults,
      skipHashCheck: false,
      sequentialDownload: false,
      firstLastPiecePrio: false,
      limitUploadSpeed: 0,
      limitDownloadSpeed: 0,
      limitRatio: 0,
      limitSeedTime: 0,
      rename: "",
      indexerId: undefined as number | undefined,
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync(value)
    },
  })

  // The dialog can mount before instance metadata resolves (magnet handler route),
  // where defaultValues fall back to the hardcoded values, and each open fetches the
  // preferences again. Seed the preference-derived fields each time preferences land,
  // skipping anything the user already changed.
  useEffect(() => {
    if (!addTorrentPreferences.loaded) return

    // Seeding is not a user edit, so leave the field meta pristine.
    const pristine = (field: keyof typeof preferenceDefaults) => !form.getFieldMeta(field)?.isDirty
    const opts = { dontUpdateMeta: true }

    if (pristine("autoTMM")) form.setFieldValue("autoTMM", preferenceDefaults.autoTMM, opts)
    if (pristine("savePath")) form.setFieldValue("savePath", preferenceDefaults.savePath, opts)
    if (pristine("contentLayout")) form.setFieldValue("contentLayout", preferenceDefaults.contentLayout, opts)
    if (pristine("tempPathEnabled")) form.setFieldValue("tempPathEnabled", preferenceDefaults.tempPathEnabled, opts)
    if (pristine("tempPath")) form.setFieldValue("tempPath", preferenceDefaults.tempPath, opts)
    if (pristine("startPaused")) form.setFieldValue("startPaused", preferenceDefaults.startPaused, opts)
  }, [form, addTorrentPreferences.loaded, preferenceDefaults])

  const setSavePath = useCallback((path: string) => {
    form.setFieldValue("savePath", path)
  }, [form])

  const setTempPath = useCallback((path: string) => {
    form.setFieldValue("tempPath", path)
  }, [form])

  const {
    suggestions: saveSuggestions,
    handleInputChange: handleSaveInputChange,
    handleSelect: handleSaveInputSelect,
    handleKeyDown: handleSaveKeyDown,
    highlightedIndex: saveHighlightedIndex,
    showSuggestions: showSaveSuggestions,
    inputRef: savePathInputRef,
    listRef: savePathListRef,
  } = usePathAutocomplete(setSavePath, instanceId);

  const {
    suggestions: tempSuggestions,
    handleInputChange: handleTempInputChange,
    handleSelect: handleTempInputSelect,
    handleKeyDown: handleTempKeyDown,
    highlightedIndex: tempHighlightedIndex,
    showSuggestions: showTempSuggestions,
    inputRef: tempPathInputRef,
    listRef: tempPathListRef,
  } = usePathAutocomplete(setTempPath, instanceId);

  const onDrop = useCallback((acceptedFiles: File[]) => {
    // Filter to .torrent files only (iOS Safari may bypass accept attribute filtering)
    const torrentFiles = acceptedFiles.filter(f => f.name.toLowerCase().endsWith(".torrent"))
    const rejectedCount = acceptedFiles.length - torrentFiles.length

    if (rejectedCount > 0) {
      toast.error(
        rejectedCount === 1
          ? t("addTorrentDialog.toast.rejectedOne")
          : t("addTorrentDialog.toast.rejectedMany", { count: rejectedCount })
      )
    }

    if (torrentFiles.length === 0) {
      return
    }

    const existingFiles = form.getFieldValue("torrentFiles") || []
    const allFiles = [...existingFiles, ...torrentFiles]
    form.setFieldValue("torrentFiles", allFiles.length > 0 ? allFiles : null)

    // Check for duplicates when files are dropped
    if (allFiles.length > 0) {
      checkForDuplicates(allFiles, form.getFieldValue("urls"))
    }
  }, [form, checkForDuplicates, t])

  const { getRootProps, getInputProps, isDragActive } = useDropzone({
    onDrop,
    accept: {
      // Multiple MIME types for better iOS compatibility
      // iOS Safari has bugs with accept attribute filtering
      "application/x-bittorrent": [".torrent"],
      "application/octet-stream": [".torrent"],  // Fallback for browsers that report torrent as generic binary
    },
    multiple: true,
    noClick: false,
  })

  const handleRemoveDuplicateSelections = useCallback(() => {
    if (duplicateFileKeys.length === 0 && duplicateUrlKeys.length === 0) {
      return
    }

    const duplicateFileKeySet = new Set(duplicateFileKeys)
    const duplicateUrlKeySet = new Set(duplicateUrlKeys)

    const rawFiles = form.getFieldValue("torrentFiles")
    const currentFiles = Array.isArray(rawFiles) ? (rawFiles as File[]) : null
    const rawUrls = form.getFieldValue("urls")
    const currentUrls = typeof rawUrls === "string" ? rawUrls : ""

    const filteredFiles = currentFiles ? currentFiles.filter((file) => !duplicateFileKeySet.has(createFileKey(file))) : []

    const filteredUrls = currentUrls
      .split("\n")
      .map((u) => u.trim())
      .filter(Boolean)
      .filter((url) => !duplicateUrlKeySet.has(url))

    const nextFiles = filteredFiles.length > 0 ? filteredFiles : null
    const nextUrls = filteredUrls.join("\n")

    form.setFieldValue("torrentFiles", nextFiles)
    form.setFieldValue("urls", nextUrls)

    if (!nextFiles) {
      if (fileInputRef.current) {
        fileInputRef.current.value = ""
      }
    }

    checkForDuplicates(nextFiles, nextUrls)
  }, [checkForDuplicates, duplicateFileKeys, duplicateUrlKeys, form])

  const handleRemoveFile = useCallback((indexToRemove: number) => {
    const rawFiles = form.getFieldValue("torrentFiles")
    const currentFiles = Array.isArray(rawFiles) ? (rawFiles as File[]) : null

    if (!currentFiles) {
      return
    }

    const filteredFiles = currentFiles.filter((_, index) => index !== indexToRemove)
    const nextFiles = filteredFiles.length > 0 ? filteredFiles : null

    form.setFieldValue("torrentFiles", nextFiles)

    if (!nextFiles) {
      if (fileInputRef.current) {
        fileInputRef.current.value = ""
      }
    }

    checkForDuplicates(nextFiles, form.getFieldValue("urls") || "")
  }, [checkForDuplicates, form])

  useEffect(() => {
    if (!dropPayload) {
      return
    }

    if (dropPayload.type === "file") {
      const files = dropPayload.files.filter((file): file is File => file instanceof File)
      setActiveTab("file")
      form.setFieldValue("torrentFiles", files.length > 0 ? files : null)
      form.setFieldValue("urls", "")
      if (fileInputRef.current) {
        fileInputRef.current.value = ""
      }
      // Check for duplicates when files are dropped
      checkForDuplicates(files, "")
    } else if (dropPayload.type === "url") {
      const urls = dropPayload.urls.map((url) => url.trim()).filter(Boolean)
      setActiveTab("url")
      form.setFieldValue("urls", urls.join("\n"))
      form.setFieldValue("torrentFiles", null)
      form.setFieldValue("indexerId", dropPayload.indexerId)
      if (fileInputRef.current) {
        fileInputRef.current.value = ""
      }
      // Check for duplicates when URLs are dropped
      checkForDuplicates(null, urls.join("\n"))
    }

    setOpen(true)
    onDropPayloadConsumed?.()
  }, [dropPayload, form, onDropPayloadConsumed, setOpen, checkForDuplicates])

  useEffect(() => {
    if (open) {
      return
    }
    form.reset()
    if (fileInputRef.current) {
      fileInputRef.current.value = ""
    }
  }, [open, form])


  const renderDuplicateMatches = (matches: string[]) => (
    <span className="block text-[11px] text-yellow-700 dark:text-yellow-500">
      {t("addTorrentDialog.duplicate.matchesExisting", { names: matches.slice(0, 2).join(", ") })}
      {matches.length > 2 && ` ${t("addTorrentDialog.duplicate.more", { count: matches.length - 2 })}`}
    </span>
  )

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <ManualCrossSeedDialog
        instanceId={instanceId}
        open={manualCrossSeedFile !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setManualCrossSeedFile(null)
          }
        }}
        initialFile={manualCrossSeedFile}
        onApplied={() => setOpen(false)}
      />
      {controlledOpen === undefined && (
        <DialogTrigger asChild>
          <Button>
            <Plus className="mr-2 h-4 w-4 transition-transform duration-200" />
            {t("addTorrentDialog.trigger")}
          </Button>
        </DialogTrigger>
      )}
      <DialogContent className="flex flex-col gap-0 w-full max-w-[95vw] sm:max-w-lg md:max-w-xl lg:max-w-2xl max-h-[90dvh] sm:max-h-[85dvh] p-0 !translate-y-0 !top-[5dvh] sm:!top-[7.5dvh]">
        <DialogHeader className="px-5 sm:px-6 pt-5 pb-3 flex-shrink-0 text-left">
          <DialogTitle>{t("addTorrentDialog.title")}</DialogTitle>
          <DialogDescription className="sr-only">
            {t("addTorrentDialog.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto px-5 sm:px-6">
          <form
            onSubmit={(e) => {
              e.preventDefault()
              form.handleSubmit()
            }}
            className="space-y-5 pt-1 pb-4"
          >
            <section className="space-y-3">
              <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as TabValue)}>
                <TabsList className="grid w-full grid-cols-2">
                  <TabsTrigger value="file" className="gap-2">
                    <Upload className="h-4 w-4" />
                    {t("addTorrentDialog.file")}
                  </TabsTrigger>
                  <TabsTrigger value="url" className="gap-2">
                    <Link className="h-4 w-4" />
                    {t("addTorrentDialog.url")}
                  </TabsTrigger>
                </TabsList>
              </Tabs>

              {activeTab === "file" ? (
                <form.Field
                  name="torrentFiles"
                  validators={{
                    onChange: ({ value }) => {
                      if ((!value || value.length === 0) && activeTab === "file") {
                        return t("addTorrentDialog.validation.selectTorrentFile")
                      }
                      return undefined
                    },
                  }}
                >
                  {(field) => {
                    const files = Array.isArray(field.state.value) ? field.state.value : []
                    const hasFiles = files.length > 0
                    return (
                      <div className="space-y-2">
                        <Label htmlFor="torrentFiles" className="sr-only">{t("addTorrentDialog.fileInput.label")}</Label>
                        <div
                          {...getRootProps({
                            className: cn(
                              "border border-dashed rounded-md cursor-pointer transition-colors",
                              "data-[drag-active]:border-primary data-[drag-active]:bg-primary/5",
                              "border-border hover:border-primary/40 hover:bg-accent/30",
                              hasFiles ? "px-3 py-2.5" : "px-4 py-6"
                            ),
                          })}
                          data-drag-active={isDragActive ? "" : undefined}
                        >
                          <input {...getInputProps({ id: "torrentFiles" })} />
                          {hasFiles ? (
                            <div className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
                              <Plus className="h-4 w-4" />
                              {isDragActive ? t("addTorrentDialog.fileInput.dropActive") : t("addTorrentDialog.fileInput.addMore")}
                            </div>
                          ) : (
                            <div className="flex flex-col items-center justify-center text-center gap-1.5">
                              <Upload className="h-7 w-7 text-muted-foreground" />
                              {isDragActive ? (
                                <p className="text-sm font-medium">{t("addTorrentDialog.fileInput.dropActive")}</p>
                              ) : (
                                <>
                                  <p className="text-sm font-medium sm:hidden">{t("addTorrentDialog.fileInput.choose")}</p>
                                  <p className="text-sm font-medium hidden sm:block">{t("addTorrentDialog.fileInput.dragDrop")}</p>
                                  <p className="text-xs text-muted-foreground hidden sm:block">{t("addTorrentDialog.fileInput.browse")}</p>
                                </>
                              )}
                            </div>
                          )}
                        </div>

                        {hasFiles && (
                          <div className="rounded-md border">
                            <div className="flex items-center gap-2 border-b px-3 py-1.5 text-xs text-muted-foreground">
                              <span>{t("addTorrentDialog.fileInput.selectedFiles", { count: files.length })}</span>
                              {duplicateFileKeys.length > 0 && (
                                <span className="flex items-center gap-1 font-medium text-yellow-600 dark:text-yellow-500">
                                  <AlertCircle className="h-3 w-3" />
                                  {t("addTorrentDialog.fileInput.duplicateFiles", { count: duplicateFileKeys.length })}
                                </span>
                              )}
                            </div>
                            <ul className="max-h-40 overflow-y-auto divide-y">
                              {files.map((file, index) => {
                                const fileKey = createFileKey(file)
                                const duplicateInfo = duplicateFileEntries[fileKey]
                                return (
                                  <li
                                    key={`${fileKey}-${index}`}
                                    className={cn(
                                      "flex items-center gap-2 pl-3 pr-1 py-1 text-sm",
                                      duplicateInfo && "bg-yellow-500/10"
                                    )}
                                  >
                                    <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
                                    <div className="min-w-0 flex-1">
                                      <span className="block break-all">{file.name}</span>
                                      {duplicateInfo?.matches.length ? renderDuplicateMatches(duplicateInfo.matches) : null}
                                    </div>
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon"
                                      onClick={() => handleRemoveFile(index)}
                                      className="h-10 w-10 shrink-0 text-muted-foreground hover:bg-destructive/10 hover:text-destructive sm:h-8 sm:w-8"
                                      aria-label={t("addTorrentDialog.fileInput.removeFile")}
                                      title={t("addTorrentDialog.fileInput.removeFile")}
                                    >
                                      <X className="h-4 w-4" />
                                    </Button>
                                  </li>
                                )
                              })}
                            </ul>
                          </div>
                        )}
                        {field.state.meta.isTouched && field.state.meta.errors[0] && (
                          <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                        )}
                      </div>
                    )
                  }}
                </form.Field>
              ) : (
                <form.Field
                  name="urls"
                  validators={{
                    onChange: ({ value }) => {
                      if (!value && activeTab === "url") {
                        return t("addTorrentDialog.validation.enterUrl")
                      }
                      return undefined
                    },
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="urls" className="sr-only">{t("addTorrentDialog.urlInput.label")}</Label>
                      <Textarea
                        id="urls"
                        placeholder={t("addTorrentDialog.urlInput.placeholder")}
                        rows={3}
                        className="max-h-40 font-mono break-all md:text-xs"
                        autoCapitalize="off"
                        autoCorrect="off"
                        spellCheck={false}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => {
                          field.handleChange(e.target.value)
                          checkForDuplicates(form.getFieldValue("torrentFiles"), e.target.value)
                        }}
                      />
                      {duplicateUrlKeys.length > 0 && (
                        <div className="rounded-md border border-yellow-600/30 bg-yellow-500/5 p-2 space-y-2 text-xs">
                          {duplicateUrlKeys.map((urlKey) => {
                            const duplicateInfo = duplicateUrlEntries[urlKey]
                            if (!duplicateInfo) {
                              return null
                            }
                            return (
                              <div key={urlKey} className="text-yellow-700 dark:text-yellow-500 space-y-1">
                                <div className="font-medium truncate">{duplicateInfo.label}</div>
                                {duplicateInfo.matches.length > 0 && renderDuplicateMatches(duplicateInfo.matches)}
                              </div>
                            )
                          })}
                        </div>
                      )}
                      {field.state.meta.isTouched && field.state.meta.errors[0] && (
                        <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                      )}
                    </div>
                  )}
                </form.Field>
              )}

              {showDuplicateCheckIndicator && (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {t("addTorrentDialog.duplicate.checking")}
                </div>
              )}

              {duplicateSelectionCount > 0 && (
                <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-yellow-600/30 bg-yellow-500/5 px-3 py-2">
                  <div className="flex min-w-0 flex-col gap-0.5 text-sm">
                    <span className="flex items-center gap-2 font-medium text-yellow-700 dark:text-yellow-500">
                      <AlertCircle className="h-4 w-4 shrink-0" />
                      {t("addTorrentDialog.duplicate.selectionDetected", { count: duplicateSelectionCount })}
                    </span>
                    <span className="text-xs text-muted-foreground break-words">
                      {duplicatePreviewNames.length > 0 ? (
                        <>
                          {t("addTorrentDialog.duplicate.existingTorrents", { names: duplicatePreviewNames.join(", ") })}
                          {duplicatePreviewRemaining > 0 && ` ${t("addTorrentDialog.duplicate.more", { count: duplicatePreviewRemaining })}`}
                        </>
                      ) : t("addTorrentDialog.duplicate.highlightedBelow")}
                    </span>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="text-yellow-700 dark:text-yellow-500 border-yellow-600/40 hover:bg-yellow-600/10"
                    onClick={handleRemoveDuplicateSelections}
                  >
                    {t("addTorrentDialog.duplicate.remove")}
                  </Button>
                </div>
              )}

              {activeTab === "file" && (
                <form.Subscribe selector={(state) => [state.values.torrentFiles, state.values.urls] as const}>
                  {([files, urls]) => {
                    if (!files || !canOfferManualCrossSeed({ fileCount: files.length, urlText: urls ?? "" })) {
                      return null
                    }
                    return (
                      <div className="flex items-center justify-between gap-3 rounded-md border p-3">
                        <div className="min-w-0">
                          <p className="text-sm font-medium">{t("manualCrossSeed.addOptionTitle")}</p>
                          <p className="text-xs text-muted-foreground">{t("manualCrossSeed.addOptionDescription")}</p>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          onClick={() => setManualCrossSeedFile(files[0])}
                        >
                          {t("manualCrossSeed.addOptionButton")}
                        </Button>
                      </div>
                    )
                  }}
                </form.Subscribe>
              )}
            </section>

            <section className="space-y-4">
              <form.Field name="category">
                {(field) => (
                  <div className="space-y-1.5">
                    <Label htmlFor="category">{t("addTorrentDialog.options.category")}</Label>
                    <CategoryPicker
                      id="category"
                      categories={categories}
                      value={field.state.value}
                      onChange={field.handleChange}
                    />
                    <form.Subscribe selector={(state) => state.values.autoTMM}>
                      {(autoTMM) => {
                        const target = resolveDestinationPath({
                          autoTMM,
                          savePath: "",
                          category: field.state.value,
                          categories,
                          defaultSavePath: preferenceDefaults.savePath,
                          parentCategoryPaths,
                        })
                        return autoTMM && target ? (
                          <p className="truncate text-xs text-muted-foreground" title={target}>
                            {t("addTorrentDialog.options.categorySavePath", { path: target })}
                          </p>
                        ) : null
                      }}
                    </form.Subscribe>
                  </div>
                )}
              </form.Field>

              <form.Field name="tags">
                {(field) => (
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between gap-2">
                      <Label>{t("addTorrentDialog.options.tags")}</Label>
                      {field.state.value.length > 1 && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="-my-1 h-8 px-2 text-xs text-muted-foreground"
                          onClick={() => field.handleChange([])}
                        >
                          {t("addTorrentDialog.options.clearAll")}
                        </Button>
                      )}
                    </div>
                    <TagPicker
                      id="tags"
                      availableTags={availableTags}
                      value={field.state.value}
                      onChange={field.handleChange}
                    />
                  </div>
                )}
              </form.Field>
            </section>

            <section className="grid grid-cols-1 sm:grid-cols-2 gap-2">
              <form.Field name="startPaused">
                {(field) => (
                  <OptionSwitch
                    id="startPaused"
                    label={t("addTorrentDialog.options.startPaused")}
                    checked={field.state.value}
                    onCheckedChange={field.handleChange}
                  />
                )}
              </form.Field>
              <form.Field name="skipHashCheck">
                {(field) => (
                  <OptionSwitch
                    id="skipHashCheck"
                    label={t("addTorrentDialog.options.skipHashCheck")}
                    checked={field.state.value}
                    onCheckedChange={field.handleChange}
                  />
                )}
              </form.Field>
            </section>

            <Collapsible
              ref={moreOptionsRef}
              open={showMoreOptions}
              onOpenChange={(next) => {
                setShowMoreOptions(next)
                if (next) {
                  requestAnimationFrame(() => moreOptionsRef.current?.scrollIntoView({ behavior: "smooth", block: "start" }))
                }
              }}
            >
              <CollapsibleTrigger asChild>
                <button
                  type="button"
                  className="flex w-full items-center gap-2 rounded-md border px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/40"
                >
                  <SlidersHorizontal className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="font-medium">{t("addTorrentDialog.options.moreOptions")}</span>
                    <span className="truncate text-xs text-muted-foreground">{t("addTorrentDialog.options.moreOptionsHint")}</span>
                  </span>
                  <form.Subscribe selector={(state) => countChangedOptions(state.values, preferenceDefaults, supportsTorrentTmpPath)}>
                    {(changed) => changed > 0 && (
                      <Badge variant="secondary" className="shrink-0">
                        {t("addTorrentDialog.options.changedCount", { count: changed })}
                      </Badge>
                    )}
                  </form.Subscribe>
                  <ChevronDown className={cn("h-4 w-4 shrink-0 text-muted-foreground transition-transform", showMoreOptions && "rotate-180")} />
                </button>
              </CollapsibleTrigger>

              <CollapsibleContent className="space-y-6 pt-5">
                <OptionGroup title={t("addTorrentDialog.options.saveLocation")}>
                  <form.Field name="autoTMM">
                    {(field) => (
                      <OptionSwitch
                        id="autoTMM"
                        label={t("addTorrentDialog.options.autoTmm")}
                        status={field.state.value ? t("addTorrentDialog.options.autoTmmStatus") : undefined}
                        checked={field.state.value}
                        onCheckedChange={field.handleChange}
                      />
                    )}
                  </form.Field>

                  <form.Subscribe selector={(state) => state.values.autoTMM}>
                    {(autoTMM) => !autoTMM && (
                      <>
                        <form.Field name="savePath">
                          {(field) => (
                            <div className="space-y-1.5">
                              <Label htmlFor="savePath">{t("addTorrentDialog.options.savePath")}</Label>
                              <Input
                                id="savePath"
                                ref={supportsPathAutocomplete ? savePathInputRef : undefined}
                                placeholder={preferenceDefaults.savePath || t("addTorrentDialog.options.savePathPlaceholder")}
                                autoComplete="off"
                                autoCapitalize="off"
                                autoCorrect="off"
                                spellCheck={false}
                                value={field.state.value}
                                onBlur={field.handleBlur}
                                onKeyDown={supportsPathAutocomplete ? handleSaveKeyDown : undefined}
                                onChange={(e) => {
                                  field.handleChange(e.target.value)
                                  if (supportsPathAutocomplete) {
                                    handleSaveInputChange(e.target.value)
                                  }
                                }}
                              />
                              {supportsPathAutocomplete && showSaveSuggestions && (
                                <PathSuggestions
                                  listRef={savePathListRef}
                                  suggestions={saveSuggestions}
                                  highlightedIndex={saveHighlightedIndex}
                                  onSelect={handleSaveInputSelect}
                                />
                              )}
                            </div>
                          )}
                        </form.Field>

                        {supportsTorrentTmpPath && (
                          <>
                            <form.Field name="tempPathEnabled">
                              {(field) => (
                                <OptionSwitch
                                  id="tempPathEnabled"
                                  label={t("addTorrentDialog.options.useTemporaryPath")}
                                  help={t("addTorrentDialog.options.useTemporaryPathDescription")}
                                  checked={field.state.value}
                                  onCheckedChange={field.handleChange}
                                />
                              )}
                            </form.Field>

                            <form.Subscribe selector={(state) => state.values.tempPathEnabled}>
                              {(tempPathEnabled) => tempPathEnabled && (
                                <form.Field name="tempPath">
                                  {(field) => (
                                    <div className="space-y-1.5">
                                      <Label htmlFor="tempPath">{t("addTorrentDialog.options.tempPath")}</Label>
                                      <Input
                                        id="tempPath"
                                        ref={supportsPathAutocomplete ? tempPathInputRef : undefined}
                                        placeholder={preferenceDefaults.tempPath || t("addTorrentDialog.options.tempPathPlaceholder")}
                                        autoComplete="off"
                                        autoCapitalize="off"
                                        autoCorrect="off"
                                        spellCheck={false}
                                        value={field.state.value}
                                        onBlur={field.handleBlur}
                                        onKeyDown={supportsPathAutocomplete ? handleTempKeyDown : undefined}
                                        onChange={(e) => {
                                          field.handleChange(e.target.value)
                                          if (supportsPathAutocomplete) {
                                            handleTempInputChange(e.target.value)
                                          }
                                        }}
                                      />
                                      {supportsPathAutocomplete && showTempSuggestions && (
                                        <PathSuggestions
                                          listRef={tempPathListRef}
                                          suggestions={tempSuggestions}
                                          highlightedIndex={tempHighlightedIndex}
                                          onSelect={handleTempInputSelect}
                                        />
                                      )}
                                    </div>
                                  )}
                                </form.Field>
                              )}
                            </form.Subscribe>
                          </>
                        )}
                      </>
                    )}
                  </form.Subscribe>

                  <form.Subscribe selector={(state) => [state.values.autoTMM, state.values.savePath, state.values.category] as const}>
                    {([autoTMM, savePath, category]) => (
                      <DestinationFreeSpace
                        instanceId={instanceId}
                        path={resolveDestinationPath({
                          autoTMM,
                          savePath,
                          category,
                          categories,
                          defaultSavePath: preferenceDefaults.savePath,
                          parentCategoryPaths,
                        })}
                      />
                    )}
                  </form.Subscribe>

                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <form.Field name="contentLayout">
                      {(field) => (
                        <div className="space-y-1.5">
                          <Label htmlFor="contentLayout">{t("addTorrentDialog.options.contentLayout")}</Label>
                          <Select value={field.state.value} onValueChange={field.handleChange}>
                            <SelectTrigger id="contentLayout" className="w-full">
                              <SelectValue placeholder={t("addTorrentDialog.options.useGlobalSetting")} />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="__global__">{t("addTorrentDialog.options.useGlobalSetting")}</SelectItem>
                              <SelectItem value="Original">{t("addTorrentDialog.options.original")}</SelectItem>
                              <SelectItem value="Subfolder">{t("addTorrentDialog.options.createSubfolder")}</SelectItem>
                              <SelectItem value="NoSubfolder">{t("addTorrentDialog.options.noSubfolder")}</SelectItem>
                            </SelectContent>
                          </Select>
                        </div>
                      )}
                    </form.Field>

                    <form.Field name="rename">
                      {(field) => (
                        <div className="space-y-1.5">
                          <Label htmlFor="rename">{t("addTorrentDialog.options.renameTorrent")}</Label>
                          <Input
                            id="rename"
                            placeholder={t("addTorrentDialog.options.renamePlaceholder")}
                            value={field.state.value}
                            onChange={(e) => field.handleChange(e.target.value)}
                          />
                        </div>
                      )}
                    </form.Field>
                  </div>
                </OptionGroup>

                <OptionGroup title={t("addTorrentDialog.options.downloadBehavior")}>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <form.Field name="sequentialDownload">
                      {(field) => (
                        <OptionSwitch
                          id="sequentialDownload"
                          label={t("addTorrentDialog.options.sequentialDownload")}
                          help={t("addTorrentDialog.options.sequentialDownloadDescription")}
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                        />
                      )}
                    </form.Field>
                    <form.Field name="firstLastPiecePrio">
                      {(field) => (
                        <OptionSwitch
                          id="firstLastPiecePrio"
                          label={t("addTorrentDialog.options.firstLastPiecePriority")}
                          help={t("addTorrentDialog.options.firstLastPiecePriorityDescription")}
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                        />
                      )}
                    </form.Field>
                  </div>
                </OptionGroup>

                <OptionGroup title={t("addTorrentDialog.options.limits")}>
                  <div className="grid grid-cols-2 gap-3">
                    <form.Field name="limitDownloadSpeed">
                      {(field) => (
                        <NumberOption
                          id="limitDownloadSpeed"
                          label={t("addTorrentDialog.options.downloadLimit")}
                          placeholder={t("addTorrentDialog.options.unlimitedPlaceholder")}
                          value={field.state.value}
                          onChange={field.handleChange}
                        />
                      )}
                    </form.Field>
                    <form.Field name="limitUploadSpeed">
                      {(field) => (
                        <NumberOption
                          id="limitUploadSpeed"
                          label={t("addTorrentDialog.options.uploadLimit")}
                          placeholder={t("addTorrentDialog.options.unlimitedPlaceholder")}
                          value={field.state.value}
                          onChange={field.handleChange}
                        />
                      )}
                    </form.Field>
                    <form.Field name="limitRatio">
                      {(field) => (
                        <NumberOption
                          id="limitRatio"
                          decimal
                          label={t("addTorrentDialog.options.ratioLimit")}
                          placeholder={t("addTorrentDialog.options.useGlobalPlaceholder")}
                          value={field.state.value}
                          onChange={field.handleChange}
                        />
                      )}
                    </form.Field>
                    <form.Field name="limitSeedTime">
                      {(field) => (
                        <NumberOption
                          id="limitSeedTime"
                          label={t("addTorrentDialog.options.seedTimeLimit")}
                          placeholder={t("addTorrentDialog.options.useGlobalPlaceholder")}
                          value={field.state.value}
                          onChange={field.handleChange}
                        />
                      )}
                    </form.Field>
                  </div>
                </OptionGroup>
              </CollapsibleContent>
            </Collapsible>

            {addTorrentPreferences.autoAddTrackers || addTorrentPreferences.excludedFileNamesEnabled ? (
              <div className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
                <p className="font-medium mb-0.5">{t("addTorrentDialog.options.autoAppliedTitle")}</p>
                {addTorrentPreferences.autoAddTrackers && (
                  <div>{t("addTorrentDialog.options.autoAddTrackers")}</div>
                )}
                {addTorrentPreferences.excludedFileNamesEnabled && addTorrentPreferences.excludedFileNames && (
                  <div className="break-words">{t("addTorrentDialog.options.fileExclusions", { value: addTorrentPreferences.excludedFileNames })}</div>
                )}
              </div>
            ) : null}
          </form>
        </div>

        <div className="flex-shrink-0 px-5 sm:px-6 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] border-t bg-background">
          <div className="flex gap-2 sm:justify-end">
            <Button
              type="button"
              variant="outline"
              className="h-11 sm:h-10 px-5"
              onClick={() => setOpen(false)}
            >
              {t("addTorrentDialog.footer.cancel")}
            </Button>
            <form.Subscribe
              selector={(state) => ({
                canSubmit: state.canSubmit,
                isSubmitting: state.isSubmitting,
                torrentFiles: state.values.torrentFiles,
                urls: state.values.urls,
              })}
            >
              {({ canSubmit, isSubmitting, torrentFiles, urls }) => {
                const count = activeTab === "file"? (Array.isArray(torrentFiles) ? torrentFiles.length : 0): urls.split("\n").filter(u => u.trim()).length
                const isPending = isSubmitting || mutation.isPending
                const isDisabled = !canSubmit || isPending || count === 0
                return (
                  <Button
                    type="button"
                    disabled={isDisabled}
                    className="h-11 sm:h-10 flex-1 sm:flex-none sm:min-w-36"
                    onClick={() => form.handleSubmit()}
                  >
                    {isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                    {isPending? t("addTorrentDialog.footer.adding"): count > 1? t("addTorrentDialog.footer.addCount", { count }): t("addTorrentDialog.footer.add")}
                  </Button>
                )
              }}
            </form.Subscribe>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function OptionGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</h3>
      {children}
    </div>
  )
}

function OptionSwitch({ id, label, help, status, checked, onCheckedChange }: {
  id: string
  label: string
  help?: string
  status?: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div
      className="flex cursor-pointer items-center justify-between gap-3 rounded-md border px-3 py-2.5"
      onClick={(e) => {
        const target = e.target as HTMLElement
        // The label, the switch, its hidden form input, and the help button handle their own clicks.
        // The help tooltip renders in a portal, so React bubbles its clicks here although they land outside the row.
        if (e.currentTarget.contains(target) && !target.closest("button, label, input")) onCheckedChange(!checked)
      }}
    >
      <div className="min-w-0 space-y-0.5">
        <div className="flex items-center gap-1.5">
          <Label htmlFor={id} className="cursor-pointer leading-snug">{label}</Label>
          {help && <FieldHelp>{help}</FieldHelp>}
        </div>
        {status && <p className="text-xs text-muted-foreground">{status}</p>}
      </div>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}

function NumberOption({ id, label, placeholder, value, onChange, decimal = false }: {
  id: string
  label: string
  placeholder: string
  value: number
  onChange: (value: number) => void
  decimal?: boolean
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-xs sm:text-sm">{label}</Label>
      <Input
        id={id}
        type="number"
        min="0"
        step={decimal ? "0.1" : undefined}
        inputMode={decimal ? "decimal" : "numeric"}
        placeholder={placeholder}
        value={value || ""}
        onChange={(e) => onChange((decimal ? parseFloat(e.target.value) : parseInt(e.target.value)) || 0)}
      />
    </div>
  )
}

function PathSuggestions({ listRef, suggestions, highlightedIndex, onSelect }: {
  listRef: React.Ref<HTMLDivElement>
  suggestions: string[]
  highlightedIndex: number
  onSelect: (entry: string) => void
}) {
  if (suggestions.length === 0) return null
  return (
    <div className="relative">
      <div ref={listRef} className="absolute z-50 mt-1 left-0 right-0 rounded-md border bg-popover text-popover-foreground shadow-md">
        <div className="max-h-55 overflow-y-auto py-1">
          {suggestions.map((entry, idx) => (
            <button
              key={entry}
              type="button"
              title={entry}
              className={cn(
                "w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground",
                highlightedIndex === idx ? "bg-accent text-accent-foreground" : "hover:bg-accent/70"
              )}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => onSelect(entry)}
            >
              <span className="block truncate text-left">{entry}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}

function countChangedOptions(
  values: FormData,
  defaults: { autoTMM: boolean; savePath: string; contentLayout: string; tempPathEnabled: boolean; tempPath: string },
  supportsTempPath: boolean
): number {
  const manual = !values.autoTMM
  return [
    values.autoTMM !== defaults.autoTMM,
    manual && values.savePath !== defaults.savePath,
    manual && supportsTempPath && (values.tempPathEnabled !== defaults.tempPathEnabled || (values.tempPathEnabled && values.tempPath !== defaults.tempPath)),
    values.contentLayout !== defaults.contentLayout && !(values.contentLayout === "__global__" && defaults.contentLayout === ""),
    values.rename !== "",
    values.sequentialDownload,
    values.firstLastPiecePrio,
    values.limitDownloadSpeed > 0,
    values.limitUploadSpeed > 0,
    values.limitRatio > 0,
    values.limitSeedTime > 0,
  ].filter(Boolean).length
}
