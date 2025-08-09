// Web Worker for filtering, searching, and sorting torrents in the background

// Types for communication
export interface Torrent {
	hash: string;
	[key: string]: any;
}

export interface WorkerRequest {
	torrents: Torrent[];
	search?: string;
	sort?: { id: string; desc?: boolean }[];
}

export interface WorkerResponse {
	filtered: Torrent[];
}


// Optimized search: pre-processes data for faster lookups, uses early exit patterns
function filterTorrents(torrents: Torrent[], search?: string): Torrent[] {
	if (!search) return torrents;
	const terms = search.toLowerCase().split(/\s+/).filter(Boolean);
	if (terms.length === 0) return torrents;
	
	// Only search in a subset of fields for performance
	const SEARCH_FIELDS = ['name', 'category', 'tags', 'state', 'hash'];
	
	// For exact term matches, use Set for O(1) lookup instead of Array.includes
	const exactMatches = new Set(terms);
	
	return torrents.filter(torrent => {
		// Check exact matches first (much faster)
		for (const field of SEARCH_FIELDS) {
			const value = torrent[field];
			if (!value) continue;
			
			// For exact matches, use Set.has() which is O(1) instead of string.includes() which is O(n)
			if (typeof value === 'string') {
				const valueLower = value.toLowerCase();
				// Early return for exact matches (performance optimization)
				if (exactMatches.has(valueLower)) return true;
				
				// For expensive substring searches, check if term exists in string once
				// instead of checking each term individually (better branch prediction)
				let allTermsMatch = true;
				for (const term of terms) {
					if (!valueLower.includes(term)) {
						allTermsMatch = false;
						break;
					}
				}
				if (allTermsMatch) return true;
			} else if (Array.isArray(value)) {
				// For arrays, check each item individually first (avoids string concatenation)
				for (const item of value) {
					if (typeof item === 'string') {
						const itemLower = item.toLowerCase();
						if (exactMatches.has(itemLower)) return true;
					}
				}
				
				// Fall back to joined approach if needed
				const joined = value.join(' ').toLowerCase();
				let allTermsMatch = true;
				for (const term of terms) {
					if (!joined.includes(term)) {
						allTermsMatch = false;
						break;
					}
				}
				if (allTermsMatch) return true;
			}
		}
		return false;
	});
}


// Optimized stable multi-column sort with type handling and value caching
function sortTorrents(torrents: Torrent[], sort?: { id: string; desc?: boolean }[]): Torrent[] {
	if (!sort || sort.length === 0) return torrents;
	
	// Create a cache for string values to avoid repeated toLowerCase() calls
	// which are expensive when sorting large arrays
	const stringCache = new Map<string, Map<string, string>>();
	
	// Create a shallow copy to avoid mutating the original array
	const result = torrents.slice(0);
	
	result.sort((a, b) => {
		for (const s of sort) {
			const { id, desc } = s;
			
			// Get values and handle special sorting cases
			let aValue = a[id];
			let bValue = b[id];
			
			// Skip if both values are null/undefined
			if (aValue == null && bValue == null) continue;
			if (aValue == null) return 1;
			if (bValue == null) return -1;
			
			// Compare numbers more efficiently
			if (typeof aValue === 'number' && typeof bValue === 'number') {
				const diff = aValue - bValue; // Faster than separate comparisons
				if (diff !== 0) return desc ? -diff : diff;
				continue;
			}
			
			// For string comparison, use cache for toLowerCase() operations
			if (typeof aValue === 'string' && typeof bValue === 'string') {
				// Get or create cache for this property
				let propCache = stringCache.get(id);
				if (!propCache) {
					propCache = new Map<string, string>();
					stringCache.set(id, propCache);
				}
				
				// Get cached lowercase values or compute and cache them
				let aLower = propCache.get(aValue);
				if (!aLower) {
					aLower = aValue.toLowerCase();
					propCache.set(aValue, aLower);
				}
				
				let bLower = propCache.get(bValue);
				if (!bLower) {
					bLower = bValue.toLowerCase();
					propCache.set(bValue, bLower);
				}
				
				// Do the comparison
				if (aLower < bLower) return desc ? 1 : -1;
				if (aLower > bLower) return desc ? -1 : 1;
			} else {
				// Fall back to string comparison for mixed types
				const aStr = String(aValue);
				const bStr = String(bValue);
				if (aStr < bStr) return desc ? 1 : -1;
				if (aStr > bStr) return desc ? -1 : 1;
			}
		}
		return 0;
	});
	
	return result;
}

self.onmessage = function (e) {
	const { torrents, search, sort } = e.data as WorkerRequest;
	let filtered = filterTorrents(torrents, search);
	filtered = sortTorrents(filtered, sort);
	const response: WorkerResponse = { filtered };
	// @ts-ignore
	self.postMessage(response);
};
