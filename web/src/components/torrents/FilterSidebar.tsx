/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger
} from "@/components/ui/accordion"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { SearchInput } from "@/components/ui/SearchInput"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"

import { useDebounce } from "@/hooks/useDebounce"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { usePersistedAccordion } from "@/hooks/usePersistedAccordion"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getLinuxCount, LINUX_CATEGORIES, LINUX_TAGS, LINUX_TRACKERS, useIncognitoMode } from "@/lib/incognito"
import { cn } from "@/lib/utils"
import type { Category, TorrentFilters } from "@/types"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  AlertCircle,
  CheckCircle2,
  Download,
  Edit,
  FolderPlus,
  Info,
  MoveRight,
  PlayCircle,
  Plus,
  RotateCw,
  StopCircle,
  Trash2,
  Upload,
  X,
  XCircle,
  type LucideIcon
} from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { CategoryTree } from "./CategoryTree"
import {
  CreateCategoryDialog,
  CreateTagDialog,
  DeleteCategoryDialog,
  DeleteEmptyCategoriesDialog,
  DeleteTagDialog,
  DeleteUnusedTagsDialog,
  EditCategoryDialog
} from "./TagCategoryManagement"
import { EditTrackerDialog } from "./TorrentDialogs"
// import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { api } from "@/lib/api"
import { useMutation } from "@tanstack/react-query"
import { toast } from "sonner"

interface FilterBadgeProps {
  count: number
  onClick: () => void
}

function FilterBadge({ count, onClick }: FilterBadgeProps) {
  return (
    <Badge
      variant="secondary"
      className="ml-2 h-5 px-1.5 text-xs cursor-pointer hover:bg-secondary/80"
      onClick={(e: React.MouseEvent) => {
        e.stopPropagation()
        onClick()
      }}
    >
      <span className="flex items-center gap-1 text-xs text-muted-foreground">
        <X className="size-3"/>
        {count}
      </span>
    </Badge>
  )
}

interface FilterSidebarProps {
  instanceId: number
  selectedFilters: TorrentFilters
  onFilterChange: (filters: TorrentFilters) => void
  torrentCounts?: Record<string, number>
  categories?: Record<string, Category>
  tags?: string[]
  useSubcategories?: boolean
  className?: string
  isStaleData?: boolean
  isLoading?: boolean
  isMobile?: boolean
}

type TriState = "include" | "exclude" | "neutral"

const LONG_PRESS_DURATION = 400

const arraysEqual = (a?: string[], b?: string[]) => {
  if (a === b) {
    return true
  }

  const aLength = a?.length ?? 0
  const bLength = b?.length ?? 0

  if (aLength !== bLength) {
    return false
  }

  if (!a || !b) {
    return aLength === bLength
  }

  for (let i = 0; i < aLength; i++) {
    if (a[i] !== b[i]) {
      return false
    }
  }

  return true
}


// Define torrent states based on qBittorrent
const TORRENT_STATES: Array<{ value: string; label: string; icon: LucideIcon }> = [
  { value: "downloading", label: "Downloading", icon: Download },
  { value: "uploading", label: "Seeding", icon: Upload },
  { value: "completed", label: "Completed", icon: CheckCircle2 },
  { value: "stopped", label: "Stopped", icon: StopCircle },
  { value: "active", label: "Active", icon: PlayCircle },
  { value: "inactive", label: "Inactive", icon: StopCircle },
  { value: "running", label: "Running", icon: PlayCircle },
  { value: "stalled", label: "Stalled", icon: AlertCircle },
  { value: "stalled_uploading", label: "Stalled Uploading", icon: AlertCircle },
  { value: "stalled_downloading", label: "Stalled Downloading", icon: AlertCircle },
  { value: "errored", label: "Error", icon: XCircle },
  { value: "checking", label: "Checking", icon: RotateCw },
  { value: "moving", label: "Moving", icon: MoveRight },
  { value: "unregistered", label: "Unregistered torrents", icon: XCircle },
  { value: "tracker_down", label: "Tracker Down", icon: AlertCircle },
]

interface TrackerIconImageProps {
  tracker: string
  trackerIcons?: Record<string, string>
}

const TrackerIconImage = memo(({ tracker, trackerIcons }: TrackerIconImageProps) => {
  const [hasError, setHasError] = useState(false)

  useEffect(() => {
    setHasError(false)
  }, [tracker, trackerIcons])

  const trimmed = tracker.trim()
  const fallbackLetter = trimmed ? trimmed.charAt(0).toUpperCase() : "#"
  const src = trackerIcons?.[trimmed] ?? null

  return (
    <div className="flex h-4 w-4 items-center justify-center rounded-sm border border-border/40 bg-muted text-[10px] font-medium uppercase leading-none">
      {src && !hasError ? (
        <img
          src={src}
          alt=""
          className="h-full w-full rounded-[2px] object-cover"
          loading="lazy"
          draggable={false}
          onError={() => setHasError(true)}
        />
      ) : (
        <span aria-hidden="true">{fallbackLetter}</span>
      )}
    </div>
  )
})

TrackerIconImage.displayName = "TrackerIconImage"

