import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'
import { usePersistedAccordion } from '@/hooks/usePersistedAccordion'
import { api } from '@/lib/api'
import {
  Circle,
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
  type LucideIcon,
} from 'lucide-react'
import {
  CreateTagDialog,
  DeleteTagDialog,
  CreateCategoryDialog,
  EditCategoryDialog,
  DeleteCategoryDialog,
  DeleteUnusedTagsDialog,
} from './TagCategoryManagement'
import { LINUX_CATEGORIES, LINUX_TAGS, LINUX_TRACKERS, useIncognitoMode } from '@/lib/incognito'

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
}


// Define torrent states based on qBittorrent
const TORRENT_STATES: Array<{ value: string; label: string; icon: LucideIcon }> = [
  { value: 'all', label: 'All', icon: Circle },
  { value: 'downloading', label: 'Downloading', icon: Download },
  { value: 'seeding', label: 'Seeding', icon: Upload },
  { value: 'completed', label: 'Completed', icon: CheckCircle2 },
  { value: 'paused', label: 'Paused', icon: PauseCircle },
  { value: 'active', label: 'Active', icon: PlayCircle },
  { value: 'inactive', label: 'Inactive', icon: StopCircle },
  { value: 'resumed', label: 'Resumed', icon: PlayCircle },
  { value: 'stalled', label: 'Stalled', icon: AlertCircle },
  { value: 'stalled_uploading', label: 'Stalled Uploading', icon: AlertCircle },
  { value: 'stalled_downloading', label: 'Stalled Downloading', icon: AlertCircle },
  { value: 'errored', label: 'Error', icon: XCircle },
  { value: 'checking', label: 'Checking', icon: RotateCw },
  { value: 'moving', label: 'Moving', icon: MoveRight },
]

