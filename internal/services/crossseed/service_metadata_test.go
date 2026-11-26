// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/metadata"
)

func TestMetadataCredentialsFingerprint_IsOneWay(t *testing.T) {
	t.Parallel()

	fingerprint := metadataCredentialsFingerprint("api-key", "1234")

	require.Len(t, fingerprint, 64)
	require.NotContains(t, fingerprint, "api-key")
	require.NotContains(t, fingerprint, "1234")
	require.Equal(t, fingerprint, metadataCredentialsFingerprint("api-key", "1234"))
	require.NotEqual(t, fingerprint, metadataCredentialsFingerprint("api-key", "5678"))
}

func TestGetMetadataService_SkipsCredentialLoadWhenRevisionUnchanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	revision := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	loaderCalls := 0
	svc := &Service{
		metadataService: metadata.NewService("", ""),
		metadataCredsRevisionLoader: func(context.Context) (time.Time, error) {
			return revision, nil
		},
		metadataCredentialLoader: func(context.Context) (string, string, error) {
			loaderCalls++
			return "", "", nil
		},
		metadataCredsRevision: revision,
	}

	got := svc.getMetadataService(ctx)

	require.NotNil(t, got)
	require.Equal(t, 0, loaderCalls)
}

func TestGetMetadataService_RefreshesServiceWhenRevisionChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	oldRevision := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	newRevision := oldRevision.Add(time.Hour)
	oldService := metadata.NewService("", "")
	loaderCalls := 0
	svc := &Service{
		metadataService: oldService,
		metadataCredsRevisionLoader: func(context.Context) (time.Time, error) {
			return newRevision, nil
		},
		metadataCredentialLoader: func(context.Context) (string, string, error) {
			loaderCalls++
			return "api-key", "1234", nil
		},
		metadataCredsRevision: oldRevision,
	}

	got := svc.getMetadataService(ctx)

	require.NotNil(t, got)
	require.NotSame(t, oldService, got)
	require.True(t, got.HasTVDB())
	require.Equal(t, 1, loaderCalls)
	require.Equal(t, newRevision, svc.metadataCredsRevision)
	require.Equal(t, metadataCredentialsFingerprint("api-key", "1234"), svc.metadataCredsFingerprint)
}

func TestGetMetadataService_DoesNotReplaceNewerCachedRevisionWithOlderSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	olderRevision := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	newerRevision := olderRevision.Add(time.Hour)
	newerService := metadata.NewService("newer-api-key", "9999")
	loaderCalls := 0
	svc := &Service{
		metadataService: newerService,
		metadataCredsRevisionLoader: func(context.Context) (time.Time, error) {
			return olderRevision, nil
		},
		metadataCredentialLoader: func(context.Context) (string, string, error) {
			loaderCalls++
			return "older-api-key", "1234", nil
		},
		metadataCredsRevision:    newerRevision,
		metadataCredsFingerprint: metadataCredentialsFingerprint("newer-api-key", "9999"),
	}

	got := svc.getMetadataService(ctx)

	require.Same(t, newerService, got)
	require.Equal(t, 1, loaderCalls)
	require.Equal(t, newerRevision, svc.metadataCredsRevision)
	require.Equal(t, metadataCredentialsFingerprint("newer-api-key", "9999"), svc.metadataCredsFingerprint)
	require.True(t, got.HasTVDB())
}

type tvMazeTransport struct{}

func (tvMazeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	body := `{"id":1}`
	if strings.HasSuffix(r.URL.Path, "/episodes") {
		body = `[{"season":1},{"season":1},{"season":1}]`
	}
	_, _ = recorder.WriteString(body)
	return recorder.Result(), nil
}

func TestSeasonTotalComesFromTVMazeOnlyWhenSonarrHasNone(t *testing.T) {
	transport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = transport })
	http.DefaultTransport = tvMazeTransport{}
	for _, tt := range []struct{ sonarr, want int }{{sonarr: 5, want: 5}, {sonarr: 0, want: 3}} {
		svc := &Service{
			releaseCache:    NewReleaseCache(),
			arrService:      &spyARRLookupService{seasonResult: &arr.SeasonEpisodeTotalResult{TotalEpisodes: tt.sonarr}},
			metadataService: metadata.NewService("", ""),
		}
		season := svc.lookupSeasonPackEpisodeTotal(t.Context(), packShow, svc.matcher().parseSeasonPackName(packShow))

		require.Equal(t, seasonLookup{total: tt.want, fromSonarr: true}, season)
	}
}
