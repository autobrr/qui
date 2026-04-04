// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type seasonPackHandlerInstanceStore struct {
	instances map[int]*models.Instance
}

func (s *seasonPackHandlerInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	inst, ok := s.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %d not found", id)
	}
	return inst, nil
}

func (s *seasonPackHandlerInstanceStore) List(context.Context) ([]*models.Instance, error) {
	instances := make([]*models.Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		instances = append(instances, inst)
	}
	return instances, nil
}

type seasonPackHandlerSyncManager struct {
	torrents map[int][]qbt.Torrent
	files    map[string]qbt.TorrentFiles
	addErr   error
}

func (s *seasonPackHandlerSyncManager) GetTorrents(_ context.Context, instanceID int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	return s.torrents[instanceID], nil
}

func (s *seasonPackHandlerSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		if files, ok := s.files[hash]; ok {
			copied := make(qbt.TorrentFiles, len(files))
			copy(copied, files)
			result[hash] = copied
		}
	}
	return result, nil
}

func (*seasonPackHandlerSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (*seasonPackHandlerSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (*seasonPackHandlerSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, errors.New("not implemented")
}

func (*seasonPackHandlerSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (s *seasonPackHandlerSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return s.addErr
}

func (*seasonPackHandlerSyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (s *seasonPackHandlerSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	torrents := s.torrents[instanceID]
	views := make([]internalqb.CrossInstanceTorrentView, 0, len(torrents))
	for i := range torrents {
		torrent := torrents[i]
		views = append(views, internalqb.CrossInstanceTorrentView{
			TorrentView: &internalqb.TorrentView{Torrent: &torrent},
			InstanceID:  instanceID,
		})
	}
	return views, nil
}

func (*seasonPackHandlerSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (*seasonPackHandlerSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, errors.New("not implemented")
}

func (*seasonPackHandlerSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return errors.New("not implemented")
}

func (*seasonPackHandlerSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return errors.New("not implemented")
}

func (*seasonPackHandlerSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return errors.New("not implemented")
}

func (*seasonPackHandlerSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*seasonPackHandlerSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*seasonPackHandlerSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

func createSeasonPackHandlerTorrent(t *testing.T, rootName string, files []string) string {
	t.Helper()

	tempDir := t.TempDir()
	for _, file := range files {
		path := filepath.Join(tempDir, rootName, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, "content for %s", file), 0o600))
	}

	info := metainfo.Info{
		Name:        rootName,
		PieceLength: 256 * 1024,
	}
	require.NoError(t, info.BuildFromFilePath(filepath.Join(tempDir, rootName)))
	info.Name = rootName

	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)

	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{{"http://tracker.example.com:8080/announce"}},
		InfoBytes:    infoBytes,
	}

	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestSeasonPackApply_Returns500ForFailedApplyResponse(t *testing.T) {
	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	packFile := "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"
	torrentData := createSeasonPackHandlerTorrent(t, packName, []string{packFile, "notes.txt"})

	metaBytes, err := base64.StdEncoding.DecodeString(torrentData)
	require.NoError(t, err)
	meta, err := crossseed.ParseTorrentMetadataWithInfo(metaBytes)
	require.NoError(t, err)

	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}

	syncManager := &seasonPackHandlerSyncManager{
		torrents: map[int][]qbt.Torrent{
			inst.ID: {{
				Hash:        "e01",
				Name:        "Cool.Show.S01E01.1080p.WEB.x264-GRP",
				ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv",
				Progress:    1.0,
			}},
		},
		files: map[string]qbt.TorrentFiles{
			"e01": {{
				Name: packFile,
				Size: meta.Files[0].Size,
			}},
		},
		addErr: errors.New("qb add failed"),
	}

	svc := &crossseed.Service{}
	setServiceField(t, svc, "instanceStore", &seasonPackHandlerInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}})
	setServiceField(t, svc, "syncManager", syncManager)
	setServiceField(t, svc, "releaseCache", crossseed.NewReleaseCache())
	setServiceField(t, svc, "automationSettingsLoader", func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return &models.CrossSeedAutomationSettings{
			SeasonPackEnabled:           true,
			SeasonPackCoverageThreshold: 1,
		}, nil
	})
	setServiceField(t, svc, "seasonPackLinkCreator", func(*hardlinktree.TreePlan) error { return nil })

	handler := &CrossSeedHandler{service: svc}

	body, err := json.Marshal(crossseed.SeasonPackApplyRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/cross-seed/season-pack/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.SeasonPackApply(resp, req)

	require.Equal(t, http.StatusInternalServerError, resp.Code)
	require.Contains(t, resp.Body.String(), "Failed to apply season pack")
}
