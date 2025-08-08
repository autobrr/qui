import { useState, useEffect } from 'react'

export function usePersistedAccordion(
  defaultValue: string[] = ['status', 'categories', 'tags']
) {
  // Global key shared across all instances
  const storageKey = `qui-accordion`
  
  // Initialize state from localStorage or default values
  const [expandedItems, setExpandedItems] = useState<string[]>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        return JSON.parse(stored)
      }
    } catch (error) {
      console.error('Failed to load accordion state from localStorage:', error)
    }
    
    return defaultValue
  })
  
  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(expandedItems))
    } catch (error) {
      console.error('Failed to save accordion state to localStorage:', error)
    }
  }, [expandedItems, storageKey])
  
  return [expandedItems, setExpandedItems] as const
}