const FilterSidebarComponent = ({
  instanceId,
  selectedFilters,
  onFilterChange,
  torrentCounts = {},
  categories: propsCategories,
  tags: propsTags,
  useSubcategories = false,
  className = "",
  isStaleData = false,
  isLoading = false,
  isMobile = false,
}: FilterSidebarProps) => {
  // Use incognito mode hook
  const [incognitoMode] = useIncognitoMode()
  const { data: trackerIcons } = useTrackerIcons()
  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const supportsTrackerHealth = capabilities?.supportsTrackerHealth ?? true
  const supportsTrackerEditing = capabilities?.supportsTrackerEditing ?? true
  const supportsSubcategories = capabilities?.supportsSubcategories ?? false
  const { preferences } = useInstancePreferences(instanceId)
  const preferenceUseSubcategories = preferences?.use_subcategories
  const subcategoriesEnabled = Boolean(
    supportsSubcategories && (preferenceUseSubcategories ?? useSubcategories ?? false)
  )

  // Use compact view state hook
  const { viewMode, cycleViewMode } = usePersistedCompactViewState("compact")

  // Helper function to get count display - shows 0 when loading to prevent showing stale counts from previous instance
  const getDisplayCount = useCallback((key: string, fallbackCount?: number): string => {
    if (incognitoMode && fallbackCount !== undefined) {
      return fallbackCount.toString()
    }

    if (isLoading) {
      return "0"
    }

    if (!torrentCounts) {
      return "..."
    }

    return (torrentCounts[key] || 0).toString()
  }, [incognitoMode, isLoading, torrentCounts])

  // Persist accordion state
  const [expandedItems, setExpandedItems] = usePersistedAccordion()

  // Dialog states
  const [showCreateTagDialog, setShowCreateTagDialog] = useState(false)
  const [showDeleteTagDialog, setShowDeleteTagDialog] = useState(false)
  const [showDeleteUnusedTagsDialog, setShowDeleteUnusedTagsDialog] = useState(false)
  const [tagToDelete, setTagToDelete] = useState("")

  const [showCreateCategoryDialog, setShowCreateCategoryDialog] = useState(false)
  const [showEditCategoryDialog, setShowEditCategoryDialog] = useState(false)
  const [showDeleteCategoryDialog, setShowDeleteCategoryDialog] = useState(false)
  const [showDeleteEmptyCategoriesDialog, setShowDeleteEmptyCategoriesDialog] = useState(false)
  const [categoryToEdit, setCategoryToEdit] = useState<Category | null>(null)
  const [categoryToDelete, setCategoryToDelete] = useState("")
  const [parentCategoryForNew, setParentCategoryForNew] = useState<string | undefined>(undefined)
  const [collapsedCategories, setCollapsedCategories] = useState<Set<string>>(() => new Set())

  // Search states for filtering large lists
  const [categorySearch, setCategorySearch] = useState("")
  const [tagSearch, setTagSearch] = useState("")
  const [trackerSearch, setTrackerSearch] = useState("")

  // Tracker dialog states
  const [showEditTrackerDialog, setShowEditTrackerDialog] = useState(false)
  const [trackerToEdit, setTrackerToEdit] = useState("")
  const [trackerFullURLs, setTrackerFullURLs] = useState<string[]>([])
  const [loadingTrackerURLs, setLoadingTrackerURLs] = useState(false)

  const visibleTorrentStates = useMemo(() => {
    if (supportsTrackerHealth) {
      return TORRENT_STATES
    }
    return TORRENT_STATES.filter(state => state.value !== "unregistered" && state.value !== "tracker_down")
  }, [supportsTrackerHealth])

  // Get selected torrents from context (not used for tracker editing, but keeping for future use)
  // const { selectedHashes } = useTorrentSelection()

  // Function to fetch tracker URLs for a specific tracker domain
  const fetchTrackerURLs = useCallback(async (trackerDomain: string) => {
    setTrackerFullURLs([])

    if (!supportsTrackerHealth) {
      setLoadingTrackerURLs(false)
      return
    }

    setLoadingTrackerURLs(true)

    try {
      // Find torrents using this tracker
      const trackerFilters: TorrentFilters = {
        status: [],
        excludeStatus: [],
        categories: [],
        excludeCategories: [],
        tags: [],
        excludeTags: [],
        trackers: [trackerDomain],
        excludeTrackers: [],
        expr: "",
      }

      const torrentsList = await api.getTorrents(instanceId, {
        filters: trackerFilters,
        limit: 1, // We only need one torrent to get the tracker URL
      })

      if (torrentsList.torrents && torrentsList.torrents.length > 0) {
        // Get trackers for the first torrent
        const firstTorrentHash = torrentsList.torrents[0].hash
        const trackers = await api.getTorrentTrackers(instanceId, firstTorrentHash)

        // Find all unique tracker URLs for this domain
        const urls = trackers
          .filter((t: { url: string }) => {
            try {
              const url = new URL(t.url)
              return url.hostname === trackerDomain
            } catch {
              return false
            }
          })
          .map((t: { url: string }) => t.url)
          .filter((url: string, index: number, self: string[]) => self.indexOf(url) === index) // Remove duplicates

        setTrackerFullURLs(urls)
      }
    } catch (error) {
      console.error("Failed to fetch tracker URLs:", error)
      toast.error("Failed to fetch tracker URLs")
    } finally {
      setLoadingTrackerURLs(false)
    }
  }, [instanceId, supportsTrackerHealth])

  // Mutation for editing trackers
  const editTrackersMutation = useMutation({
    mutationFn: async ({ oldURL, newURL, tracker }: { oldURL: string; newURL: string; tracker: string }) => {
      // Use selectAll with tracker filter to update all torrents with this tracker
      await api.bulkAction(instanceId, {
        hashes: [], // Empty when using selectAll
        action: "editTrackers",
        trackerOldURL: oldURL,
        trackerNewURL: newURL,
        selectAll: true,
        filters: {
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [tracker], // Filter to only torrents with this tracker
          excludeTrackers: [],
          expr: "",
        },
      })
    },
    onSuccess: () => {
      toast.success("Updated tracker URL across all affected torrents")
      setShowEditTrackerDialog(false)
      setTrackerFullURLs([])
    },
    onError: (error: Error) => {
      toast.error("Failed to update tracker", {
        description: error.message,
      })
    },
  })

  // Debounce search terms for better performance
  const debouncedCategorySearch = useDebounce(categorySearch, 300)
  const debouncedTagSearch = useDebounce(tagSearch, 300)
  const debouncedTrackerSearch = useDebounce(trackerSearch, 300)

  // Use fake data if in incognito mode, otherwise use props
  // When loading or showing stale data, show empty data to prevent stale data from previous instance
  const categories = useMemo(() => {
    if (incognitoMode) return LINUX_CATEGORIES
    if (isLoading || isStaleData) return {}  // Clear categories during loading or when stale
    return propsCategories || {}
  }, [incognitoMode, propsCategories, isLoading, isStaleData])

  const tags = useMemo(() => {
    if (incognitoMode) return LINUX_TAGS
    if (isLoading || isStaleData) return []  // Clear tags during loading or when stale
    return propsTags || []
  }, [incognitoMode, propsTags, isLoading, isStaleData])

  const realCategoryNames = useMemo(() => new Set(Object.keys(categories)), [categories])

  const categoryEntries = useMemo(() => {
    const baseEntries = Object.entries(categories) as [string, Category][]

    if (!subcategoriesEnabled) {
      return baseEntries
    }

    const merged = new Map<string, Category>()
    for (const [name, category] of baseEntries) {
      merged.set(name, category)
    }

    const counts = torrentCounts ?? {}
    for (const key of Object.keys(counts)) {
      if (!key.startsWith("category:")) {
        continue
      }
      const categoryName = key.slice("category:".length)
      if (!categoryName || merged.has(categoryName)) {
        continue
      }
      merged.set(categoryName, { name: categoryName, savePath: "" })
    }

    return Array.from(merged.entries()).sort((a, b) => a[0].localeCompare(b[0]))
  }, [categories, torrentCounts, subcategoriesEnabled])

  const syntheticCategorySet = useMemo(() => {
    if (!subcategoriesEnabled) {
      return new Set<string>()
    }

    const synthetic = new Set<string>()
    for (const [name] of categoryEntries) {
      if (!realCategoryNames.has(name) && name !== "") {
        synthetic.add(name)
      }
    }
    return synthetic
  }, [categoryEntries, realCategoryNames, subcategoriesEnabled])

  const categoriesForTree = useMemo(() => Object.fromEntries(categoryEntries), [categoryEntries])

  const allowSubcategories = subcategoriesEnabled

  const getCategoryCountForTree = useCallback((categoryName: string) => {
    const key = categoryName ? `category:${categoryName}` : "category:"
    return getDisplayCount(key, incognitoMode ? getLinuxCount(categoryName, 50) : undefined)
  }, [getDisplayCount, incognitoMode])

  const expandCategoryList = useCallback((list: string[]) => {
    if (!allowSubcategories || list.length === 0) {
      return list
    }

    const uniqueBase = Array.from(new Set(list))
    const parentCategories = Array.from(new Set(uniqueBase.filter(category => category && category.length > 0)))

    if (parentCategories.length === 0) {
      return uniqueBase
    }

    const existing = new Set(uniqueBase)

    for (const [name] of categoryEntries) {
      if (!name || existing.has(name)) {
        continue
      }

      const hasParent = parentCategories.some(parent => name.startsWith(`${parent}/`))

      if (hasParent) {
        existing.add(name)
        uniqueBase.push(name)
      }
    }

    return uniqueBase
  }, [categoryEntries, allowSubcategories])

  const applyFilterChange = useCallback((nextFilters: TorrentFilters) => {
    const filtersWithExpansion: TorrentFilters = {
      ...nextFilters,
    }

    if (allowSubcategories) {
      filtersWithExpansion.expandedCategories = expandCategoryList(nextFilters.categories)
      filtersWithExpansion.expandedExcludeCategories = expandCategoryList(nextFilters.excludeCategories)
    } else {
      filtersWithExpansion.expandedCategories = undefined
      filtersWithExpansion.expandedExcludeCategories = undefined
    }

    onFilterChange(filtersWithExpansion)
  }, [allowSubcategories, expandCategoryList, onFilterChange])

  const selectedIncludeCategories = selectedFilters.categories
  const selectedExcludeCategories = selectedFilters.excludeCategories
  const selectedExpandedCategories = selectedFilters.expandedCategories
  const selectedExpandedExcludeCategories = selectedFilters.expandedExcludeCategories

  useEffect(() => {
    if (!allowSubcategories) {
      if ((selectedExpandedCategories?.length ?? 0) > 0 || (selectedExpandedExcludeCategories?.length ?? 0) > 0) {
        applyFilterChange({
          ...selectedFilters,
          categories: [...selectedIncludeCategories],
          excludeCategories: [...selectedExcludeCategories],
        })
      }
      return
    }

    const expandedIncluded = expandCategoryList(selectedIncludeCategories)
    const expandedExcluded = expandCategoryList(selectedExcludeCategories)

    const includeMismatch = !arraysEqual(selectedExpandedCategories, expandedIncluded)
    const excludeMismatch = !arraysEqual(selectedExpandedExcludeCategories, expandedExcluded)

    if (includeMismatch || excludeMismatch) {
      applyFilterChange({
        ...selectedFilters,
        categories: [...selectedIncludeCategories],
        excludeCategories: [...selectedExcludeCategories],
      })
    }
  }, [
    allowSubcategories,
    applyFilterChange,
    expandCategoryList,
    selectedExcludeCategories,
    selectedExpandedCategories,
    selectedExpandedExcludeCategories,
    selectedFilters,
    selectedIncludeCategories,
  ])

  // Helper function to check if we have received data from the server
  const hasReceivedData = useCallback((data: Record<string, Category> | string[] | Record<string, number> | undefined) => {
    return !incognitoMode && !isLoading && !isStaleData && data !== undefined
  }, [incognitoMode, isLoading, isStaleData])

  const hasReceivedCategoriesData = hasReceivedData(propsCategories)
  const hasReceivedTagsData = hasReceivedData(propsTags)
  const hasReceivedTrackersData = hasReceivedData(torrentCounts)

  const emptyCategoryNames = useMemo(() => {
    if (!hasReceivedCategoriesData || !hasReceivedTrackersData) {
      return []
    }

    return Object.keys(categories).filter(categoryName => {
      const count = torrentCounts ? torrentCounts[`category:${categoryName}`] || 0 : 0
      return count === 0
    })
  }, [categories, hasReceivedCategoriesData, hasReceivedTrackersData, torrentCounts])

  const hasEmptyCategories = emptyCategoryNames.length > 0

  // Use fake trackers if in incognito mode or extract from torrentCounts
  // When loading or showing stale data, show empty data to prevent stale data from previous instance
  const trackers = useMemo(() => {
    if (incognitoMode) return LINUX_TRACKERS
    if (isLoading || isStaleData) return []  // Clear trackers during loading or when stale

    // Extract unique trackers from torrentCounts
    const realTrackers = torrentCounts ? Object.keys(torrentCounts)
      .filter(key => key.startsWith("tracker:"))
      .map(key => key.replace("tracker:", ""))
      .filter(tracker => torrentCounts[`tracker:${tracker}`] > 0)
      .sort() : []

    return realTrackers
  }, [incognitoMode, torrentCounts, isLoading, isStaleData])

  // Use virtual scrolling for large lists to handle performance efficiently
  const VIRTUAL_THRESHOLD = 30 // Use virtual scrolling for lists > 30 items

  // Refs for virtual scrolling
  const categoryListRef = useRef<HTMLDivElement>(null)
  const tagListRef = useRef<HTMLDivElement>(null)
  const trackerListRef = useRef<HTMLDivElement>(null)
  const skipNextToggleRef = useRef<string | null>(null)
  const longPressTimeoutRef = useRef<number | null>(null)

  const cancelLongPress = useCallback(() => {
    if (longPressTimeoutRef.current !== null) {
      if (typeof window !== "undefined") {
        window.clearTimeout(longPressTimeoutRef.current)
      }
      longPressTimeoutRef.current = null
    }
  }, [])

  const scheduleLongPressExclude = useCallback((key: string, onLongPress: () => void) => {
    if (typeof window === "undefined") {
      return
    }

    cancelLongPress()
    longPressTimeoutRef.current = window.setTimeout(() => {
      skipNextToggleRef.current = key
      onLongPress()
      cancelLongPress()
    }, LONG_PRESS_DURATION)
  }, [cancelLongPress])

  useEffect(() => {
    if (typeof window === "undefined") {
      return
    }

    const handlePointerEnd = () => {
      cancelLongPress()
    }

    window.addEventListener("pointerup", handlePointerEnd)
    window.addEventListener("pointercancel", handlePointerEnd)
    window.addEventListener("pointerleave", handlePointerEnd)

    return () => {
      window.removeEventListener("pointerup", handlePointerEnd)
      window.removeEventListener("pointercancel", handlePointerEnd)
      window.removeEventListener("pointerleave", handlePointerEnd)
    }
  }, [cancelLongPress])

  useEffect(() => {
    return () => {
      cancelLongPress()
    }
  }, [cancelLongPress])

  const makeToggleKey = useCallback((group: "status" | "category" | "tag" | "tracker", value: string) => {
    return `${group}:${value === "" ? "__empty__" : value}`
  }, [])

  const includeStatusSet = useMemo(() => new Set(selectedFilters.status), [selectedFilters.status])
  const excludeStatusSet = useMemo(() => new Set(selectedFilters.excludeStatus), [selectedFilters.excludeStatus])

  const includeCategorySet = useMemo(() => new Set(selectedFilters.categories), [selectedFilters.categories])
  const excludeCategorySet = useMemo(() => new Set(selectedFilters.excludeCategories), [selectedFilters.excludeCategories])

  const includeTagSet = useMemo(() => new Set(selectedFilters.tags), [selectedFilters.tags])
  const excludeTagSet = useMemo(() => new Set(selectedFilters.excludeTags), [selectedFilters.excludeTags])

  const includeTrackerSet = useMemo(() => new Set(selectedFilters.trackers), [selectedFilters.trackers])
  const excludeTrackerSet = useMemo(() => new Set(selectedFilters.excludeTrackers), [selectedFilters.excludeTrackers])

  const getStatusState = useCallback((status: string): TriState => {
    if (includeStatusSet.has(status)) return "include"
    if (excludeStatusSet.has(status)) return "exclude"
    return "neutral"
  }, [includeStatusSet, excludeStatusSet])

  const setStatusState = useCallback((status: string, state: TriState) => {
    let nextIncluded = selectedFilters.status
    let nextExcluded = selectedFilters.excludeStatus

    const isIncluded = includeStatusSet.has(status)
    const isExcluded = excludeStatusSet.has(status)

    switch (state) {
      case "include":
        if (!isIncluded) {
          nextIncluded = [...selectedFilters.status, status]
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeStatus.filter(s => s !== status)
        }
        break
      case "exclude":
        if (isIncluded) {
          nextIncluded = selectedFilters.status.filter(s => s !== status)
        }
        if (!isExcluded) {
          nextExcluded = [...selectedFilters.excludeStatus, status]
        }
        break
      case "neutral":
        if (isIncluded) {
          nextIncluded = selectedFilters.status.filter(s => s !== status)
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeStatus.filter(s => s !== status)
        }
        break
    }

    if (nextIncluded === selectedFilters.status && nextExcluded === selectedFilters.excludeStatus) {
      return
    }

    applyFilterChange({
      ...selectedFilters,
      status: nextIncluded,
      excludeStatus: nextExcluded,
    })
  }, [applyFilterChange, excludeStatusSet, includeStatusSet, selectedFilters])

  const getCategoryState = useCallback((category: string): TriState => {
    if (includeCategorySet.has(category)) return "include"
    if (excludeCategorySet.has(category)) return "exclude"
    return "neutral"
  }, [excludeCategorySet, includeCategorySet])

  const setCategoryState = useCallback((category: string, state: TriState) => {
    let nextIncluded = selectedFilters.categories
    let nextExcluded = selectedFilters.excludeCategories

    const isIncluded = includeCategorySet.has(category)
    const isExcluded = excludeCategorySet.has(category)

    switch (state) {
      case "include":
        if (!isIncluded) {
          nextIncluded = [...selectedFilters.categories, category]
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeCategories.filter(c => c !== category)
        }
        break
      case "exclude":
        if (isIncluded) {
          nextIncluded = selectedFilters.categories.filter(c => c !== category)
        }
        if (!isExcluded) {
          nextExcluded = [...selectedFilters.excludeCategories, category]
        }
        break
      case "neutral":
        if (isIncluded) {
          nextIncluded = selectedFilters.categories.filter(c => c !== category)
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeCategories.filter(c => c !== category)
        }
        break
    }

    if (nextIncluded === selectedFilters.categories && nextExcluded === selectedFilters.excludeCategories) {
      return
    }

    applyFilterChange({
      ...selectedFilters,
      categories: nextIncluded,
      excludeCategories: nextExcluded,
    })
  }, [applyFilterChange, excludeCategorySet, includeCategorySet, selectedFilters])

  const getTagState = useCallback((tag: string): TriState => {
    if (includeTagSet.has(tag)) return "include"
    if (excludeTagSet.has(tag)) return "exclude"
    return "neutral"
  }, [includeTagSet, excludeTagSet])

  const setTagState = useCallback((tag: string, state: TriState) => {
    let nextIncluded = selectedFilters.tags
    let nextExcluded = selectedFilters.excludeTags

    const isIncluded = includeTagSet.has(tag)
    const isExcluded = excludeTagSet.has(tag)

    switch (state) {
      case "include":
        if (!isIncluded) {
          nextIncluded = [...selectedFilters.tags, tag]
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeTags.filter(t => t !== tag)
        }
        break
      case "exclude":
        if (isIncluded) {
          nextIncluded = selectedFilters.tags.filter(t => t !== tag)
        }
        if (!isExcluded) {
          nextExcluded = [...selectedFilters.excludeTags, tag]
        }
        break
      case "neutral":
        if (isIncluded) {
          nextIncluded = selectedFilters.tags.filter(t => t !== tag)
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeTags.filter(t => t !== tag)
        }
        break
    }

    if (nextIncluded === selectedFilters.tags && nextExcluded === selectedFilters.excludeTags) {
      return
    }

    applyFilterChange({
      ...selectedFilters,
      tags: nextIncluded,
      excludeTags: nextExcluded,
    })
  }, [applyFilterChange, excludeTagSet, includeTagSet, selectedFilters])

  const getTrackerState = useCallback((tracker: string): TriState => {
    if (includeTrackerSet.has(tracker)) return "include"
    if (excludeTrackerSet.has(tracker)) return "exclude"
    return "neutral"
  }, [excludeTrackerSet, includeTrackerSet])

  const setTrackerState = useCallback((tracker: string, state: TriState) => {
    let nextIncluded = selectedFilters.trackers
    let nextExcluded = selectedFilters.excludeTrackers

    const isIncluded = includeTrackerSet.has(tracker)
    const isExcluded = excludeTrackerSet.has(tracker)

    switch (state) {
      case "include":
        if (!isIncluded) {
          nextIncluded = [...selectedFilters.trackers, tracker]
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeTrackers.filter(t => t !== tracker)
        }
        break
      case "exclude":
        if (isIncluded) {
          nextIncluded = selectedFilters.trackers.filter(t => t !== tracker)
        }
        if (!isExcluded) {
          nextExcluded = [...selectedFilters.excludeTrackers, tracker]
        }
        break
      case "neutral":
        if (isIncluded) {
          nextIncluded = selectedFilters.trackers.filter(t => t !== tracker)
        }
        if (isExcluded) {
          nextExcluded = selectedFilters.excludeTrackers.filter(t => t !== tracker)
        }
        break
    }

    if (nextIncluded === selectedFilters.trackers && nextExcluded === selectedFilters.excludeTrackers) {
      return
    }

    applyFilterChange({
      ...selectedFilters,
      trackers: nextIncluded,
      excludeTrackers: nextExcluded,
    })
  }, [applyFilterChange, excludeTrackerSet, includeTrackerSet, selectedFilters])

  const getCheckboxVisualState = useCallback((state: "include" | "exclude" | "neutral"): boolean | "indeterminate" => {
    if (state === "include") return true
    if (state === "exclude") return "indeterminate"
    return false
  }, [])

  const handleStatusIncludeToggle = useCallback((status: string) => {
    const currentState = getStatusState(status)

    if (currentState === "include" || currentState === "exclude") {
      setStatusState(status, "neutral")
      return
    }

    setStatusState(status, "include")
  }, [getStatusState, setStatusState])

  const handleStatusExcludeToggle = useCallback((status: string) => {
    const currentState = getStatusState(status)
    const nextState = currentState === "exclude" ? "neutral" : "exclude"
    setStatusState(status, nextState)
  }, [getStatusState, setStatusState])

  const handleStatusCheckboxChange = useCallback((status: string) => {
    const key = makeToggleKey("status", status)
    if (skipNextToggleRef.current === key) {
      skipNextToggleRef.current = null
      return
    }

    skipNextToggleRef.current = null
    handleStatusIncludeToggle(status)
  }, [handleStatusIncludeToggle, makeToggleKey])

  const handleStatusPointerDown = useCallback((event: React.PointerEvent<HTMLElement>, status: string) => {
    if (event.button !== 0) {
      skipNextToggleRef.current = null
      cancelLongPress()
      return
    }

    if (event.metaKey || event.ctrlKey) {
      event.preventDefault()
      event.stopPropagation()
      skipNextToggleRef.current = makeToggleKey("status", status)
      handleStatusExcludeToggle(status)
      cancelLongPress()
      return
    }

    skipNextToggleRef.current = null

    const isTouchLike =
      event.pointerType === "touch" ||
      event.pointerType === "pen" ||
      (event.pointerType === "" && isMobile)

    if (isTouchLike) {
      const key = makeToggleKey("status", status)
      scheduleLongPressExclude(key, () => handleStatusExcludeToggle(status))
    } else {
      cancelLongPress()
    }
  }, [cancelLongPress, handleStatusExcludeToggle, isMobile, makeToggleKey, scheduleLongPressExclude])

  const handleCategoryIncludeToggle = useCallback((category: string) => {
    const currentState = getCategoryState(category)

    if (currentState === "include" || currentState === "exclude") {
      setCategoryState(category, "neutral")
      return
    }

    setCategoryState(category, "include")
  }, [getCategoryState, setCategoryState])

  const handleCategoryExcludeToggle = useCallback((category: string) => {
    const currentState = getCategoryState(category)
    const nextState = currentState === "exclude" ? "neutral" : "exclude"
    setCategoryState(category, nextState)
  }, [getCategoryState, setCategoryState])

  const handleCategoryCheckboxChange = useCallback((category: string) => {
    const key = makeToggleKey("category", category)
    if (skipNextToggleRef.current === key) {
      skipNextToggleRef.current = null
      return
    }

    skipNextToggleRef.current = null
    handleCategoryIncludeToggle(category)
  }, [handleCategoryIncludeToggle, makeToggleKey])

  const handleCategoryPointerDown = useCallback((event: React.PointerEvent<HTMLElement>, category: string) => {
    if (event.button !== 0) {
      skipNextToggleRef.current = null
      cancelLongPress()
      return
    }

    if (event.metaKey || event.ctrlKey) {
      event.preventDefault()
      event.stopPropagation()
      skipNextToggleRef.current = makeToggleKey("category", category)
      handleCategoryExcludeToggle(category)
      cancelLongPress()
      return
    }

    skipNextToggleRef.current = null

    const isTouchLike =
      event.pointerType === "touch" ||
      event.pointerType === "pen" ||
      (event.pointerType === "" && isMobile)

    if (isTouchLike) {
      const key = makeToggleKey("category", category)
      scheduleLongPressExclude(key, () => handleCategoryExcludeToggle(category))
    } else {
      cancelLongPress()
    }
  }, [cancelLongPress, handleCategoryExcludeToggle, isMobile, makeToggleKey, scheduleLongPressExclude])

  const handleTagIncludeToggle = useCallback((tag: string) => {
    const currentState = getTagState(tag)

    if (currentState === "include" || currentState === "exclude") {
      setTagState(tag, "neutral")
      return
    }

    setTagState(tag, "include")
  }, [getTagState, setTagState])

  const handleTagExcludeToggle = useCallback((tag: string) => {
    const currentState = getTagState(tag)
    const nextState = currentState === "exclude" ? "neutral" : "exclude"
    setTagState(tag, nextState)
  }, [getTagState, setTagState])

  const handleTagCheckboxChange = useCallback((tag: string) => {
    const key = makeToggleKey("tag", tag)
    if (skipNextToggleRef.current === key) {
      skipNextToggleRef.current = null
      return
    }

    skipNextToggleRef.current = null
    handleTagIncludeToggle(tag)
  }, [handleTagIncludeToggle, makeToggleKey])

  const handleTagPointerDown = useCallback((event: React.PointerEvent<HTMLElement>, tag: string) => {
    if (event.button !== 0) {
      skipNextToggleRef.current = null
      cancelLongPress()
      return
    }

    if (event.metaKey || event.ctrlKey) {
      event.preventDefault()
      event.stopPropagation()
      skipNextToggleRef.current = makeToggleKey("tag", tag)
      handleTagExcludeToggle(tag)
      cancelLongPress()
      return
    }

    skipNextToggleRef.current = null

    const isTouchLike =
      event.pointerType === "touch" ||
      event.pointerType === "pen" ||
      (event.pointerType === "" && isMobile)

    if (isTouchLike) {
      const key = makeToggleKey("tag", tag)
      scheduleLongPressExclude(key, () => handleTagExcludeToggle(tag))
    } else {
      cancelLongPress()
    }
  }, [cancelLongPress, handleTagExcludeToggle, isMobile, makeToggleKey, scheduleLongPressExclude])

  const handleTrackerIncludeToggle = useCallback((tracker: string) => {
    const currentState = getTrackerState(tracker)

    if (currentState === "include" || currentState === "exclude") {
      setTrackerState(tracker, "neutral")
      return
    }

    setTrackerState(tracker, "include")
  }, [getTrackerState, setTrackerState])

  const handleTrackerExcludeToggle = useCallback((tracker: string) => {
    const currentState = getTrackerState(tracker)
    const nextState = currentState === "exclude" ? "neutral" : "exclude"
    setTrackerState(tracker, nextState)
  }, [getTrackerState, setTrackerState])

  const handleTrackerCheckboxChange = useCallback((tracker: string) => {
    const key = makeToggleKey("tracker", tracker)
    if (skipNextToggleRef.current === key) {
      skipNextToggleRef.current = null
      return
    }

    skipNextToggleRef.current = null
    handleTrackerIncludeToggle(tracker)
  }, [handleTrackerIncludeToggle, makeToggleKey])

  const handleTrackerPointerDown = useCallback((event: React.PointerEvent<HTMLElement>, tracker: string) => {
    if (event.button !== 0) {
      skipNextToggleRef.current = null
      cancelLongPress()
      return
    }

    if (event.metaKey || event.ctrlKey) {
      event.preventDefault()
      event.stopPropagation()
      skipNextToggleRef.current = makeToggleKey("tracker", tracker)
      handleTrackerExcludeToggle(tracker)
      cancelLongPress()
      return
    }

    skipNextToggleRef.current = null

    const isTouchLike =
      event.pointerType === "touch" ||
      event.pointerType === "pen" ||
      (event.pointerType === "" && isMobile)

    if (isTouchLike) {
      const key = makeToggleKey("tracker", tracker)
      scheduleLongPressExclude(key, () => handleTrackerExcludeToggle(tracker))
    } else {
      cancelLongPress()
    }
  }, [cancelLongPress, handleTrackerExcludeToggle, isMobile, makeToggleKey, scheduleLongPressExclude])

  const untaggedState = getTagState("")
  const uncategorizedState = getCategoryState("")
  const noTrackerState = getTrackerState("")

  // Filtered categories for performance
  const filteredCategories = useMemo(() => {
    if (!debouncedCategorySearch) {
      return categoryEntries
    }

    const searchLower = debouncedCategorySearch.toLowerCase()
    return categoryEntries.filter(([name]) =>
      name.toLowerCase().includes(searchLower)
    )
  }, [categoryEntries, debouncedCategorySearch])

  // Filtered tags for performance
  const filteredTags = useMemo(() => {
    if (!debouncedTagSearch) {
      return tags
    }

    const searchLower = debouncedTagSearch.toLowerCase()
    return tags.filter(tag =>
      tag.toLowerCase().includes(searchLower)
    )
  }, [tags, debouncedTagSearch])

  // Filtered trackers for performance
  const filteredTrackers = useMemo(() => {
    if (!debouncedTrackerSearch) {
      return trackers
    }

    const searchLower = debouncedTrackerSearch.toLowerCase()
    return trackers.filter(tracker =>
      tracker.toLowerCase().includes(searchLower)
    )
  }, [trackers, debouncedTrackerSearch])

  const nonEmptyFilteredTrackers = useMemo(() => {
    return filteredTrackers.filter(tracker => tracker !== "")
  }, [filteredTrackers])

  // Virtual scrolling for categories
  const categoryVirtualizer = useVirtualizer({
    count: filteredCategories.length,
    getScrollElement: () => categoryListRef.current,
    estimateSize: () => 36, // Approximate height of each category item
    overscan: 10,
  })

  // Virtual scrolling for tags
  const tagVirtualizer = useVirtualizer({
    count: filteredTags.length,
    getScrollElement: () => tagListRef.current,
    estimateSize: () => 36, // Approximate height of each tag item
    overscan: 10,
  })

  // Virtual scrolling for trackers
  const trackerVirtualizer = useVirtualizer({
    count: nonEmptyFilteredTrackers.length,
    getScrollElement: () => trackerListRef.current,
    estimateSize: () => 36, // Approximate height of each tracker item
    overscan: 10,
  })

  const clearFilters = () => {
    applyFilterChange({
      status: [],
      excludeStatus: [],
      categories: [],
      excludeCategories: [],
      tags: [],
      excludeTags: [],
      trackers: [],
      excludeTrackers: [],
    })
    // Optionally reset accordion state to defaults
    // setExpandedItems(['status', 'categories', 'tags'])
  }

  const clearStatusFilter = () => {
    applyFilterChange({
      ...selectedFilters,
      status: [],
      excludeStatus: [],
    })
  }

  const clearCategoriesFilter = () => {
    applyFilterChange({
      ...selectedFilters,
      categories: [],
      excludeCategories: [],
    })
  }

  const clearTrackersFilter = () => {
    applyFilterChange({
      ...selectedFilters,
      trackers: [],
      excludeTrackers: [],
    })
  }
  const clearTagsFilter = () => {
    applyFilterChange({
      ...selectedFilters,
      tags: [],
      excludeTags: [],
    })
  }

  const handleCreateSubcategory = useCallback((categoryName: string) => {
    if (!subcategoriesEnabled) {
      return
    }
    setParentCategoryForNew(categoryName)
    setShowCreateCategoryDialog(true)
  }, [subcategoriesEnabled])

  const handleToggleCollapse = useCallback((categoryName: string) => {
    setCollapsedCategories((prev) => {
      const next = new Set(prev)
      if (next.has(categoryName)) {
        next.delete(categoryName)
      } else {
        next.add(categoryName)
      }
      return next
    })
  }, [])

  const handleEditCategoryByName = useCallback((categoryName: string) => {
    const category = categories[categoryName]
    if (!category) {
      return
    }
    setCategoryToEdit(category)
    setShowEditCategoryDialog(true)
  }, [categories, setCategoryToEdit, setShowEditCategoryDialog])

  const handleDeleteCategoryByName = useCallback((categoryName: string) => {
    setCategoryToDelete(categoryName)
    setShowDeleteCategoryDialog(true)
  }, [setCategoryToDelete, setShowDeleteCategoryDialog])

  const handleRemoveEmptyCategories = useCallback(() => {
    setShowDeleteEmptyCategoriesDialog(true)
  }, [setShowDeleteEmptyCategoriesDialog])

  useEffect(() => {
    if (!allowSubcategories) {
      setCollapsedCategories(new Set())
    }
  }, [allowSubcategories, setCollapsedCategories])

  const hasActiveFilters =
    selectedFilters.status.length > 0 ||
    selectedFilters.excludeStatus.length > 0 ||
    selectedFilters.categories.length > 0 ||
    selectedFilters.excludeCategories.length > 0 ||
    selectedFilters.tags.length > 0 ||
    selectedFilters.excludeTags.length > 0 ||
    selectedFilters.trackers.length > 0 ||
    selectedFilters.excludeTrackers.length > 0

  // Simple slide animation - sidebar slides in/out from the left
  return (
    <div
      className={`${className} h-full w-full xl:max-w-xs flex flex-col xl:flex-shrink-0 xl:border-r xl:bg-muted/10 ${
        isStaleData ? "opacity-75 transition-opacity duration-200" : ""
      }`}
    >
      <ScrollArea className="h-full flex-1 overscroll-contain select-none">
        <div className="p-4">
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-2">
              <h3 className="font-semibold">Filters</h3>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground"
                    aria-label="Filter selection tips"
                  >
                    <Info className="h-4 w-4" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom" align="start" className="max-w-[220px]">
                  Left click cycles include and neutral. Cmd/Ctrl + click or a long press toggles exclusion.
                </TooltipContent>
              </Tooltip>
              {(isLoading || isStaleData) && (
                <span className="text-xs text-muted-foreground animate-pulse">Loading...</span>
              )}
            </div>
            {hasActiveFilters && (
              <button
                onClick={clearFilters}
                className="text-xs text-muted-foreground hover:text-foreground"
              >
                Clear all
              </button>
            )}
          </div>

          {/* View Mode Toggle - only show on mobile */}
          {isMobile && (
            <div className="flex items-center justify-between p-3 mb-4 bg-muted/20 rounded-lg">
              <div className="flex flex-col gap-1">
                <span className="text-sm font-medium">View Mode</span>
                <span className="text-xs text-muted-foreground">
                  {viewMode === "normal" ? "Full torrent cards" :viewMode === "compact" ? "Compact cards" : "Ultra compact"}
                </span>
              </div>
              <button
                onClick={cycleViewMode}
                className="px-3 py-1 text-xs font-medium rounded border bg-background hover:bg-muted"
              >
                {viewMode === "normal" ? "Normal" :viewMode === "compact" ? "Compact" : "Ultra"}
              </button>
            </div>
          )}

          <Accordion
            type="multiple"
            value={expandedItems}
            onValueChange={setExpandedItems}
            className="space-y-2"
          >
            {/* Status Filter */}
            <AccordionItem value="status" className="border rounded-lg">
              <AccordionTrigger className="px-3 py-2 hover:no-underline">
                <div className="flex items-center justify-between w-full">
                  <span className="text-sm font-medium">Status</span>
                  {selectedFilters.status.length + selectedFilters.excludeStatus.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.status.length + selectedFilters.excludeStatus.length}
                      onClick={clearStatusFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="flex flex-col">
                  {visibleTorrentStates.map((state) => {
                    const statusState = getStatusState(state.value)
                    return (
                      <label
                        key={state.value}
                        className={cn(
                          "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                          statusState === "exclude"
                            ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                            : "hover:bg-muted"
                        )}
                        onPointerDown={(event) => handleStatusPointerDown(event, state.value)}
                      >
                        <Checkbox
                          checked={getCheckboxVisualState(statusState)}
                          onCheckedChange={() => handleStatusCheckboxChange(state.value)}
                        />
                        <span
                          className={cn(
                            "text-sm flex-1 flex items-center gap-2",
                            statusState === "exclude" ? "text-destructive" : undefined
                          )}
                        >
                          <state.icon className="h-4 w-4" />
                          <span>{state.label}</span>
                        </span>
                        <span
                          className={cn(
                            "text-xs",
                            statusState === "exclude" ? "text-destructive" : "text-muted-foreground"
                          )}
                        >
                          {getDisplayCount(`status:${state.value}`)}
                        </span>
                      </label>
                    )
                  })}
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Categories Filter */}
            <AccordionItem value="categories" className="border rounded-lg">
              <AccordionTrigger className="px-3 py-2 hover:no-underline">
                <div className="flex items-center justify-between w-full">
                  <span className="text-sm font-medium">Categories</span>
                  {selectedFilters.categories.length + selectedFilters.excludeCategories.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.categories.length + selectedFilters.excludeCategories.length}
                      onClick={clearCategoriesFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="flex flex-col gap-0">
                  {/* Add new category button */}
                  <button
                    className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground py-1.5 px-2 w-full cursor-pointer"
                    onClick={() => {
                      setParentCategoryForNew(undefined)
                      setShowCreateCategoryDialog(true)
                    }}
                  >
                    <Plus className="h-3 w-3" />
                    Add category
                  </button>

                  {/* Search input for categories */}
                  <div className="mb-2">
                    <SearchInput
                      placeholder="Search categories..."
                      value={categorySearch}
                      onChange={(e) => setCategorySearch(e.target.value)}
                      onClear={() => setCategorySearch("")}
                      className="h-7 text-xs"
                    />
                  </div>

                  {/* Uncategorized option */}
                  {!allowSubcategories && (
                    <label
                      className={cn(
                        "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                        uncategorizedState === "exclude"? "bg-destructive/10 text-destructive hover:bg-destructive/15": "hover:bg-muted"
                      )}
                      onPointerDown={(event) => handleCategoryPointerDown(event, "")}
                    >
                      <Checkbox
                        checked={getCheckboxVisualState(uncategorizedState)}
                        onCheckedChange={() => handleCategoryCheckboxChange("")}
                        className="rounded border-input"
                      />
                      <span
                        className={cn(
                          "text-sm flex-1 italic",
                          uncategorizedState === "exclude" ? "text-destructive" : "text-muted-foreground"
                        )}
                      >
                        Uncategorized
                      </span>
                      <span
                        className={cn(
                          "text-xs",
                          uncategorizedState === "exclude" ? "text-destructive" : "text-muted-foreground"
                        )}
                      >
                        {getDisplayCount("category:")}
                      </span>
                    </label>
                  )}

                  {/* Loading message for categories */}
                  {!hasReceivedCategoriesData && !incognitoMode && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic animate-pulse">
                      Loading categories...
                    </div>
                  )}

                  {/* No results message for categories */}
                  {hasReceivedCategoriesData && debouncedCategorySearch && filteredCategories.length === 0 && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic">
                      No categories found matching "{debouncedCategorySearch}"
                    </div>
                  )}

                  {/* Empty categories message */}
                  {hasReceivedCategoriesData && !debouncedCategorySearch && categoryEntries.length === 0 && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic">
                      No categories available
                    </div>
                  )}

                  {/* Category list - use filtered categories for performance or virtual scrolling for large lists */}
                  {allowSubcategories ? (
                    <CategoryTree
                      categories={categoriesForTree}
                      counts={torrentCounts ?? {}}
                      useSubcategories={allowSubcategories}
                      collapsedCategories={collapsedCategories}
                      onToggleCollapse={handleToggleCollapse}
                      searchTerm={debouncedCategorySearch}
                      getCategoryState={getCategoryState}
                      getCheckboxState={getCheckboxVisualState}
                      onCategoryCheckboxChange={handleCategoryCheckboxChange}
                      onCategoryPointerDown={handleCategoryPointerDown}
                      onCreateSubcategory={handleCreateSubcategory}
                      onEditCategory={handleEditCategoryByName}
                      onDeleteCategory={handleDeleteCategoryByName}
                      onRemoveEmptyCategories={handleRemoveEmptyCategories}
                      hasEmptyCategories={hasEmptyCategories}
                      syntheticCategories={syntheticCategorySet}
                      getCategoryCount={getCategoryCountForTree}
                    />
                  ) : filteredCategories.length > VIRTUAL_THRESHOLD ? (
                    <div ref={categoryListRef} className="max-h-96 overflow-auto">
                      <div
                        className="relative"
                        style={{ height: `${categoryVirtualizer.getTotalSize()}px` }}
                      >
                        {categoryVirtualizer.getVirtualItems().map((virtualRow) => {
                          const [name, category] = filteredCategories[virtualRow.index] || ["", {}]
                          if (!name) return null
                          const categoryState = getCategoryState(name)
                          const indentLevel = allowSubcategories ? Math.max(0, name.split("/").length - 1) : 0
                          const displayName = allowSubcategories ? (name.split("/").pop() ?? name) : name
                          const isSynthetic = syntheticCategorySet.has(name)

                          return (
                            <div
                              key={virtualRow.key}
                              data-index={virtualRow.index}
                              ref={categoryVirtualizer.measureElement}
                              style={{
                                position: "absolute",
                                top: 0,
                                left: 0,
                                width: "100%",
                                transform: `translateY(${virtualRow.start}px)`,
                              }}
                            >
                              <ContextMenu>
                                <ContextMenuTrigger asChild>
                                  <label
                                    className={cn(
                                      "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                                      categoryState === "exclude"
                                        ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                                        : "hover:bg-muted"
                                    )}
                                    onPointerDown={(event) => handleCategoryPointerDown(event, name)}
                                  >
                                    <Checkbox
                                      checked={getCheckboxVisualState(categoryState)}
                                      onCheckedChange={() => handleCategoryCheckboxChange(name)}
                                    />
                                    {allowSubcategories && indentLevel > 0 && (
                                      <span
                                        className="shrink-0"
                                        style={{ width: `${indentLevel * 12}px` }}
                                      />
                                    )}
                                    <span
                                      className={cn(
                                        "text-sm flex-1 truncate w-8",
                                        categoryState === "exclude" ? "text-destructive" : undefined
                                      )}
                                      title={name}
                                    >
                                      {displayName}
                                    </span>
                                    <span
                                      className={cn(
                                        "text-xs",
                                        categoryState === "exclude" ? "text-destructive" : "text-muted-foreground"
                                      )}
                                    >
                                      {getDisplayCount(`category:${name}`, incognitoMode ? getLinuxCount(name, 50) : undefined)}
                                    </span>
                                  </label>
                                </ContextMenuTrigger>
                                <ContextMenuContent>
                                  {allowSubcategories && (
                                    <>
                                      <ContextMenuItem onClick={() => handleCreateSubcategory(name)}>
                                        <FolderPlus className="mr-2 h-4 w-4" />
                                        Create Subcategory
                                      </ContextMenuItem>
                                      <ContextMenuSeparator />
                                    </>
                                  )}
                                  <ContextMenuItem
                                    disabled={isSynthetic}
                                    onClick={() => {
                                      if (isSynthetic) {
                                        return
                                      }
                                      setCategoryToEdit(category)
                                      setShowEditCategoryDialog(true)
                                    }}
                                  >
                                    <Edit className="mr-2 h-4 w-4" />
                                    Edit Category
                                  </ContextMenuItem>
                                  <ContextMenuSeparator />
                                  <ContextMenuItem
                                    disabled={isSynthetic}
                                    onClick={() => {
                                      if (isSynthetic) {
                                        return
                                      }
                                      setCategoryToDelete(name)
                                      setShowDeleteCategoryDialog(true)
                                    }}
                                    className="text-destructive"
                                  >
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    Delete Category
                                  </ContextMenuItem>
                                  <ContextMenuItem
                                    onClick={handleRemoveEmptyCategories}
                                    disabled={!hasEmptyCategories}
                                    className="text-destructive"
                                  >
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    Remove Empty Categories
                                  </ContextMenuItem>
                                </ContextMenuContent>
                              </ContextMenu>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  ) : (
                    filteredCategories.map(([name, category]: [string, Category]) => {
                      const categoryState = getCategoryState(name)
                      const indentLevel = allowSubcategories ? Math.max(0, name.split("/").length - 1) : 0
                      const displayName = allowSubcategories ? (name.split("/").pop() ?? name) : name
                      const isSynthetic = syntheticCategorySet.has(name)
                      return (
                        <ContextMenu key={name}>
                          <ContextMenuTrigger asChild>
                            <label
                              className={cn(
                                "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                                categoryState === "exclude"
                                  ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                                  : "hover:bg-muted"
                              )}
                              onPointerDown={(event) => handleCategoryPointerDown(event, name)}
                            >
                              <Checkbox
                                checked={getCheckboxVisualState(categoryState)}
                                onCheckedChange={() => handleCategoryCheckboxChange(name)}
                              />
                              {allowSubcategories && indentLevel > 0 && (
                                <span
                                  className="shrink-0"
                                  style={{ width: `${indentLevel * 12}px` }}
                                />
                              )}
                              <span
                                className={cn(
                                  "text-sm flex-1 truncate w-8",
                                  categoryState === "exclude" ? "text-destructive" : undefined
                                )}
                                title={name}
                              >
                                {displayName}
                              </span>
                              <span
                                className={cn(
                                  "text-xs",
                                  categoryState === "exclude" ? "text-destructive" : "text-muted-foreground"
                                )}
                              >
                                {getDisplayCount(`category:${name}`, incognitoMode ? getLinuxCount(name, 50) : undefined)}
                              </span>
                            </label>
                          </ContextMenuTrigger>
                          <ContextMenuContent>
                            {allowSubcategories && (
                              <>
                                <ContextMenuItem onClick={() => handleCreateSubcategory(name)}>
                                  <FolderPlus className="mr-2 h-4 w-4" />
                                  Create Subcategory
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                              </>
                            )}
                            <ContextMenuItem
                              disabled={isSynthetic}
                              onClick={() => {
                                if (isSynthetic) {
                                  return
                                }
                                setCategoryToEdit(category)
                                setShowEditCategoryDialog(true)
                              }}
                            >
                              <Edit className="mr-2 h-4 w-4" />
                              Edit Category
                            </ContextMenuItem>
                            <ContextMenuSeparator />
                            <ContextMenuItem
                              disabled={isSynthetic}
                              onClick={() => {
                                if (isSynthetic) {
                                  return
                                }
                                setCategoryToDelete(name)
                                setShowDeleteCategoryDialog(true)
                              }}
                              className="text-destructive"
                            >
                              <Trash2 className="mr-2 h-4 w-4" />
                              Delete Category
                            </ContextMenuItem>
                            <ContextMenuItem
                              onClick={handleRemoveEmptyCategories}
                              disabled={!hasEmptyCategories}
                              className="text-destructive"
                            >
                              <Trash2 className="mr-2 h-4 w-4" />
                              Remove Empty Categories
                            </ContextMenuItem>
                          </ContextMenuContent>
                        </ContextMenu>
                      )
                    })
                  )}
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Tags Filter */}
            <AccordionItem value="tags" className="border rounded-lg">
              <AccordionTrigger className="px-3 py-2 hover:no-underline">
                <div className="flex items-center justify-between w-full">
                  <span className="text-sm font-medium">Tags</span>
                  {selectedFilters.tags.length + selectedFilters.excludeTags.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.tags.length + selectedFilters.excludeTags.length}
                      onClick={clearTagsFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="flex flex-col gap-0">
                  {/* Add new tag button */}
                  <button
                    className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground py-1.5 px-2 w-full cursor-pointer"
                    onClick={() => setShowCreateTagDialog(true)}
                  >
                    <Plus className="h-3 w-3" />
                    Add tag
                  </button>

                  {/* Search input for tags */}
                  <div className="mb-2">
                    <SearchInput
                      placeholder="Search tags..."
                      value={tagSearch}
                      onChange={(e) => setTagSearch(e.target.value)}
                      onClear={() => setTagSearch("")}
                      className="h-7 text-xs"
                    />
                  </div>

                  {/* Untagged option */}
                  <label
                    className={cn(
                      "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                      untaggedState === "exclude" ? "bg-destructive/10 text-destructive hover:bg-destructive/15" : "hover:bg-muted"
                    )}
                    onPointerDown={(event) => handleTagPointerDown(event, "")}
                  >
                    <Checkbox
                      checked={getCheckboxVisualState(untaggedState)}
                      onCheckedChange={() => handleTagCheckboxChange("")}
                      className="rounded border-input"
                    />
                    <span
                      className={cn(
                        "text-sm flex-1 italic",
                        untaggedState === "exclude" ? "text-destructive" : "text-muted-foreground"
                      )}
                    >
                      Untagged
                    </span>
                    <span
                      className={cn(
                        "text-xs",
                        untaggedState === "exclude" ? "text-destructive" : "text-muted-foreground"
                      )}
                    >
                      {getDisplayCount("tag:")}
                    </span>
                  </label>

                  {/* Loading message for tags */}
                  {!hasReceivedTagsData && !incognitoMode && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic animate-pulse">
                      Loading tags...
                    </div>
                  )}

                  {/* No results message for tags */}
                  {hasReceivedTagsData && debouncedTagSearch && filteredTags.length === 0 && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic">
                      No tags found matching "{debouncedTagSearch}"
                    </div>
                  )}

                  {/* Empty tags message */}
                  {hasReceivedTagsData && !debouncedTagSearch && tags.length === 0 && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic">
                      No tags available
                    </div>
                  )}

                  {/* Tag list - use filtered tags for performance or virtual scrolling for large lists */}
                  {filteredTags.length > VIRTUAL_THRESHOLD ? (
                    <div ref={tagListRef} className="max-h-96 overflow-auto">
                      <div
                        className="relative"
                        style={{ height: `${tagVirtualizer.getTotalSize()}px` }}
                      >
                        {tagVirtualizer.getVirtualItems().map((virtualRow) => {
                          const tag = filteredTags[virtualRow.index]
                          if (!tag) return null
                          const tagState = getTagState(tag)

                          return (
                            <div
                              key={virtualRow.key}
                              data-index={virtualRow.index}
                              ref={tagVirtualizer.measureElement}
                              style={{
                                position: "absolute",
                                top: 0,
                                left: 0,
                                width: "100%",
                                transform: `translateY(${virtualRow.start}px)`,
                              }}
                            >
                              <ContextMenu>
                                <ContextMenuTrigger asChild>
                                  <label
                                    className={cn(
                                      "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                                      tagState === "exclude"
                                        ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                                        : "hover:bg-muted"
                                    )}
                                    onPointerDown={(event) => handleTagPointerDown(event, tag)}
                                  >
                                    <Checkbox
                                      checked={getCheckboxVisualState(tagState)}
                                      onCheckedChange={() => handleTagCheckboxChange(tag)}
                                    />
                                    <span
                                      className={cn(
                                        "text-sm flex-1 truncate w-8",
                                        tagState === "exclude" ? "text-destructive" : undefined
                                      )}
                                      title={tag}
                                    >
                                      {tag}
                                    </span>
                                    <span
                                      className={cn(
                                        "text-xs",
                                        tagState === "exclude" ? "text-destructive" : "text-muted-foreground"
                                      )}
                                    >
                                      {getDisplayCount(`tag:${tag}`, incognitoMode ? getLinuxCount(tag, 30) : undefined)}
                                    </span>
                                  </label>
                                </ContextMenuTrigger>
                                <ContextMenuContent>
                                  <ContextMenuItem
                                    onClick={() => {
                                      setTagToDelete(tag)
                                      setShowDeleteTagDialog(true)
                                    }}
                                    className="text-destructive"
                                  >
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    Delete Tag
                                  </ContextMenuItem>
                                  <ContextMenuSeparator />
                                  <ContextMenuItem
                                    onClick={() => setShowDeleteUnusedTagsDialog(true)}
                                    className="text-destructive"
                                  >
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    Delete All Unused Tags
                                  </ContextMenuItem>
                                </ContextMenuContent>
                              </ContextMenu>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  ) : (
                    filteredTags.map((tag: string) => {
                      const tagState = getTagState(tag)
                      return (
                        <ContextMenu key={tag}>
                          <ContextMenuTrigger asChild>
                            <label
                              className={cn(
                                "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                                tagState === "exclude"
                                  ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                                  : "hover:bg-muted"
                              )}
                              onPointerDown={(event) => handleTagPointerDown(event, tag)}
                            >
                              <Checkbox
                                checked={getCheckboxVisualState(tagState)}
                                onCheckedChange={() => handleTagCheckboxChange(tag)}
                              />
                              <span
                                className={cn(
                                  "text-sm flex-1 truncate w-8",
                                  tagState === "exclude" ? "text-destructive" : undefined
                                )}
                                title={tag}
                              >
                                {tag}
                              </span>
                              <span
                                className={cn(
                                  "text-xs",
                                  tagState === "exclude" ? "text-destructive" : "text-muted-foreground"
                                )}
                              >
                                {getDisplayCount(`tag:${tag}`, incognitoMode ? getLinuxCount(tag, 30) : undefined)}
                              </span>
                            </label>
                          </ContextMenuTrigger>
                          <ContextMenuContent>
                            <ContextMenuItem
                              onClick={() => {
                                setTagToDelete(tag)
                                setShowDeleteTagDialog(true)
                              }}
                              className="text-destructive"
                            >
                              <Trash2 className="mr-2 h-4 w-4" />
                              Delete Tag
                            </ContextMenuItem>
                            <ContextMenuSeparator />
                            <ContextMenuItem
                              onClick={() => setShowDeleteUnusedTagsDialog(true)}
                              className="text-destructive"
                            >
                              <Trash2 className="mr-2 h-4 w-4" />
                              Delete All Unused Tags
                            </ContextMenuItem>
                          </ContextMenuContent>
                        </ContextMenu>
                      )
                    })
                  )}
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Trackers Filter */}
            <AccordionItem value="trackers" className="border rounded-lg last:border-b">
              <AccordionTrigger className="px-3 py-2 hover:no-underline">
                <div className="flex items-center justify-between w-full">
                  <span className="text-sm font-medium">Trackers</span>
                  {selectedFilters.trackers.length + selectedFilters.excludeTrackers.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.trackers.length + selectedFilters.excludeTrackers.length}
                      onClick={clearTrackersFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="flex flex-col gap-0">
                  {/* Search input for trackers */}
                  <div className="mb-2">
                    <SearchInput
                      placeholder="Search trackers..."
                      value={trackerSearch}
                      onChange={(e) => setTrackerSearch(e.target.value)}
                      onClear={() => setTrackerSearch("")}
                      className="h-7 text-xs"
                    />
                  </div>

                  {/* No tracker option */}
                  <label
                    className={cn(
                      "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                      noTrackerState === "exclude"
                        ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                        : "hover:bg-muted"
                    )}
                    onPointerDown={(event) => handleTrackerPointerDown(event, "")}
                  >
                    <Checkbox
                      checked={getCheckboxVisualState(noTrackerState)}
                      onCheckedChange={() => handleTrackerCheckboxChange("")}
                      className="rounded border-input"
                    />
                    <span
                      className={cn(
                        "text-sm flex-1 italic",
                        noTrackerState === "exclude" ? "text-destructive" : "text-muted-foreground"
                      )}
                    >
                      No tracker
                    </span>
                    <span
                      className={cn(
                        "text-xs",
                        noTrackerState === "exclude" ? "text-destructive" : "text-muted-foreground"
                      )}
                    >
                      {getDisplayCount("tracker:")}
                    </span>
                  </label>

                  {/* Loading message for trackers */}
                  {!hasReceivedTrackersData && !incognitoMode && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic animate-pulse">
                      Loading trackers...
                    </div>
                  )}

                  {/* No results message for trackers */}
                  {hasReceivedTrackersData && debouncedTrackerSearch && nonEmptyFilteredTrackers.length === 0 && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic">
                      No trackers found matching "{debouncedTrackerSearch}"
                    </div>
                  )}

                  {/* Tracker list - use filtered trackers for performance or virtual scrolling for large lists */}
                  {nonEmptyFilteredTrackers.length > VIRTUAL_THRESHOLD ? (
                    <div ref={trackerListRef} className="max-h-96 overflow-auto">
                      <div
                        className="relative"
                        style={{ height: `${trackerVirtualizer.getTotalSize()}px` }}
                      >
                        {trackerVirtualizer.getVirtualItems().map((virtualRow) => {
                          const tracker = nonEmptyFilteredTrackers[virtualRow.index]
                          if (!tracker) return null
                          const trackerState = getTrackerState(tracker)

                          return (
                            <div
                              key={virtualRow.key}
                              data-index={virtualRow.index}
                              ref={trackerVirtualizer.measureElement}
                              style={{
                                position: "absolute",
                                top: 0,
                                left: 0,
                                width: "100%",
                                transform: `translateY(${virtualRow.start}px)`,
                              }}
                            >
                              <ContextMenu>
                                <ContextMenuTrigger asChild>
                                  <label
                                    className={cn(
                                      "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                                      trackerState === "exclude"
                                        ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                                        : "hover:bg-muted"
                                    )}
                                    onPointerDown={(event) => handleTrackerPointerDown(event, tracker)}
                                  >
                                    <Checkbox
                                      checked={getCheckboxVisualState(trackerState)}
                                      onCheckedChange={() => handleTrackerCheckboxChange(tracker)}
                                    />
                                    <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />
                                    <span
                                      className={cn(
                                        "text-sm flex-1 truncate w-8",
                                        trackerState === "exclude" ? "text-destructive" : undefined
                                      )}
                                      title={tracker}
                                    >
                                      {tracker}
                                    </span>
                                    <span
                                      className={cn(
                                        "text-xs",
                                        trackerState === "exclude" ? "text-destructive" : "text-muted-foreground"
                                      )}
                                    >
                                      {getDisplayCount(`tracker:${tracker}`, incognitoMode ? getLinuxCount(tracker, 100) : undefined)}
                                    </span>
                                  </label>
                                </ContextMenuTrigger>
                                <ContextMenuContent>
                                  <ContextMenuItem
                                    disabled={!supportsTrackerEditing}
                                    onClick={async () => {
                                      if (!supportsTrackerEditing) {
                                        return
                                      }
                                      setTrackerToEdit(tracker)
                                      await fetchTrackerURLs(tracker)
                                      setShowEditTrackerDialog(true)
                                    }}
                                  >
                                    <Edit className="mr-2 h-4 w-4" />
                                    Edit Tracker URL
                                  </ContextMenuItem>
                                </ContextMenuContent>
                              </ContextMenu>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  ) : (
                    nonEmptyFilteredTrackers.map((tracker) => {
                      const trackerState = getTrackerState(tracker)
                      return (
                        <ContextMenu key={tracker}>
                          <ContextMenuTrigger asChild>
                            <label
                              className={cn(
                                "flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer",
                                trackerState === "exclude"
                                  ? "bg-destructive/10 text-destructive hover:bg-destructive/15"
                                  : "hover:bg-muted"
                              )}
                              onPointerDown={(event) => handleTrackerPointerDown(event, tracker)}
                            >
                              <Checkbox
                                checked={getCheckboxVisualState(trackerState)}
                                onCheckedChange={() => handleTrackerCheckboxChange(tracker)}
                              />
                              <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />
                              <span
                                className={cn(
                                  "text-sm flex-1 truncate w-8",
                                  trackerState === "exclude" ? "text-destructive" : undefined
                                )}
                                title={tracker}
                              >
                                {tracker}
                              </span>
                              <span
                                className={cn(
                                  "text-xs",
                                  trackerState === "exclude" ? "text-destructive" : "text-muted-foreground"
                                )}
                              >
                                {getDisplayCount(`tracker:${tracker}`, incognitoMode ? getLinuxCount(tracker, 100) : undefined)}
                              </span>
                            </label>
                          </ContextMenuTrigger>
                          <ContextMenuContent>
                            <ContextMenuItem
                              disabled={!supportsTrackerEditing}
                              onClick={async () => {
                                if (!supportsTrackerEditing) {
                                  return
                                }
                                setTrackerToEdit(tracker)
                                await fetchTrackerURLs(tracker)
                                setShowEditTrackerDialog(true)
                              }}
                            >
                              <Edit className="mr-2 h-4 w-4" />
                              Edit Tracker URL
                            </ContextMenuItem>
                          </ContextMenuContent>
                        </ContextMenu>
                      )
                    })
                  )}
                </div>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        </div>
      </ScrollArea>

      {/* Dialogs */}
      <CreateTagDialog
        open={showCreateTagDialog}
        onOpenChange={setShowCreateTagDialog}
        instanceId={instanceId}
      />

      <DeleteTagDialog
        open={showDeleteTagDialog}
        onOpenChange={setShowDeleteTagDialog}
        instanceId={instanceId}
        tag={tagToDelete}
      />

      <CreateCategoryDialog
        open={showCreateCategoryDialog}
        onOpenChange={(open) => {
          setShowCreateCategoryDialog(open)
          if (!open) {
            setParentCategoryForNew(undefined)
          }
        }}
        instanceId={instanceId}
        parent={parentCategoryForNew}
      />

      {categoryToEdit && (
        <EditCategoryDialog
          open={showEditCategoryDialog}
          onOpenChange={setShowEditCategoryDialog}
          instanceId={instanceId}
          category={categoryToEdit}
        />
      )}

      <DeleteCategoryDialog
        open={showDeleteCategoryDialog}
        onOpenChange={setShowDeleteCategoryDialog}
        instanceId={instanceId}
        categoryName={categoryToDelete}
      />

      <DeleteEmptyCategoriesDialog
        open={showDeleteEmptyCategoriesDialog}
        onOpenChange={setShowDeleteEmptyCategoriesDialog}
        instanceId={instanceId}
        categories={categories}
        torrentCounts={torrentCounts}
      />

      <DeleteUnusedTagsDialog
        open={showDeleteUnusedTagsDialog}
        onOpenChange={setShowDeleteUnusedTagsDialog}
        instanceId={instanceId}
        tags={tags}
        torrentCounts={torrentCounts}
      />

      <EditTrackerDialog
        open={showEditTrackerDialog}
        onOpenChange={(open) => {
          setShowEditTrackerDialog(open)
          if (!open) {
            setTrackerFullURLs([])
          }
        }}
        instanceId={instanceId}
        tracker={trackerToEdit}
        trackerURLs={trackerFullURLs}
        loadingURLs={loadingTrackerURLs}
        selectedHashes={[]} // Not using selected hashes, will update all torrents with this tracker
        onConfirm={(oldURL, newURL) => editTrackersMutation.mutate({ oldURL, newURL, tracker: trackerToEdit })}
        isPending={editTrackersMutation.isPending}
      />
    </div>
  )
}

// Memoize the component to prevent unnecessary re-renders during polling
export const FilterSidebar = memo(FilterSidebarComponent, (prevProps, nextProps) => {
  if (prevProps.instanceId !== nextProps.instanceId) return false
  if (prevProps.className !== nextProps.className) return false
  if (prevProps.isStaleData !== nextProps.isStaleData) return false
  if (prevProps.isLoading !== nextProps.isLoading) return false
  if (prevProps.isMobile !== nextProps.isMobile) return false
  if (prevProps.onFilterChange !== nextProps.onFilterChange) return false
  if ((prevProps.useSubcategories ?? false) !== (nextProps.useSubcategories ?? false)) return false

  return (
    prevProps.selectedFilters === nextProps.selectedFilters &&
    prevProps.torrentCounts === nextProps.torrentCounts &&
    prevProps.categories === nextProps.categories &&
    prevProps.tags === nextProps.tags
  )
})
