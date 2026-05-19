// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

// roundTrip marshals v to JSON and unmarshals it into a new value of the same type.
// Fails the test on any error and returns the round-tripped value.
func roundTrip[T any](t *testing.T, v T) T {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err, "marshal")
	var out T
	require.NoError(t, json.Unmarshal(data, &out), "unmarshal")
	return out
}

func TestCommand_RoundTrip(t *testing.T) {
	args, _ := json.Marshal(StatRequest{Paths: []string{"/data/file.mkv"}})
	cmd := Command{
		RequestID: "abc-123",
		Op:        OpStat,
		Args:      args,
		Deadline:  "2026-04-28T12:00:00Z",
	}
	got := roundTrip(t, cmd)
	assert.Equal(t, cmd.RequestID, got.RequestID)
	assert.Equal(t, cmd.Op, got.Op)
	assert.JSONEq(t, string(cmd.Args), string(got.Args))
	assert.Equal(t, cmd.Deadline, got.Deadline)
}

func TestCommand_OmitsEmptyDeadline(t *testing.T) {
	cmd := Command{RequestID: "x", Op: OpStat, Args: json.RawMessage(`{}`)}
	data, err := json.Marshal(cmd)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "deadline")
}

func TestResult_RoundTrip(t *testing.T) {
	payload, _ := json.Marshal(StatResponse{Entries: []StatEntry{{Path: "/a", Exists: true}}})
	r := Result{
		RequestID: "abc-123",
		OK:        true,
		Payload:   payload,
		Done:      true,
	}
	got := roundTrip(t, r)
	assert.Equal(t, r.RequestID, got.RequestID)
	assert.True(t, got.OK)
	assert.True(t, got.Done)
	assert.JSONEq(t, string(r.Payload), string(got.Payload))
}

func TestResult_ErrorFields(t *testing.T) {
	r := Result{
		RequestID: "err-1",
		OK:        false,
		Code:      CodePathNotAllowed,
		Error:     "path /etc is not under any allowed root",
	}
	got := roundTrip(t, r)
	assert.False(t, got.OK)
	assert.Equal(t, CodePathNotAllowed, got.Code)
	assert.Equal(t, r.Error, got.Error)
}

func TestResult_StreamingFrame(t *testing.T) {
	r := Result{
		RequestID: "walk-1",
		OK:        true,
		Payload:   json.RawMessage(`{"path":"/data/file.mkv"}`),
		Frame:     42,
	}
	got := roundTrip(t, r)
	assert.Equal(t, 42, got.Frame)
	assert.False(t, got.Done)
}

func TestHelloBanner_RoundTrip(t *testing.T) {
	banner := HelloBanner{
		HelperVersion: "1.0.0",
		ProtoVersion:  "1",
		Capabilities:  []string{"fs.stat", "fs.walk", "tree.hardlink"},
		AllowedRoots:  []string{"/home/user/data", "/home/user/seed"},
		ReflinkRoots:  []string{"/home/user/data"},
		Platform:      "linux",
		Hostname:      "seedbox01",
		PID:           12345,
		StartedAt:     "2026-04-28T10:00:00Z",
	}
	got := roundTrip(t, banner)
	assert.Equal(t, banner.HelperVersion, got.HelperVersion)
	assert.Equal(t, banner.ProtoVersion, got.ProtoVersion)
	assert.Equal(t, banner.Capabilities, got.Capabilities)
	assert.Equal(t, banner.AllowedRoots, got.AllowedRoots)
	assert.Equal(t, banner.ReflinkRoots, got.ReflinkRoots)
	assert.Equal(t, banner.Platform, got.Platform)
	assert.Equal(t, banner.Hostname, got.Hostname)
	assert.Equal(t, banner.PID, got.PID)
	assert.Equal(t, banner.StartedAt, got.StartedAt)
}

