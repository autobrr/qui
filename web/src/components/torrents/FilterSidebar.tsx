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
import { useDebounce } from "@/hooks/useDebounce"
import { usePersistedAccordion } from "@/hooks/usePersistedAccordion"
import { getLinuxCount, LINUX_CATEGORIES, LINUX_TAGS, LINUX_TRACKERS, useIncognitoMode } from "@/lib/incognito"
import type { Category } from "@/types"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  AlertCircle,
  CheckCircle2,
  Download,
  Edit,
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
import { memo, useCallback, useMemo, useRef, useState } from "react"
import {
  CreateCategoryDialog,
  CreateTagDialog,
  DeleteCategoryDialog,
  DeleteTagDialog,
  DeleteUnusedTagsDialog,
  EditCategoryDialog
} from "./TagCategoryManagement"

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
  selectedFilters: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
  onFilterChange: (filters: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }) => void
  torrentCounts?: Record<string, number>
  categories?: Record<string, Category>
  tags?: string[]
  className?: string
  isStaleData?: boolean
  isLoading?: boolean
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
]

const FilterSidebarComponent = ({
  instanceId,
  selectedFilters,
  onFilterChange,
  torrentCounts = {},
  categories: propsCategories,
  tags: propsTags,
  className = "",
  isStaleData = false,
  isLoading = false,
}: FilterSidebarProps) => {
  // Use incognito mode hook
  const [incognitoMode] = useIncognitoMode()

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
  const [categoryToEdit, setCategoryToEdit] = useState<Category | null>(null)
  const [categoryToDelete, setCategoryToDelete] = useState("")

  // Search states for filtering large lists
  const [categorySearch, setCategorySearch] = useState("")
  const [tagSearch, setTagSearch] = useState("")
  const [trackerSearch, setTrackerSearch] = useState("")

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

  // Helper function to check if we have received data from the server
  const hasReceivedData = useCallback((data: Record<string, Category> | string[] | Record<string, number> | undefined) => {
    return !incognitoMode && !isLoading && !isStaleData && data !== undefined
  }, [incognitoMode, isLoading, isStaleData])

  const hasReceivedCategoriesData = hasReceivedData(propsCategories)
  const hasReceivedTagsData = hasReceivedData(propsTags)
  const hasReceivedTrackersData = hasReceivedData(torrentCounts)

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
  const VIRTUAL_THRESHOLD = 100 // Use virtual scrolling for lists > 100 items

  // Refs for virtual scrolling
  const categoryListRef = useRef<HTMLDivElement>(null)
  const tagListRef = useRef<HTMLDivElement>(null)
  const trackerListRef = useRef<HTMLDivElement>(null)

  // Filtered categories for performance
  const filteredCategories = useMemo(() => {
    const categoryEntries = Object.entries(categories) as [string, Category][]

    if (debouncedCategorySearch) {
      const searchLower = debouncedCategorySearch.toLowerCase()
      return categoryEntries.filter(([name]) =>
        name.toLowerCase().includes(searchLower)
      )
    }

    // Show selected categories first, then others
    const selectedCategories = categoryEntries.filter(([name]) =>
      selectedFilters.categories.includes(name)
    )
    const unselectedCategories = categoryEntries.filter(([name]) =>
      !selectedFilters.categories.includes(name)
    )

    return [...selectedCategories, ...unselectedCategories]
  }, [categories, debouncedCategorySearch, selectedFilters.categories])

  // Filtered tags for performance
  const filteredTags = useMemo(() => {
    if (debouncedTagSearch) {
      const searchLower = debouncedTagSearch.toLowerCase()
      return tags.filter(tag =>
        tag.toLowerCase().includes(searchLower)
      )
    }

    // Show selected tags first, then others
    const selectedTags = tags.filter(tag =>
      selectedFilters.tags.includes(tag)
    )
    const unselectedTags = tags.filter(tag =>
      !selectedFilters.tags.includes(tag)
    )

    return [...selectedTags, ...unselectedTags]
  }, [tags, debouncedTagSearch, selectedFilters.tags])

  // Filtered trackers for performance
  const filteredTrackers = useMemo(() => {
    if (debouncedTrackerSearch) {
      const searchLower = debouncedTrackerSearch.toLowerCase()
      return trackers.filter(tracker =>
        tracker.toLowerCase().includes(searchLower)
      )
    }

    // Show selected trackers first, then others
    const selectedTrackers = trackers.filter(tracker =>
      selectedFilters.trackers.includes(tracker)
    )
    const unselectedTrackers = trackers.filter(tracker =>
      !selectedFilters.trackers.includes(tracker)
    )

    return [...selectedTrackers, ...unselectedTrackers]
  }, [trackers, debouncedTrackerSearch, selectedFilters.trackers])

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
    count: filteredTrackers.filter(tracker => tracker !== "").length,
    getScrollElement: () => trackerListRef.current,
    estimateSize: () => 36, // Approximate height of each tracker item
    overscan: 10,
  })

  const handleStatusToggle = useCallback((status: string) => {
    const newStatus = selectedFilters.status.includes(status)? selectedFilters.status.filter(s => s !== status): [...selectedFilters.status, status]

    onFilterChange({
      ...selectedFilters,
      status: newStatus,
    })
  }, [selectedFilters, onFilterChange])

  const handleCategoryToggle = useCallback((category: string) => {
    const newCategories = selectedFilters.categories.includes(category)? selectedFilters.categories.filter(c => c !== category): [...selectedFilters.categories, category]

    onFilterChange({
      ...selectedFilters,
      categories: newCategories,
    })
  }, [selectedFilters, onFilterChange])

  const handleTagToggle = useCallback((tag: string) => {
    const newTags = selectedFilters.tags.includes(tag)? selectedFilters.tags.filter(t => t !== tag): [...selectedFilters.tags, tag]

    onFilterChange({
      ...selectedFilters,
      tags: newTags,
    })
  }, [selectedFilters, onFilterChange])

  const handleTrackerToggle = useCallback((tracker: string) => {
    const newTrackers = selectedFilters.trackers.includes(tracker)? selectedFilters.trackers.filter(t => t !== tracker): [...selectedFilters.trackers, tracker]

    onFilterChange({
      ...selectedFilters,
      trackers: newTrackers,
    })
  }, [selectedFilters, onFilterChange])

  const clearFilters = () => {
    onFilterChange({
      status: [],
      categories: [],
      tags: [],
      trackers: [],
    })
    // Optionally reset accordion state to defaults
    // setExpandedItems(['status', 'categories', 'tags'])
  }

  const createClearFilter = (property: keyof typeof selectedFilters) => () => {
    onFilterChange({
      ...selectedFilters,
      [property]: [],
    })
  }

  const clearStatusFilter = createClearFilter("status")
  const clearCategoriesFilter = createClearFilter("categories")
  const clearTagsFilter = createClearFilter("tags")
  const clearTrackersFilter = createClearFilter("trackers")

  const hasActiveFilters =
    selectedFilters.status.length > 0 ||
    selectedFilters.categories.length > 0 ||
    selectedFilters.tags.length > 0 ||
    selectedFilters.trackers.length > 0

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
                  {selectedFilters.status.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.status.length}
                      onClick={clearStatusFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="space-y-1">
                  {TORRENT_STATES.map((state) => (
                    <label
                      key={state.value}
                      className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer"
                    >
                      <Checkbox
                        checked={selectedFilters.status.includes(state.value)}
                        onCheckedChange={() => handleStatusToggle(state.value)}
                      />
                      <span className="text-sm flex-1 flex items-center gap-2">
                        <state.icon className="h-4 w-4" />
                        <span>{state.label}</span>
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {getDisplayCount(`status:${state.value}`)}
                      </span>
                    </label>
                  ))}
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Categories Filter */}
            <AccordionItem value="categories" className="border rounded-lg">
              <AccordionTrigger className="px-3 py-2 hover:no-underline">
                <div className="flex items-center justify-between w-full">
                  <span className="text-sm font-medium">Categories</span>
                  {selectedFilters.categories.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.categories.length}
                      onClick={clearCategoriesFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="space-y-1">
                  {/* Add new category button */}
                  <button
                    className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors py-1 px-2 w-full cursor-pointer"
                    onClick={() => setShowCreateCategoryDialog(true)}
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
                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                    <Checkbox
                      checked={selectedFilters.categories.includes("")}
                      onCheckedChange={() => handleCategoryToggle("")}
                      className="rounded border-input"
                    />
                    <span className="text-sm flex-1 italic text-muted-foreground">
                      Uncategorized
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {getDisplayCount("category:")}
                    </span>
                  </label>

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
                  {hasReceivedCategoriesData && !debouncedCategorySearch && Object.keys(categories).length === 0 && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic">
                      No categories available
                    </div>
                  )}

                  {/* Category list - use filtered categories for performance or virtual scrolling for large lists */}
                  {Object.keys(categories).length > VIRTUAL_THRESHOLD ? (
                    <div ref={categoryListRef} className="max-h-96 overflow-auto">
                      <div
                        className="relative"
                        style={{ height: `${categoryVirtualizer.getTotalSize()}px` }}
                      >
                        {categoryVirtualizer.getVirtualItems().map((virtualRow) => {
                          const [name, category] = filteredCategories[virtualRow.index] || ["", {}]
                          if (!name) return null

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
                                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                                    <Checkbox
                                      checked={selectedFilters.categories.includes(name)}
                                      onCheckedChange={() => handleCategoryToggle(name)}
                                    />
                                    <span className="text-sm flex-1 truncate w-8" title={name}>
                                      {name}
                                    </span>
                                    <span className="text-xs text-muted-foreground">
                                      {getDisplayCount(`category:${name}`, incognitoMode ? getLinuxCount(name, 50) : undefined)}
                                    </span>
                                  </label>
                                </ContextMenuTrigger>
                                <ContextMenuContent>
                                  <ContextMenuItem
                                    onClick={() => {
                                      setCategoryToEdit(category)
                                      setShowEditCategoryDialog(true)
                                    }}
                                  >
                                    <Edit className="mr-2 h-4 w-4" />
                                    Edit Category
                                  </ContextMenuItem>
                                  <ContextMenuSeparator />
                                  <ContextMenuItem
                                    onClick={() => {
                                      setCategoryToDelete(name)
                                      setShowDeleteCategoryDialog(true)
                                    }}
                                    className="text-destructive"
                                  >
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    Delete Category
                                  </ContextMenuItem>
                                </ContextMenuContent>
                              </ContextMenu>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  ) : (
                    filteredCategories.map(([name, category]: [string, Category]) => (
                      <ContextMenu key={name}>
                        <ContextMenuTrigger asChild>
                          <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                            <Checkbox
                              checked={selectedFilters.categories.includes(name)}
                              onCheckedChange={() => handleCategoryToggle(name)}
                            />
                            <span className="text-sm flex-1 truncate w-8" title={name}>
                              {name}
                            </span>
                            <span className="text-xs text-muted-foreground">
                              {getDisplayCount(`category:${name}`, incognitoMode ? getLinuxCount(name, 50) : undefined)}
                            </span>
                          </label>
                        </ContextMenuTrigger>
                        <ContextMenuContent>
                          <ContextMenuItem
                            onClick={() => {
                              setCategoryToEdit(category)
                              setShowEditCategoryDialog(true)
                            }}
                          >
                            <Edit className="mr-2 h-4 w-4" />
                            Edit Category
                          </ContextMenuItem>
                          <ContextMenuSeparator />
                          <ContextMenuItem
                            onClick={() => {
                              setCategoryToDelete(name)
                              setShowDeleteCategoryDialog(true)
                            }}
                            className="text-destructive"
                          >
                            <Trash2 className="mr-2 h-4 w-4" />
                            Delete Category
                          </ContextMenuItem>
                        </ContextMenuContent>
                      </ContextMenu>
                    ))
                  )}
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Tags Filter */}
            <AccordionItem value="tags" className="border rounded-lg">
              <AccordionTrigger className="px-3 py-2 hover:no-underline">
                <div className="flex items-center justify-between w-full">
                  <span className="text-sm font-medium">Tags</span>
                  {selectedFilters.tags.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.tags.length}
                      onClick={clearTagsFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="space-y-1">
                  {/* Add new tag button */}
                  <button
                    className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors py-1 px-2 w-full cursor-pointer"
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
                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                    <Checkbox
                      checked={selectedFilters.tags.includes("")}
                      onCheckedChange={() => handleTagToggle("")}
                      className="rounded border-input"
                    />
                    <span className="text-sm flex-1 italic text-muted-foreground">
                      Untagged
                    </span>
                    <span className="text-xs text-muted-foreground">
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
                  {tags.length > VIRTUAL_THRESHOLD ? (
                    <div ref={tagListRef} className="max-h-96 overflow-auto">
                      <div
                        className="relative"
                        style={{ height: `${tagVirtualizer.getTotalSize()}px` }}
                      >
                        {tagVirtualizer.getVirtualItems().map((virtualRow) => {
                          const tag = filteredTags[virtualRow.index]
                          if (!tag) return null

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
                                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                                    <Checkbox
                                      checked={selectedFilters.tags.includes(tag)}
                                      onCheckedChange={() => handleTagToggle(tag)}
                                    />
                                    <span className="text-sm flex-1 truncate w-8" title={tag}>
                                      {tag}
                                    </span>
                                    <span className="text-xs text-muted-foreground">
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
                    filteredTags.map((tag: string) => (
                      <ContextMenu key={tag}>
                        <ContextMenuTrigger asChild>
                          <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                            <Checkbox
                              checked={selectedFilters.tags.includes(tag)}
                              onCheckedChange={() => handleTagToggle(tag)}
                            />
                            <span className="text-sm flex-1 truncate w-8" title={tag}>
                              {tag}
                            </span>
                            <span className="text-xs text-muted-foreground">
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
                    ))
                  )}
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Trackers Filter */}
            <AccordionItem value="trackers" className="border rounded-lg last:border-b">
              <AccordionTrigger className="px-3 py-2 hover:no-underline">
                <div className="flex items-center justify-between w-full">
                  <span className="text-sm font-medium">Trackers</span>
                  {selectedFilters.trackers.length > 0 && (
                    <FilterBadge
                      count={selectedFilters.trackers.length}
                      onClick={clearTrackersFilter}
                    />
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="space-y-1">
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
                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                    <Checkbox
                      checked={selectedFilters.trackers.includes("")}
                      onCheckedChange={() => handleTrackerToggle("")}
                      className="rounded border-input"
                    />
                    <span className="text-sm flex-1 italic text-muted-foreground">
                      No tracker
                    </span>
                    <span className="text-xs text-muted-foreground">
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
                  {hasReceivedTrackersData && debouncedTrackerSearch && filteredTrackers.filter(tracker => tracker !== "").length === 0 && (
                    <div className="text-xs text-muted-foreground px-2 py-3 text-center italic">
                      No trackers found matching "{debouncedTrackerSearch}"
                    </div>
                  )}

                  {/* Tracker list - use filtered trackers for performance or virtual scrolling for large lists */}
                  {trackers.length > VIRTUAL_THRESHOLD ? (
                    <div ref={trackerListRef} className="max-h-96 overflow-auto">
                      <div
                        className="relative"
                        style={{ height: `${trackerVirtualizer.getTotalSize()}px` }}
                      >
                        {trackerVirtualizer.getVirtualItems().map((virtualRow) => {
                          const tracker = filteredTrackers.filter(t => t !== "")[virtualRow.index]
                          if (!tracker) return null

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
                              <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                                <Checkbox
                                  checked={selectedFilters.trackers.includes(tracker)}
                                  onCheckedChange={() => handleTrackerToggle(tracker)}
                                />
                                <span className="text-sm flex-1 truncate w-8" title={tracker}>
                                  {tracker}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                  {getDisplayCount(`tracker:${tracker}`, incognitoMode ? getLinuxCount(tracker, 100) : undefined)}
                                </span>
                              </label>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  ) : (
                    filteredTrackers.filter(tracker => tracker !== "").map((tracker) => (
                      <label
                        key={tracker}
                        className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer"
                      >
                        <Checkbox
                          checked={selectedFilters.trackers.includes(tracker)}
                          onCheckedChange={() => handleTrackerToggle(tracker)}
                        />
                        <span className="text-sm flex-1 truncate w-8" title={tracker}>
                          {tracker}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {getDisplayCount(`tracker:${tracker}`, incognitoMode ? getLinuxCount(tracker, 100) : undefined)}
                        </span>
                      </label>
                    ))
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
        onOpenChange={setShowCreateCategoryDialog}
        instanceId={instanceId}
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

      <DeleteUnusedTagsDialog
        open={showDeleteUnusedTagsDialog}
        onOpenChange={setShowDeleteUnusedTagsDialog}
        instanceId={instanceId}
        tags={tags}
        torrentCounts={torrentCounts}
      />
    </div>
  )
}

// Memoize the component to prevent unnecessary re-renders during polling
export const FilterSidebar = memo(FilterSidebarComponent, (prevProps, nextProps) => {
  // Custom comparison function - only re-render if these props change
  return (
    prevProps.instanceId === nextProps.instanceId &&
    JSON.stringify(prevProps.selectedFilters) === JSON.stringify(nextProps.selectedFilters) &&
    JSON.stringify(prevProps.torrentCounts) === JSON.stringify(nextProps.torrentCounts) &&
    JSON.stringify(prevProps.categories) === JSON.stringify(nextProps.categories) &&
    JSON.stringify(prevProps.tags) === JSON.stringify(nextProps.tags) &&
    prevProps.className === nextProps.className &&
    prevProps.isStaleData === nextProps.isStaleData &&
    prevProps.isLoading === nextProps.isLoading
  )
})