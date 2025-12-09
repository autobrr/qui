package crossseed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldEnableAutoTMM(t *testing.T) {
	tests := []struct {
		name                   string
		crossCategory          string
		matchedAutoManaged     bool
		useCategoryFromIndexer bool
		actualCategorySavePath string
		matchedSavePath        string
		wantEnabled            bool
		wantPathsMatch         bool
	}{
		{
			name:                   "all conditions met - enabled",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "/downloads/tv",
			matchedSavePath:        "/downloads/tv",
			wantEnabled:            true,
			wantPathsMatch:         true,
		},
		{
			name:                   "no cross category - disabled",
			crossCategory:          "",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "/downloads/tv",
			matchedSavePath:        "/downloads/tv",
			wantEnabled:            false,
			wantPathsMatch:         true,
		},
		{
			name:                   "matched not auto managed - disabled",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     false,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "/downloads/tv",
			matchedSavePath:        "/downloads/tv",
			wantEnabled:            false,
			wantPathsMatch:         true,
		},
		{
			name:                   "using indexer category - disabled",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: true,
			actualCategorySavePath: "/downloads/tv",
			matchedSavePath:        "/downloads/tv",
			wantEnabled:            false,
			wantPathsMatch:         true,
		},
		{
			name:                   "paths do not match - disabled",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "/downloads/tv",
			matchedSavePath:        "/downloads/movies",
			wantEnabled:            false,
			wantPathsMatch:         false,
		},
		{
			name:                   "category save path empty - disabled",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "",
			matchedSavePath:        "/downloads/tv",
			wantEnabled:            false,
			wantPathsMatch:         false,
		},
		{
			name:                   "matched save path empty - disabled",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "/downloads/tv",
			matchedSavePath:        "",
			wantEnabled:            false,
			wantPathsMatch:         false,
		},
		{
			name:                   "both paths empty - disabled",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "",
			matchedSavePath:        "",
			wantEnabled:            false,
			wantPathsMatch:         false,
		},
		{
			name:                   "paths match with trailing slash normalization",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "/downloads/tv/",
			matchedSavePath:        "/downloads/tv",
			wantEnabled:            true,
			wantPathsMatch:         true,
		},
		{
			name:                   "paths match with backslash normalization",
			crossCategory:          "tv.cross",
			matchedAutoManaged:     true,
			useCategoryFromIndexer: false,
			actualCategorySavePath: "C:\\downloads\\tv",
			matchedSavePath:        "C:/downloads/tv",
			wantEnabled:            true,
			wantPathsMatch:         true,
		},
		{
			name:                   "all conditions false",
			crossCategory:          "",
			matchedAutoManaged:     false,
			useCategoryFromIndexer: true,
			actualCategorySavePath: "",
			matchedSavePath:        "",
			wantEnabled:            false,
			wantPathsMatch:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEnableAutoTMM(
				tt.crossCategory,
				tt.matchedAutoManaged,
				tt.useCategoryFromIndexer,
				tt.actualCategorySavePath,
				tt.matchedSavePath,
			)

			assert.Equal(t, tt.wantEnabled, got.Enabled, "Enabled")
			assert.Equal(t, tt.wantPathsMatch, got.PathsMatch, "PathsMatch")
		})
	}
}
