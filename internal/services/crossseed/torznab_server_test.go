// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

type torznabServer struct {
	jackett  *jackett.Service
	server   *httptest.Server
	fallback []byte

	mu       sync.Mutex
	files    map[string][]byte
	statuses map[string]int
	got      []string
}

func newTorznabServer(t *testing.T, indexerID int) *torznabServer {
	t.Helper()
	s := &torznabServer{
		fallback: createTestTorrent(t, "Torznab.Fixture.2024.1080p.WEB-DL-GROUP", []string{"Torznab.Fixture.2024.1080p.WEB-DL-GROUP.mkv"}, 16384),
		files:    map[string][]byte{},
		statuses: map[string]int{},
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)

	s.jackett = newJackettServiceWithIndexers([]*models.TorznabIndexer{
		{ID: indexerID, Name: fmt.Sprintf("Indexer %d", indexerID), BaseURL: s.server.URL, Enabled: true},
	})
	return s
}

func (s *torznabServer) handle(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	s.mu.Lock()
	s.got = append(s.got, strings.TrimPrefix(r.URL.RequestURI(), "/"))
	status, refused := s.statuses[name]
	data, served := s.files[name]
	s.mu.Unlock()

	if refused {
		w.WriteHeader(status)
		return
	}
	if !served {
		data = s.fallback
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	_, _ = w.Write(data)
}

func (s *torznabServer) url(name string) string {
	return s.server.URL + "/" + name
}

func (s *torznabServer) serve(name string, data []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[name] = data
	return s.url(name)
}

func (s *torznabServer) refuse(name string, status int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[name] = status
	return s.url(name)
}

func (s *torznabServer) downloads() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

func (s *torznabServer) downloadFromGUIDs(results []jackett.SearchResult) []jackett.SearchResult {
	pointed := slices.Clone(results)
	for i := range pointed {
		pointed[i].DownloadURL = s.url(pointed[i].GUID)
	}
	return pointed
}
