package clientmigrate

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mholt/archives"
	"github.com/pkg/errors"
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
	opts Options
}

func New(opts Options) Migrater {
	return Migrater{opts: opts}
}

func (m Migrater) Migrate(ctx context.Context) error {
	var imp ClientMigrater
	switch m.opts.Source {
	case "rtorrent":
		imp = NewRTorrentImporter(m.opts)

	case "deluge":
		imp = NewDelugeImporter(m.opts)

	default:
		return fmt.Errorf("unsupported source client: %s", m.opts.Source)
	}

	var (
		dryRun     = m.opts.DryRun
		qbitDir    = m.opts.QbitDir
		source     = m.opts.Source
		sourceDir  = m.opts.SourceDir
		skipBackup = m.opts.SkipBackup
	)

	// Backup data before running
	if !skipBackup {
		log.Print("prepare to backup torrent data before import..\n")

		timeStamp := time.Now().Format("20060102150405")

		sourceBackupArchive := filepath.Join("qbt_backup", source+"_backup_"+timeStamp+".tar.gz")
		qbitBackupArchive := filepath.Join("qbt_backup", "qBittorrent_backup_"+timeStamp+".tar.gz")

		if dryRun {
			log.Printf("dry-run: creating %s backup of directory: %s to %s ...\n", source, sourceDir, sourceBackupArchive)
		} else {
			log.Printf("creating %s backup of directory: %s to %s ...\n", source, sourceDir, sourceBackupArchive)

			// map files on disk to their paths in the archive using default settings (second arg)
			files, err := archives.FilesFromDisk(ctx, nil, map[string]string{
				sourceDir: "",
			})
			if err != nil {
				return err
			}

			// create the output file we'll write to
			out, err := os.Create(sourceBackupArchive)
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
		}

		if dryRun {
			log.Printf("dry-run: creating qBittorrent backup of directory: %s to %s ...\n", qbitDir, qbitBackupArchive)
		} else {
			log.Printf("creating qBittorrent backup of directory: %s to %s ...\n", qbitDir, qbitBackupArchive)

			// map files on disk to their paths in the archive using default settings (second arg)
			files, err := archives.FilesFromDisk(ctx, nil, map[string]string{
				qbitDir: "",
			})
			if err != nil {
				return err
			}

			// create the output file we'll write to
			out, err := os.Create(qbitBackupArchive)
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
		}

		log.Print("Backup completed!\n")
	}

	start := time.Now()

	if dryRun {
		log.Printf("dry-run: preparing to import torrents from: %s dir: %s\n", source, sourceDir)
		log.Println("dry-run: no data will be written")
	} else {
		log.Printf("preparing to import torrents from: %s dir: %s\n", source, sourceDir)
	}

	if err := imp.Migrate(); err != nil {
		return errors.Wrapf(err, "could not import from %s", source)
	}

	elapsed := time.Since(start)

	log.Printf("\nImport finished in: %s\n", elapsed)

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
