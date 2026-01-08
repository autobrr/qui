package clientmigrate

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mholt/archives"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type Options struct {
	Source     string
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
	case "deluge":
		m.imp = NewDelugeImporter(m.opts)
	case "rtorrent":
		m.imp = NewRTorrentImporter(m.opts)
	case "transmission":
		m.imp = NewTransmissionImporter(m.opts)
	default:
		log.Fatal().Str("source", m.opts.Source).Msg("unsupported source client")
	}

	return m
}

func (m Migrater) Migrate(ctx context.Context) error {
	var (
		dryRun     = m.opts.DryRun
		qbitDir    = m.opts.QbitDir
		source     = m.opts.Source
		sourceDir  = m.opts.SourceDir
		skipBackup = m.opts.SkipBackup
	)

	// Backup data before running
	if !skipBackup {
		if err := m.Backup(ctx, source, dryRun, sourceDir, qbitDir); err != nil {
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

func (m Migrater) Backup(ctx context.Context, source string, dryRun bool, sourceDir string, qbitDir string) error {
	log.Info().Msg("prepare to backup torrent data before import..")

	timeStamp := time.Now().Format("20060102150405")

	backupDir := "qbt_backup"
	if err := MkDirIfNotExists(backupDir); err != nil {
		return errors.Wrap(err, "could not create backup directory")
	}

	sourceBackupArchive := filepath.Join(backupDir, source+"_backup_"+timeStamp+".tar.gz")
	qbitBackupArchive := filepath.Join(backupDir, "qBittorrent_backup_"+timeStamp+".tar.gz")

	if dryRun {
		log.Info().Msgf("dry-run: creating %s backup of directory: %s to %s ...", source, sourceDir, sourceBackupArchive)
		log.Info().Msgf("dry-run: creating qBittorrent backup of directory: %s to %s ...", qbitDir, qbitBackupArchive)
	} else {
		log.Info().Msgf("creating %s backup of directory: %s to %s ...", source, sourceDir, sourceBackupArchive)

		if err := m.archiveDir(ctx, sourceDir, sourceBackupArchive); err != nil {
			return errors.Wrapf(err, "could not create %s backup of directory: %s to %s ...", source, sourceDir, sourceBackupArchive)
		}

		log.Info().Msgf("creating qBittorrent backup of directory: %s to %s ...", qbitDir, qbitBackupArchive)

		if err := m.archiveDir(ctx, qbitDir, qbitBackupArchive); err != nil {
			return errors.Wrapf(err, "could not create qBittorrent backup of directory: %s", qbitDir)
		}
	}

	log.Info().Msg("Backup completed!")

	return nil
}

func (m Migrater) archiveDir(ctx context.Context, dir, archiveName string) error {
	// map files on disk to their paths in the archive using default settings (second arg)
	files, err := archives.FilesFromDisk(ctx, nil, map[string]string{
		dir: "",
	})
	if err != nil {
		return err
	}

	// create the output file we'll write to
	out, err := os.Create(archiveName)
	if err != nil {
		return err
	}
	defer out.Close()

	format := archives.CompressedArchive{
		Compression: archives.Gz{},
		Archival:    archives.Tar{},
	}

	// create the archive
	err = format.Archive(ctx, out, files)
	if err != nil {
		return errors.Wrapf(err, "could not create backup archive: %s", out.Name())
	}

	return nil
}

// MkDirIfNotExists check if export dir exists, if not then lets create it
func MkDirIfNotExists(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(dir, os.ModePerm); err != nil {
				return err
			}

			return nil
		}

		return err
	}

	return nil
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
