package qbittorrent

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
)

func TestFilterTorrentsByExactTag_Regression_bcDoesNotMatchAbcd(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "hash-bc", Name: "Only BC", Tags: "bc"},
		{Hash: "hash-abcd", Name: "Only ABCD", Tags: "abcd"},
	}

	filtered := filterTorrentsByExactTag(append([]qbt.Torrent(nil), torrents...), "bc")

	assert.Len(t, filtered, 1, "should keep only torrents with tag 'bc'")
	if assert.NotEmpty(t, filtered) {
		assert.Equal(t, "hash-bc", filtered[0].Hash)
		assert.True(t, containsTagNoAlloc(filtered[0].Tags, "bc"))
	}
}

func TestFilterTorrentsByExactTag_Regression_MultiTagged(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "hash-bc", Name: "Only BC", Tags: "bc"},
		{Hash: "hash-multi", Name: "BC With Others", Tags: "bc, abcd, abcde"},
		{Hash: "hash-abcd", Name: "Only ABCD", Tags: "abcd"},
	}

	filtered := filterTorrentsByExactTag(append([]qbt.Torrent(nil), torrents...), "bc")

	assert.Len(t, filtered, 2, "should keep torrents that contain bc among multiple tags")
	hashes := []string{filtered[0].Hash, filtered[1].Hash}
	assert.ElementsMatch(t, []string{"hash-bc", "hash-multi"}, hashes)
}
