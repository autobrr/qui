// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"fmt"
	"runtime"
	"testing"
)

// Simulated torrent file structure (mirrors qbt.TorrentFiles element)
type testTorrentFile struct {
	Name     string
	Size     int64
	Index    int
	Priority int
}

// Simulated torrent structure (mirrors key string fields from qbt.Torrent)
type testTorrent struct {
	Hash         string
	Name         string
	Category     string
	SavePath     string
	DownloadPath string
	ContentPath  string
	Tags         string
	State        string
	Tracker      string
	MagnetURI    string
	Comment      string
	CreatedBy    string
}

// Common values that repeat across many torrents
var (
	categories   = []string{"movies", "tv", "music", "software", "games", "ebooks", "xxx"}
	savePaths    = []string{"/data/downloads", "/data/media/movies", "/data/media/tv", "/data/media/music", "/mnt/storage/torrents"}
	states       = []string{"uploading", "downloading", "pausedUP", "pausedDL", "stalledUP", "stalledDL", "checkingUP", "queuedUP"}
	trackers     = []string{"https://tracker1.example.com/announce", "https://tracker2.example.org/announce", "udp://tracker3.net:6969"}
	tagSets      = []string{"cross-seed", "radarr", "sonarr", "lidarr", "manual", "private", "public"}
	createdBySet = []string{"qBittorrent", "Transmission", "rTorrent", "Deluge", "libtorrent"}
)

// generateTorrents creates n test torrents with realistic field repetition patterns
func generateTorrents(n int) []testTorrent {
	torrents := make([]testTorrent, n)
	for i := 0; i < n; i++ {
		torrents[i] = testTorrent{
			Hash:         fmt.Sprintf("%040x", i), // Unique hash per torrent
			Name:         fmt.Sprintf("Ubuntu.22.04.LTS.x64.ISO-%d", i%100),
			Category:     categories[i%len(categories)],
			SavePath:     savePaths[i%len(savePaths)],
			DownloadPath: savePaths[i%len(savePaths)],
			ContentPath:  savePaths[i%len(savePaths)] + "/content",
			Tags:         tagSets[i%len(tagSets)],
			State:        states[i%len(states)],
			Tracker:      trackers[i%len(trackers)],
			MagnetURI:    fmt.Sprintf("magnet:?xt=urn:btih:%040x", i),
			Comment:      "Downloaded from example.com",
			CreatedBy:    createdBySet[i%len(createdBySet)],
		}
	}
	return torrents
}

// generateTorrentFiles creates realistic file lists for torrents
// Season pack with 10 episodes, each with similar naming patterns
func generateTorrentFiles(n int) []testTorrentFile {
	files := make([]testTorrentFile, n)
	for i := 0; i < n; i++ {
		files[i] = testTorrentFile{
			Name:     fmt.Sprintf("Show.Name.S01E%02d.1080p.WEB-DL.x264-GROUP.mkv", (i%10)+1),
			Size:     1_500_000_000 + int64(i%100)*1_000_000,
			Index:    i,
			Priority: 1,
		}
	}
	return files
}

// generateAttributeMaps creates attribute maps similar to Torznab result attributes
func generateAttributeMaps(n int) []map[string]string {
	keys := []string{"category", "size", "files", "grabs", "seeders", "peers", "infohash", "minimumratio", "minimumseedtime", "downloadvolumefactor", "uploadvolumefactor"}
	maps := make([]map[string]string, n)
	for i := 0; i < n; i++ {
		m := make(map[string]string, len(keys))
		for j, k := range keys {
			m[k] = fmt.Sprintf("value_%d_%d", j, i%10) // Values repeat every 10 items
		}
		maps[i] = m
	}
	return maps
}

// BenchmarkInternTorrentFields benchmarks interning typical torrent string fields
func BenchmarkInternTorrentFields(b *testing.B) {
	torrents := generateTorrents(10000)

	b.Run("NoIntern", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, t := range torrents {
				_ = t.Category
				_ = t.SavePath
				_ = t.State
				_ = t.Tracker
				_ = t.Tags
			}
		}
	})

	b.Run("WithIntern", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, t := range torrents {
				_ = Intern(t.Category)
				_ = Intern(t.SavePath)
				_ = Intern(t.State)
				_ = Intern(t.Tracker)
				_ = Intern(t.Tags)
			}
		}
	})
}