func TestStatRequest_RoundTrip(t *testing.T) {
	req := StatRequest{Paths: []string{"/data/a.mkv", "/data/b.mkv"}}
	got := roundTrip(t, req)
	assert.Equal(t, req.Paths, got.Paths)
}

func TestStatResponse_RoundTrip(t *testing.T) {
	resp := StatResponse{Entries: []StatEntry{
		{Path: "/data/a.mkv", Exists: true, Size: 1024, ModTime: "2026-01-01T00:00:00Z", IsDir: false, Mode: 0o644},
		{Path: "/data/gone.mkv", Exists: false, Err: "not_found"},
	}}
	got := roundTrip(t, resp)
	require.Len(t, got.Entries, 2)
	assert.Equal(t, int64(1024), got.Entries[0].Size)
	assert.True(t, got.Entries[0].Exists)
	assert.Equal(t, "not_found", got.Entries[1].Err)
	assert.False(t, got.Entries[1].Exists)
}

func TestLstatRequest_RoundTrip(t *testing.T) {
	req := LstatRequest{Paths: []string{"/data/link"}, WantFileID: true, WantNlinks: true}
	got := roundTrip(t, req)
	assert.Equal(t, req.Paths, got.Paths)
	assert.True(t, got.WantFileID)
	assert.True(t, got.WantNlinks)
}

func TestLstatEntry_RoundTrip(t *testing.T) {
	entry := LstatEntry{
		StatEntry: StatEntry{Path: "/data/file", Exists: true, Size: 512, Mode: 0o755},
		IsSymlink: true,
		FileID:    []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
		Nlinks:    3,
	}
	got := roundTrip(t, entry)
	assert.Equal(t, "/data/file", got.Path)
	assert.True(t, got.IsSymlink)
	assert.Equal(t, entry.FileID, got.FileID)
	assert.Equal(t, uint64(3), got.Nlinks)
}

func TestWalkRequest_RoundTrip(t *testing.T) {
	req := WalkRequest{
		Root:           "/data",
		SkipHidden:     true,
		IgnoreDirNames: []string{".git", "node_modules"},
		IgnorePaths:    []string{"/data/.cache"},
		WantFileID:     true,
		WantNlinks:     false,
		MaxEntries:     10000,
	}
	got := roundTrip(t, req)
	assert.Equal(t, req.Root, got.Root)
	assert.True(t, got.SkipHidden)
	assert.Equal(t, req.IgnoreDirNames, got.IgnoreDirNames)
	assert.Equal(t, req.IgnorePaths, got.IgnorePaths)
	assert.True(t, got.WantFileID)
	assert.False(t, got.WantNlinks)
	assert.Equal(t, 10000, got.MaxEntries)
}

func TestWalkEntry_RoundTrip(t *testing.T) {
	entry := WalkEntry{
		Path:    "/data/movies/file.mkv",
		RelPath: "movies/file.mkv",
		IsDir:   false,
		Size:    1_000_000,
		ModTime: "2026-01-15T14:30:00.123456789Z",
		Mode:    0o644,
		FileID:  []byte{0x01, 0x02},
		Nlinks:  2,
	}
	got := roundTrip(t, entry)
	assert.Equal(t, entry.Path, got.Path)
	assert.Equal(t, entry.RelPath, got.RelPath)
	assert.Equal(t, entry.Size, got.Size)
	assert.Equal(t, entry.FileID, got.FileID)
	assert.Equal(t, entry.Nlinks, got.Nlinks)
}

func TestWalkEntry_Truncated(t *testing.T) {
	entry := WalkEntry{Truncated: true}
	got := roundTrip(t, entry)
	assert.True(t, got.Truncated)
}

func TestWalkEntry_Error(t *testing.T) {
	entry := WalkEntry{Path: "/data/secret", Err: "permission"}
	got := roundTrip(t, entry)
	assert.Equal(t, "permission", got.Err)
}

func TestStatfsRequest_RoundTrip(t *testing.T) {
	req := StatfsRequest{Path: "/data"}
	got := roundTrip(t, req)
	assert.Equal(t, "/data", got.Path)
}

