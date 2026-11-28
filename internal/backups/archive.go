// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/andybalholm/brotli"
	kgzip "github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog/log"
	"github.com/ulikunitz/xz"

	"github.com/autobrr/qui/internal/models"
)

type ArchiveFormat string

const (
	ArchiveZip    ArchiveFormat = "zip"
	ArchiveTar    ArchiveFormat = "tar"
	ArchiveTarGz  ArchiveFormat = "tar.gz"
	ArchiveTarZst ArchiveFormat = "tar.zst"
	ArchiveTarBr  ArchiveFormat = "tar.br"
	ArchiveTarXz  ArchiveFormat = "tar.xz"
)

type archiveFormatSpec struct {
	contentType string
	compressor  func(io.Writer) (io.WriteCloser, error)
}

var archiveFormats = map[ArchiveFormat]archiveFormatSpec{
	ArchiveZip: {contentType: "application/zip"},
	ArchiveTar: {contentType: "application/x-tar", compressor: func(w io.Writer) (io.WriteCloser, error) {
		return nopWriteCloser{w}, nil
	}},
	ArchiveTarGz: {contentType: "application/gzip", compressor: func(w io.Writer) (io.WriteCloser, error) {
		return kgzip.NewWriterLevel(w, kgzip.DefaultCompression)
	}},
	ArchiveTarZst: {contentType: "application/zstd", compressor: func(w io.Writer) (io.WriteCloser, error) {
		return zstd.NewWriter(w)
	}},
	ArchiveTarBr: {contentType: "application/x-brotli", compressor: func(w io.Writer) (io.WriteCloser, error) {
		return brotli.NewWriter(w), nil
	}},
	ArchiveTarXz: {contentType: "application/x-xz", compressor: func(w io.Writer) (io.WriteCloser, error) {
		return xz.NewWriter(w)
	}},
}

var errUnsupportedArchiveFormat = errors.New("unsupported archive format")

func ParseArchiveFormat(s string) (ArchiveFormat, error) {
	if s == "" {
		return ArchiveZip, nil
	}
	format := ArchiveFormat(s)
	if _, ok := archiveFormats[format]; !ok {
		return "", fmt.Errorf("%w: %q", errUnsupportedArchiveFormat, s)
	}
	return format, nil
}

func (f ArchiveFormat) ContentType() string {
	return archiveFormats[f].contentType
}

func (f ArchiveFormat) Extension() string {
	return string(f)
}

type Archive struct {
	service      *Service
	runID        int64
	modTime      time.Time
	manifest     *Manifest
	manifestJSON []byte
}

func (s *Service) LoadArchive(ctx context.Context, run *models.BackupRun) (*Archive, error) {
	manifest, err := s.LoadManifest(ctx, run.ID)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return &Archive{service: s, runID: run.ID, modTime: run.RequestedAt, manifest: manifest, manifestJSON: manifestJSON}, nil
}

type archiveEntryWriter interface {
	add(name string, size int64, r io.Reader) error
	Close() error
}

func (a *Archive) Write(w io.Writer, format ArchiveFormat) error {
	spec, ok := archiveFormats[format]
	if !ok {
		return fmt.Errorf("%w: %q", errUnsupportedArchiveFormat, string(format))
	}
	if spec.compressor == nil {
		zw := &zipEntryWriter{zw: zip.NewWriter(w), modTime: a.modTime}
		return errors.Join(a.writeEntries(zw), zw.Close())
	}

	compressor, err := spec.compressor(w)
	if err != nil {
		return fmt.Errorf("create %s compressor: %w", format, err)
	}
	tw := &tarEntryWriter{tw: tar.NewWriter(compressor), modTime: a.modTime}
	return errors.Join(a.writeEntries(tw), tw.Close(), compressor.Close())
}

func (a *Archive) writeEntries(ew archiveEntryWriter) error {
	if err := ew.add("manifest.json", int64(len(a.manifestJSON)), bytes.NewReader(a.manifestJSON)); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	skipped := 0
	for _, item := range a.manifest.Items {
		if item.TorrentBlob == "" {
			continue
		}
		file, size, err := a.openTorrent(item.TorrentBlob)
		if err != nil {
			skipped++
			log.Debug().Err(err).Int64("runID", a.runID).Str("hash", item.Hash).Str("archivePath", item.ArchivePath).Msg("Left a torrent file out of the backup archive")
			continue
		}
		err = ew.add(item.ArchivePath, size, file)
		file.Close()
		if err != nil {
			return fmt.Errorf("write %s: %w", item.ArchivePath, err)
		}
	}
	if skipped > 0 {
		log.Warn().Int64("runID", a.runID).Int("skipped", skipped).Int("items", len(a.manifest.Items)).Msg("Backup archive is missing torrent files")
	}
	return nil
}

func (a *Archive) openTorrent(blob string) (*os.File, int64, error) {
	file, err := os.Open(a.service.ResolveBackupPath(blob))
	if err != nil {
		return nil, 0, err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	return file, stat.Size(), nil
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

type zipEntryWriter struct {
	zw      *zip.Writer
	modTime time.Time
}

func (z *zipEntryWriter) add(name string, _ int64, r io.Reader) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: z.modTime}
	entry, err := z.zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, r)
	return err
}

func (z *zipEntryWriter) Close() error {
	return z.zw.Close()
}

type tarEntryWriter struct {
	tw      *tar.Writer
	modTime time.Time
}

func (t *tarEntryWriter) add(name string, size int64, r io.Reader) error {
	header := &tar.Header{Name: name, Size: size, Mode: 0o644, ModTime: t.modTime}
	if err := t.tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := io.Copy(t.tw, r)
	return err
}

func (t *tarEntryWriter) Close() error {
	return t.tw.Close()
}
