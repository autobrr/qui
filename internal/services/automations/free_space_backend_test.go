// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// statfsBackend answers Statfs with a fixed result, as the SSH host would.
type statfsBackend struct {
	fsops.Backend
	available int64
	err       error
}

func (statfsBackend) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b statfsBackend) Statfs(context.Context, string) (*fsops.StatfsResult, error) {
	if b.err != nil {
		return nil, b.err
	}
	return &fsops.StatfsResult{BytesAvailable: b.available}, nil
}

var errStatfs = errors.New("connection refused")

func TestFreeSpaceBytesPathSource(t *testing.T) {
	t.Parallel()

	remote := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}
	src := &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"}

	for _, test := range []struct {
		name    string
		row     *models.Instance
		backend statfsBackend
		want    int64
		wantErr error
	}{
		{name: "remote reads the SSH host", row: remote, backend: statfsBackend{available: 42}, want: 42},
		{name: "remote statfs fails", row: remote, backend: statfsBackend{err: errStatfs}, wantErr: errStatfs},
		{name: "no filesystem access", row: &models.Instance{ID: 1}, wantErr: fsops.ErrNotCapable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{backendPool: fsops.NewPoolWithRemote(rowGetter{row: test.row}, localbackend.NewBackend(),
				func(*models.Instance) fsops.Backend { return test.backend })}

			got, err := service.freeSpaceBytes(t.Context(), &models.Instance{ID: 1}, src)
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, got)
		})
	}
}
