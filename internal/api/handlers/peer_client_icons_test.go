// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/peerclienticons"
)

const testSVG = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M0 0h24v24H0z"/></svg>`

func sha(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

type iconRig struct {
	router  http.Handler
	service *peerclienticons.Service
	iconDir string
	hits    *atomic.Int32
}

// newIconRig serves body from a fake upstream through the real service and handler.
func newIconRig(t *testing.T, upstream http.HandlerFunc, icons func(base string) []peerclienticons.Icon) iconRig {
	t.Helper()
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		upstream(w, r)
	}))
	t.Cleanup(srv.Close)

	dataDir := t.TempDir()
	svc, err := peerclienticons.NewService(dataDir, "qui-test", icons(srv.URL))
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/api/peer-client-icons/{name}", NewPeerClientIconHandler(svc).GetPeerClientIcon)
	return iconRig{router: r, service: svc, iconDir: filepath.Join(dataDir, "peer-client-icons"), hits: hits}
}

func (rig iconRig) get(name string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/peer-client-icons/"+name, nil))
	return rr
}

// storedIcons lists the icon files on disk, without the retry state.
func (rig iconRig) storedIcons(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(rig.iconDir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if e.Name() != "fetch-failures.json" {
			names = append(names, e.Name())
		}
	}
	return names
}

func serveBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

func oneIcon(name, hash, fill string) func(string) []peerclienticons.Icon {
	return func(base string) []peerclienticons.Icon {
		return []peerclienticons.Icon{{Name: name, URL: base + "/" + name + ".svg", SHA256: hash, ContentType: "image/svg+xml", Fill: fill}}
	}
}

func TestPeerClientIcon_UnknownNameSendsNoRequest(t *testing.T) {
	rig := newIconRig(t, serveBody(testSVG), oneIcon("deluge", sha(testSVG), ""))

	require.Equal(t, http.StatusNotFound, rig.get("nope").Code)
	require.Zero(t, rig.hits.Load())
}

func TestPeerClientIcon_DownloadsOnceThenServesFromDisk(t *testing.T) {
	rig := newIconRig(t, serveBody(testSVG), oneIcon("deluge", sha(testSVG), ""))

	for range 2 {
		rr := rig.get("deluge")
		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, testSVG, rr.Body.String())
		require.Equal(t, "image/svg+xml", rr.Header().Get("Content-Type"))
		require.Equal(t, "default-src 'none'; style-src 'unsafe-inline'; sandbox", rr.Header().Get("Content-Security-Policy"))
		require.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	}
	require.EqualValues(t, 1, rig.hits.Load())
	require.Equal(t, []string{"deluge-" + sha(testSVG) + ".svg"}, rig.storedIcons(t))
}

func TestPeerClientIcon_BadDownloadStoresNothingAndWaits(t *testing.T) {
	tests := []struct {
		name     string
		hash     string
		upstream http.HandlerFunc
	}{
		{"wrong hash", sha(testSVG), serveBody(testSVG + " ")},
		{"status 500", sha(testSVG), func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(testSVG))
		}},
		{"redirect", sha(testSVG), func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/moved" {
				_, _ = w.Write([]byte(testSVG))
				return
			}
			http.Redirect(w, r, "/moved", http.StatusFound)
		}},
		// The hash matches the whole body, so only the size limit rejects it.
		{"over 1 MiB", sha(strings.Repeat("a", 1<<20) + "a"), serveBody(strings.Repeat("a", 1<<20) + "a")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := newIconRig(t, tt.upstream, oneIcon("deluge", tt.hash, ""))

			rr := rig.get("deluge")
			require.Equal(t, http.StatusNotFound, rr.Code)
			require.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
			require.Empty(t, rig.storedIcons(t))

			hits := rig.hits.Load()
			require.Equal(t, http.StatusNotFound, rig.get("deluge").Code)
			require.Equal(t, hits, rig.hits.Load(), "a second request within the retry wait must not download")
		})
	}
}

func TestPeerClientIcon_SwitchOffSendsNoRequestAndServesDisk(t *testing.T) {
	rig := newIconRig(t, serveBody(testSVG), func(base string) []peerclienticons.Icon {
		return []peerclienticons.Icon{
			{Name: "deluge", URL: base + "/deluge.svg", SHA256: sha(testSVG), ContentType: "image/svg+xml"},
			{Name: "porla", URL: base + "/porla.svg", SHA256: sha(testSVG), ContentType: "image/svg+xml"},
		}
	})
	require.Equal(t, http.StatusOK, rig.get("deluge").Code)

	rig.service.SetFetchEnabled(false)
	require.Equal(t, http.StatusOK, rig.get("deluge").Code)
	require.Equal(t, http.StatusNotFound, rig.get("porla").Code)
	require.EqualValues(t, 1, rig.hits.Load())

	rig.service.SetFetchEnabled(true)
	require.Equal(t, http.StatusOK, rig.get("porla").Code)
}

func TestPeerClientIcon_FileDroppedInDirectoryIsNotServed(t *testing.T) {
	rig := newIconRig(t, serveBody(testSVG), oneIcon("deluge", sha(testSVG), ""))
	rig.service.SetFetchEnabled(false)

	path := filepath.Join(rig.iconDir, "deluge-"+sha(testSVG)+".svg")
	require.NoError(t, os.WriteFile(path, []byte(`<svg><script>alert(1)</script></svg>`), 0o600))
	require.Equal(t, http.StatusNotFound, rig.get("deluge").Code)
}

func TestPeerClientIcon_FillGoesOnRootElementOnce(t *testing.T) {
	rig := newIconRig(t, serveBody(testSVG), oneIcon("utorrent", sha(testSVG), "#76b83f"))

	for range 2 { // downloaded, then from disk
		body := rig.get("utorrent").Body.String()
		require.True(t, strings.HasPrefix(body, `<svg fill="#76b83f" `), body)
		require.Equal(t, 1, strings.Count(body, "fill="), body)
	}
}
