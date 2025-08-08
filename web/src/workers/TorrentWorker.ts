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


// More efficient and flexible search: supports multiple terms, skips large fields, caches lowercased values
function filterTorrents(torrents: Torrent[], search?: string): Torrent[] {
	if (!search) return torrents;
	const terms = search.toLowerCase().split(/\s+/).filter(Boolean);
	if (terms.length === 0) return torrents;
	// Only search in a subset of fields for performance
	const SEARCH_FIELDS = ['name', 'category', 'tags', 'state', 'hash'];
	return torrents.filter(torrent => {
		for (const field of SEARCH_FIELDS) {
			const value = torrent[field];
			if (typeof value === 'string') {
				const lower = value.toLowerCase();
				if (terms.every(term => lower.includes(term))) return true;
			} else if (Array.isArray(value)) {
				// For tags or arrays
				const joined = value.join(' ').toLowerCase();
				if (terms.every(term => joined.includes(term))) return true;
			}
		}
		return false;
	});
}


// Stable, multi-column sort with type handling
function sortTorrents(torrents: Torrent[], sort?: { id: string; desc?: boolean }[]): Torrent[] {
	if (!sort || sort.length === 0) return torrents;
	// Support multi-column sort if needed
	return [...torrents].sort((a, b) => {
		for (const s of sort) {
			const { id, desc } = s;
			let aValue = a[id];
			let bValue = b[id];
			// Normalize for undefined/null
			if (aValue == null && bValue == null) continue;
			if (aValue == null) return 1;
			if (bValue == null) return -1;
			// Compare numbers
			if (typeof aValue === 'number' && typeof bValue === 'number') {
				if (aValue < bValue) return desc ? 1 : -1;
				if (aValue > bValue) return desc ? -1 : 1;
			} else {
				// Compare as strings
				aValue = String(aValue).toLowerCase();
				bValue = String(bValue).toLowerCase();
				if (aValue < bValue) return desc ? 1 : -1;
				if (aValue > bValue) return desc ? -1 : 1;
			}
		}
		return 0;
	});
}

self.onmessage = function (e) {
	const { torrents, search, sort } = e.data as WorkerRequest;
	let filtered = filterTorrents(torrents, search);
	filtered = sortTorrents(filtered, sort);
	const response: WorkerResponse = { filtered };
	// @ts-ignore
	self.postMessage(response);
};
