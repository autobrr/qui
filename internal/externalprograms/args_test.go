// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"reflect"
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func TestSplitArgs(t *testing.T) {
	t.Parallel()

	in := `-s --form-string "token=abc" --form-string 'user=def' --form-string "message=Cross-Seed added {name}" https://api.example.com/1/messages.json`
	want := []string{
		"-s",
		"--form-string",
		"token=abc",
		"--form-string",
		"user=def",
		"--form-string",
		"message=Cross-Seed added {name}",
		"https://api.example.com/1/messages.json",
	}

	got := SplitArgs(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitArgs() mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildArguments_Substitution(t *testing.T) {
	t.Parallel()

	template := `--form-string "message=Cross-Seed added {name}"`
	data := map[string]string{
		"name": "Some Torrent Name",
	}

	want := []string{"--form-string", "message=Cross-Seed added Some Torrent Name"}
	got := BuildArguments(template, data)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArguments() mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestApplyPathMappings_LongestPrefixWins(t *testing.T) {
	t.Parallel()

	mappings := []models.PathMapping{
		{From: "/data", To: "/mnt/data"},
		{From: "/data/torrents", To: "/mnt/torrents"},
	}

	got := ApplyPathMappings("/data/torrents/movies", mappings)
	want := "/mnt/torrents/movies"
	if got != want {
		t.Fatalf("ApplyPathMappings() mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestApplyPathMappings_PrefixBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		mappings []models.PathMapping
		want     string
	}{
		{
			name: "unix: should not match similar prefix",
			path: "/data-backup/file.txt",
			mappings: []models.PathMapping{
				{From: "/data", To: "/mnt/data"},
			},
			want: "/data-backup/file.txt", // unchanged
		},
		{
			name: "unix: should match exact prefix with subpath",
			path: "/data/backup/file.txt",
			mappings: []models.PathMapping{
				{From: "/data", To: "/mnt/data"},
			},
			want: "/mnt/data/backup/file.txt",
		},
		{
			name: "unix: should match exact prefix only",
			path: "/data",
			mappings: []models.PathMapping{
				{From: "/data", To: "/mnt/data"},
			},
			want: "/mnt/data",
		},
		{
			name: "windows: should not match similar prefix",
			path: `C:\data-backup\file.txt`,
			mappings: []models.PathMapping{
				{From: `C:\data`, To: `D:\data`},
			},
			want: `C:\data-backup\file.txt`, // unchanged
		},
		{
			name: "windows: should match exact prefix with subpath",
			path: `C:\data\backup\file.txt`,
			mappings: []models.PathMapping{
				{From: `C:\data`, To: `D:\data`},
			},
			want: `D:\data\backup\file.txt`,
		},
		// Trailing separator cases
		{
			name: "unix: From with trailing slash should match",
			path: "/data/torrents/file.txt",
			mappings: []models.PathMapping{
				{From: "/data/", To: "/mnt/data/"},
			},
			want: "/mnt/data/torrents/file.txt",
		},
		{
			name: "unix: From with trailing slash should not match similar prefix",
			path: "/data-backup/file.txt",
			mappings: []models.PathMapping{
				{From: "/data/", To: "/mnt/data/"},
			},
			want: "/data-backup/file.txt", // unchanged - doesn't start with "/data/"
		},
		{
			name: "unix: root mapping should work",
			path: "/torrents/file.txt",
			mappings: []models.PathMapping{
				{From: "/", To: "/mnt/"},
			},
			want: "/mnt/torrents/file.txt",
		},
		{
			name: "windows: From with trailing backslash should match",
			path: `C:\data\torrents\file.txt`,
			mappings: []models.PathMapping{
				{From: `C:\data\`, To: `D:\data\`},
			},
			want: `D:\data\torrents\file.txt`,
		},
		{
			name: "windows: From with trailing backslash should not match similar prefix",
			path: `C:\data-backup\file.txt`,
			mappings: []models.PathMapping{
				{From: `C:\data\`, To: `D:\data\`},
			},
			want: `C:\data-backup\file.txt`, // unchanged
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ApplyPathMappings(tc.path, tc.mappings)
			if got != tc.want {
				t.Errorf("ApplyPathMappings(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
