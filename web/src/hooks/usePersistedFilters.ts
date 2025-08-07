import { useState, useEffect } from 'react'

interface Filters {
  status: string[]
  categories: string[]
  tags: string[]
  trackers: string[]
}

export function usePersistedFilters() {
  // Global key shared across all instances
  const storageKey = `qui-filters`
  
  // Initialize state from localStorage or default values
  const [filters, setFilters] = useState<Filters>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        return JSON.parse(stored)
      }
    } catch (error) {
      console.error('Failed to load filters from localStorage:', error)
    }
    
    // Default filters
    return {
      status: [],
      categories: [],
      tags: [],
      trackers: [],
    }
  })
  
  // Persist to localStorage whenever filters change
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(filters))
    } catch (error) {
      console.error('Failed to save filters to localStorage:', error)
    }
  }, [filters, storageKey])
  
  return [filters, setFilters] as const
}