func TestStatfsResponse_RoundTrip(t *testing.T) {
	resp := StatfsResponse{BytesAvailable: 1_000_000_000, BytesTotal: 2_000_000_000, Filesystem: "ext4"}
	got := roundTrip(t, resp)
	assert.Equal(t, int64(1_000_000_000), got.BytesAvailable)
	assert.Equal(t, int64(2_000_000_000), got.BytesTotal)
	assert.Equal(t, "ext4", got.Filesystem)
}

func TestReadDirRequest_RoundTrip(t *testing.T) {
	req := ReadDirRequest{Path: "/data/movies", MaxEntries: 100}
	got := roundTrip(t, req)
	assert.Equal(t, "/data/movies", got.Path)
	assert.Equal(t, 100, got.MaxEntries)
}

func TestReadDirResponse_RoundTrip(t *testing.T) {
	resp := ReadDirResponse{
		Entries: []DirEntry{
			{Name: "movie.mkv", Size: 5000, Mode: 0o644},
			{Name: "subs", IsDir: true, Mode: 0o755},
			{Name: "link", IsSymlink: true},
		},
		Truncated: false,
	}
	got := roundTrip(t, resp)
	require.Len(t, got.Entries, 3)
	assert.Equal(t, "movie.mkv", got.Entries[0].Name)
	assert.True(t, got.Entries[1].IsDir)
	assert.True(t, got.Entries[2].IsSymlink)
}

func TestSameFSRequest_RoundTrip(t *testing.T) {
	req := SameFSRequest{Path1: "/data/a", Path2: "/data/b"}
	got := roundTrip(t, req)
	assert.Equal(t, "/data/a", got.Path1)
	assert.Equal(t, "/data/b", got.Path2)
}

func TestSameFSResponse_RoundTrip(t *testing.T) {
	resp := SameFSResponse{Same: true}
	got := roundTrip(t, resp)
	assert.True(t, got.Same)
}

func TestMkdirRequest_RoundTrip(t *testing.T) {
	req := MkdirRequest{Path: "/data/new-dir", Perm: 0o755}
	got := roundTrip(t, req)
	assert.Equal(t, "/data/new-dir", got.Path)
	assert.Equal(t, uint32(0o755), got.Perm)
}

func TestRemoveRequest_RoundTrip(t *testing.T) {
	req := RemoveRequest{
		Path:        "/data/old-file",
		Recursive:   true,
		IgnorePaths: []string{"/data/old-file/.keep"},
	}
	got := roundTrip(t, req)
	assert.Equal(t, "/data/old-file", got.Path)
	assert.True(t, got.Recursive)
	assert.Equal(t, []string{"/data/old-file/.keep"}, got.IgnorePaths)
}

func TestRemoveResponse_RoundTrip(t *testing.T) {
	resp := RemoveResponse{Removed: true, Disposition: "deleted", RemovedBytes: 4096}
	got := roundTrip(t, resp)
	assert.True(t, got.Removed)
	assert.Equal(t, "deleted", got.Disposition)
	assert.Equal(t, int64(4096), got.RemovedBytes)
}

func TestTreeCreateRequest_RoundTrip(t *testing.T) {
	req := TreeCreateRequest{
		Plan: hardlinktree.TreePlan{
			RootDir: "/data/cross-seed/torrent-abc",
			Files: []hardlinktree.FilePlan{
				{SourcePath: "/data/movies/file.mkv", TargetPath: "/data/cross-seed/torrent-abc/file.mkv"},
				{SourcePath: "/data/movies/subs.srt", TargetPath: "/data/cross-seed/torrent-abc/subs.srt"},
			},
		},
		Mode:     "hardlink",
		SourceFS: "ext4",
	}
	got := roundTrip(t, req)
	assert.Equal(t, "hardlink", got.Mode)
	assert.Equal(t, "ext4", got.SourceFS)
	assert.Equal(t, req.Plan.RootDir, got.Plan.RootDir)
	require.Len(t, got.Plan.Files, 2)
	assert.Equal(t, req.Plan.Files[0].SourcePath, got.Plan.Files[0].SourcePath)
	assert.Equal(t, req.Plan.Files[1].TargetPath, got.Plan.Files[1].TargetPath)
}

