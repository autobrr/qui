package qbittorrent

import (
	"fmt"
	"strings"
	testing "testing"

	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

func buildBenchmarkTorrents(numTorrents, tagsPerTorrent, uniqueTags int) []qbt.Torrent {
	torrents := make([]qbt.Torrent, numTorrents)
	tagPool := make([]string, uniqueTags)
	for i := 0; i < uniqueTags; i++ {
		tagPool[i] = fmt.Sprintf("tag-%03d", i)
	}

	for i := range torrents {
		parts := make([]string, tagsPerTorrent)
		for j := 0; j < tagsPerTorrent; j++ {
			parts[j] = tagPool[(i+j)%uniqueTags]
		}
		torrents[i] = qbt.Torrent{Tags: strings.Join(parts, ", ")}
	}

	return torrents
}

func containsTagNoAllocManual(tags string, target string) bool {
	if tags == "" || target == "" {
		return false
	}

	i := 0
	n := len(tags)
	for i < n {
		for i < n && tags[i] == ' ' {
			i++
		}
		start := i
		for i < n && tags[i] != ',' {
			i++
		}
		end := i
		for end > start && tags[end-1] == ' ' {
			end--
		}

		if end-start == len(target) && tags[start:end] == target {
			return true
		}

		i++
	}

	return false
}

func containsTagSplitSeq(tags string, target string) bool {
	if tags == "" || target == "" {
		return false
	}

	for tag := range strings.SplitSeq(tags, ",") {
		if strings.TrimSpace(tag) == target {
			return true
		}
	}

	return false
}

func filterWithDeleteFunc(torrents []qbt.Torrent, tag string) []qbt.Torrent {
	filtered := append([]qbt.Torrent(nil), torrents...)
	filtered = slices.DeleteFunc(filtered, func(t qbt.Torrent) bool {
		return !containsTagNoAllocManual(t.Tags, tag)
	})
	return filtered
}

func filterWithInPlace(torrents []qbt.Torrent, tag string) []qbt.Torrent {
	filtered := append([]qbt.Torrent(nil), torrents...)
	keep := filtered[:0]
	for _, t := range filtered {
		if containsTagSplitSeq(t.Tags, tag) {
			keep = append(keep, t)
		}
	}

	return keep
}

func BenchmarkContainsTagNoAllocManual(b *testing.B) {
	b.ReportAllocs()
	tagString := "tag-000, tag-050, tag-099"
	for i := 0; i < b.N; i++ {
		if !containsTagNoAllocManual(tagString, "tag-050") {
			b.Fatal("tag not found")
		}
	}
}

func BenchmarkContainsTagSplitSeq(b *testing.B) {
	b.ReportAllocs()
	tagString := "tag-000, tag-050, tag-099"
	for i := 0; i < b.N; i++ {
		if !containsTagSplitSeq(tagString, "tag-050") {
			b.Fatal("tag not found")
		}
	}
}

func BenchmarkFilterSingleTagDeleteFunc(b *testing.B) {
	b.ReportAllocs()
	torrents := buildBenchmarkTorrents(10000, 4, 100)
	for i := 0; i < b.N; i++ {
		_ = filterWithDeleteFunc(torrents, "tag-050")
	}
}

func BenchmarkFilterSingleTagInPlace(b *testing.B) {
	b.ReportAllocs()
	torrents := buildBenchmarkTorrents(10000, 4, 100)
	for i := 0; i < b.N; i++ {
		_ = filterWithInPlace(torrents, "tag-050")
	}
}
