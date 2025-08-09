import { useState, useMemo, useCallback, memo, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { Search } from 'lucide-react'
import { useDebounce } from '@/hooks/useDebounce'

interface FilterItem {
  key: string
  label: string
  count: number
  selected: boolean
}

interface VirtualizedFilterListProps {
  items: FilterItem[]
  onToggle: (key: string) => void
  height: number
  maxVisibleItems?: number
  searchPlaceholder?: string
  emptyLabel?: string
  showSearch?: boolean
}

const ITEM_HEIGHT = 36 // Height of each filter item in pixels

const FilterListItem = memo(({ index, items, onToggle }: { 
  index: number
  items: FilterItem[]
  onToggle: (key: string) => void
}) => {
  const item = items[index]
  
  if (!item) return null

  return (
    <label className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer mx-1">
      <Checkbox
        checked={item.selected}
        onCheckedChange={() => onToggle(item.key)}
        className="rounded border-input"
      />
      <span className="text-sm flex-1 truncate" title={item.label}>
        {item.label}
      </span>
      <span className="text-xs text-muted-foreground">
        {item.count}
      </span>
    </label>
  )
})

FilterListItem.displayName = 'FilterListItem'

export const VirtualizedFilterList = memo(function VirtualizedFilterList({
  items,
  onToggle,
  height,
  maxVisibleItems = 500, // Only render first 500 items initially
  searchPlaceholder = "Search...",
  emptyLabel = "No items",
  showSearch = true
}: VirtualizedFilterListProps) {
  const [searchTerm, setSearchTerm] = useState('')
  const debouncedSearch = useDebounce(searchTerm, 200) // Shorter debounce for filtering
  const parentRef = useRef<HTMLDivElement>(null)

  // Filter and limit items for performance
  const filteredItems = useMemo(() => {
    let filtered = items
    
    // Apply search filter
    if (debouncedSearch) {
      const searchLower = debouncedSearch.toLowerCase()
      filtered = items.filter(item => 
        item.label.toLowerCase().includes(searchLower)
      )
    }
    
    // For very large lists, limit the initial render to improve performance
    // Show all selected items first, then unselected up to the limit
    const selectedItems = filtered.filter(item => item.selected)
    const unselectedItems = filtered.filter(item => !item.selected)
    
    if (!debouncedSearch && filtered.length > maxVisibleItems) {
      // Only limit when not searching, to ensure search results are complete
      const limitedUnselected = unselectedItems.slice(0, maxVisibleItems - selectedItems.length)
      return [...selectedItems, ...limitedUnselected]
    }
    
    return filtered
  }, [items, debouncedSearch, maxVisibleItems])

  const handleToggle = useCallback((key: string) => {
    onToggle(key)
  }, [onToggle])

  // Use virtualization for large lists
  const virtualizer = useVirtualizer({
    count: filteredItems.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ITEM_HEIGHT,
    overscan: 5,
  })

  if (items.length === 0) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        {emptyLabel}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {showSearch && items.length > 20 && (
        <div className="relative">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
          <Input
            placeholder={searchPlaceholder}
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-7 h-7 text-xs"
          />
        </div>
      )}
      
      {filteredItems.length === 0 ? (
        <div className="text-sm text-muted-foreground text-center py-4">
          No matches found
        </div>
      ) : (
        <>
          {filteredItems.length > 100 ? (
            // Use virtualization for large lists
            <div 
              ref={parentRef}
              className="relative overflow-auto"
              style={{ height: Math.min(height, filteredItems.length * ITEM_HEIGHT) }}
            >
              <div
                style={{
                  height: `${virtualizer.getTotalSize()}px`,
                  width: '100%',
                  position: 'relative',
                }}
              >
                {virtualizer.getVirtualItems().map(virtualRow => (
                  <div
                    key={virtualRow.index}
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      height: `${virtualRow.size}px`,
                      transform: `translateY(${virtualRow.start}px)`,
                    }}
                  >
                    <FilterListItem
                      index={virtualRow.index}
                      items={filteredItems}
                      onToggle={handleToggle}
                    />
                  </div>
                ))}
              </div>
            </div>
          ) : (
            // Render normally for smaller lists
            <div className="space-y-1" style={{ maxHeight: height, overflowY: 'auto' }}>
              {filteredItems.map((item) => (
                <label 
                  key={item.key}
                  className="flex items-center space-x-2 py-1 px-2 hover:bg-muted rounded cursor-pointer"
                >
                  <Checkbox
                    checked={item.selected}
                    onCheckedChange={() => handleToggle(item.key)}
                    className="rounded border-input"
                  />
                  <span className="text-sm flex-1 truncate" title={item.label}>
                    {item.label}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {item.count}
                  </span>
                </label>
              ))}
            </div>
          )}
          
          {/* Show info about limited results */}
          {!debouncedSearch && items.length > maxVisibleItems && filteredItems.length >= maxVisibleItems && (
            <div className="text-xs text-muted-foreground text-center py-1">
              Showing {filteredItems.length} of {items.length} items. Use search to find more.
            </div>
          )}
        </>
      )}
    </div>
  )
})
