// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"errors"
	"fmt"
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
	tests := []struct {
		name string
		err  error
		want exportFailureKind
	}{
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: exportFailureRecoverable,
		},
		{
			name: "wrapped deadline exceeded",
			err:  fmt.Errorf("wrap: %w", context.DeadlineExceeded),
			want: exportFailureRecoverable,
		},
		{
			name: "metadata unavailable",
			err:  qbt.ErrTorrentMetdataNotDownloadedYet,
			want: exportFailureMetadataUnavailable,
		},
		{
			name: "fatal 400 response",
			err:  errors.New("status code: 400: bad request"),
			want: exportFailureFatal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyExportFailure(tt.err); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
