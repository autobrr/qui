import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`
}

export function formatSpeed(bytesPerSecond: number, compact: boolean = false): string {
  if (!bytesPerSecond || bytesPerSecond === 0) return compact ? '0' : '0 B/s'
  const k = 1024
  const sizes = compact ? ['B', 'KiB', 'MiB', 'GiB'] : ['B/s', 'KiB/s', 'MiB/s', 'GiB/s']
  const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k))
  const value = parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(i > 0 ? 1 : 0))
  return `${value}${sizes[i]}`
}

export function formatTimestamp(timestamp: number): string {
  if (!timestamp || timestamp === 0) return 'N/A'
  return new Date(timestamp * 1000).toLocaleString()
}

/**
 * Get the appropriate color for a torrent ratio based on predefined thresholds
 * @param ratio - The ratio value (uploaded/downloaded)
 * @returns CSS custom property string for the appropriate color
 */
export function getRatioColor(ratio: number): string {
  if (ratio < 0) return ''
  
  if (ratio < 0.5) {
    return 'var(--chart-5)' // very bad - lowest/darkest
  } else if (ratio < 1.0) {
    return 'var(--chart-4)' // bad - below 1.0
  } else if (ratio < 2.0) {
    return 'var(--chart-3)' // okay - above 1.0
  } else if (ratio < 5.0) {
    return 'var(--chart-2)' // good - healthy ratio
  } else {
    return 'var(--chart-1)' // excellent - best ratio
  }
}

export function formatDuration(seconds: number): string {
  if (seconds === 0) return '0s'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60
  
  const parts = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0) parts.push(`${hours}h`)
  if (minutes > 0) parts.push(`${minutes}m`)
  if (secs > 0) parts.push(`${secs}s`)
  
  return parts.join(' ')
}

export function debounce<T extends (...args: any[]) => any>(
  func: T,
  wait: number
): (...args: Parameters<T>) => void {
  let timeout: ReturnType<typeof setTimeout>
  return (...args: Parameters<T>) => {
    clearTimeout(timeout)
    timeout = setTimeout(() => func(...args), wait)
  }
}