export function FilterSidebar({
  instanceId,
  selectedFilters,
  onFilterChange,
  torrentCounts = {},
}: FilterSidebarProps) {
  // Use incognito mode hook
  const [incognitoMode] = useIncognitoMode()
  
  // Persist accordion state
  const [expandedItems, setExpandedItems] = usePersistedAccordion(instanceId)
  
  // Dialog states
  const [showCreateTagDialog, setShowCreateTagDialog] = useState(false)
  const [showDeleteTagDialog, setShowDeleteTagDialog] = useState(false)
  const [showDeleteUnusedTagsDialog, setShowDeleteUnusedTagsDialog] = useState(false)
  const [tagToDelete, setTagToDelete] = useState('')
  
  const [showCreateCategoryDialog, setShowCreateCategoryDialog] = useState(false)
  const [showEditCategoryDialog, setShowEditCategoryDialog] = useState(false)
  const [showDeleteCategoryDialog, setShowDeleteCategoryDialog] = useState(false)
  const [categoryToEdit, setCategoryToEdit] = useState<{ name: string; savePath: string } | null>(null)
  const [categoryToDelete, setCategoryToDelete] = useState('')

  // Fetch categories
  const { data: realCategories = {} } = useQuery({
    queryKey: ['categories', instanceId],
    queryFn: () => api.getCategories(instanceId),
    staleTime: 60000, // 1 minute
  })

  // Fetch tags
  const { data: realTags = [] } = useQuery({
    queryKey: ['tags', instanceId],
    queryFn: () => api.getTags(instanceId),
    staleTime: 60000, // 1 minute
  })
  
  // Use fake data if in incognito mode
  const categories = useMemo(() => {
    return incognitoMode ? LINUX_CATEGORIES : realCategories
  }, [incognitoMode, realCategories])
  
  const tags = useMemo(() => {
    return incognitoMode ? LINUX_TAGS : realTags
  }, [incognitoMode, realTags])


  const handleStatusToggle = (status: string) => {
    const newStatus = selectedFilters.status.includes(status)
      ? selectedFilters.status.filter(s => s !== status)
      : [...selectedFilters.status, status]
    
    onFilterChange({
      ...selectedFilters,
      status: newStatus,
    })
  }

  const handleCategoryToggle = (category: string) => {
    const newCategories = selectedFilters.categories.includes(category)
      ? selectedFilters.categories.filter(c => c !== category)
      : [...selectedFilters.categories, category]
    
    onFilterChange({
      ...selectedFilters,
      categories: newCategories,
    })
  }

  const handleTagToggle = (tag: string) => {
    const newTags = selectedFilters.tags.includes(tag)
      ? selectedFilters.tags.filter(t => t !== tag)
      : [...selectedFilters.tags, tag]
    
    onFilterChange({
      ...selectedFilters,
      tags: newTags,
    })
  }

  const handleTrackerToggle = (tracker: string) => {
    const newTrackers = selectedFilters.trackers.includes(tracker)
      ? selectedFilters.trackers.filter(t => t !== tracker)
      : [...selectedFilters.trackers, tracker]
    
    onFilterChange({
      ...selectedFilters,
      trackers: newTrackers,
    })
  }

  // Extract unique trackers from torrentCounts
  const realTrackers = Object.keys(torrentCounts)
    .filter(key => key.startsWith('tracker:'))
    .map(key => key.replace('tracker:', ''))
    .filter(tracker => torrentCounts[`tracker:${tracker}`] > 0)
    .sort()
  
  // Use fake trackers if in incognito mode
  const trackers = useMemo(() => {
    return incognitoMode ? LINUX_TRACKERS : realTrackers
  }, [incognitoMode, realTrackers])

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

  const hasActiveFilters = 
    selectedFilters.status.length > 0 ||
    selectedFilters.categories.length > 0 ||
    selectedFilters.tags.length > 0 ||
    selectedFilters.trackers.length > 0

  return (
    <div className="w-full h-full flex flex-col xl:min-w-fit xl:max-w-xs xl:flex-shrink-0 xl:border-r xl:bg-muted/10">
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
                    <Badge variant="secondary" className="ml-2 h-5 px-1.5 text-xs">
                      {selectedFilters.status.length}
                    </Badge>
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
                        {torrentCounts[`status:${state.value}`] || 0}
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
                    <Badge variant="secondary" className="ml-2 h-5 px-1.5 text-xs">
                      {selectedFilters.categories.length}
                    </Badge>
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
                  
                  {/* Uncategorized option */}
                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                    <Checkbox
                      checked={selectedFilters.categories.includes('')}
                      onCheckedChange={() => handleCategoryToggle('')}
                      className="rounded border-input"
                    />
                    <span className="text-sm flex-1 italic text-muted-foreground">
                      Uncategorized
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {torrentCounts['category:'] || 0}
                    </span>
                  </label>
                  
                  {/* Category list */}
                  {Object.entries(categories).map(([name, category]) => (
                    <ContextMenu key={name}>
                      <ContextMenuTrigger asChild>
                        <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                          <Checkbox
                            checked={selectedFilters.categories.includes(name)}
                            onCheckedChange={() => handleCategoryToggle(name)}
                          />
                          <span className="text-sm flex-1 truncate" title={name}>
                            {name}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            {incognitoMode ? Math.floor(Math.random() * 50) + 1 : (torrentCounts[`category:${name}`] || 0)}
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
                    <Badge variant="secondary" className="ml-2 h-5 px-1.5 text-xs">
                      {selectedFilters.tags.length}
                    </Badge>
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
                  
                  {/* Untagged option */}
                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                    <Checkbox
                      checked={selectedFilters.tags.includes('')}
                      onCheckedChange={() => handleTagToggle('')}
                      className="rounded border-input"
                    />
                    <span className="text-sm flex-1 italic text-muted-foreground">
                      Untagged
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {torrentCounts['tag:'] || 0}
                    </span>
                  </label>
                  
                  {/* Tag list */}
                  {tags.map((tag) => (
                    <ContextMenu key={tag}>
                      <ContextMenuTrigger asChild>
                        <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                          <Checkbox
                            checked={selectedFilters.tags.includes(tag)}
                            onCheckedChange={() => handleTagToggle(tag)}
                          />
                          <span className="text-sm flex-1 truncate" title={tag}>
                            {tag}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            {incognitoMode ? Math.floor(Math.random() * 30) + 1 : (torrentCounts[`tag:${tag}`] || 0)}
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
                    <Badge variant="secondary" className="ml-2 h-5 px-1.5 text-xs">
                      {selectedFilters.trackers.length}
                    </Badge>
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-2">
                <div className="space-y-1">
                  {/* No tracker option */}
                  <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer">
                    <Checkbox
                      checked={selectedFilters.trackers.includes('')}
                      onCheckedChange={() => handleTrackerToggle('')}
                      className="rounded border-input"
                    />
                    <span className="text-sm flex-1 italic text-muted-foreground">
                      No tracker
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {torrentCounts['tracker:'] || 0}
                    </span>
                  </label>
                  
                  {/* Tracker list */}
                  {trackers.filter(tracker => tracker !== '').map((tracker) => (
                    <label 
                      key={tracker} 
                      className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer"
                    >
                      <Checkbox
                        checked={selectedFilters.trackers.includes(tracker)}
                        onCheckedChange={() => handleTrackerToggle(tracker)}
                      />
                      <span className="text-sm flex-1 truncate" title={tracker}>
                        {tracker}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {incognitoMode ? Math.floor(Math.random() * 100) + 10 : (torrentCounts[`tracker:${tracker}`] || 0)}
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