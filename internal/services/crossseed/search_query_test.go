package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"
)

func TestBuildSafeSearchQuery_AnimeAbsolute(t *testing.T) {
	name := "[Fansub] Example Show - 1140 (1080p) [EEC80774]"
	release := rls.Release{Type: rls.Unknown}

	q := buildSafeSearchQuery(name, release, "")

	require.Equal(t, "example show 1140", q.Query)
	require.Nil(t, q.Season)
	require.NotNil(t, q.Episode)
	require.Equal(t, 1140, *q.Episode)
}

func TestBuildSafeSearchQuery_KeepsParsedTitle(t *testing.T) {
	release := rls.Release{
		Type:    rls.Episode,
		Title:   "Some Show",
		Series:  1,
		Episode: 2,
	}

	q := buildSafeSearchQuery("Some.Show.S01E02.mkv", release, release.Title)

	require.Equal(t, "Some Show", q.Query)
	require.NotNil(t, q.Season)
	require.NotNil(t, q.Episode)
	require.Equal(t, 1, *q.Season)
	require.Equal(t, 2, *q.Episode)
}

func TestParseEpisodeNumber_FiltersResolutionAndYear(t *testing.T) {
	require.Equal(t, 0, parseEpisodeNumber("1080"))
	require.Equal(t, 0, parseEpisodeNumber("2025"))
	require.Equal(t, 999, parseEpisodeNumber("999"))
}
