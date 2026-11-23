// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package helpertorrent

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
)

var ErrV2Unsupported = errors.New("v2 and hybrid torrents are unsupported")

type Helper struct {
	TorrentFile   []byte
	InfoHash      string
	ContentLayout string
}

const (
	layoutOriginal    = "Original"
	layoutNoSubfolder = "NoSubfolder"
)

type helperTorrent struct {
	Info bencode.Bytes `bencode:"info"`
}

type helperInfo struct {
	PieceLength int64        `bencode:"piece length"`
	Pieces      []byte       `bencode:"pieces"`
	Name        string       `bencode:"name"`
	Length      int64        `bencode:"length,omitempty"`
	Files       []helperFile `bencode:"files,omitempty"`
	Private     int          `bencode:"private"`
	Source      string       `bencode:"source"`
}

type helperFile struct {
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"`
	Attr   string   `bencode:"attr,omitempty"`
}

// Build makes a helper torrent from the exported .torrent of a real torrent.
// The helper is private, has the same pieces, keeps no tracker, comment, web
// seed, or source tag, and gets an infohash that the salt fixes. fileNames are
// the file paths that qBittorrent reports, in torrent order. Added at the same
// save path with ContentLayout, the helper finds each file at its path.
// A v2 or hybrid torrent gives ErrV2Unsupported.
func Build(torrent []byte, fileNames []string, salt string) (Helper, error) {
	if salt == "" {
		return Helper{}, errors.New("helper torrent needs a salt")
	}
	mi, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		return Helper{}, fmt.Errorf("decode torrent: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return Helper{}, fmt.Errorf("decode info: %w", err)
	}
	if info.HasV2() {
		return Helper{}, ErrV2Unsupported
	}

	files := info.UpvertedFiles()
	realFiles := 0
	for _, file := range files {
		if !IsPadding(file) {
			realFiles++
		}
	}
	if len(fileNames) != realFiles {
		return Helper{}, fmt.Errorf("got %d file names for %d files", len(fileNames), realFiles)
	}
	names := make([][]string, len(fileNames))
	for i, name := range fileNames {
		if names[i], err = splitRelativePath(name); err != nil {
			return Helper{}, err
		}
	}

	helper := helperInfo{
		PieceLength: info.PieceLength,
		Pieces:      info.Pieces,
		Private:     1,
		Source:      salt,
	}
	layout := layoutOriginal
	switch root := commonRootFolder(names); {
	case !info.IsDir() && len(names[0]) == 1:
		helper.Name, helper.Length = names[0][0], info.Length
	case root != "":
		helper.Name = root
		for i := range names {
			names[i] = names[i][1:]
		}
		helper.Files = renameFiles(files, names)
	default:
		helper.Name, layout = info.BestName(), layoutNoSubfolder
		helper.Files = renameFiles(files, names)
	}

	infoBytes, err := bencode.Marshal(helper)
	if err != nil {
		return Helper{}, fmt.Errorf("encode info: %w", err)
	}
	torrentFile, err := bencode.Marshal(helperTorrent{Info: infoBytes})
	if err != nil {
		return Helper{}, fmt.Errorf("encode torrent: %w", err)
	}
	return Helper{
		TorrentFile:   torrentFile,
		InfoHash:      metainfo.HashBytes(infoBytes).HexString(),
		ContentLayout: layout,
	}, nil
}

func commonRootFolder(names [][]string) string {
	root := ""
	for _, name := range names {
		if len(name) < 2 || (root != "" && name[0] != root) {
			return ""
		}
		root = name[0]
	}
	return root
}

func renameFiles(files []metainfo.FileInfo, names [][]string) []helperFile {
	renamed := make([]helperFile, 0, len(files))
	next := 0
	for _, file := range files {
		filePath := file.Path
		if !IsPadding(file) {
			filePath = names[next]
			next++
		}
		renamed = append(renamed, helperFile{Length: file.Length, Path: filePath, Attr: file.Attr})
	}
	return renamed
}

// IsPadding reports whether a file of a .torrent is a padding file, which
// qBittorrent does not list. It follows libtorrent, which also counts the
// padding files of BitComet, so that the files left match the file list of
// qBittorrent.
func IsPadding(file metainfo.FileInfo) bool {
	return strings.Contains(file.Attr, "p") || strings.Contains(strings.Join(file.Path, "/"), "_____padding_file_")
}

func splitRelativePath(name string) ([]string, error) {
	parts := strings.Split(name, "/")
	if name == "." || path.Clean(name) != name || path.IsAbs(name) || strings.ContainsRune(name, '\\') ||
		hasDriveLetter(name) || slices.Contains(parts, "..") {
		return nil, fmt.Errorf("file name %q is not a relative path", name)
	}
	return parts, nil
}

func hasDriveLetter(name string) bool {
	return len(name) >= 2 && name[1] == ':' && ('a' <= name[0]|0x20 && name[0]|0x20 <= 'z')
}
