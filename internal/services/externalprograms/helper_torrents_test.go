// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
)

type fakeHelpers map[string]bool

func (f fakeHelpers) IsHelper(instanceID int, hash string) bool { return instanceID == 1 && f[hash] }

func TestExternalProgramsSkipHelperTorrents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this fixture needs a POSIX shell; Windows uses the native recorder test")
	}
	const (
		helper = "cccc000000000000000000000000000000000003"
		own    = "dddd000000000000000000000000000000000004"
	)

	for _, tc := range []struct {
		name    string
		helpers fakeHelpers
		want    []string
	}{
		{name: "a helper torrent is left out", helpers: fakeHelpers{helper: true}, want: []string{own}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "record.sh")
			//nolint:gosec // G306: the test runs this script, so it must be executable
			require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch \"$(dirname \"$0\")/ran-$1\"\n"), 0o700))
			s := NewService(nil, nil, &domain.Config{ExternalProgramAllowList: []string{dir}})
			s.SetHelperTorrents(tc.helpers)
			program := &models.ExternalProgram{ID: 1, Name: "record", Enabled: true, Path: script, ArgsTemplate: "{hash}"}

			for _, hash := range []string{helper, own} {
				result := s.Execute(t.Context(), ExecuteRequest{
					Program: program, Torrent: &qbt.Torrent{Hash: hash, Name: "Quiet.Harbor.2024.1080p.WEB-DL-GRP", Tags: "qui-transfer"}, InstanceID: 1,
				})
				require.Equal(t, tc.helpers[hash], !result.Success, hash)
			}
			require.Eventually(t, func() bool { return s.admitted.Load() == 0 }, 5*time.Second, 10*time.Millisecond)

			var ran []string
			for _, hash := range []string{helper, own} {
				if _, err := os.Stat(filepath.Join(dir, "ran-"+hash)); err == nil {
					ran = append(ran, hash)
				}
			}
			require.Equal(t, tc.want, ran)
		})
	}
}