func TestTreeCreateResponse_RoundTrip(t *testing.T) {
	resp := TreeCreateResponse{
		Created:       5,
		SkippedExists: 2,
		RolledBack:    false,
	}
	got := roundTrip(t, resp)
	assert.Equal(t, 5, got.Created)
	assert.Equal(t, 2, got.SkippedExists)
	assert.False(t, got.RolledBack)
}

func TestTreeCreateResponse_WithRollback(t *testing.T) {
	resp := TreeCreateResponse{
		Created:    3,
		RolledBack: true,
		Err:        "partial failure at file 4",
		DiagFiles:  []string{"file1.mkv", "file2.mkv", "file3.mkv"},
	}
	got := roundTrip(t, resp)
	assert.True(t, got.RolledBack)
	assert.Equal(t, "partial failure at file 4", got.Err)
	assert.Equal(t, []string{"file1.mkv", "file2.mkv", "file3.mkv"}, got.DiagFiles)
}

func TestTreeRemoveRequest_RoundTrip(t *testing.T) {
	req := TreeRemoveRequest{
		Plan: hardlinktree.TreePlan{
			RootDir: "/data/cross-seed/torrent-abc",
			Files: []hardlinktree.FilePlan{
				{SourcePath: "/data/movies/file.mkv", TargetPath: "/data/cross-seed/torrent-abc/file.mkv"},
			},
		},
	}
	got := roundTrip(t, req)
	assert.Equal(t, req.Plan.RootDir, got.Plan.RootDir)
	require.Len(t, got.Plan.Files, 1)
}

func TestCancelRequest_RoundTrip(t *testing.T) {
	req := CancelRequest{RequestIDs: []string{"req-1", "req-2", "req-3"}}
	got := roundTrip(t, req)
	assert.Equal(t, []string{"req-1", "req-2", "req-3"}, got.RequestIDs)
}

func TestDiagEchoRequest_RoundTrip(t *testing.T) {
	req := DiagEchoRequest{Message: "hello helper"}
	got := roundTrip(t, req)
	assert.Equal(t, "hello helper", got.Message)
}

func TestDiagEchoResponse_RoundTrip(t *testing.T) {
	resp := DiagEchoResponse{Message: "hello helper"}
	got := roundTrip(t, resp)
	assert.Equal(t, "hello helper", got.Message)
}

func TestIsStreamingOp(t *testing.T) {
	assert.True(t, IsStreamingOp(OpWalk))
	assert.False(t, IsStreamingOp(OpStat))
	assert.False(t, IsStreamingOp(OpLstat))
	assert.False(t, IsStreamingOp(OpTreeHardlink))
	assert.False(t, IsStreamingOp(OpDiagEcho))
	assert.False(t, IsStreamingOp(OpControlCancel))
}

func TestOmitsZeroValues(t *testing.T) {
	// Verify omitempty fields are not present in JSON for zero values.
	entry := StatEntry{Path: "/data/file", Exists: true}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	s := string(data)
	assert.NotContains(t, s, "size")
	assert.NotContains(t, s, "modTime")
	assert.NotContains(t, s, "isDir")
	assert.NotContains(t, s, "mode")
	assert.NotContains(t, s, "err")
}

func TestCommand_NDJSONLine(t *testing.T) {
	// Verify a Command can be serialized as a single NDJSON line (no embedded newlines).
	args, _ := json.Marshal(WalkRequest{Root: "/data", MaxEntries: 100})
	cmd := Command{RequestID: "walk-1", Op: OpWalk, Args: args}
	data, err := json.Marshal(cmd)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "\n")
}