// BenchmarkInternStringMap benchmarks interning attribute maps
func BenchmarkInternStringMap(b *testing.B) {
	maps := generateAttributeMaps(1000)

	b.Run("NoIntern", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, m := range maps {
				// Simulate copying map without interning
				result := make(map[string]string, len(m))
				for k, v := range m {
					result[k] = v
				}
				_ = result
			}
		}
	})

	b.Run("WithInternStringMap", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, m := range maps {
				_ = InternStringMap(m)
			}
		}
	})
}

// BenchmarkMemoryUsage measures actual memory savings from interning
// by simulating repeated access patterns where the same strings appear many times
func BenchmarkMemoryUsage(b *testing.B) {
	// Test with 50k torrents (realistic for large users)
	const numTorrents = 50000

	b.Run("WithoutInterning_Storage", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Simulate storing torrent data without interning
			// Each string is a separate allocation copied from source
			storage := make([]map[string]string, numTorrents)
			for j := 0; j < numTorrents; j++ {
				// Force new string allocations by concatenating
				storage[j] = map[string]string{
					"category":   string([]byte(categories[j%len(categories)])),
					"save_path":  string([]byte(savePaths[j%len(savePaths)])),
					"state":      string([]byte(states[j%len(states)])),
					"tracker":    string([]byte(trackers[j%len(trackers)])),
					"tags":       string([]byte(tagSets[j%len(tagSets)])),
					"created_by": string([]byte(createdBySet[j%len(createdBySet)])),
				}
			}
			runtime.KeepAlive(storage)
		}
	})

	b.Run("WithInterning_Storage", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Pre-intern common values (simulates startup interning)
			internedCategories := InternAll(categories)
			internedSavePaths := InternAll(savePaths)
			internedStates := InternAll(states)
			internedTrackers := InternAll(trackers)
			internedTags := InternAll(tagSets)
			internedCreatedBy := InternAll(createdBySet)

			// Now store using interned values - each map reuses the same string pointers
			storage := make([]map[string]string, numTorrents)
			for j := 0; j < numTorrents; j++ {
				storage[j] = map[string]string{
					"category":   internedCategories[j%len(internedCategories)],
					"save_path":  internedSavePaths[j%len(internedSavePaths)],
					"state":      internedStates[j%len(internedStates)],
					"tracker":    internedTrackers[j%len(internedTrackers)],
					"tags":       internedTags[j%len(internedTags)],
					"created_by": internedCreatedBy[j%len(internedCreatedBy)],
				}
			}
			runtime.KeepAlive(storage)
		}
	})
}

// BenchmarkHandleVsStringEquality benchmarks Handle vs string equality at scale
func BenchmarkHandleVsStringEquality(b *testing.B) {
	categories := []string{"movies", "tv", "music", "software", "games"}
	// Create many strings that equal these categories
	testStrings := make([]string, 10000)
	for i := range testStrings {
		testStrings[i] = categories[i%len(categories)]
	}

	b.Run("StringEquality", func(b *testing.B) {
		target := "movies"
		count := 0
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, s := range testStrings {
				if s == target {
					count++
				}
			}
		}
		_ = count
	})

	b.Run("HandleEquality", func(b *testing.B) {
		// Pre-create handles
		handles := make([]Handle, len(testStrings))
		for i, s := range testStrings {
			handles[i] = MakeHandle(s)
		}
		target := MakeHandle("movies")
		count := 0
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, h := range handles {
				if h == target {
					count++
				}
			}
		}
		_ = count
	})
}

// BenchmarkInternAll benchmarks batch interning of string slices
func BenchmarkInternAll(b *testing.B) {
	// Simulate file names from a season pack
	files := make([]string, 1000)
	for i := range files {
		files[i] = fmt.Sprintf("Show.S01E%02d.1080p.WEB-DL.mkv", (i%10)+1)
	}

	b.Run("Individual", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			result := make([]string, len(files))
			for j, f := range files {
				result[j] = Intern(f)
			}
			_ = result
		}
	})

	b.Run("Batch", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = InternAll(files)
		}
	})
}
