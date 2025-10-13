package trackericons

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceBuildBaseCandidatesOrder(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		host        string
		trackerURL  string
		wantFirst   string
		requireBoth bool
	}

	tests := []testCase{
		{
			name:        "no tracker URL defaults to https first",
			host:        "tracker.example",
			trackerURL:  "",
			wantFirst:   "https://tracker.example/",
			requireBoth: true,
		},
		{
			name:        "https tracker preserves scheme priority",
			host:        "tracker.example",
			trackerURL:  "https://tracker.example/announce",
			wantFirst:   "https://tracker.example/",
			requireBoth: true,
		},
		{
			name:        "http tracker stays first",
			host:        "tracker.example",
			trackerURL:  "http://tracker.example/announce",
			wantFirst:   "http://tracker.example/",
			requireBoth: true,
		},
		{
			name:        "non http tracker falls back to https first",
			host:        "tracker.example",
			trackerURL:  "udp://tracker.example:1337/announce",
			wantFirst:   "https://tracker.example:1337/",
			requireBoth: true,
		},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &Service{}
			var got []string

			for _, u := range svc.buildBaseCandidates(tc.host, tc.trackerURL) {
				got = append(got, u.String())
			}

			require.NotEmpty(t, got, "no base candidates returned")
			require.Equal(t, tc.wantFirst, got[0], "unexpected first candidate: full=%v", got)

			if tc.requireBoth {
				var hasHTTP, hasHTTPS bool
				for _, candidate := range got {
					if strings.HasPrefix(candidate, "http://") {
						hasHTTP = true
					}
					if strings.HasPrefix(candidate, "https://") {
						hasHTTPS = true
					}
				}

				require.True(t, hasHTTP && hasHTTPS, "expected both http and https candidates, got %v", got)
			}
		})
	}
}

func TestServiceFetchToggle(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	require.False(t, svc.FetchEnabled())

	svc.SetFetchEnabled(true)
	require.True(t, svc.FetchEnabled())

	svc.SetFetchEnabled(false)
	require.False(t, svc.FetchEnabled())
}
