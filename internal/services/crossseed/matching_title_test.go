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
