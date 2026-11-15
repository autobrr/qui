// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/livesamples"
)

type fakeLiveSamples livesamples.Window

func (f fakeLiveSamples) Samples() livesamples.Window { return livesamples.Window(f) }

func TestLiveSamplesListsTheSamplesOfEachInstance(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		window livesamples.Window
		want   string
	}{
		{
			name: "two instances",
			window: livesamples.Window{Now: now, Instances: []livesamples.InstanceSamples{
				{InstanceID: 1, Samples: []livesamples.Sample{{Time: now.Add(-4 * time.Second), Up: 3743000, Down: 0}, {Time: now.Add(-2 * time.Second), Up: 3000000, Down: 12}}},
				{InstanceID: 3, Samples: []livesamples.Sample{{Time: now.Add(-time.Minute), Up: 0, Down: 0}}},
			}},
			want: `{"now":"2026-11-15T12:00:00Z","instances":[
				{"instanceId":1,"samples":[{"time":"2026-11-15T11:59:56Z","up":3743000,"down":0},{"time":"2026-11-15T11:59:58Z","up":3000000,"down":12}]},
				{"instanceId":3,"samples":[{"time":"2026-11-15T11:59:00Z","up":0,"down":0}]}]}`,
		},
		{
			name:   "no instance",
			window: livesamples.Window{Now: now, Instances: []livesamples.InstanceSamples{}},
			want:   `{"now":"2026-11-15T12:00:00Z","instances":[]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			(&LiveSamplesHandler{recorder: fakeLiveSamples(tc.window)}).List(rec, httptest.NewRequest(http.MethodGet, "/api/live-samples", nil))

			require.Equal(t, http.StatusOK, rec.Code)
			require.JSONEq(t, tc.want, rec.Body.String())
		})
	}
}
