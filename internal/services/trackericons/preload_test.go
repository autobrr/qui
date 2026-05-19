// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackericons

import (
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAIAAACQkWg2AAAAHElEQVR4nGL5n8ZAEmAiTfmohlENQ0kDIAAA//+BAQGIuD7g2wAAAABJRU5ErkJggg=="

func TestPreloadIconsFromDisk(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	iconDir := filepath.Join(dataDir, iconDirName)
	require.NoError(t, os.MkdirAll(iconDir, 0o755))

	preload := `const trackerIcons = {
        "tracker.example.com": " data:image/png;base64,` + tinyPNG + `",
        "www.alias.example": "data:image/png;base64,` + tinyPNG + `"
    };`

	require.NoError(t, os.WriteFile(filepath.Join(iconDir, "preload.json"), []byte(preload), 0o644))

	svc, err := NewService(dataDir, "test-agent")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc })

	trackerPath := filepath.Join(iconDir, "tracker.example.com.png")
	require.FileExists(t, trackerPath)

	aliasPath := filepath.Join(iconDir, "alias.example.png")
	require.FileExists(t, aliasPath)

	for _, p := range []string{trackerPath, aliasPath} {
		f, err := os.Open(p)
		require.NoError(t, err)
		img, err := png.Decode(f)
		require.NoError(t, err)
		require.Equal(t, 16, img.Bounds().Dx())
		require.Equal(t, 16, img.Bounds().Dy())
		require.NoError(t, f.Close())
	}
}

func TestParseIconMapping(t *testing.T) {
	t.Parallel()

	input := []byte(`const trackerIcons = {
        "foo": "data:image/png;base64,` + tinyPNG + `",
    };`)

	mapping, err := parseIconMapping(input)
	require.NoError(t, err)
	require.Equal(t, "data:image/png;base64,"+tinyPNG, mapping["foo"])
}

func TestParseDataURL(t *testing.T) {
	t.Parallel()

	payload := "data:image/png;base64," + tinyPNG
	data, mediaType, err := parseDataURL(payload)
	require.NoError(t, err)
	require.Equal(t, "image/png", mediaType)
	require.NotEmpty(t, data)
}

func TestDecodeImageFromPreload(t *testing.T) {
	t.Parallel()

	payload := "data:image/png;base64," + tinyPNG
	data, mediaType, err := parseDataURL(payload)
	require.NoError(t, err)

	img, err := decodeImage(data, mediaType, "preload:test")
	require.NoError(t, err)
	require.Equal(t, 16, img.Bounds().Dx())
	require.Equal(t, 16, img.Bounds().Dy())
}

func TestDecodeImageFallsBackToICOWithPngContentType(t *testing.T) {
	t.Parallel()

	icoData := "AAABAAEAEBAAAAEAIABoBAAAFgAAACgAAAAQAAAAIAAAAAEAIAAAAAAAQAQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABM9dQATPXUAUz11ABM9dQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAEz21gBM9tkATPXuAEz1/wBM9f8ATPX/AEz1/wBM9fkATPXUAUz11ABM9dQAAEz11AAAAAAAAAAAAAAAAAAAAAAAAEz11wBM9vsATPX/AEz1/wBM9f8ATPX/AEz1/0DG/w5M9f8ATPX/AEz1/wBM9f8ATPX/AEz1+gBM9dQATPXUAEz11AAAAAAAAAAAAAAAATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11ABM9dQAAAAAAAAAAABM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11AAAAAAAAAAAAEz11wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9dQAAAAAAAAAAEz11gBM9f8ATPX/AEz1/wBM9f8AbsP+BL/h/0DH/xBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wDs5/8DTPX/AEz1/wBM9f8ATPX/AEz11gAAAAAAAAAAAEz11wBM9f8ATPX/AEz1/wBM9f8ATvX/AOXq/y3v/wAATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8A7Of/AUz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11wBM9f8ATPX/AEz1/wBM9f8A8uj/AMr6/zkltAAATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8A7Of/AUz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11wBM9f8ATPX/AEz1/wBM9f8A9u3/AO3m/0r1/wAATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8A7Of/AUz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8A//v/AEz1/wBM9f8ATPX/AEz1/wBM9f8A7Of/AUz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11gBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AP/7/wD/+v8A//v/AEz1/wBM9f8A4eP/AEz1/wBM9f8ATPWkAEz11gAAAAAAAAAAAEz11ABM9vYATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AP/7/wD/+v8A//v/ADno/wA+iv8AMo//AEz1/wBM9f8ATPXUAEz11AAAAAAAAAAAAEz11wBM9vYATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9vYAPer/AD3f/wA3lf8ATPX/AEz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11wBM9f8ATPX/AEz1/wBM9f8ATPX/AEn0/wiY/wIATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11gBM9f8ATPX/AEz1/wBM9f8ATPX/BPj/AGD//xkATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11gAAAAAAAAAAAEz11ABM9vYATPX/AEz1/wBM9f8ATPX/AEz1/wA3/f8A0v//XgBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11ABM9dQAAAAAAAAAAABM9dYATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11gAAAAAAAAAAAEz11wBM9vYATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAEz11ABM9dYATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11ABM9dQAAAAAAAAAAAAAAAAAAAEz11gBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f4ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11gAAAAAAAAAAAAAAAAAAAEz11wBM9vYATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz1/wBM9vsATPX/AEz1/wBM9f8ATPX/AEz1/wBM9f8ATPX/AEz11wAAAAAAAAAAAAAAAAAAAEz11ABM9dQATPXUAEz11ABM9dQATPXUAEz11ABM9dQATPXUAEz11ABM9dQATPXUAEz11ABM9dQATPXUAEz11ABM9dQATPXUAEz11AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAP8AAP//AAD//wAA8AcAAOAPAADwBwAA8AcAAPAPAADwDwAA8H8AAPB/AAD4/wAA+P8AAPz/AAD//wAA//8AAA=="

	data, err := base64.StdEncoding.DecodeString(icoData)
	require.NoError(t, err)

	img, err := decodeImage(data, "image/png", "https://example.com/favicon.ico")
	require.NoError(t, err)
	require.NotNil(t, img)
}
