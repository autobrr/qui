/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
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
  TabsContent,
  TabsList,
  TabsTrigger
} from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities.ts"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { usePersistedStartPaused } from "@/hooks/usePersistedStartPaused"
import { api } from "@/lib/api"
import { cn } from '@/lib/utils'
import type { AddTorrentResponse, Torrent } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Link, Loader2, Plus, Upload, X } from "lucide-react"
import parseTorrent from "parse-torrent"
import { useCallback, useEffect, useRef, useState } from "react"
import { useDropzone } from "react-dropzone"
import { toast } from "sonner"

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
  const timeoutId = window.setTimeout(() => {
  }, 10000) // 10 second timeout

  try {
    const arrayBuffer = await file.arrayBuffer()
    const parsed = await parseTorrent(new Uint8Array(arrayBuffer))
    const parsedTorrent = parsed as parseTorrent.Instance & { infoHashV2?: string }

    if (!parsedTorrent) {
      return null
    }

    const hash = parsedTorrent.infoHash || parsedTorrent.infoHashV2

    if (!hash) {
      return null
    }

    const normalized = hash.toLowerCase()
    return normalized
  } catch {
    return null
  } finally {
    window.clearTimeout(timeoutId)
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
  const [internalOpen, setInternalOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<TabValue>("file")
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [newTag, setNewTag] = useState("")
  const [showFileList, setShowFileList] = useState(false)
  const [categorySearch, setCategorySearch] = useState("")
  const [tagSearch, setTagSearch] = useState("")
  const [duplicateSummary, setDuplicateSummary] = useState<DuplicateSummary>(() => createEmptyDuplicateSummary())
  const [duplicateCheckStatus, setDuplicateCheckStatus] = useState<"idle" | "pending" | "visible">("idle")
  const fileInputRef = useRef<HTMLInputElement>(null)
  const duplicateCheckRequestRef = useRef(0)
  const duplicateCheckIndicatorTimeoutRef = useRef<number | null>(null)
  const queryClient = useQueryClient()
  // NOTE: Use localStorage-persisted preference instead of qBittorrent's preference
  // This works around qBittorrent API not supporting start_paused_enabled setting
  const [startPausedEnabled] = usePersistedStartPaused(instanceId, false)

  // Use controlled state if provided, otherwise use internal state
  const open = controlledOpen !== undefined ? controlledOpen : internalOpen
  const setOpen = onOpenChange || setInternalOpen

  // Fetch metadata (categories, tags, preferences) with single API call
  const { data: metadata } = useInstanceMetadata(instanceId)
  const categories = metadata?.categories
  const availableTags = metadata?.tags
  const preferences = metadata?.preferences

  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const supportsTorrentTmpPath = capabilities?.supportsTorrentTmpPath ?? false
  const supportsPathAutocomplete = capabilities?.supportsPathAutocomplete ?? false

  // Reset tag state when dialog closes
  useEffect(() => {
    if (!open) {
      setSelectedTags([])
      setNewTag("")
      setTagSearch("")
      setShowFileList(false)
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
    setDuplicateSummary(createEmptyDuplicateSummary())
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
          "Existing torrent"
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
  }, [instanceId, torrents])


  // Combine API tags with temporarily added new tags and sort alphabetically
  const allAvailableTags = [...(availableTags || []), ...selectedTags.filter(tag => !availableTags?.includes(tag))].sort()

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
          !autoTMM && (data.tempPathEnabled !== (preferences?.temp_path_enabled ?? false) ||
        (data.tempPathEnabled && data.tempPath !== (preferences?.temp_path || "")))

      const submitData: Parameters<typeof api.addTorrent>[1] = {
        startPaused: data.startPaused,
        savePath: !autoTMM && data.savePath ? data.savePath : undefined,
        useDownloadPath: tempPathChanged ? data.tempPathEnabled : undefined,
        downloadPath: tempPathChanged && data.tempPathEnabled ? data.tempPath : undefined,
        autoTMM: autoTMM,
        category: data.category === "__none__" ? undefined : data.category || undefined,
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
      let description = "Please verify the torrent file or URL and try again."
      if (error instanceof Error && error.message && !error.message.startsWith("HTTP error! status:")) {
        description = error.message
      }

      toast.error("Failed to add torrent", {
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
        toast.success(response.added === 1
          ? "Torrent added successfully"
          : `${response.added} torrents added successfully`)
      } else if (response.added === 0) {
        // All failed
        const failedDetails = [
          ...(response.failedURLs?.map(f => `${f.url}: ${f.error}`) ?? []),
          ...(response.failedFiles?.map(f => `${f.filename}: ${f.error}`) ?? [])
        ]
        toast.error(`Failed to add ${response.failed} torrent(s)`, {
          description: failedDetails.length > 0 ? failedDetails.slice(0, 3).join("\n") : undefined,
          duration: 5000,
        })
      } else {
        // Partial success
        const failedDetails = [
          ...(response.failedURLs?.map(f => `${f.url}: ${f.error}`) ?? []),
          ...(response.failedFiles?.map(f => `${f.filename}: ${f.error}`) ?? [])
        ]
        toast.warning(`Added ${response.added}, failed ${response.failed}`, {
          description: failedDetails.length > 0 ? failedDetails.slice(0, 3).join("\n") : undefined,
          duration: 5000,
        })
      }

      setOpen(false)
      form.reset()
      setSelectedTags([])
      setNewTag("")
      setTagSearch("")
    },
  })

  const form = useForm({
    defaultValues: {
      torrentFiles: null as File[] | null,
      urls: "",
      category: "",
      tags: [] as string[],
      startPaused: startPausedEnabled,
      autoTMM: preferences?.auto_tmm_enabled ?? true,
      savePath: preferences?.save_path || "",
      skipHashCheck: false,
      sequentialDownload: false,
      firstLastPiecePrio: false,
      limitUploadSpeed: 0,
      limitDownloadSpeed: 0,
      limitRatio: 0,
      limitSeedTime: 0,
      contentLayout: preferences?.torrent_content_layout || "",
      rename: "",
      tempPathEnabled: preferences?.temp_path_enabled ?? false,
      tempPath: preferences?.temp_path || "",
      indexerId: undefined as number | undefined,
    },
    onSubmit: async ({ value }) => {
      // Use the currently selected tags
      const allTags = [...selectedTags]
      await mutation.mutateAsync({ ...value, tags: allTags })
    },
  })

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
    inputRef: savePathInputRef
  } = usePathAutocomplete(setSavePath, instanceId);

  const {
    suggestions: tempSuggestions,
    handleInputChange: handleTempInputChange,
    handleSelect: handleTempInputSelect,
    handleKeyDown: handleTempKeyDown,
    highlightedIndex: tempHighlightedIndex,
    showSuggestions: showTempSuggestions,
    inputRef: tempPathInputRef
  } = usePathAutocomplete(setTempPath, instanceId);

  const onDrop = useCallback((acceptedFiles: File[]) => {
    // Filter to .torrent files only (iOS Safari may bypass accept attribute filtering)
    const torrentFiles = acceptedFiles.filter(f => f.name.toLowerCase().endsWith('.torrent'))
    const rejectedCount = acceptedFiles.length - torrentFiles.length

    if (rejectedCount > 0) {
      toast.error(
        rejectedCount === 1
          ? "1 file rejected (not a .torrent file)"
          : `${rejectedCount} files rejected (not .torrent files)`
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
      checkForDuplicates(allFiles, "")
    }
  }, [form, checkForDuplicates])

  const { getRootProps, getInputProps, isDragActive } = useDropzone({
    onDrop,
    accept: {
      // Multiple MIME types for better iOS compatibility
      // iOS Safari has bugs with accept attribute filtering
      'application/x-bittorrent': ['.torrent'],
      'application/octet-stream': ['.torrent'],  // Fallback for browsers that report torrent as generic binary
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
      setShowFileList(false)
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
      setShowFileList(false)
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
      setShowFileList(files.length > 0)
      if (fileInputRef.current) {
        fileInputRef.current.value = ""
      }
      // Check for duplicates when files are dropped
      checkForDuplicates(files, "")
    } else if (dropPayload.type === "url") {
      const urls = dropPayload.urls.map((url) => url.trim()).filter(Boolean)
      setActiveTab("url")
      setShowFileList(false)
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

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {controlledOpen === undefined && (
        <DialogTrigger asChild>
          <Button>
            <Plus className="mr-2 h-4 w-4 transition-transform duration-200" />
            Add Torrent
          </Button>
        </DialogTrigger>
      )}
      <DialogContent className="flex flex-col w-full max-w-[95vw] sm:max-w-lg md:max-w-xl lg:max-w-2xl max-h-[90vh] sm:max-h-[85vh] p-0 !translate-y-0 !top-[5vh] sm:!top-[7.5vh]">
        <DialogHeader className="px-6 pt-6 pb-4 flex-shrink-0">
          <DialogTitle>Add New Torrent</DialogTitle>
          <DialogDescription>
            Add a torrent file or magnet link to start downloading
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto px-6">
          <form
            onSubmit={(e) => {
              e.preventDefault()
              form.handleSubmit()
            }}
            className="space-y-4 pb-2"
          >
            {/* Tab selection */}
            <div className="flex rounded-md bg-muted p-1">
              <button
                type="button"
                onClick={() => setActiveTab("file")}
                className={cn(
                  "flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors flex items-center justify-center",
                  activeTab === "file" ? "bg-accent text-accent-foreground shadow-sm" : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                )}
              >
                <Upload className="mr-2 h-4 w-4" />
                File
              </button>
              <button
                type="button"
                onClick={() => setActiveTab("url")}
                className={cn(
                  "flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors flex items-center justify-center",
                  activeTab === "url" ? "bg-accent text-accent-foreground shadow-sm" : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                )}
              >
                <Link className="mr-2 h-4 w-4" />
                URL
              </button>
            </div>

            {showDuplicateCheckIndicator && (
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                Checking for duplicates…
              </div>
            )}

            {duplicateSelectionCount > 0 && (
              <div className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-border bg-muted/60 px-3 py-2">
                <div className="flex flex-col gap-1 text-sm">
                  <span className="flex items-center gap-2 font-medium text-yellow-500">
                    <AlertCircle className="h-4 w-4" />
                    {duplicateSelectionCount} duplicate selection{duplicateSelectionCount > 1 ? "s" : ""} detected
                  </span>
                  {duplicatePreviewNames.length > 0 ? (
                    <span className="text-xs text-muted-foreground">
                      Existing torrents: {duplicatePreviewNames.join(", ")}
                      {duplicatePreviewRemaining > 0 && ` (+${duplicatePreviewRemaining} more)`}
                    </span>
                  ) : (
                    <span className="text-xs text-muted-foreground">
                      Duplicate selections are highlighted below.
                    </span>
                  )}
                </div>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="text-yellow-600 border-yellow-600/40 hover:bg-yellow-600/10 hover:text-yellow-700"
                  onClick={handleRemoveDuplicateSelections}
                >
                  Remove duplicates
                </Button>
              </div>
            )}

            {/* Main Content Tabs */}
            <Tabs defaultValue="basic" className="w-full">
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="basic">Basic</TabsTrigger>
                <TabsTrigger value="advanced">Advanced</TabsTrigger>
              </TabsList>

              <TabsContent value="basic" className="space-y-4 mt-4">
                {/* File upload or URL input */}
                {activeTab === "file" ? (
                  <form.Field
                    name="torrentFiles"
                    validators={{
                      onChange: ({ value }) => {
                        if ((!value || value.length === 0) && activeTab === "file") {
                          return "Please select at least one torrent file"
                        }
                        return undefined
                      },
                    }}
                  >
                    {(field) => (
                      <div className="space-y-2">
                        <Label htmlFor="torrentFiles">Torrent Files</Label>
                        <div
                          {...getRootProps({
                            className: cn(
                              "mt-2 border border-dashed rounded-md p-6 cursor-pointer transition-colors backdrop-blur-md",
                              "data-[drag-active]:border-primary data-[drag-active]:bg-background/10",
                              "border-border hover:border-primary/30 hover:bg-accent/30"
                            )
                          })}
                          data-drag-active={isDragActive ? "" : undefined}
                        >
                          <input {...getInputProps({ id: "torrentFiles" })} />
                          <div className="flex flex-col items-center justify-center text-center space-y-2 h-22">
                            <Upload className="h-8 w-8 text-muted-foreground" />
                            {isDragActive ? (
                              <p className="text-sm font-medium">Drop the torrent files here...</p>
                            ) : (
                              <>
                                <p className="text-sm font-medium">Drag & drop torrent files here</p>
                                <p className="text-xs text-muted-foreground">or click to browse</p>
                              </>
                            )}
                          </div>
                        </div>
                        {field.state.value && field.state.value.length > 0 && (
                          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                            <span>
                              {field.state.value.length} file{field.state.value.length > 1 ? "s" : ""} selected
                            </span>
                            {duplicateFileKeys.length > 0 && (
                              <span className="flex items-center gap-1 text-xs font-medium text-yellow-500">
                                <AlertCircle className="h-3 w-3" />
                                {duplicateFileKeys.length} duplicate file{duplicateFileKeys.length > 1 ? "s" : ""}
                              </span>
                            )}
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <button
                                  type="button"
                                  className="text-xs underline hover:text-foreground"
                                  onClick={() => setShowFileList(!showFileList)}
                                >
                                  {showFileList ? "Hide" : "Show"} files
                                </button>
                              </TooltipTrigger>
                              <TooltipContent>
                                <div className="max-w-xs">
                                  {field.state.value.slice(0, 3).map((file, index) => {
                                    const fileKey = createFileKey(file)
                                    const duplicateInfo = duplicateFileEntries[fileKey]
                                    return (
                                      <div
                                        key={`${fileKey}-${index}`}
                                        className={`text-xs truncate ${duplicateInfo ? "text-yellow-500" : ""}`}
                                      >
                                        • {file.name}
                                      </div>
                                    )
                                  })}
                                  {field.state.value.length > 3 && (
                                    <div className="text-xs">... and {field.state.value.length - 3} more</div>
                                  )}
                                </div>
                              </TooltipContent>
                            </Tooltip>
                          </div>
                        )}
                        {showFileList && field.state.value && field.state.value.length > 0 && (
                          <div className="max-h-24 overflow-y-auto border rounded-md p-2">
                            <div className="space-y-1 text-xs">
                              {field.state.value.map((file, index) => {
                                const fileKey = createFileKey(file)
                                const duplicateInfo = duplicateFileEntries[fileKey]
                                const isDuplicate = Boolean(duplicateInfo)
                                return (
                                  <div
                                    key={`${fileKey}-${index}`}
                                    className={`flex items-start gap-2 rounded-sm px-2 py-1 ${isDuplicate ? "bg-yellow-500/10 text-yellow-600" : "text-muted-foreground"}`}
                                  >
                                    <span className="select-none leading-5">•</span>
                                    <div className="flex-1 break-all">
                                      <span>{file.name}</span>
                                      {isDuplicate && duplicateInfo?.matches.length ? (
                                        <span className="block text-[11px] text-yellow-700">
                                          Matches existing: {duplicateInfo.matches.slice(0, 2).join(", ")}
                                          {duplicateInfo.matches.length > 2 && ` (+${duplicateInfo.matches.length - 2} more)`}
                                        </span>
                                      ) : null}
                                    </div>
                                    <button
                                      type="button"
                                      onClick={() => handleRemoveFile(index)}
                                      className="shrink-0 h-5 w-5 rounded-sm hover:bg-destructive/10 hover:text-destructive flex items-center justify-center transition-colors"
                                      title="Remove file"
                                    >
                                      <X className="h-3 w-3" />
                                    </button>
                                  </div>
                                )
                              })}
                            </div>
                          </div>
                        )}
                        {field.state.meta.isTouched && field.state.meta.errors[0] && (
                          <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                        )}
                      </div>
                    )}
                  </form.Field>
                ) : (
                  <form.Field
                    name="urls"
                    validators={{
                      onChange: ({ value }) => {
                        if (!value && activeTab === "url") {
                          return "Please enter at least one URL or magnet link"
                        }
                        return undefined
                      },
                    }}
                  >
                    {(field) => (
                      <div className="space-y-2">
                        <Label htmlFor="urls">URLs / Magnet Links</Label>
                        <Textarea
                          id="urls"
                          placeholder="Enter URLs or magnet links (one per line)"
                          rows={4}
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(e) => {
                            field.handleChange(e.target.value)
                            // Check for duplicates when URLs are entered
                            checkForDuplicates(null, e.target.value)
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
                                <div key={urlKey} className="text-yellow-600 space-y-1">
                                  <div className="font-medium truncate">{duplicateInfo.label}</div>
                                  {duplicateInfo.matches.length > 0 && (
                                    <div className="text-yellow-700 text-[11px]">
                                      Matches existing: {duplicateInfo.matches.slice(0, 2).join(", ")}
                                      {duplicateInfo.matches.length > 2 && ` (+${duplicateInfo.matches.length - 2} more)`}
                                    </div>
                                  )}
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

                {/* Basic Toggles */}
                <div className="flex items-center justify-center gap-8">
                  <form.Field name="startPaused">
                    {(field) => (
                      <div className="flex items-center space-x-2">
                        <Switch
                          id="startPaused-left"
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                        />
                        <Label htmlFor="startPaused-left">Start paused</Label>
                      </div>
                    )}
                  </form.Field>

                  <div className="w-px h-6 bg-border" />

                  <form.Field name="skipHashCheck">
                    {(field) => (
                      <div className="flex items-center space-x-2">
                        <Switch
                          id="skipHashCheck-left"
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                        />
                        <Label htmlFor="skipHashCheck-left">Skip hash check</Label>
                      </div>
                    )}
                  </form.Field>
                </div>

                {/* Category */}
                <div className="space-y-3">
                  <form.Field name="category">
                    {(field) => (
                      <>
                        {/* Header with search */}
                        <div className="flex items-center gap-2 w-full">
                          <Label className="shrink-0">Category</Label>
                          <Input
                            id="categorySearch"
                            value={categorySearch}
                            onChange={(e) => setCategorySearch(e.target.value)}
                            placeholder="Search categories..."
                            className="h-8 text-sm flex-1 min-w-0"
                            onKeyDown={(e) => {
                              if (e.key === "Enter" && categorySearch.trim()) {
                                e.preventDefault()
                                // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                const filtered = Object.entries(categories || {}).filter(([_key, cat]) =>
                                  cat.name.toLowerCase().includes(categorySearch.toLowerCase())
                                )

                                // If there's exactly one filtered category, select it
                                if (filtered.length === 1) {
                                  field.handleChange(filtered[0][1].name)
                                  setCategorySearch("")
                                }
                              }
                              if (e.key === "Escape") {
                                setCategorySearch("")
                              }
                            }}
                          />
                        </div>

                        {/* Available categories */}
                        {categories && Object.entries(categories).length > 0 && (
                          <div className="space-y-2">
                            <Label className="text-xs text-muted-foreground">
                              Available Categories (click to select) {categorySearch && `- filtering: "${categorySearch}"`}
                            </Label>
                            <div className="flex flex-wrap gap-1.5 max-h-20 overflow-y-auto">
                              {[
                                // Selected category first (if it matches search)
                                ...(field.state.value && field.state.value !== "__none__" &&
                                  (categorySearch === "" || field.state.value.toLowerCase().includes(categorySearch.toLowerCase())) ? [{ name: field.state.value, isSelected: true }] : []),
                                // Then unselected categories
                                ...Object.entries(categories)
                                  // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                  .filter(([_key, cat]) => cat.name !== field.state.value)
                                  // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                  .filter(([_key, cat]) => categorySearch === "" || cat.name.toLowerCase().includes(categorySearch.toLowerCase()))
                                  // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                  .map(([_key, cat]) => ({ name: cat.name, isSelected: false })),
                              ].map((cat) => (
                                <Badge
                                  key={cat.name}
                                  variant={field.state.value === cat.name ? "secondary" : "outline"}
                                  className="text-xs py-0.5 px-2 cursor-pointer hover:bg-accent"
                                  onClick={() => field.handleChange(field.state.value === cat.name ? "__none__" : cat.name)}
                                >
                                  {cat.name}
                                </Badge>
                              ))}
                            </div>
                            {/* eslint-disable-next-line @typescript-eslint/no-unused-vars */}
                            {categorySearch && Object.entries(categories).filter(([_key, cat]) => cat.name.toLowerCase().includes(categorySearch.toLowerCase())).length === 0 && (
                              <p className="text-xs text-muted-foreground">No categories match "{categorySearch}"</p>
                            )}
                          </div>
                        )}
                      </>
                    )}
                  </form.Field>
                </div>

                {/* Tags */}
                <div className="space-y-3 pt-2">
                  <div className="flex items-center gap-2 w-full">
                    <Label className="shrink-0">Tags</Label>
                    <Input
                      id="newTag"
                      value={newTag}
                      onChange={(e) => {
                        const value = e.target.value
                        setNewTag(value)
                        setTagSearch(value) // Update search filter
                      }}
                      placeholder="Create new tag or search available tags..."
                      className="h-8 text-sm flex-1 min-w-0"
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && newTag.trim()) {
                          e.preventDefault()
                          const filteredAvailable = allAvailableTags?.filter(tag =>
                            !selectedTags.includes(tag) &&
                            tag.toLowerCase().includes(newTag.toLowerCase())
                          ) || []

                          // If there's exactly one filtered tag, add it
                          if (filteredAvailable.length === 1) {
                            setSelectedTags([...selectedTags, filteredAvailable[0]])
                            setNewTag("")
                            setTagSearch("")
                          }
                          // Otherwise, create new tag
                          else if (!selectedTags.includes(newTag.trim())) {
                            setSelectedTags([...selectedTags, newTag.trim()])
                            setNewTag("")
                            setTagSearch("")
                          }
                        }
                        if (e.key === "Escape") {
                          setNewTag("")
                          setTagSearch("")
                        }
                      }}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        if (newTag.trim() && !selectedTags.includes(newTag.trim())) {
                          setSelectedTags([...selectedTags, newTag.trim()])
                          setNewTag("")
                          setTagSearch("")
                        }
                      }}
                      disabled={!newTag.trim() || selectedTags.includes(newTag.trim())}
                      className="h-8 px-2"
                    >
                      <Plus className="h-3 w-3" />
                    </Button>
                    {selectedTags.length > 0 && (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() => setSelectedTags([])}
                        className="h-8 px-2 text-xs"
                      >
                        Clear All
                      </Button>
                    )}
                  </div>

                  {/* Available tags */}
                  {allAvailableTags && allAvailableTags.length > 0 && (
                    <div className="space-y-2">
                      <Label className="text-xs text-muted-foreground">
                        Available Tags (click to select/deselect) {tagSearch && `- filtering: "${tagSearch}"`}
                      </Label>
                      <div className="flex flex-wrap gap-1.5 max-h-20 overflow-y-auto">
                        {[...selectedTags.filter(tag => tagSearch === "" || tag.toLowerCase().includes(tagSearch.toLowerCase())),
                        ...allAvailableTags
                          .filter(tag => !selectedTags.includes(tag))
                          .filter(tag => tagSearch === "" || tag.toLowerCase().includes(tagSearch.toLowerCase()))]
                          .map((tag) => (
                            <Badge
                              key={tag}
                              variant={selectedTags.includes(tag) ? "secondary" : "outline"}
                              className="text-xs py-0.5 px-2 cursor-pointer hover:bg-accent"
                              onClick={() => {
                                if (selectedTags.includes(tag)) {
                                  setSelectedTags(selectedTags.filter(t => t !== tag))
                                } else {
                                  setSelectedTags([...selectedTags, tag])
                                }
                              }}
                            >
                              {tag}
                              {!allAvailableTags.includes(tag) && (
                                <span className="ml-1 text-[10px] opacity-70">(new)</span>
                              )}
                            </Badge>
                          ))}
                      </div>
                      {tagSearch &&
                        [...selectedTags, ...allAvailableTags]
                          .filter(tag => tagSearch === "" || tag.toLowerCase().includes(tagSearch.toLowerCase()))
                          .length === 0 && (
                          <p className="text-xs text-muted-foreground">No tags match "{tagSearch}"</p>
                        )}
                    </div>
                  )}
                </div>
              </TabsContent>

              <TabsContent value="advanced" className="space-y-4 mt-4">

                {/* Automatic Torrent Management */}
                <form.Field name="autoTMM">
                  {(field) => (
                    <div className="flex items-center space-x-2">
                      <Switch
                        id="autoTMM"
                        checked={field.state.value}
                        onCheckedChange={field.handleChange}
                      />
                      <Label htmlFor="autoTMM">Automatic Torrent Management</Label>
                    </div>
                  )}
                </form.Field>

                {/* Save Path - show based on TMM toggle */}
                <form.Field name="autoTMM">
                  {(autoTMMField) => (
                    <>
                      {!autoTMMField.state.value ? (
                        <>
                          <form.Field name="savePath">
                            {(field) => (
                              <div className="space-y-2">
                                <Label htmlFor="savePath">Save Path</Label>
                                <Input
                                  id="savePath"
                                  ref={supportsPathAutocomplete ? savePathInputRef : undefined}
                                  placeholder={preferences?.save_path || "Leave empty for default"}
                                  autoComplete="off"
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

                                {supportsPathAutocomplete && showSaveSuggestions && saveSuggestions.length > 0 && (
                                  <div className="relative">
                                    <div className="absolute z-50 mt-1 left-0 right-0 rounded-md border bg-popover text-popover-foreground shadow-md">
                                      <div className="max-h-55 overflow-y-auto py-1">
                                        {saveSuggestions.map((entry, idx) => (
                                          <button
                                            key={entry}
                                            type="button"
                                            title={entry}
                                            className={cn(
                                              "w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground",
                                              saveHighlightedIndex === idx
                                                ? "bg-accent text-accent-foreground"
                                                : "hover:bg-accent/70",
                                            )}
                                            onMouseDown={(e) => e.preventDefault()}
                                            onClick={() => handleSaveInputSelect(entry)}
                                          >
                                            <span className="block truncate text-left">{entry}</span>
                                          </button>
                                        ))}
                                      </div>
                                    </div>
                                  </div>
                                )}

                                <p className="text-xs text-muted-foreground">
                                  Manual save path (TMM disabled)
                                </p>
                              </div>
                            )}
                          </form.Field>

                            {supportsTorrentTmpPath ? (
                                <>
                                    <form.Field name="tempPathEnabled">
                                        {(field) => (
                                            <div className="space-y-2">
                                                <div className="flex items-center gap-2">
                                                    <Switch
                                                        id="tempPathEnabled"
                                                        checked={field.state.value}
                                                        onCheckedChange={field.handleChange}
                                                    />
                                                    <Label htmlFor="tempPathEnabled" className="text-sm font-medium">Use
                                                        Temporary Path</Label>
                                                </div>
                                                <p className="text-xs text-muted-foreground">
                                                    Download to temporary path before moving to save path
                                                </p>
                                            </div>
                                        )}
                                    </form.Field>

                                    <form.Field name="tempPath">
                                      {(field) => (
                                        <form.Subscribe selector={(state) => state.values.tempPathEnabled}>
                                          {(tempPathEnabled) => {
                                            return (
                                              <div className="space-y-2 pl-4 border-l-2 border-primary border-opacity-50 data-[temp-path-enabled=true]:block hidden" data-temp-path-enabled={tempPathEnabled}>
                                                <Label htmlFor="tempPath">Temporary Download Path</Label>
                                                <Input
                                                  id="tempPath"
                                                  ref={supportsPathAutocomplete ? tempPathInputRef : undefined}
                                                  placeholder={preferences?.temp_path || "Leave empty for default"}
                                                  autoComplete="off"
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

                                                {supportsPathAutocomplete && showTempSuggestions && tempSuggestions.length > 0 && (
                                                  <div className="relative">
                                                    <div className="absolute z-50 mt-1 left-0 right-0 rounded-md border bg-popover text-popover-foreground shadow-md">
                                                      <div className="max-h-55 overflow-y-auto py-1">
                                                        {tempSuggestions.map((entry, idx) => (
                                                          <button
                                                            key={entry}
                                                            type="button"
                                                            title={entry}
                                                            className={cn(
                                                              "w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground",
                                                              tempHighlightedIndex === idx
                                                                ? "bg-accent text-accent-foreground"
                                                                : "hover:bg-accent/70",
                                                            )}
                                                            onMouseDown={(e) => e.preventDefault()}
                                                            onClick={() => handleTempInputSelect(entry)}
                                                          >
                                                            <span className="block truncate text-left">{entry}</span>
                                                          </button>
                                                        ))}
                                                      </div>
                                                    </div>
                                                  </div>
                                                )}

                                                <p className="text-xs text-muted-foreground">
                                                  Torrents will be downloaded here before moving to save path
                                                </p>
                                              </div>
                                            )
                                          }}
                                        </form.Subscribe>
                                      )}
                                    </form.Field>
                                  </>
                                ) : null}
                              </>
                            ) : (
                              <div className="space-y-2">
                                <Label>Save Path</Label>
                                <div className="px-3 py-2 bg-muted rounded-md">
                                  <p className="text-sm text-muted-foreground">
                                    Automatic Torrent Management is enabled. Save path will be determined by category settings.
                                  </p>
                                </div>
                              </div>
                            )}
                        </>
                      )}
                    </form.Field>


                {/* Advanced Options */}
                <div className="space-y-4">
                  <Label className="text-sm font-medium">Advanced Options</Label>
                  {/* Sequential Download & First/Last Piece Priority */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <form.Field name="sequentialDownload">
                      {(field) => (
                        <div className="flex items-center space-x-2">
                          <Switch
                            id="sequentialDownload"
                            checked={field.state.value}
                            onCheckedChange={field.handleChange}
                          />
                          <Label htmlFor="sequentialDownload">Sequential download</Label>
                          <span className="text-xs text-muted-foreground ml-2">
                            (useful for media files)
                          </span>
                        </div>
                      )}
                    </form.Field>

                    {/* First/Last Piece Priority */}
                    <form.Field name="firstLastPiecePrio">
                      {(field) => (
                        <div className="flex items-center space-x-2">
                          <Switch
                            id="firstLastPiecePrio"
                            checked={field.state.value}
                            onCheckedChange={field.handleChange}
                          />
                          <Label htmlFor="firstLastPiecePrio">First/last piece priority</Label>
                          <span className="text-xs text-muted-foreground ml-2">
                            (start playback faster)
                          </span>
                        </div>
                      )}
                    </form.Field>

                  </div>

                  {/* Speed Limits */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <form.Field name="limitDownloadSpeed">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitDownloadSpeed">Download limit (KB/s)</Label>
                          <Input
                            id="limitDownloadSpeed"
                            type="number"
                            min="0"
                            placeholder="0 = unlimited"
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>

                    <form.Field name="limitUploadSpeed">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitUploadSpeed">Upload limit (KB/s)</Label>
                          <Input
                            id="limitUploadSpeed"
                            type="number"
                            min="0"
                            placeholder="0 = unlimited"
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>
                  </div>

                  {/* Seeding Limits */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <form.Field name="limitRatio">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitRatio">Ratio limit</Label>
                          <Input
                            id="limitRatio"
                            type="number"
                            min="0"
                            step="0.1"
                            placeholder="0 = use global"
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseFloat(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>

                    <form.Field name="limitSeedTime">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitSeedTime">Seed time limit (minutes)</Label>
                          <Input
                            id="limitSeedTime"
                            type="number"
                            min="0"
                            placeholder="0 = use global"
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>
                  </div>

                  {/* Content Layout & Rename - available regardless of TMM */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <form.Field name="contentLayout">
                      {(field) => (
                        <div className="space-y-2">
                          <Label>Content layout</Label>
                          <Select
                            value={field.state.value}
                            onValueChange={field.handleChange}
                          >
                            <SelectTrigger id="contentLayout">
                              <SelectValue placeholder="Use global setting" />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="__global__">Use global setting</SelectItem>
                              <SelectItem value="Original">Original</SelectItem>
                              <SelectItem value="Subfolder">Create subfolder</SelectItem>
                              <SelectItem value="NoSubfolder">Don't create subfolder</SelectItem>
                            </SelectContent>
                          </Select>
                        </div>
                      )}
                    </form.Field>

                    {/* Rename Torrent */}
                    <form.Field name="rename">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="rename">Rename torrent</Label>
                          <Input
                            id="rename"
                            placeholder="Leave empty to use original name"
                            value={field.state.value}
                            onChange={(e) => field.handleChange(e.target.value)}
                          />
                        </div>
                      )}
                    </form.Field>
                  </div>
                </div>
              </TabsContent>
            </Tabs>

            {/* Auto-applied Settings Info - Compact */}
            {(preferences?.add_trackers_enabled && preferences?.add_trackers) || preferences?.excluded_file_names_enabled ? (
              <div className="bg-muted rounded-md p-3 text-xs text-muted-foreground">
                <p className="font-medium mb-1">Auto-applied:</p>
                <div className="space-y-0.5">
                  {preferences?.add_trackers_enabled && preferences?.add_trackers && (
                    <div>• Auto-add trackers</div>
                  )}
                  {preferences?.excluded_file_names_enabled && preferences?.excluded_file_names && (
                    <div>• File exclusions: {preferences.excluded_file_names}</div>
                  )}
                </div>
              </div>
            ) : null}

          </form>
        </div>

        {/* Fixed footer with submit buttons */}
        <div className="flex-shrink-0 px-6 py-3 border-t bg-background">
          <div className="flex flex-col sm:flex-row gap-3 sm:gap-2">
            <form.Subscribe
              selector={(state) => ({
                canSubmit: state.canSubmit,
                isSubmitting: state.isSubmitting,
                torrentFiles: state.values.torrentFiles,
              })}
            >
              {({ canSubmit, isSubmitting, torrentFiles }) => {
                const hasSelectedFiles = Array.isArray(torrentFiles) && torrentFiles.length > 0
                const requiresFileSelection = activeTab === "file" && !hasSelectedFiles
                const isDisabled = !canSubmit || isSubmitting || mutation.isPending || requiresFileSelection
                return (
                  <Button
                    type="submit"
                    disabled={isDisabled}
                    className="w-full sm:flex-1 h-11 sm:h-10 order-1 sm:order-2"
                    onClick={() => form.handleSubmit()}
                  >
                    {isSubmitting || mutation.isPending ? "Adding..." : "Add Torrent"}
                  </Button>
                )
              }}
            </form.Subscribe>
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto px-6 sm:px-4 h-11 sm:h-10 order-2 sm:order-1"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
