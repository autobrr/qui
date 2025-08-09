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


// Async processing with yielding for non-blocking operations
async function processInBatches<T>(items: T[], batchSize: number, processor: (batch: T[]) => T[]): Promise<T[]> {
	const results: T[] = [];
	
	for (let i = 0; i < items.length; i += batchSize) {
		const batch = items.slice(i, i + batchSize);
		const processed = processor(batch);
		results.push(...processed);
		
		// Yield control back to the main thread every batch
		if (i + batchSize < items.length) {
			await new Promise(resolve => setTimeout(resolve, 0));
		}
	}
	
	return results;
}

// Optimized search with async batching for large datasets
async function filterTorrentsAsync(torrents: Torrent[], search?: string): Promise<Torrent[]> {
	if (!search) return torrents;
	const terms = search.toLowerCase().split(/\s+/).filter(Boolean);
	if (terms.length === 0) return torrents;
	
	// For small datasets, use sync processing
	if (torrents.length < 5000) {
		return filterTorrentsSync(torrents, search);
	}
	
	// For large datasets, process in batches to avoid blocking
	const BATCH_SIZE = 1000;
	const SEARCH_FIELDS = ['name', 'category', 'tags', 'state', 'hash'];
	const exactMatches = new Set(terms);
	
	return processInBatches(torrents, BATCH_SIZE, (batch) => {
		return batch.filter(torrent => {
			for (const field of SEARCH_FIELDS) {
				const value = torrent[field];
				if (!value) continue;
				
				if (typeof value === 'string') {
					const valueLower = value.toLowerCase();
					if (exactMatches.has(valueLower)) return true;
					
					let allTermsMatch = true;
					for (const term of terms) {
						if (!valueLower.includes(term)) {
							allTermsMatch = false;
							break;
						}
					}
					if (allTermsMatch) return true;
				} else if (Array.isArray(value)) {
					for (const item of value) {
						if (typeof item === 'string') {
							const itemLower = item.toLowerCase();
							if (exactMatches.has(itemLower)) return true;
						}
					}
					
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
	});
}

// Synchronous version for smaller datasets
function filterTorrentsSync(torrents: Torrent[], search?: string): Torrent[] {
	if (!search) return torrents;
	const terms = search.toLowerCase().split(/\s+/).filter(Boolean);
	if (terms.length === 0) return torrents;
	
	const SEARCH_FIELDS = ['name', 'category', 'tags', 'state', 'hash'];
	const exactMatches = new Set(terms);
	
	return torrents.filter(torrent => {
		for (const field of SEARCH_FIELDS) {
			const value = torrent[field];
			if (!value) continue;
			
			if (typeof value === 'string') {
				const valueLower = value.toLowerCase();
				if (exactMatches.has(valueLower)) return true;
				
				let allTermsMatch = true;
				for (const term of terms) {
					if (!valueLower.includes(term)) {
						allTermsMatch = false;
						break;
					}
				}
				if (allTermsMatch) return true;
			} else if (Array.isArray(value)) {
				for (const item of value) {
					if (typeof item === 'string') {
						const itemLower = item.toLowerCase();
						if (exactMatches.has(itemLower)) return true;
					}
				}
				
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

// Enhanced async message handling
self.onmessage = async function (e) {
	const { torrents, search, sort } = e.data as WorkerRequest;
	
	try {
		// Use async filtering for better non-blocking behavior
		let filtered = await filterTorrentsAsync(torrents, search);
		filtered = sortTorrents(filtered, sort);
		
		const response: WorkerResponse = { filtered };
		// @ts-ignore
		self.postMessage(response);
	} catch (error) {
		// Fallback to sync processing if async fails
		console.warn('Async processing failed, falling back to sync:', error);
		let filtered = filterTorrentsSync(torrents, search);
		filtered = sortTorrents(filtered, sort);
		
		const response: WorkerResponse = { filtered };
		// @ts-ignore
		self.postMessage(response);
	}
};
