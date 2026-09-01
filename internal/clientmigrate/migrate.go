package clientmigrate

import (
	"archive/tar"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/gzip"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type ClientType string

var (
	ClientTypeDeluge       ClientType = "deluge"
	ClientTypeRTorrent     ClientType = "rtorrent"
	ClientTypeTransmission ClientType = "transmission"
)

type Options struct {
	Source     ClientType
	SourceDir  string
	QbitDir    string
	DryRun     bool
	SkipBackup bool
}

type ClientMigrater interface {
	Migrate() error
}

type Migrater struct {
	imp  ClientMigrater
	opts Options
}

func New(opts Options) Migrater {
	m := Migrater{opts: opts}

	switch m.opts.Source {
	case ClientTypeDeluge:
		m.imp = NewDelugeImporter(m.opts)
	case ClientTypeRTorrent:
		m.imp = NewRTorrentImporter(m.opts)
	case ClientTypeTransmission:
		m.imp = NewTransmissionImporter(m.opts)
	default:
		log.Fatal().Str("source", string(m.opts.Source)).Msg("unsupported source client")
	}

	return m
}

func (m Migrater) Migrate(ctx context.Context) error {
	var (
		dryRun    = m.opts.DryRun
		source    = m.opts.Source
		sourceDir = m.opts.SourceDir
	)

	// Backup data before running
	if !m.opts.SkipBackup {
		if err := m.Backup(); err != nil {
			log.Error().Err(err).Msgf("Could not backup files")
			return err
		}
	}

	start := time.Now()

	if dryRun {
		log.Info().Msgf("dry-run: preparing to import torrents from: %s dir: %s", source, sourceDir)
		log.Info().Msg("dry-run: no data will be written")
	} else {
		log.Info().Msgf("preparing to import torrents from: %s dir: %s", source, sourceDir)
	}

	if err := m.imp.Migrate(); err != nil {
		return errors.Wrapf(err, "could not import from %s", source)
	}

	elapsed := time.Since(start)

	log.Info().Msgf("Import finished in: %s", elapsed)

	return nil
}

func (m Migrater) Backup() error {
	log.Info().Msg("prepare to backup torrent data before import..")

	var (
		source    = m.opts.Source
		sourceDir = m.opts.SourceDir
		qbitDir   = m.opts.QbitDir
	)

	timeStamp := time.Now().Format("20060102150405")

	backupDir := "qbt_backup"

	sourceBackupArchive := filepath.Join(backupDir, string(source)+"_backup_"+timeStamp+".tar.gz")
	qbitBackupArchive := filepath.Join(backupDir, "qBittorrent_backup_"+timeStamp+".tar.gz")

	if m.opts.DryRun {
		log.Info().Msgf("dry-run: creating %s backup of directory: %s to %s ...", source, sourceDir, sourceBackupArchive)
		log.Info().Msgf("dry-run: creating qBittorrent backup of directory: %s to %s ...", qbitDir, qbitBackupArchive)
	} else {
		if err := os.MkdirAll(backupDir, os.ModePerm); err != nil {
			return errors.Wrap(err, "could not create backup directory")
		}

		log.Info().Msgf("creating %s backup of directory: %s to %s ...", source, sourceDir, sourceBackupArchive)

		if err := archiveDir(sourceDir, sourceBackupArchive); err != nil {
			return errors.Wrapf(err, "could not create %s backup of directory: %s to %s", source, sourceDir, sourceBackupArchive)
		}

		// a fresh qBittorrent install may not have a BT_backup dir yet
		if _, err := os.Stat(qbitDir); os.IsNotExist(err) {
			log.Info().Msgf("qBittorrent directory does not exist yet, skipping backup: %s", qbitDir)
		} else {
			log.Info().Msgf("creating qBittorrent backup of directory: %s to %s ...", qbitDir, qbitBackupArchive)

			if err := archiveDir(qbitDir, qbitBackupArchive); err != nil {
				return errors.Wrapf(err, "could not create qBittorrent backup of directory: %s", qbitDir)
			}
		}
	}

	log.Info().Msg("Backup completed!")

	return nil
}

// archiveDir writes every regular file under dir into a tar.gz archive with
// paths relative to dir
func archiveDir(dir, archiveName string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	// with --source-dir . the archive lands inside the walked tree; it must
	// not be written into itself
	archiveAbs, err := filepath.Abs(archiveName)
	if err != nil {
		return err
	}

	out, err := os.Create(archiveName)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	walkErr := fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		if abs, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(path))); err == nil && abs == archiveAbs {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = path

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := root.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
	})
	if walkErr != nil {
		out.Close()
		return errors.Wrapf(walkErr, "could not create backup archive: %s", archiveName)
	}

	if err := tw.Close(); err != nil {
		out.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		return err
	}

	return out.Close()
}

// logImportSummary logs the per-importer end result with accurate counts
func logImportSummary(dryRun bool, imported, failed, skipped, total int) {
	switch {
	case dryRun:
		log.Info().Msgf("dry-run: would import %d of %d torrents, %d failed, %d skipped", imported, total, failed, skipped)
	case failed > 0 || skipped > 0:
		log.Warn().Msgf("imported %d of %d torrents, %d failed, %d skipped", imported, total, failed, skipped)
	default:
		log.Info().Msgf("successfully imported %d torrents!", imported)
	}
}

// wantedPieces reports, for each piece, whether any wanted file overlaps it,
// given the file lengths in torrent order and per-file wanted flags
func wantedPieces(fileLengths []int64, wanted []bool, pieceLen int64, numPieces int) []bool {
	wp := make([]bool, numPieces)

	var offset int64
	for i, length := range fileLengths {
		if wanted[i] && length > 0 {
			start := int(offset / pieceLen)
			end := int((offset + length - 1) / pieceLen)
			for p := start; p <= end && p < numPieces; p++ {
				wp[p] = true
			}
		}
		offset += length
	}

	return wp
}

// firstNonZero returns the first non-zero value
func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}

	return 0
}

// CopyFile copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file. The file mode will be copied from the source and
// the copied data is synced/flushed to stable storage.
func CopyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		if e := out.Close(); e != nil {
			err = e
		}
	}()

	_, err = io.Copy(out, in)
	if err != nil {
		return
	}

	err = out.Sync()
	if err != nil {
		return
	}

	si, err := os.Stat(src)
	if err != nil {
		return
	}
	err = os.Chmod(dst, si.Mode())
	if err != nil {
		return
	}

	return
}
