/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useMemo, memo, useCallback } from "react"
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger
} from "@/components/ui/accordion"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
import { usePersistedAccordion } from "@/hooks/usePersistedAccordion"
import { useDebounce } from "@/hooks/useDebounce"
import {
  Download,
  Upload,
  CheckCircle2,
  PauseCircle,
  PlayCircle,
  StopCircle,
  AlertCircle,
  XCircle,
  Plus,
  Edit,
  Trash2,
  RotateCw,
  MoveRight,
  Search,
  X,
  type LucideIcon
} from "lucide-react"
import {
  CreateTagDialog,
  DeleteTagDialog,
  CreateCategoryDialog,
  EditCategoryDialog,
  DeleteCategoryDialog,
  DeleteUnusedTagsDialog
} from "./TagCategoryManagement"
import { LINUX_CATEGORIES, LINUX_TAGS, LINUX_TRACKERS, useIncognitoMode } from "@/lib/incognito"
import type { Category } from "@/types";

interface FilterBadgeProps {
  count: number
  onClick: () => void
}

function FilterBadge({ count, onClick }: FilterBadgeProps) {
  return (
    <Badge variant="secondary" className="ml-2 h-5 px-1.5 text-xs">
      <button
        onClick={(e) => {
          e.stopPropagation()
          onClick()
        }}
        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <X className="size-3"/>
        {count}
      </button>
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
}


// Define torrent states based on qBittorrent
const TORRENT_STATES: Array<{ value: string; label: string; icon: LucideIcon }> = [
  { value: "downloading", label: "Downloading", icon: Download },
  { value: "seeding", label: "Seeding", icon: Upload },
  { value: "completed", label: "Completed", icon: CheckCircle2 },
  { value: "paused", label: "Paused", icon: PauseCircle },
  { value: "active", label: "Active", icon: PlayCircle },
  { value: "inactive", label: "Inactive", icon: StopCircle },
  { value: "resumed", label: "Resumed", icon: PlayCircle },
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
}: FilterSidebarProps) => {
  // Use incognito mode hook
  const [incognitoMode] = useIncognitoMode()

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
  const [categoryToEdit, setCategoryToEdit] = useState<{ name: string; savePath: string } | null>(null)
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
  const categories = useMemo(() => {
    return incognitoMode ? LINUX_CATEGORIES : (propsCategories || {})
  }, [incognitoMode, propsCategories])

  const tags = useMemo(() => {
    return incognitoMode ? LINUX_TAGS : (propsTags || [])
  }, [incognitoMode, propsTags])

  // Extract unique trackers from torrentCounts (move this before filtered trackers)
  const realTrackers = torrentCounts? Object.keys(torrentCounts)
    .filter(key => key.startsWith("tracker:"))
    .map(key => key.replace("tracker:", ""))
    .filter(tracker => torrentCounts[`tracker:${tracker}`] > 0)
    .sort(): []

  // Use fake trackers if in incognito mode
  const trackers = useMemo(() => {
    return incognitoMode ? LINUX_TRACKERS : realTrackers
  }, [incognitoMode, realTrackers])

  // Optimize large lists by limiting initial render and providing search
  const MAX_INITIAL_ITEMS = 200

  // Filtered and limited categories for performance
  const filteredCategories = useMemo(() => {
    const categoryEntries = Object.entries(categories)

    if (debouncedCategorySearch) {
      const searchLower = debouncedCategorySearch.toLowerCase()
      return categoryEntries.filter(([name]) =>
        name.toLowerCase().includes(searchLower)
      )
    }

    // Show selected categories first, then others up to limit
    const selectedCategories = categoryEntries.filter(([name]) =>
      selectedFilters.categories.includes(name)
    )
    const unselectedCategories = categoryEntries.filter(([name]) =>
      !selectedFilters.categories.includes(name)
    )

    if (categoryEntries.length > MAX_INITIAL_ITEMS) {
      const remainingSlots = Math.max(0, MAX_INITIAL_ITEMS - selectedCategories.length)
      return [...selectedCategories, ...unselectedCategories.slice(0, remainingSlots)]
    }

    return categoryEntries
  }, [categories, debouncedCategorySearch, selectedFilters.categories])

  // Filtered and limited tags for performance
  const filteredTags = useMemo(() => {
    if (debouncedTagSearch) {
      const searchLower = debouncedTagSearch.toLowerCase()
      return tags.filter(tag =>
        tag.toLowerCase().includes(searchLower)
      )
    }

    // Show selected tags first, then others up to limit
    const selectedTags = tags.filter(tag =>
      selectedFilters.tags.includes(tag)
    )
    const unselectedTags = tags.filter(tag =>
      !selectedFilters.tags.includes(tag)
    )

    if (tags.length > MAX_INITIAL_ITEMS) {
      const remainingSlots = Math.max(0, MAX_INITIAL_ITEMS - selectedTags.length)
      return [...selectedTags, ...unselectedTags.slice(0, remainingSlots)]
    }

    return tags
  }, [tags, debouncedTagSearch, selectedFilters.tags])

  // Filtered and limited trackers for performance
  const filteredTrackers = useMemo(() => {
    if (debouncedTrackerSearch) {
      const searchLower = debouncedTrackerSearch.toLowerCase()
      return trackers.filter(tracker =>
        tracker.toLowerCase().includes(searchLower)
      )
    }

    // Show selected trackers first, then others up to limit
    const selectedTrackers = trackers.filter(tracker =>
      selectedFilters.trackers.includes(tracker)
    )
    const unselectedTrackers = trackers.filter(tracker =>
      !selectedFilters.trackers.includes(tracker)
    )

    if (trackers.length > MAX_INITIAL_ITEMS) {
      const remainingSlots = Math.max(0, MAX_INITIAL_ITEMS - selectedTrackers.length)
      return [...selectedTrackers, ...unselectedTrackers.slice(0, remainingSlots)]
    }

    return trackers
  }, [trackers, debouncedTrackerSearch, selectedFilters.trackers])


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
      className={`${className} h-full w-full xl:max-w-xs flex flex-col xl:flex-shrink-0 xl:border-r xl:bg-muted/10`}
    >
      <ScrollArea className="h-full flex-1 overscroll-contain">
        <div className="p-4">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-semibold">Filters</h3>
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
                        {torrentCounts ? (torrentCounts[`status:${state.value}`] || 0) : "..."}
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

                  {/* Search input for large category lists */}
                  {Object.keys(categories).length > 20 && (
                    <div className="relative mb-2">
                      <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                      <Input
                        placeholder="Search categories..."
                        value={categorySearch}
                        onChange={(e) => setCategorySearch(e.target.value)}
                        className="pl-7 h-7 text-xs"
                      />
                    </div>
                  )}

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
                      {torrentCounts ? (torrentCounts["category:"] || 0) : "..."}
                    </span>
                  </label>

                  {/* Category list - use filtered categories for performance */}
                  {filteredCategories.map(([name, category]: [string, any]) => (
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
                            {incognitoMode ? Math.floor(Math.random() * 50) + 1 : (torrentCounts ? (torrentCounts[`category:${name}`] || 0) : "...")}
                          </span>
                        </label>
                      </ContextMenuTrigger>
                      <ContextMenuContent>
                        <ContextMenuItem
                          onClick={() => {
                            setCategoryToEdit({ name, savePath: category.savePath })
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
                  ))}
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

                  {/* Search input for large tag lists */}
                  {tags.length > 20 && (
                    <div className="relative mb-2">
                      <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                      <Input
                        placeholder="Search tags..."
                        value={tagSearch}
                        onChange={(e) => setTagSearch(e.target.value)}
                        className="pl-7 h-7 text-xs"
                      />
                    </div>
                  )}

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
                      {torrentCounts ? (torrentCounts["tag:"] || 0) : "..."}
                    </span>
                  </label>

                  {/* Tag list - use filtered tags for performance */}
                  {filteredTags.map((tag: string) => (
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
                            {incognitoMode ? Math.floor(Math.random() * 30) + 1 : (torrentCounts ? (torrentCounts[`tag:${tag}`] || 0) : "...")}
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
                  ))}
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
                  {/* Search input for large tracker lists */}
                  {trackers.length > 20 && (
                    <div className="relative mb-2">
                      <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                      <Input
                        placeholder="Search trackers..."
                        value={trackerSearch}
                        onChange={(e) => setTrackerSearch(e.target.value)}
                        className="pl-7 h-7 text-xs"
                      />
                    </div>
                  )}

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
                      {torrentCounts ? (torrentCounts["tracker:"] || 0) : "..."}
                    </span>
                  </label>

                  {/* Tracker list - use filtered trackers for performance */}
                  {filteredTrackers.filter(tracker => tracker !== "").map((tracker) => (
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
                        {incognitoMode ? Math.floor(Math.random() * 100) + 10 : (torrentCounts ? (torrentCounts[`tracker:${tracker}`] || 0) : "...")}
                      </span>
                    </label>
                  ))}
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
    prevProps.className === nextProps.className
  )
})