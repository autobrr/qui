// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type archiveExportResult struct {
	data          []byte
	suggestedName string
	trackerDomain string
	err           error
}

type archiveExporterFake map[string]archiveExportResult

func (f archiveExporterFake) ExportTorrent(_ context.Context, instanceID int, hash string) ([]byte, string, string, error) {
	result := f[archiveFakeKey(instanceID, hash)]
	return result.data, result.suggestedName, result.trackerDomain, result.err
}

func TestWriteTorrentArchiveGroupsByInstanceAndCategory(t *testing.T) {
	t.Parallel()

	targets := []torrentArchiveTarget{
		{InstanceID: 1, InstanceName: "Home", Hash: "aaaaa11111", Category: "Movies/HD"},
		{InstanceID: 2, InstanceName: "Seedbox", Hash: "bbbbb22222"},
	}
	exporter := archiveExporterFake{
		archiveFakeKey(1, "aaaaa11111"): {data: []byte("alpha"), suggestedName: "Alpha"},
		archiveFakeKey(2, "bbbbb22222"): {data: []byte("beta"), suggestedName: "Beta"},
	}

	var output bytes.Buffer
	require.NoError(t, writeTorrentArchive(context.Background(), &output, targets, exporter))

	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{
		"Home/Movies/HD/Alpha - aaaaa.torrent",
		"Seedbox/Uncategorized/Beta - bbbbb.torrent",
	}, zipEntryNames(reader.File))
	require.Equal(t, "alpha", readZipEntry(t, reader.File[0]))
	require.Equal(t, "beta", readZipEntry(t, reader.File[1]))
}

func TestWriteTorrentArchiveIncludesPartialFailures(t *testing.T) {
	t.Parallel()

	targets := []torrentArchiveTarget{
		{InstanceID: 1, InstanceName: "Home", Hash: "aaaaa11111", Category: "Movies"},
		{InstanceID: 1, InstanceName: "Home", Hash: "bbbbb22222", Category: "TV"},
	}
	exporter := archiveExporterFake{
		archiveFakeKey(1, "aaaaa11111"): {data: []byte("alpha"), suggestedName: "Alpha"},
		archiveFakeKey(1, "bbbbb22222"): {err: errors.New("export unavailable")},
	}

	var output bytes.Buffer
	require.NoError(t, writeTorrentArchive(context.Background(), &output, targets, exporter))

	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{
		"Movies/Alpha - aaaaa.torrent",
		"export-errors.txt",
	}, zipEntryNames(reader.File))
	require.Equal(t, "Home/TV (bbbbb22222): export unavailable\n", readZipEntry(t, reader.File[1]))
}

func TestWriteTorrentArchiveReturnsErrorReportWhenAllExportsFail(t *testing.T) {
	t.Parallel()

	targets := []torrentArchiveTarget{{InstanceID: 1, Hash: "aaaaa11111"}}
	exporter := archiveExporterFake{
		archiveFakeKey(1, "aaaaa11111"): {err: errors.New("export unavailable")},
	}

	var output bytes.Buffer
	require.NoError(t, writeTorrentArchive(context.Background(), &output, targets, exporter))

	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{"export-errors.txt"}, zipEntryNames(reader.File))
}

func TestWriteTorrentArchiveDeduplicatesFilenamesWithinCategory(t *testing.T) {
	t.Parallel()

	targets := []torrentArchiveTarget{
		{InstanceID: 1, Hash: "zzzzz", Category: "Movies"},
		{InstanceID: 1, Hash: "yyyyy", Category: "Movies"},
	}
	exporter := archiveExporterFake{
		archiveFakeKey(1, "zzzzz"): {data: []byte("first"), suggestedName: "Same"},
		archiveFakeKey(1, "yyyyy"): {data: []byte("second"), suggestedName: "same"},
	}

	var output bytes.Buffer
	require.NoError(t, writeTorrentArchive(context.Background(), &output, targets, exporter))

	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{
		"Movies/Same.torrent",
		"Movies/same (2).torrent",
	}, zipEntryNames(reader.File))
}

func TestWriteTorrentArchiveSanitizesFolderPaths(t *testing.T) {
	t.Parallel()

	targets := []torrentArchiveTarget{
		{InstanceID: 1, InstanceName: "../Home", Hash: "aaaaa11111", Category: "../Movies\\4K"},
		{InstanceID: 2, InstanceName: "Other", Hash: "bbbbb22222", Category: "TV"},
	}
	exporter := archiveExporterFake{
		archiveFakeKey(1, "aaaaa11111"): {data: []byte("alpha"), suggestedName: "Alpha"},
		archiveFakeKey(2, "bbbbb22222"): {data: []byte("beta"), suggestedName: "Beta"},
	}

	var output bytes.Buffer
	require.NoError(t, writeTorrentArchive(context.Background(), &output, targets, exporter))

	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{
		"..Home/_/Movies/4K/Alpha - aaaaa.torrent",
		"Other/TV/Beta - bbbbb.torrent",
	}, zipEntryNames(reader.File))
}

func TestWriteTorrentArchiveUsesRequestedFilename(t *testing.T) {
	t.Parallel()

	targets := []torrentArchiveTarget{
		{InstanceID: 1, Hash: "aaaaa11111", Category: "Movies", Filename: "ubuntu.iso.torrent"},
	}
	exporter := archiveExporterFake{
		archiveFakeKey(1, "aaaaa11111"): {data: []byte("alpha"), suggestedName: "Secret Movie"},
	}

	var output bytes.Buffer
	require.NoError(t, writeTorrentArchive(context.Background(), &output, targets, exporter))

	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{"Movies/ubuntu.iso.torrent"}, zipEntryNames(reader.File))
}

func TestExportTorrentArchiveStreamsZip(t *testing.T) {
	t.Parallel()

	handler := &TorrentsHandler{archiveExporter: archiveExporterFake{
		archiveFakeKey(1, "aaaaa11111"): {data: []byte("alpha"), suggestedName: "Alpha"},
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/torrents/export", strings.NewReader(`{
		"targets": [{"instanceId": 1, "instanceName": "Home", "hash": "aaaaa11111", "category": "Movies"}]
	}`))
	response := httptest.NewRecorder()

	handler.ExportTorrentArchive(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/zip", response.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="qui-torrents.zip"`, response.Header().Get("Content-Disposition"))

	reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{"Movies/Alpha - aaaaa.torrent"}, zipEntryNames(reader.File))
}

func archiveFakeKey(instanceID int, hash string) string {
	return strconv.Itoa(instanceID) + ":" + hash
}

func zipEntryNames(files []*zip.File) []string {
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = file.Name
	}
	return names
}

func readZipEntry(t *testing.T, file *zip.File) string {
	t.Helper()

	reader, err := file.Open()
	require.NoError(t, err)
	defer reader.Close()

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(data)
}
