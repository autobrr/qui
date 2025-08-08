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

function filterTorrents(torrents: Torrent[], search?: string): Torrent[] {
	if (!search) return torrents;
	const lower = search.toLowerCase();
	return torrents.filter(t =>
		Object.values(t).some(v =>
			typeof v === 'string' && v.toLowerCase().includes(lower)
		)
	);
}

function sortTorrents(torrents: Torrent[], sort?: { id: string; desc?: boolean }[]): Torrent[] {
	if (!sort || sort.length === 0) return torrents;
	const { id, desc } = sort[0];
	return [...torrents].sort((a, b) => {
		const aValue = a[id];
		const bValue = b[id];
		if (aValue == null) return 1;
		if (bValue == null) return -1;
		if (aValue < bValue) return desc ? 1 : -1;
		if (aValue > bValue) return desc ? -1 : 1;
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
