import { useState, useEffect } from 'react'
import type { SortingState } from '@tanstack/react-table'

export function usePersistedColumnSorting(
  defaultSorting: SortingState = []
) {
  // Global key shared across all instances
  const storageKey = `qui-column-sorting`
  
  // Initialize state from localStorage or default values
  const [sorting, setSorting] = useState<SortingState>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        return JSON.parse(stored)
      }
    } catch (error) {
      console.error('Failed to load column sorting from localStorage:', error)
    }
    
    return defaultSorting
  })
  
  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(sorting))
    } catch (error) {
      console.error('Failed to save column sorting to localStorage:', error)
    }
  }, [sorting, storageKey])
  
  return [sorting, setSorting] as const
}