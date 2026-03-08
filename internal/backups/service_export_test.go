// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestIsExportMetadataUnavailable(t *testing.T) {
	if !isExportMetadataUnavailable(qbt.ErrTorrentMetdataNotDownloadedYet) {
		t.Fatal("expected metadata-not-downloaded error to be treated as skippable")
	}

	err := errors.New("could not get export; torrent hash: deadbeef | status code: 409: unexpected status code")
	if !isExportMetadataUnavailable(err) {
		t.Fatal("expected 409 status to be treated as skippable")
	}

	err = errors.New("could not get export; torrent hash: deadbeef | status code: 500: unexpected status code")
	if isExportMetadataUnavailable(err) {
		t.Fatal("expected non-409 status to be non-skippable")
	}
}

func TestClassifyExportFailure(t *testing.T) {
	if kind := classifyExportFailure(errors.New("context deadline exceeded")); kind != exportFailureRecoverable {
		t.Fatalf("expected deadline exceeded to be recoverable, got %v", kind)
	}

	if kind := classifyExportFailure(qbt.ErrTorrentMetdataNotDownloadedYet); kind != exportFailureMetadataUnavailable {
		t.Fatalf("expected metadata-not-downloaded to be classified as metadata unavailable, got %v", kind)
	}

	if kind := classifyExportFailure(errors.New("status code: 400: bad request")); kind != exportFailureFatal {
		t.Fatalf("expected 400 to be fatal, got %v", kind)
	}
}
