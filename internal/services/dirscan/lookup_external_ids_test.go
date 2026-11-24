// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestLookupExternalIDsUsesAPartialAnswer(t *testing.T) {
	const parsed = `{"series": {"id": 42, "title": "Solitude", "tvdbId": 471000}}`
	tests := []struct {
		name       string
		answers    map[string]string
		wantTitles []string
	}{
		{
			name: "complete",
			answers: map[string]string{
				"/api/v3/parse":     parsed,
				"/api/v3/series/42": `{"id": 42, "title": "Solitude", "tvdbId": 471000, "alternateTitles": [{"title": "Kodoku no Yado"}]}`,
			},
			wantTitles: []string{"Solitude", "Kodoku no Yado"},
		},
		{
			name:       "the series details fail",
			answers:    map[string]string{"/api/v3/parse": parsed},
			wantTitles: []string{"Solitude"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, ok := tt.answers[r.URL.Path]
				if !ok {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(srv.Close)
			db := testdb.NewMigratedSQLite(t, "dirscan-lookup")
			instances, err := models.NewArrInstanceStore(db, []byte(strings.Repeat("k", 32)))
			require.NoError(t, err)
			_, err = instances.Create(t.Context(), models.ArrInstanceTypeSonarr, "Sonarr", srv.URL, "api-key", nil, nil, true, 0, 15)
			require.NoError(t, err)
			service := &Service{arrService: arr.NewService(instances, models.NewArrIDCacheStore(db))}
			meta, log := &SearcheeMetadata{}, zerolog.Nop()

			titles := service.lookupExternalIDs(t.Context(), meta, "tv", "Solitude.S01E01.1080p.WEB-DL.H.264-QUILL", &log)

			require.Equal(t, tt.wantTitles, titles)
			require.Equal(t, 471000, meta.TRaSH.TVDbID)
		})
	}
}
