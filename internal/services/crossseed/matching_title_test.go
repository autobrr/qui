package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestReleasesMatch_NonTVRequiresExactTitle(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	base := rls.Release{
		Title: "Test Movie",
		Year:  2025,
	}

	same := rls.Release{
		Title: "Test Movie",
		Year:  2025,
	}

	variantTitle := rls.Release{
		Title: "Test Movie Extended",
		Year:  2025,
	}

	require.True(t, s.releasesMatch(&base, &same, false), "identical non-TV titles should match")
	require.False(t, s.releasesMatch(&base, &variantTitle, false), "non-TV titles must match exactly after normalization")
}

func TestReleasesMatch_NonTVRequiresCompatibleType(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	movie := rls.Release{
		Type:  rls.Movie,
		Title: "Shared Title",
		Year:  2025,
	}

	music := rls.Release{
		Type:  rls.Music,
		Title: "Shared Title",
		Year:  2025,
	}

	unknown := rls.Release{
		Type:  rls.Unknown,
		Title: "Shared Title",
		Year:  2025,
	}

	require.False(t, s.releasesMatch(&movie, &music, false), "movie and music with same title/year should not match")
	require.True(t, s.releasesMatch(&movie, &unknown, false), "unknown type should not block matching when other metadata agrees")
	require.True(t, s.releasesMatch(&unknown, &music, false), "unknown type should not block matching when other metadata agrees")
}

func TestReleasesMatch_ArtistMustMatch(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Different artists with same title should NOT match (regression test for 0day scene)
	adamBeyer := rls.Release{
		Type:   rls.Music,
		Artist: "Adam Beyer",
		Title:  "Dance Department",
		Year:   2025,
		Month:  10,
		Day:    4,
		Source: "CABLE",
		Group:  "TALiON",
	}

	arminVanBuuren := rls.Release{
		Type:   rls.Music,
		Artist: "Armin van Buuren",
		Title:  "Dance Department",
		Year:   2025,
		Month:  11,
		Day:    29,
		Source: "CABLE",
		Group:  "TALiON",
	}

	sameArtist := rls.Release{
		Type:   rls.Music,
		Artist: "Adam Beyer",
		Title:  "Dance Department",
		Year:   2025,
		Month:  10,
		Day:    4,
		Source: "CABLE",
		Group:  "TALiON",
	}

	require.False(t, s.releasesMatch(&adamBeyer, &arminVanBuuren, false),
		"different artists with same title should NOT match")
	require.True(t, s.releasesMatch(&adamBeyer, &sameArtist, false),
		"same artist with same title should match")
}

func TestReleasesMatch_DateBasedReleasesRequireExactDate(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Same year but different month/day should NOT match (0day scene releases)
	oct4 := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Show",
		Year:   2025,
		Month:  10,
		Day:    4,
		Group:  "GROUP",
	}

	nov29 := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Show",
		Year:   2025,
		Month:  11,
		Day:    29,
		Group:  "GROUP",
	}

	sameDate := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Show",
		Year:   2025,
		Month:  10,
		Day:    4,
		Group:  "GROUP",
	}

	// Release with only year (no month/day) - e.g. albums
	yearOnly := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Album",
		Year:   2025,
		Group:  "GROUP",
	}

	require.False(t, s.releasesMatch(&oct4, &nov29, false),
		"same year but different month/day should NOT match")
	require.True(t, s.releasesMatch(&oct4, &sameDate, false),
		"same year/month/day should match")

	// Year-only releases should still be allowed to match each other
	anotherYearOnly := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Album",
		Year:   2025,
		Group:  "GROUP",
	}
	require.True(t, s.releasesMatch(&yearOnly, &anotherYearOnly, false),
		"year-only releases should match when year is same")
}
