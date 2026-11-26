// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/config"
)

type installFilesFixture struct {
	root         string
	config       *config.AppConfig
	installFiles []string
}

func newInstallFilesFixture(t *testing.T) installFilesFixture {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "etc", "config.toml")
	dataDir := filepath.Join(root, "data")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, config.WriteDefaultConfig(configPath))

	cfg, err := config.New(configPath, "test")
	require.NoError(t, err)
	t.Cleanup(cfg.StopWatching)
	cfg.SetDataDir(dataDir)

	database := cfg.GetDatabasePath()
	installFiles := []string{configPath, database, database + "-wal", database + "-shm", database + "-journal"}
	for _, path := range installFiles[1:] {
		require.NoError(t, os.WriteFile(path, []byte("dummy "+filepath.Base(path)), 0o600))
	}

	return installFilesFixture{root: root, config: cfg, installFiles: installFiles}
}

func (f installFilesFixture) download(t *testing.T, relativePath string) *httptest.ResponseRecorder {
	t.Helper()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	files := qbt.TorrentFiles{{Index: 0, Name: filepath.ToSlash(relativePath)}}
	handler := &TorrentsHandler{
		instanceStore: instanceStore,
		contentResolver: &mockContentResolver{
			files:      &files,
			properties: &qbt.TorrentProperties{SavePath: f.root},
		},
		installPaths: f.config,
	}

	rec := httptest.NewRecorder()
	handler.DownloadTorrentContentFile(rec, newDownloadRequest(t, instanceID, "hash123", "0"))
	return rec
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()

	err := os.Symlink(target, link)
	if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
		t.Skipf("creating symlinks needs a privilege on Windows: %v", err)
	}
	require.NoError(t, err)
}

func TestDownloadTorrentContentFile_RefusesInstallFiles(t *testing.T) {
	fixture := newInstallFilesFixture(t)
	mediaDir := filepath.Join(fixture.root, "media")
	require.NoError(t, os.MkdirAll(mediaDir, 0o755))

	links := []struct {
		name string
		link func(t *testing.T, installFile string) string
	}{
		{name: "direct", link: func(_ *testing.T, installFile string) string {
			return installFile
		}},
		{name: "symlink", link: func(t *testing.T, installFile string) string {
			link := filepath.Join(mediaDir, "symlink-"+filepath.Base(installFile)+".mkv")
			symlinkOrSkip(t, installFile, link)
			return link
		}},
		{name: "hardlink", link: func(t *testing.T, installFile string) string {
			link := filepath.Join(mediaDir, "hardlink-"+filepath.Base(installFile)+".mkv")
			require.NoError(t, os.Link(installFile, link))
			return link
		}},
	}

	for _, installFile := range fixture.installFiles {
		for _, tc := range links {
			t.Run(filepath.Base(installFile)+"/"+tc.name, func(t *testing.T) {
				relativePath, err := filepath.Rel(fixture.root, tc.link(t, installFile))
				require.NoError(t, err)

				rec := fixture.download(t, relativePath)

				require.Equal(t, http.StatusForbidden, rec.Code)
				require.Contains(t, rec.Body.String(), "qui does not serve its own config or database files")
			})
		}
	}
}

func TestDownloadTorrentContentFile_ServesFilesNamedLikeInstallFiles(t *testing.T) {
	fixture := newInstallFilesFixture(t)

	for _, installFile := range fixture.installFiles {
		name := filepath.Base(installFile)
		t.Run(name, func(t *testing.T) {
			relativePath := filepath.Join("media", name)
			content := "torrent content named " + name
			require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, "media"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(fixture.root, relativePath), []byte(content), 0o600))

			rec := fixture.download(t, relativePath)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, content, rec.Body.String())
		})
	}
}
